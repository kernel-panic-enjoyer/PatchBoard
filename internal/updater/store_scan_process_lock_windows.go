//go:build windows

package updater

import (
	"context"
	"fmt"

	"golang.org/x/sys/windows"
)

func acquireStoreScanProcessLock(ctx context.Context, userSID string) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	name, err := windows.UTF16PtrFromString(`Local\WindowsUpdaterWebUIStoreScan-` + shortHash(userSID))
	if err != nil {
		return nil, err
	}
	handle, err := windows.CreateMutex(nil, false, name)
	if err != nil && (handle == 0 || err != windows.ERROR_ALREADY_EXISTS) {
		return nil, fmt.Errorf("could not create Store scan mutex: %w", err)
	}
	wait, err := windows.WaitForSingleObject(handle, 0)
	if err != nil {
		_ = windows.CloseHandle(handle)
		return nil, fmt.Errorf("could not acquire Store scan mutex: %w", err)
	}
	switch wait {
	case windows.WAIT_OBJECT_0, windows.WAIT_ABANDONED:
		return func() {
			_ = windows.ReleaseMutex(handle)
			_ = windows.CloseHandle(handle)
		}, nil
	case uint32(windows.WAIT_TIMEOUT):
		_ = windows.CloseHandle(handle)
		return nil, errStoreScanAlreadyRunning
	default:
		_ = windows.CloseHandle(handle)
		return nil, fmt.Errorf("unexpected Store scan mutex wait result: %d", wait)
	}
}
