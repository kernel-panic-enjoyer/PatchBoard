//go:build windows

package updater

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// RULE-100: an elevated WinGet batch must run as the requesting user in the
// requesting session.
func TestElevatedWingetBatchRequiresRequesterUserAndSession(t *testing.T) {
	sid, err := currentUserSID()
	if err != nil {
		t.Fatal(err)
	}
	session, err := currentSessionID()
	if err != nil {
		t.Fatal(err)
	}
	winget := []Package{{Manager: managerWinget, ID: "Test.App"}}
	choco := []Package{{Manager: managerChoco, ID: "git"}}

	if err := validateBatchWorkerIdentity(elevatedWorkerAuthContext{UserSID: strings.ToLower(sid), SessionID: session}, winget); err != nil {
		t.Fatalf("matching user and session rejected: %v", err)
	}
	if err := validateBatchWorkerIdentity(elevatedWorkerAuthContext{UserSID: "S-1-5-21-1-2-3-500", SessionID: session}, winget); err == nil || !strings.Contains(err.Error(), "does not match requester") {
		t.Fatalf("foreign user accepted, err=%v", err)
	}
	if err := validateBatchWorkerIdentity(elevatedWorkerAuthContext{UserSID: sid, SessionID: session + 1}, winget); err == nil || !strings.Contains(err.Error(), "does not match requester session") {
		t.Fatalf("foreign session accepted, err=%v", err)
	}
	if err := validateBatchWorkerIdentity(elevatedWorkerAuthContext{UserSID: "S-1-5-21-1-2-3-500"}, choco); err != nil {
		t.Fatalf("Chocolatey-only batch must not be bound to the requester: %v", err)
	}
}

// RULE-267: uninstall refuses any folder other than PatchBoard's own install
// folder and deletes nothing.
func TestApplicationUninstallRejectsForeignTargetAndDeletesNothing(t *testing.T) {
	configureApplicationInstallTestRoot(t, []byte("current executable"))
	paths := applicationInstallPathsProvider()
	foreign := filepath.Join(filepath.Dir(paths.InstallDirectory), "OtherApp")
	if err := os.MkdirAll(foreign, 0o755); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(foreign, "keep.txt")
	if err := os.WriteFile(keep, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	replaceApplicationInstallRegistryDeleter(t, func() error {
		t.Fatal("registry entry deleted for a foreign target")
		return nil
	})
	replaceUninstallSelfDelete(t, func() { t.Fatal("self-delete scheduled for a foreign target") })

	for _, target := range []string{foreign, "", paths.InstallDirectory + `\..\OtherApp`} {
		err := runApplicationUninstallApply(target, 0)
		if err == nil {
			t.Fatalf("uninstall accepted target %q", target)
		}
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("foreign folder was touched: %v", err)
	}
}

// RULE-110: the apply request is refused unless the source is a staged file,
// the target is PatchBoard.exe and the hash is one digest.
func TestSelfUpdateApplyRequestEligibility(t *testing.T) {
	setProcessExecutionModeForTest(t, processModeInteractive)
	tempRoot := t.TempDir()
	t.Setenv("UPDATER_TEMP_DIR", tempRoot)
	stagingDir := filepath.Join(tempRoot, "self-update")
	if err := os.MkdirAll(stagingDir, 0o700); err != nil {
		t.Fatal(err)
	}
	staged := filepath.Join(stagingDir, "PatchBoard-new.exe")
	outside := filepath.Join(t.TempDir(), "PatchBoard-new.exe")
	for _, path := range []string{staged, outside} {
		if err := os.WriteFile(path, []byte("new"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	target := filepath.Join(t.TempDir(), "PatchBoard.exe")
	digest := strings.Repeat("ab", 32)

	valid := selfUpdateApplyRequest{SourcePath: staged, TargetPath: target, ExpectedSHA256: digest}
	if err := validateSelfUpdateApplyRequest(valid); err != nil {
		t.Fatalf("valid request rejected: %v", err)
	}
	for name, request := range map[string]selfUpdateApplyRequest{
		"source outside staging": {SourcePath: outside, TargetPath: target, ExpectedSHA256: digest},
		"missing source":         {SourcePath: filepath.Join(stagingDir, "gone.exe"), TargetPath: target, ExpectedSHA256: digest},
		"target not PatchBoard":  {SourcePath: staged, TargetPath: filepath.Join(filepath.Dir(target), "other.exe"), ExpectedSHA256: digest},
		"source equals target":   {SourcePath: staged, TargetPath: staged, ExpectedSHA256: digest},
		"hash with extra text":   {SourcePath: staged, TargetPath: target, ExpectedSHA256: digest + " x"},
		"empty hash":             {SourcePath: staged, TargetPath: target},
		"empty source":           {TargetPath: target, ExpectedSHA256: digest},
		"empty target":           {SourcePath: staged, ExpectedSHA256: digest},
	} {
		if err := validateSelfUpdateApplyRequest(request); err == nil {
			t.Errorf("%s: request accepted", name)
		}
	}
}

// RULE-002: a staged file whose hash does not match leaves the installed
// executable untouched and leaves no temporary copy behind.
func TestSelfUpdateReplaceWithWrongHashKeepsTarget(t *testing.T) {
	setProcessExecutionModeForTest(t, processModeInteractive)
	tempRoot := t.TempDir()
	t.Setenv("UPDATER_TEMP_DIR", tempRoot)
	stagingDir := filepath.Join(tempRoot, "self-update")
	if err := os.MkdirAll(stagingDir, 0o700); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(stagingDir, "PatchBoard-new.exe")
	targetDir := t.TempDir()
	target := filepath.Join(targetDir, "PatchBoard.exe")
	if err := os.WriteFile(source, []byte("tampered"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("new"))
	err := replaceExecutableForSelfUpdate(selfUpdateApplyRequest{SourcePath: source, TargetPath: target, ExpectedSHA256: hex.EncodeToString(sum[:])})
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("expected checksum mismatch, got %v", err)
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "old" {
		t.Fatalf("target changed: %q, %v", data, err)
	}
	entries, err := os.ReadDir(targetDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Fatalf("leftover files next to the target: %v", names)
	}
}
