//go:build !windows

package updater

import (
	"errors"
	"os"
)

func replaceFileKeepingBackup(tempPath, targetPath, backupPath string) error {
	if backupPath != "" {
		if data, err := os.ReadFile(targetPath); err == nil {
			if writeErr := writeUserPrivateFile(backupPath, data); writeErr != nil {
				return writeErr
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return os.Rename(tempPath, targetPath)
}
