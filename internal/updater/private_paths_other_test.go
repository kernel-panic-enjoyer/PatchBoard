//go:build !windows

package updater

import (
	"os"
	"testing"
)

func assertUserPrivatePermissions(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if permissions := info.Mode().Perm(); permissions&0o077 != 0 {
		t.Fatalf("%s should not grant group/world permissions, got %o", path, permissions)
	}
}
