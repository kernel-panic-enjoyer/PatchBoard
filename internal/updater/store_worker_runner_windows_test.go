//go:build windows

package updater

import (
	"context"
	"testing"
	"time"
)

// Each Store worker keeps its own deadline on the shared runner: the given
// timeout when set, else that worker's default.
func TestStoreWorkerContextUsesEachWorkersTimeout(t *testing.T) {
	for name, tc := range map[string]struct {
		timeout, fallback, want time.Duration
	}{
		"inventory default":        {0, storePackagedInventoryTimeout, storePackagedInventoryTimeout},
		"update discovery default": {0, storeUpdateDiscoveryTimeout, storeUpdateDiscoveryTimeout},
		"explicit timeout":         {2 * time.Second, storeUpdateDiscoveryTimeout, 2 * time.Second},
	} {
		started := time.Now()
		ctx, cancel, deadline := storeWorkerContext(context.Background(), tc.timeout, tc.fallback)
		ctxDeadline, ok := ctx.Deadline()
		cancel()
		if !ok || !ctxDeadline.UTC().Equal(deadline) {
			t.Fatalf("%s: request deadline %v differs from context deadline %v", name, deadline, ctxDeadline)
		}
		if got := deadline.Sub(started); got < tc.want-time.Second || got > tc.want+time.Second {
			t.Errorf("%s: deadline in %v, want about %v", name, got, tc.want)
		}
	}
}
