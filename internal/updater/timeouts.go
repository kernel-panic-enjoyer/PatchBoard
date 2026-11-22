package updater

import "time"

// Timeouts for running package updates, in one place. How they nest:
//
//	update job batch deadline (updateBatchDeadline, per job)
//	├─ elevated worker startup: UAC prompt and worker start
//	└─ package action timeout: each package-manager command
//	   └─ winget stall timeout: no output and no installer activity
//
//	scheduled auto-update run (defaultScheduledAutoUpdateDeadline)
//	├─ inventory, Store scan and persistence phases
//	└─ package action: the same packageActionTimeout
//
// After a timeout or cancel, a process gets commandTerminationWaitTimeout to
// exit and commandOutputDrainTimeout to flush its output.
const (
	// Mutable package-manager commands can hand off to silent installers.
	// Keep the timeout tight enough that a hidden installer cannot leave an
	// update job looking stuck for an hour.
	packageActionTimeout = 20 * time.Minute

	// A winget mutation with no output and no child-process activity for
	// this long is treated as stalled.
	wingetMutationStallTimeout      = 3 * time.Minute
	commandActivitySamplingInterval = 5 * time.Second

	commandTerminationWaitTimeout = 10 * time.Second
	commandOutputDrainTimeout     = 5 * time.Second

	elevatedWorkerStartupTimeout  = 2 * time.Minute
	elevatedWorkerShutdownTimeout = 5 * time.Second
	elevatedWorkerCancelTimeout   = 10 * time.Second

	// updateBatchOverhead covers the work around the packages of an update
	// job: the refresh before and after, and the elevated worker start.
	updateBatchOverhead = 15 * time.Minute

	defaultScheduledAutoUpdateDeadline = 45 * time.Minute
	scheduledInventoryDeadline         = 10 * time.Minute
	scheduledStoreScanDeadline         = defaultStoreProviderTimeout + 3*time.Minute
	scheduledPackageActionDeadline     = packageActionTimeout
	scheduledPersistenceDeadline       = 30 * time.Second
)

// updateBatchDeadline bounds a whole update job so it always finishes. It is
// generous on purpose: every package may use its full action timeout. Only a
// hung step outside those timeouts can reach it, and then the packages not
// yet finished are reported as timed out, not failed.
func updateBatchDeadline(packageCount int) time.Duration {
	if packageCount < 1 {
		packageCount = 1
	}
	return updateBatchOverhead + time.Duration(packageCount)*packageActionTimeout
}
