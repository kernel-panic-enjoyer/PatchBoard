//go:build !windows

package updater

import "errors"

func validateDownloadedSelfUpdateExecutable(_ string, _ selfUpdateReleaseMetadata) error {
	return errors.New("self-update executable validation is only supported on Windows")
}
