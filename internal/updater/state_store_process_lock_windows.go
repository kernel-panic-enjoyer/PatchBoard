//go:build windows

package updater

import (
	"context"
	"fmt"

	"golang.org/x/sys/windows"
)

func acquireStateStoreProcessLock(ctx context.Context, dir string) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	name, err := windows.UTF16PtrFromString(`Local\WindowsUpdaterWebUIState-` + shortHash(dir))
	if err != nil {
		return nil, err
	}
	handle, err := windows.CreateMutex(nil, false, name)
	if err != nil && (handle == 0 || err != windows.ERROR_ALREADY_EXISTS) {
		return nil, fmt.Errorf("could not create state mutex: %w", err)
	}
	wait, err := windows.WaitForSingleObject(handle, windows.INFINITE)
	if err != nil {
		_ = windows.CloseHandle(handle)
		return nil, fmt.Errorf("could not acquire state mutex: %w", err)
	}
	switch wait {
	case windows.WAIT_OBJECT_0, windows.WAIT_ABANDONED:
		return func() {
			_ = windows.ReleaseMutex(handle)
			_ = windows.CloseHandle(handle)
		}, nil
	default:
		_ = windows.CloseHandle(handle)
		return nil, fmt.Errorf("unexpected state mutex wait result: %d", wait)
	}
}
