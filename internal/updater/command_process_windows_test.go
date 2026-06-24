//go:build windows

package updater

import (
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestWindowsCommandProcessOwnerTerminatesGrandchild(t *testing.T) {
	dir := t.TempDir()
	gatePath := dir + `\gate.txt`
	pidPath := dir + `\child.pid`
	script := `
$gate = ` + powerShellSingleQuoted(gatePath) + `
$pidPath = ` + powerShellSingleQuoted(pidPath) + `
while (!(Test-Path -LiteralPath $gate)) { Start-Sleep -Milliseconds 25 }
$child = Start-Process powershell.exe -PassThru -WindowStyle Hidden -ArgumentList '-NoProfile','-ExecutionPolicy','Bypass','-Command','Start-Sleep -Seconds 60'
Set-Content -LiteralPath $pidPath -Value $child.Id
Start-Sleep -Seconds 60
`
	cmd := exec.Command("powershell.exe", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", script)
	cmd.SysProcAttr = hiddenSysProcAttr()
	owner, err := newCommandProcessOwner(true)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer terminateStartedCommand(cmd, owner)
	if err := owner.Assign(cmd); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(gatePath, []byte("go"), 0o600); err != nil {
		t.Fatal(err)
	}
	childPID := waitForPIDFile(t, pidPath)
	if !processRunning(childPID) {
		t.Fatalf("expected child process %d to be running before termination", childPID)
	}

	owner.Terminate()
	waitDone := make(chan error, 1)
	go func() { waitDone <- cmd.Wait() }()
	select {
	case <-waitDone:
	case <-time.After(5 * time.Second):
		t.Fatal("owned process did not exit after job termination")
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if !processRunning(childPID) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("grandchild process %d survived job termination", childPID)
}

func powerShellSingleQuoted(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func waitForPIDFile(t *testing.T, path string) uint32 {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil {
			pid, parseErr := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 32)
			if parseErr != nil {
				t.Fatal(parseErr)
			}
			return uint32(pid)
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("pid file %s was not written", path)
	return 0
}

func processRunning(pid uint32) bool {
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return false
	}
	defer windows.CloseHandle(handle)
	event, err := windows.WaitForSingleObject(handle, 0)
	return err == nil && event == uint32(windows.WAIT_TIMEOUT)
}

func TestRunCommandContextCancellationTerminatesOwnedPackageProcessTree(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result := runCommandContext(ctx, time.Minute, "cmd.exe", "/d", "/c", "winget", "--version")
	if result.Code != commandCancelledCode {
		t.Fatalf("expected cancelled owned command, got %#v", result)
	}
}
