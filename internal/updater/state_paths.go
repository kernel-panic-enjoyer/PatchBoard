package updater

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func appRoot() string {
	exe, err := os.Executable()
	if err != nil {
		wd, _ := os.Getwd()
		return wd
	}
	return filepath.Dir(exe)
}

func stateDir() (string, error) {
	if override := os.Getenv("UPDATER_STATE_DIR"); override != "" {
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
		if err := os.MkdirAll(candidate, 0o755); err == nil && canWriteDir(candidate) {
			return candidate, nil
		}
	}
	return "", errors.New("could not create a state directory")
}

func appTempDir() (string, error) {
	if override := os.Getenv("UPDATER_TEMP_DIR"); override != "" {
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
