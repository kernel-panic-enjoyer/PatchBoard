//go:build !windows

package updater

func applyUserPrivatePathAccess(path string, directory bool) error {
	return nil
}
