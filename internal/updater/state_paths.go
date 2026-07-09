package updater

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const legacyAppDirName = "WindowsUpdaterWebUI"

func appRoot() string {
	exe, err := os.Executable()
	if err != nil {
		wd, _ := os.Getwd()
		return wd
	}
	return filepath.Dir(exe)
}

func stateDir() (string, error) {
	if override := os.Getenv("UPDATER_STATE_DIR"); override != "" && userEnvironmentOverridesAllowed() {
		if err := os.MkdirAll(override, 0o755); err != nil {
			return "", err
		}
		if !canWriteDir(override) {
			return "", fmt.Errorf("state directory is not writable: %s", override)
		}
		return override, nil
	}

	var candidates []string
	for _, env := range []string{"LOCALAPPDATA", "APPDATA", "USERPROFILE", "ProgramData"} {
		if value := os.Getenv(env); value != "" {
			candidates = append(candidates, filepath.Join(value, appDirName))
		}
	}
	candidates = append(candidates, filepath.Join(appRoot(), ".state"))

	for _, candidate := range candidates {
		migrateLegacyStateDirectory(candidate)
		if err := os.MkdirAll(candidate, 0o755); err == nil && canWriteDir(candidate) {
			return candidate, nil
		}
	}
	return "", errors.New("could not create a state directory")
}

func migrateLegacyStateDirectory(newDirectory string) {
	if legacyAppDirName == "" || legacyAppDirName == appDirName {
		return
	}
	oldDirectory, ok := legacyStateDirectoryFor(newDirectory)
	if !ok || oldDirectory == newDirectory {
		return
	}
	if directoryHasState(newDirectory) || !directoryHasState(oldDirectory) {
		return
	}
	if err := copyDirectoryContents(oldDirectory, newDirectory); err != nil {
		appLog("Could not migrate legacy state from %s to %s: %s.", oldDirectory, newDirectory, err)
		return
	}
	appLog("Migrated legacy state from %s to %s.", oldDirectory, newDirectory)
}

func legacyStateDirectoryFor(newDirectory string) (string, bool) {
	cleanDirectory := filepath.Clean(newDirectory)
	if filepath.Base(cleanDirectory) != appDirName {
		return "", false
	}
	return filepath.Join(filepath.Dir(cleanDirectory), legacyAppDirName), true
}

func directoryHasState(directory string) bool {
	if directory == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(directory, "state.json"))
	return err == nil
}

func copyDirectoryContents(sourceDirectory, targetDirectory string) error {
	return filepath.WalkDir(sourceDirectory, func(sourcePath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relativePath, err := filepath.Rel(sourceDirectory, sourcePath)
		if err != nil {
			return err
		}
		if relativePath == "." {
			return os.MkdirAll(targetDirectory, 0o755)
		}
		if strings.HasPrefix(relativePath, ".."+string(filepath.Separator)) || filepath.IsAbs(relativePath) {
			return fmt.Errorf("legacy state path escaped source directory: %s", sourcePath)
		}
		targetPath := filepath.Join(targetDirectory, relativePath)
		if entry.IsDir() {
			return os.MkdirAll(targetPath, 0o755)
		}
		if entry.Type()&os.ModeType != 0 {
			return nil
		}
		return copyFile(sourcePath, targetPath)
	})
}

func copyFile(sourcePath, targetPath string) error {
	data, err := os.ReadFile(sourcePath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(targetPath, data, 0o644)
}

func appTempDir() (string, error) {
	if override := os.Getenv("UPDATER_TEMP_DIR"); override != "" && userEnvironmentOverridesAllowed() {
		if err := os.MkdirAll(override, 0o755); err != nil {
			return "", err
		}
		if !canWriteDir(override) {
			return "", fmt.Errorf("temporary directory is not writable: %s", override)
		}
		return override, nil
	}

	candidates := []string{}
	if value := os.TempDir(); value != "" {
		candidates = append(candidates, filepath.Join(value, appDirName))
	}

	for _, candidate := range candidates {
		if err := os.MkdirAll(candidate, 0o755); err == nil && canWriteDir(candidate) {
			return candidate, nil
		}
	}
	return "", errors.New("could not create a temporary directory")
}

func canWriteDir(dir string) bool {
	file, err := os.CreateTemp(dir, fmt.Sprintf(".write-test-%d-", os.Getpid()))
	if err != nil {
		return false
	}
	path := file.Name()
	_, writeErr := file.Write([]byte("ok"))
	closeErr := file.Close()
	_ = os.Remove(path)
	return writeErr == nil && closeErr == nil
}
