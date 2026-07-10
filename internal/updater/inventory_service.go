package updater

import (
	"sync"
	"time"
)

// inventoryService owns the immutable manager/native inventory cache and the
// lifecycle state of refreshes and background Microsoft Store scans. Published
// Store assessments stay outside this cache and are overlaid only onto copied
// effective snapshots.
type inventoryService struct {
	mu sync.RWMutex

	cache             Inventory
	loading           bool
	queued            bool
	refreshGeneration int64
	fetchedAt         time.Time
	err               string

	// The Store scan runs after the fast manager cache refresh. Its retry and
	// publication timestamps are distinct so an incomplete scan cannot be
	// treated as a successful inventory refresh.
	storeScanLoading           bool
	storeScanQueued            bool
	storeScanLastAttemptAt     time.Time
	storeScanLastPublishedAt   time.Time
	storeScanLastFailureAt     time.Time
	storeBackgroundScanEnabled bool
}
