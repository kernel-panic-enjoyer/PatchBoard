package updater

import (
	"context"
	"sync"
)

var defaultStoreScanCoordinator = newStoreScanCoordinator()

type storeScanCoordinator struct {
	mu      sync.Mutex
	running map[string]bool
}

func newStoreScanCoordinator() *storeScanCoordinator {
	return &storeScanCoordinator{running: map[string]bool{}}
}

func (coordinator *storeScanCoordinator) acquire(ctx context.Context, userSID string) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if coordinator == nil {
		coordinator = newStoreScanCoordinator()
	}
	key := userScopeHash(userSID)
	coordinator.mu.Lock()
	if coordinator.running[key] {
		coordinator.mu.Unlock()
		return nil, errStoreScanAlreadyRunning
	}
	coordinator.running[key] = true
	coordinator.mu.Unlock()

	processRelease, err := acquireStoreScanProcessLock(ctx, userSID)
	if err != nil {
		coordinator.mu.Lock()
		delete(coordinator.running, key)
		coordinator.mu.Unlock()
		return nil, err
	}

	var once sync.Once
	return func() {
		once.Do(func() {
			processRelease()
			coordinator.mu.Lock()
			delete(coordinator.running, key)
			coordinator.mu.Unlock()
		})
	}, nil
}
