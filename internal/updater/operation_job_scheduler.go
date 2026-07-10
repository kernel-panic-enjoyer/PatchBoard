package updater

import "sync"

// operationJobScheduler owns the mutable operation queue and retained job
// history. App coordinates job execution, while this service keeps scheduler
// state and its lock from leaking into unrelated HTTP, inventory, and status
// state.
type operationJobScheduler struct {
	mu       sync.Mutex
	jobs     map[string]*OperationJob
	revision int64
	sequence int64
	queue    []string
	active   bool
}
