//go:build windows

package updater

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplicationInstallStatusReportsNotInstalledInstalledAndRepair(t *testing.T) {
	source := configureApplicationInstallTestRoot(t, []byte("current executable"))
	paths := applicationInstallPathsProvider()

	status := currentApplicationInstallStatus()
	if status.Installed || status.RepairRequired {
		t.Fatalf("missing target should be not installed, got %#v", status)
	}

	if err := os.MkdirAll(paths.InstallDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.TargetPath, []byte("old executable"), 0o755); err != nil {
		t.Fatal(err)
	}
	status = currentApplicationInstallStatus()
	if !status.RepairRequired || !strings.Contains(status.RepairReason, "differs") {
		t.Fatalf("mismatched target should require repair, got %#v", status)
	}

	if err := os.WriteFile(paths.TargetPath, []byte("current executable"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(paths.StartMenuShortcut), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.StartMenuShortcut, []byte("shortcut"), 0o644); err != nil {
		t.Fatal(err)
	}
	registryExists := false
	replaceApplicationInstallRegistryExists(t, func(applicationInstallPaths) bool { return registryExists })
	status = currentApplicationInstallStatus()
	if !status.RepairRequired || !strings.Contains(status.RepairReason, "Installed Apps") {
		t.Fatalf("missing registry should require repair, got %#v", status)
	}

	registryExists = true
	status = currentApplicationInstallStatus()
	if !status.Installed || status.RepairRequired || !status.ExecutableMatches || !status.StartMenuShortcutExists || !status.InstalledAppsEntryExists {
		t.Fatalf("complete install should report installed, got %#v", status)
	}
	if status.TargetPath != paths.TargetPath || status.InstallDirectory != paths.InstallDirectory {
		t.Fatalf("status should expose fixed install paths, got %#v", status)
	}
	if sameWindowsPath(source, paths.TargetPath) {
		t.Fatal("test source should be outside the install target")
	}
}

func TestInstallApplicationDirectCopiesExecutableAndRegistersWindowsIntegration(t *testing.T) {
	configureApplicationInstallTestRoot(t, []byte("current executable"))
	paths := applicationInstallPathsProvider()
	var registryWritten bool
	replaceApplicationInstallRegistryExists(t, func(applicationInstallPaths) bool { return registryWritten })
	replaceApplicationInstallRegistryWriter(t, func(got applicationInstallPaths) error {
		if got.TargetPath != paths.TargetPath {
			t.Fatalf("registry writer target = %q, want %q", got.TargetPath, paths.TargetPath)
		}
		registryWritten = true
		return nil
	})
	replaceApplicationShortcutCreator(t, func(_ context.Context, got applicationInstallPaths) error {
		if got.StartMenuShortcut != paths.StartMenuShortcut {
			t.Fatalf("shortcut target = %q, want %q", got.StartMenuShortcut, paths.StartMenuShortcut)
		}
		if err := os.MkdirAll(filepath.Dir(got.StartMenuShortcut), 0o755); err != nil {
			return err
		}
		return os.WriteFile(got.StartMenuShortcut, []byte("shortcut"), 0o644)
	})

	payload, err := currentApplicationInstallPayload()
	if err != nil {
		t.Fatal(err)
	}
	result := installApplicationDirectContext(context.Background(), payload.SourceExe, payload.TargetExe)
	if !result.OK {
		t.Fatalf("install failed: %#v", result)
	}
	installedData, err := os.ReadFile(paths.TargetPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(installedData) != "current executable" {
		t.Fatalf("installed executable data = %q", installedData)
	}
	if !fileExists(paths.StartMenuShortcut) || !registryWritten {
		t.Fatalf("install did not create shortcut/registry: shortcut=%t registry=%t", fileExists(paths.StartMenuShortcut), registryWritten)
	}
}

func TestRestartInstalledApplicationRequiresValidInstall(t *testing.T) {
	configureApplicationInstallTestRoot(t, []byte("current executable"))
	result := restartInstalledApplicationContext(context.Background())
	if result.OK || !strings.Contains(result.Stderr, "not installed") {
		t.Fatalf("restart should reject missing install, got %#v", result)
	}

	paths := applicationInstallPathsProvider()
	if err := os.MkdirAll(paths.InstallDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.TargetPath, []byte("current executable"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(paths.StartMenuShortcut), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.StartMenuShortcut, []byte("shortcut"), 0o644); err != nil {
		t.Fatal(err)
	}
	replaceApplicationInstallRegistryExists(t, func(applicationInstallPaths) bool { return true })
	var launched string
	replaceInstalledApplicationLauncher(t, func(target string) error {
		launched = target
		return nil
	})

	result = restartInstalledApplicationContext(context.Background())
	if !result.OK || !sameWindowsPath(launched, paths.TargetPath) {
		t.Fatalf("restart should launch installed copy, result=%#v launched=%q", result, launched)
	}
}

func TestApplicationUninstallApplyRemovesOnlyPatchBoardInstallArtifacts(t *testing.T) {
	configureApplicationInstallTestRoot(t, []byte("current executable"))
	paths := applicationInstallPathsProvider()
	outside := filepath.Join(filepath.Dir(paths.InstallDirectory), "OtherApp")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "keep.txt"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(paths.InstallDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.TargetPath, []byte("current executable"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(paths.StartMenuShortcut), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.StartMenuShortcut, []byte("shortcut"), 0o644); err != nil {
		t.Fatal(err)
	}
	var registryDeleted bool
	replaceApplicationInstallRegistryDeleter(t, func() error {
		registryDeleted = true
		return nil
	})
	replaceUninstallSelfDelete(t, func() {})

	if err := runApplicationUninstallApply(paths.InstallDirectory, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(paths.InstallDirectory); !os.IsNotExist(err) {
		t.Fatalf("install directory should be removed, err=%v", err)
	}
	if _, err := os.Stat(paths.StartMenuShortcut); !os.IsNotExist(err) {
		t.Fatalf("shortcut should be removed, err=%v", err)
	}
	if !registryDeleted {
		t.Fatal("uninstall did not delete registry entry")
	}
	if _, err := os.Stat(filepath.Join(outside, "keep.txt")); err != nil {
		t.Fatalf("uninstall removed unrelated folder: %v", err)
	}
}

func configureApplicationInstallTestRoot(t *testing.T, sourceContents []byte) string {
	t.Helper()
	dir := t.TempDir()
	source := filepath.Join(dir, "running", "PatchBoard.exe")
	if err := os.MkdirAll(filepath.Dir(source), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, sourceContents, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ProgramFiles", filepath.Join(dir, "Program Files"))
	t.Setenv("ProgramData", filepath.Join(dir, "ProgramData"))
	originalExecutable := applicationInstallCurrentExecutable
	applicationInstallCurrentExecutable = func() (string, error) { return source, nil }
	t.Cleanup(func() { applicationInstallCurrentExecutable = originalExecutable })
	replaceApplicationInstallRegistryExists(t, func(applicationInstallPaths) bool { return false })
	return source
}

func replaceApplicationInstallRegistryExists(t *testing.T, replacement func(applicationInstallPaths) bool) {
	t.Helper()
	original := applicationInstalledAppsEntryExists
	applicationInstalledAppsEntryExists = replacement
	t.Cleanup(func() { applicationInstalledAppsEntryExists = original })
}

func replaceApplicationInstallRegistryWriter(t *testing.T, replacement func(applicationInstallPaths) error) {
	t.Helper()
	original := writeApplicationInstalledAppsEntry
	writeApplicationInstalledAppsEntry = replacement
	t.Cleanup(func() { writeApplicationInstalledAppsEntry = original })
}

func replaceApplicationInstallRegistryDeleter(t *testing.T, replacement func() error) {
	t.Helper()
	original := deleteApplicationInstalledAppsEntry
	deleteApplicationInstalledAppsEntry = replacement
	t.Cleanup(func() { deleteApplicationInstalledAppsEntry = original })
}

func replaceApplicationShortcutCreator(t *testing.T, replacement func(context.Context, applicationInstallPaths) error) {
	t.Helper()
	original := createApplicationStartMenuShortcut
	createApplicationStartMenuShortcut = replacement
	t.Cleanup(func() { createApplicationStartMenuShortcut = original })
}

func replaceInstalledApplicationLauncher(t *testing.T, replacement func(string) error) {
	t.Helper()
	original := launchInstalledApplicationExecutable
	launchInstalledApplicationExecutable = replacement
	t.Cleanup(func() { launchInstalledApplicationExecutable = original })
}

func replaceUninstallSelfDelete(t *testing.T, replacement func()) {
	t.Helper()
	original := scheduleApplicationUninstallSelfDelete
	scheduleApplicationUninstallSelfDelete = replacement
	t.Cleanup(func() { scheduleApplicationUninstallSelfDelete = original })
}
