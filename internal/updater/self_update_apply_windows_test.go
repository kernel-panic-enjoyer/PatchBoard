//go:build windows

package updater

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestValidateSelfUpdateLaunchTargetAcceptsRunningExecutable(t *testing.T) {
	runningExecutable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := validateSelfUpdateLaunchTarget(runningExecutable); err != nil {
		t.Fatalf("running executable should be accepted: %v", err)
	}
}

func TestValidateSelfUpdateLaunchTargetRejectsUnrelatedExecutable(t *testing.T) {
	target := filepath.Join(t.TempDir(), "PatchBoard.exe")
	err := validateSelfUpdateLaunchTarget(target)
	if err == nil || !strings.Contains(err.Error(), "running executable or installed PatchBoard executable") {
		t.Fatalf("expected unrelated target rejection, got %v", err)
	}
}

func TestStagedSelfUpdateHelperReplacesPortableOriginal(t *testing.T) {
	if os.Getenv("PATCHBOARD_SELF_UPDATE_PORTABLE_PARENT") == "1" {
		runPortableSelfUpdateParent(t)
		return
	}

	tempRoot := t.TempDir()
	stagingDir := filepath.Join(tempRoot, "self-update")
	portableDir := filepath.Join(tempRoot, "portable")
	if err := os.MkdirAll(stagingDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(portableDir, 0o700); err != nil {
		t.Fatal(err)
	}

	stagedHelper := filepath.Join(stagingDir, "PatchBoard-update.exe")
	buildSelfUpdateIntegrationHelper(t, stagedHelper)
	helpersum, err := fileSHA256(stagedHelper)
	if err != nil {
		t.Fatal(err)
	}

	currentTestExecutable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	portableOriginal := filepath.Join(portableDir, releaseAssetExecutable)
	copyExecutableForTest(t, currentTestExecutable, portableOriginal)
	originalSum, err := fileSHA256(portableOriginal)
	if err != nil {
		t.Fatal(err)
	}
	if strings.EqualFold(originalSum, helpersum) {
		t.Fatal("portable original and staged helper unexpectedly have the same digest")
	}

	parent := exec.Command(portableOriginal, "-test.run=^TestStagedSelfUpdateHelperReplacesPortableOriginal$")
	parent.Env = append(os.Environ(),
		"PATCHBOARD_SELF_UPDATE_PORTABLE_PARENT=1",
		"PATCHBOARD_SELF_UPDATE_ARTIFACT="+stagedHelper,
		"PATCHBOARD_SELF_UPDATE_SHA256="+helpersum,
		"UPDATER_TEMP_DIR="+tempRoot,
	)
	if output, err := parent.CombinedOutput(); err != nil {
		t.Fatalf("portable parent failed: %v\n%s", err, output)
	}

	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		updatedSum, hashErr := fileSHA256(portableOriginal)
		if hashErr == nil && strings.EqualFold(updatedSum, helpersum) {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	updatedSum, hashErr := fileSHA256(portableOriginal)
	outcome, _ := os.ReadFile(selfUpdateApplyOutcomePath(stagedHelper))
	t.Fatalf("staged helper did not replace portable original: digest=%q error=%v want=%q outcome=%s", updatedSum, hashErr, helpersum, outcome)
}

func TestSelfUpdateHelperMustAcknowledgeReadiness(t *testing.T) {
	helperPath := os.Getenv("ComSpec")
	if helperPath == "" {
		t.Skip("ComSpec is unavailable")
	}
	userSID, err := currentUserSID()
	if err != nil {
		t.Fatal(err)
	}
	sessionID, err := currentSessionID()
	if err != nil {
		t.Fatal(err)
	}
	targetPath, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err = launchSelfUpdateApplyHelper(ctx, helperPath, selfUpdateApplyRequest{
		SourcePath:      helperPath,
		TargetPath:      targetPath,
		ExpectedSHA256:  strings.Repeat("a", 64),
		ParentPID:       os.Getpid(),
		ParentUserSID:   userSID,
		ParentSessionID: sessionID,
		DeadlineUnixMS:  time.Now().Add(time.Minute).UnixMilli(),
	}, false)
	if err == nil || !strings.Contains(err.Error(), "did not connect") {
		t.Fatalf("helper start without readiness must fail, got %v", err)
	}
}

func runPortableSelfUpdateParent(t *testing.T) {
	t.Helper()
	artifactPath := os.Getenv("PATCHBOARD_SELF_UPDATE_ARTIFACT")
	expectedSHA256 := os.Getenv("PATCHBOARD_SELF_UPDATE_SHA256")
	targetPath, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := launchSelfUpdateApply(ctx, selfUpdateArtifact{
		Path:   artifactPath,
		SHA256: expectedSHA256,
	}, targetPath); err != nil {
		t.Fatal(err)
	}
	os.Exit(0)
}

func buildSelfUpdateIntegrationHelper(t *testing.T, destination string) {
	t.Helper()
	moduleFile, err := filepath.Abs(filepath.Join("..", "..", "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	moduleRoot := filepath.Dir(moduleFile)
	sourceDir, err := os.MkdirTemp(moduleRoot, ".self-update-integration-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(sourceDir) })
	sourcePath := filepath.Join(sourceDir, "main.go")
	source := `package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"patchboard/internal/updater"
)

func main() {
	if len(os.Args) > 1 {
		updater.Main()
		return
	}
	directory := filepath.Join(os.Getenv("UPDATER_TEMP_DIR"), "self-update")
	entries, _ := os.ReadDir(directory)
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), "health-") || !strings.HasSuffix(entry.Name(), ".request.json") {
			continue
		}
		payload, err := os.ReadFile(filepath.Join(directory, entry.Name()))
		if err != nil {
			continue
		}
		var request map[string]any
		if json.Unmarshal(payload, &request) != nil {
			continue
		}
		protocolVersion, protocolOK := request["protocol_version"].(float64)
		requestID, requestOK := request["request_id"].(string)
		expectedSHA256, hashOK := request["expected_sha256"].(string)
		if !protocolOK || !requestOK || !hashOK {
			continue
		}
		ack, _ := json.Marshal(map[string]any{
			"protocol_version": int(protocolVersion),
			"request_id": requestID,
			"executable_sha256": expectedSHA256,
			"acknowledged_at": "2026-07-12T12:00:00Z",
		})
		ackPath := strings.TrimSuffix(filepath.Join(directory, entry.Name()), ".request.json") + ".ack.json"
		_ = os.WriteFile(ackPath, ack, 0600)
		return
	}
}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("go", "build", "-o", destination, sourcePath)
	command.Dir = moduleRoot
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build integration helper: %v\n%s", err, output)
	}
}

func copyExecutableForTest(t *testing.T, sourcePath, destinationPath string) {
	t.Helper()
	data, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destinationPath, data, 0o755); err != nil {
		t.Fatal(err)
	}
}
