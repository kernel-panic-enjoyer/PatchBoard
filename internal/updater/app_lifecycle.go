package updater

import (
	"context"
	"runtime/debug"
	"sync"
	"time"
)

// appLifecycle owns cancellation, background goroutine accounting, and
// shutdown cleanup ordering. Keeping these fields together prevents inventory,
// HTTP, and job code from manipulating lifecycle state directly.
type appLifecycle struct {
	mu           sync.Mutex
	rootCtx      context.Context
	rootCancel   context.CancelFunc
	shuttingDown bool
	backgroundWg sync.WaitGroup
	shutdownOnce sync.Once
	cleanupMu    sync.Mutex
	cleanups     []func()
}

func (lifecycle *appLifecycle) ensureRootContextLocked() context.Context {
	if lifecycle.rootCtx == nil {
		lifecycle.rootCtx, lifecycle.rootCancel = context.WithCancel(context.Background())
	}
	return lifecycle.rootCtx
}

func (lifecycle *appLifecycle) isShuttingDown() bool {
	lifecycle.mu.Lock()
	defer lifecycle.mu.Unlock()
	return lifecycle.shuttingDown
}

func (lifecycle *appLifecycle) startBackgroundWork(workName string, runWork func(context.Context)) bool {
	lifecycle.mu.Lock()
	if lifecycle.shuttingDown {
		lifecycle.mu.Unlock()
		appLog("Skipping %s because shutdown is in progress.", workName)
		return false
	}
	rootCtx := lifecycle.ensureRootContextLocked()
	lifecycle.backgroundWg.Add(1)
	lifecycle.mu.Unlock()

	go func() {
		defer lifecycle.backgroundWg.Done()
		runWork(rootCtx)
	}()
	return true
}

func (lifecycle *appLifecycle) rootContext() context.Context {
	lifecycle.mu.Lock()
	defer lifecycle.mu.Unlock()
	return lifecycle.ensureRootContextLocked()
}

func (lifecycle *appLifecycle) beginShutdown() {
	lifecycle.mu.Lock()
	lifecycle.shuttingDown = true
	if lifecycle.rootCancel != nil {
		lifecycle.rootCancel()
	}
	lifecycle.mu.Unlock()
}

func (lifecycle *appLifecycle) waitForBackgroundWork(timeout time.Duration) bool {
	backgroundDone := make(chan struct{})
	go func() {
		lifecycle.backgroundWg.Wait()
		close(backgroundDone)
	}()
	timeoutTimer := time.NewTimer(timeout)
	defer timeoutTimer.Stop()
	select {
	case <-backgroundDone:
		return true
	case <-timeoutTimer.C:
		return false
	}
}

func (lifecycle *appLifecycle) addShutdownCleanup(cleanup func()) {
	if cleanup == nil {
		return
	}
	lifecycle.cleanupMu.Lock()
	defer lifecycle.cleanupMu.Unlock()
	lifecycle.cleanups = append(lifecycle.cleanups, cleanup)
}

func (lifecycle *appLifecycle) runShutdownCleanups() {
	lifecycle.cleanupMu.Lock()
	pendingCleanups := append([]func(){}, lifecycle.cleanups...)
	lifecycle.cleanups = nil
	lifecycle.cleanupMu.Unlock()

	for cleanupIndex := len(pendingCleanups) - 1; cleanupIndex >= 0; cleanupIndex-- {
		func(cleanup func()) {
			defer func() {
				if panicValue := recover(); panicValue != nil {
					appLog("Shutdown cleanup failed: %v\n%s", panicValue, debug.Stack())
				}
			}()
			cleanup()
		}(pendingCleanups[cleanupIndex])
	}
}

func (lifecycle *appLifecycle) runShutdownOnce(shutdown func()) {
	lifecycle.shutdownOnce.Do(shutdown)
}
