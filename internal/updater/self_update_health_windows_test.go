//go:build windows

package updater

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAcknowledgePendingSelfUpdateHealthRequiresExactRunningExecutable(t *testing.T) {
	tempRoot := t.TempDir()
	t.Setenv("UPDATER_TEMP_DIR", tempRoot)
	currentExecutable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	currentSHA256, err := fileSHA256(currentExecutable)
	if err != nil {
		t.Fatal(err)
	}
	userSID, err := currentUserSID()
	if err != nil {
		t.Fatal(err)
	}
	sessionID, err := currentSessionID()
	if err != nil {
		t.Fatal(err)
	}
	healthRequest, requestPath, ackPath, err := createSelfUpdateStartupHealthRequest(selfUpdateApplyRequest{
		TargetPath:      currentExecutable,
		ExpectedSHA256:  currentSHA256,
		ParentUserSID:   userSID,
		ParentSessionID: sessionID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := acknowledgePendingSelfUpdateHealth(); err != nil {
		t.Fatal(err)
	}
	ack, err := readSelfUpdateStartupHealthAck(ackPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateSelfUpdateStartupHealthAck(healthRequest, ack); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(requestPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("acknowledged health request still exists: %v", err)
	}

	wrongTargetRequest, wrongRequestPath, wrongAckPath, err := createSelfUpdateStartupHealthRequest(selfUpdateApplyRequest{
		TargetPath:      filepath.Join(t.TempDir(), releaseAssetExecutable),
		ExpectedSHA256:  currentSHA256,
		ParentUserSID:   userSID,
		ParentSessionID: sessionID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if wrongTargetRequest.RequestID == "" {
		t.Fatal("wrong-target health request is missing its request ID")
	}
	if err := acknowledgePendingSelfUpdateHealth(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(wrongAckPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("wrong-target request was acknowledged: %v", err)
	}
	if _, err := os.Stat(wrongRequestPath); err != nil {
		t.Fatalf("wrong-target request should remain pending for its intended executable: %v", err)
	}
}

func TestSelfUpdateTransactionRollsBackWhenRestartFails(t *testing.T) {
	setProcessExecutionModeForTest(t, processModeInteractive)
	tempRoot := t.TempDir()
	t.Setenv("UPDATER_TEMP_DIR", tempRoot)
	stagingDir := filepath.Join(tempRoot, "self-update")
	if err := os.MkdirAll(stagingDir, 0o700); err != nil {
		t.Fatal(err)
	}
	newExecutable := []byte("new executable")
	sourcePath := filepath.Join(stagingDir, "PatchBoard-update.exe")
	if err := os.WriteFile(sourcePath, newExecutable, 0o700); err != nil {
		t.Fatal(err)
	}
	targetPath := filepath.Join(t.TempDir(), releaseAssetExecutable)
	oldExecutable := []byte("old executable")
	if err := os.WriteFile(targetPath, oldExecutable, 0o700); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(newExecutable)
	userSID, err := currentUserSID()
	if err != nil {
		t.Fatal(err)
	}
	sessionID, err := currentSessionID()
	if err != nil {
		t.Fatal(err)
	}
	err = applySelfUpdateReplacementTransactionWithRestart(selfUpdateApplyRequest{
		SourcePath:      sourcePath,
		TargetPath:      targetPath,
		ExpectedSHA256:  hex.EncodeToString(digest[:]),
		ParentUserSID:   userSID,
		ParentSessionID: sessionID,
		Restart:         true,
	}, func(string) (selfUpdateRestartedProcess, error) {
		return selfUpdateRestartedProcess{}, errors.New("simulated restart failure")
	})
	if err == nil || !strings.Contains(err.Error(), "restored previous executable") {
		t.Fatalf("expected restart failure with rollback, got %v", err)
	}
	restored, readErr := os.ReadFile(targetPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(restored) != string(oldExecutable) {
		t.Fatalf("rollback did not restore previous executable: got %q want %q", restored, oldExecutable)
	}
	failed, readErr := os.ReadFile(targetPath + ".failed")
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(failed) != string(newExecutable) {
		t.Fatalf("failed replacement was not retained for diagnostics: got %q want %q", failed, newExecutable)
	}
	if _, err := os.Stat(targetPath + ".bak"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("rollback backup was not consumed: %v", err)
	}
}

func TestSelfUpdateTransactionCommitsAfterStartupHealthAcknowledgement(t *testing.T) {
	setProcessExecutionModeForTest(t, processModeInteractive)
	tempRoot := t.TempDir()
	t.Setenv("UPDATER_TEMP_DIR", tempRoot)
	stagingDir := filepath.Join(tempRoot, "self-update")
	if err := os.MkdirAll(stagingDir, 0o700); err != nil {
		t.Fatal(err)
	}
	newExecutable := []byte("new executable")
	sourcePath := filepath.Join(stagingDir, "PatchBoard-update.exe")
	if err := os.WriteFile(sourcePath, newExecutable, 0o700); err != nil {
		t.Fatal(err)
	}
	targetPath := filepath.Join(t.TempDir(), releaseAssetExecutable)
	if err := os.WriteFile(targetPath, []byte("old executable"), 0o700); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(newExecutable)
	userSID, err := currentUserSID()
	if err != nil {
		t.Fatal(err)
	}
	sessionID, err := currentSessionID()
	if err != nil {
		t.Fatal(err)
	}
	err = applySelfUpdateReplacementTransactionWithRestart(selfUpdateApplyRequest{
		SourcePath:      sourcePath,
		TargetPath:      targetPath,
		ExpectedSHA256:  hex.EncodeToString(digest[:]),
		ParentUserSID:   userSID,
		ParentSessionID: sessionID,
		Restart:         true,
	}, func(string) (selfUpdateRestartedProcess, error) {
		directory, directoryErr := selfUpdateDownloadDir()
		if directoryErr != nil {
			return selfUpdateRestartedProcess{}, directoryErr
		}
		entries, directoryErr := os.ReadDir(directory)
		if directoryErr != nil {
			return selfUpdateRestartedProcess{}, directoryErr
		}
		for _, entry := range entries {
			if strings.HasSuffix(entry.Name(), selfUpdateHealthRequestFileSuffix) {
				healthRequest, readErr := readSelfUpdateStartupHealthRequest(filepath.Join(directory, entry.Name()))
				if readErr != nil {
					return selfUpdateRestartedProcess{}, readErr
				}
				ackPayload, marshalErr := json.Marshal(selfUpdateStartupHealthAck{
					ProtocolVersion:  selfUpdateApplyProtocolVersion,
					RequestID:        healthRequest.RequestID,
					ExecutableSHA256: healthRequest.ExpectedSHA256,
					AcknowledgedAt:   "2026-07-12T12:00:00Z",
				})
				if marshalErr != nil {
					return selfUpdateRestartedProcess{}, marshalErr
				}
				ackPath := strings.TrimSuffix(filepath.Join(directory, entry.Name()), selfUpdateHealthRequestFileSuffix) + selfUpdateHealthAckFileSuffix
				if writeErr := writeUserPrivateFile(ackPath, ackPayload); writeErr != nil {
					return selfUpdateRestartedProcess{}, writeErr
				}
				return selfUpdateRestartedProcess{}, nil
			}
		}
		return selfUpdateRestartedProcess{}, errors.New("startup health request was not created")
	})
	if err != nil {
		t.Fatal(err)
	}
	updated, readErr := os.ReadFile(targetPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(updated) != string(newExecutable) {
		t.Fatalf("updated executable mismatch: got %q want %q", updated, newExecutable)
	}
	if _, err := os.Stat(targetPath + ".bak"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("verified update retained backup: %v", err)
	}
}
