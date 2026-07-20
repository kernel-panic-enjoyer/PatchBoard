//go:build windows

package updater

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

const namedJobHelperEnvironment = "PATCHBOARD_NAMED_JOB_HELPER"

func TestNamedCommandProcessOwnerLetsWorkerJoinItself(t *testing.T) {
	if jobName := os.Getenv(namedJobHelperEnvironment); jobName != "" {
		if err := assignCurrentProcessToNamedJob(jobName); err != nil {
			t.Fatal(err)
		}
		fmt.Println("joined")
		time.Sleep(time.Minute)
		return
	}

	jobName := `Local\PatchBoard-Test-` + strconv.FormatInt(time.Now().UnixNano(), 10)
	userSID, err := currentUserSID()
	if err != nil {
		t.Fatal(err)
	}
	processOwner, err := newNamedCommandProcessOwner(jobName, userSID)
	if err != nil {
		t.Fatal(err)
	}
	defer processOwner.Close()

	helper := exec.Command(os.Args[0], "-test.run=^TestNamedCommandProcessOwnerLetsWorkerJoinItself$")
	helper.Env = append(os.Environ(), namedJobHelperEnvironment+"="+jobName)
	helper.SysProcAttr = hiddenSysProcAttr()
	stdout, err := helper.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := helper.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if helper.Process != nil {
			_ = helper.Process.Kill()
		}
	}()

	ready := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		if scanner.Scan() {
			ready <- scanner.Text()
			return
		}
		ready <- ""
	}()
	select {
	case line := <-ready:
		if line != "joined" {
			t.Fatalf("worker did not join named Job Object: %q", line)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not join named Job Object")
	}

	processOwner.Terminate()
	exited := make(chan error, 1)
	go func() { exited <- helper.Wait() }()
	select {
	case <-exited:
	case <-time.After(5 * time.Second):
		t.Fatal("named Job Object did not terminate worker")
	}
}

func TestCommandProcessOwnerTerminatesGrandchildProcessTree(t *testing.T) {
	testDir := t.TempDir()
	grandchildStartGatePath := testDir + `\gate.txt`
	grandchildPIDPath := testDir + `\child.pid`
	launcherScript := `
$gate = ` + quotePowerShellSingleQuotedString(grandchildStartGatePath) + `
$pidPath = ` + quotePowerShellSingleQuotedString(grandchildPIDPath) + `
while (!(Test-Path -LiteralPath $gate)) { Start-Sleep -Milliseconds 25 }
$child = Start-Process powershell.exe -PassThru -WindowStyle Hidden -ArgumentList '-NoProfile','-ExecutionPolicy','Bypass','-Command','Start-Sleep -Seconds 60'
Set-Content -LiteralPath $pidPath -Value $child.Id
Start-Sleep -Seconds 60
`
	parentCommand := exec.Command("powershell.exe", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", launcherScript)
	parentCommand.SysProcAttr = hiddenSysProcAttr()
	processOwner, err := newCommandProcessOwner(true)
	if err != nil {
		t.Fatal(err)
	}
	defer processOwner.Close()
	if err := parentCommand.Start(); err != nil {
		t.Fatal(err)
	}
	defer terminateStartedCommand(parentCommand, processOwner)
	if err := processOwner.Assign(parentCommand); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(grandchildStartGatePath, []byte("go"), 0o600); err != nil {
		t.Fatal(err)
	}
	grandchildPID := waitForProcessIDFile(t, grandchildPIDPath)
	if !isProcessRunning(grandchildPID) {
		t.Fatalf("expected grandchild process %d to be running before termination", grandchildPID)
	}

	processOwner.Terminate()
	commandExited := make(chan error, 1)
	go func() { commandExited <- parentCommand.Wait() }()
	select {
	case <-commandExited:
	case <-time.After(5 * time.Second):
		t.Fatal("owned process did not exit after job termination")
	}
	grandchildExitDeadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(grandchildExitDeadline) {
		if !isProcessRunning(grandchildPID) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("grandchild process %d survived job termination", grandchildPID)
}

func TestCommandProcessOwnerAssignsBeforeResumingMutableCommand(t *testing.T) {
	markerPath := t.TempDir() + `\started.txt`
	command := exec.Command(
		"powershell.exe",
		"-NoProfile",
		"-ExecutionPolicy",
		"Bypass",
		"-Command",
		"Set-Content -NoNewline -LiteralPath "+quotePowerShellSingleQuotedString(markerPath)+" -Value started",
	)
	command.SysProcAttr = hiddenSysProcAttrWithFlags(true)

	processOwner, err := newCommandProcessOwner(true)
	if err != nil {
		t.Fatal(err)
	}
	defer processOwner.Close()
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer terminateStartedCommand(command, processOwner)

	// A suspended command must not execute its payload before Job Object ownership.
	time.Sleep(100 * time.Millisecond)
	if _, err := os.Stat(markerPath); !os.IsNotExist(err) {
		t.Fatalf("suspended command executed before Job Object assignment: %v", err)
	}
	if err := processOwner.Assign(command); err != nil {
		t.Fatal(err)
	}
	if err := processOwner.Resume(command); err != nil {
		t.Fatal(err)
	}
	if err := command.Wait(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(markerPath); err != nil {
		t.Fatalf("resumed command did not execute: %v", err)
	}
}

func quotePowerShellSingleQuotedString(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func waitForProcessIDFile(t *testing.T, path string) uint32 {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err != nil {
			time.Sleep(25 * time.Millisecond)
			continue
		}

		processID, parseErr := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 32)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		return uint32(processID)
	}
	t.Fatalf("pid file %s was not written", path)
	return 0
}

func isProcessRunning(processID uint32) bool {
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_QUERY_LIMITED_INFORMATION, false, processID)
	if err != nil {
		return false
	}
	defer windows.CloseHandle(handle)
	waitResult, err := windows.WaitForSingleObject(handle, 0)
	return err == nil && waitResult == uint32(windows.WAIT_TIMEOUT)
}

func TestRunCommandContextCancellationTerminatesOwnedPackageProcessTree(t *testing.T) {
	cancelledContext, cancel := context.WithCancel(context.Background())
	cancel()
	commandResult := runCommandContext(cancelledContext, time.Minute, "cmd.exe", "/d", "/c", "winget", "--version")
	if commandResult.Code != commandCancelledCode {
		t.Fatalf("expected cancelled owned command, got %#v", commandResult)
	}
}
