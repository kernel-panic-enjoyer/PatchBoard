//go:build !windows

package updater

import "context"

func acquireStoreScanProcessLock(ctx context.Context, userSID string) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return func() {}, nil
}
