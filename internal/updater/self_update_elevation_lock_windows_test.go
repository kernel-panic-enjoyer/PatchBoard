//go:build windows

package updater

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestLockVerifiedSelfUpdateExecutableBlocksSwapUntilClosed(t *testing.T) {
	directory := t.TempDir()
	helper := filepath.Join(directory, "PatchBoard-v9.9.9.exe")
	content := []byte("verified helper")
	if err := os.WriteFile(helper, content, 0o700); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(content)
	lock, err := lockVerifiedSelfUpdateExecutable(helper, hex.EncodeToString(sum[:]))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(helper, []byte("attacker"), 0o700); err == nil {
		t.Fatal("helper could be overwritten while locked")
	}
	if err := os.Rename(helper, helper+".old"); err == nil {
		t.Fatal("helper could be renamed away while locked")
	}
	if err := os.Remove(helper); err == nil {
		t.Fatal("helper could be deleted while locked")
	}
	if _, err := os.ReadFile(helper); err != nil {
		t.Fatalf("UAC must still be able to read the locked helper: %v", err)
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(helper, helper+".old"); err != nil {
		t.Fatalf("lock was not released: %v", err)
	}
}

func TestLockVerifiedSelfUpdateExecutableRejectsChangedFile(t *testing.T) {
	helper := filepath.Join(t.TempDir(), "PatchBoard-v9.9.9.exe")
	if err := os.WriteFile(helper, []byte("swapped"), 0o700); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("verified helper"))
	if lock, err := lockVerifiedSelfUpdateExecutable(helper, hex.EncodeToString(sum[:])); err == nil {
		_ = lock.Close()
		t.Fatal("a helper whose hash changed was accepted")
	}
}

// The real caller locks its own running executable.
func TestLockVerifiedSelfUpdateExecutableWorksOnRunningImage(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	expected, err := fileSHA256(executable)
	if err != nil {
		t.Fatal(err)
	}
	lock, err := lockVerifiedSelfUpdateExecutable(executable, expected)
	if err != nil {
		t.Fatalf("could not lock the running executable: %v", err)
	}
	_ = lock.Close()
}
