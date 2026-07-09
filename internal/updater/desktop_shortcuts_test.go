package updater

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDesktopShortcutCleanupRemovesOnlyNewShortcutFiles(t *testing.T) {
	desktopDir := t.TempDir()
	preExistingShortcut := filepath.Join(desktopDir, "keep.lnk")
	newShortcut := filepath.Join(desktopDir, "new-app.lnk")
	newTextFile := filepath.Join(desktopDir, "new-app.txt")
	shortcutNamedDirectory := filepath.Join(desktopDir, "folder.lnk")
	if err := os.WriteFile(preExistingShortcut, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(shortcutNamedDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	restoreDesktopShortcutDirectories := desktopShortcutDirectories
	desktopShortcutDirectories = func() []string { return []string{desktopDir} }
	defer func() { desktopShortcutDirectories = restoreDesktopShortcutDirectories }()

	ctx := withPackageMutationOptions(context.Background(), packageMutationOptions{RemoveNewDesktopShortcuts: true})
	result := runPackageMutationWithDesktopShortcutCleanup(ctx, "test action", func() CommandResult {
		if err := os.WriteFile(newShortcut, []byte("new"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(newTextFile, []byte("not a shortcut"), 0o644); err != nil {
			t.Fatal(err)
		}
		return CommandResult{OK: true, Command: "test action", Stdout: "ok"}
	})

	if !result.OK || !strings.Contains(result.Stdout, "Removed 1 newly-created desktop shortcut") {
		t.Fatalf("expected successful cleanup note without changing command success: %#v", result)
	}
	assertPathExists(t, preExistingShortcut)
	assertPathMissing(t, newShortcut)
	assertPathExists(t, newTextFile)
	assertPathExists(t, shortcutNamedDirectory)
}

func TestDesktopShortcutCleanupDisabledLeavesNewShortcut(t *testing.T) {
	desktopDir := t.TempDir()
	newShortcut := filepath.Join(desktopDir, "new-app.lnk")
	restoreDesktopShortcutDirectories := desktopShortcutDirectories
	desktopShortcutDirectories = func() []string { return []string{desktopDir} }
	defer func() { desktopShortcutDirectories = restoreDesktopShortcutDirectories }()

	result := runPackageMutationWithDesktopShortcutCleanup(context.Background(), "test action", func() CommandResult {
		if err := os.WriteFile(newShortcut, []byte("new"), 0o644); err != nil {
			t.Fatal(err)
		}
		return CommandResult{OK: true, Command: "test action"}
	})

	if !result.OK {
		t.Fatalf("expected command result to pass through: %#v", result)
	}
	assertPathExists(t, newShortcut)
}

func assertPathExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected %s to exist: %v", path, err)
	}
}

func assertPathMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected %s to be absent, stat err=%v", path, err)
	}
}
