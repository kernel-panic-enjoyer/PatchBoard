//go:build windows

package updater

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	err := runSelfUpdateApply(selfUpdateApplyRequest{TargetPath: target})
	if err == nil || !strings.Contains(err.Error(), "running executable or installed PatchBoard executable") {
		t.Fatalf("expected unrelated target rejection, got %v", err)
	}
}
