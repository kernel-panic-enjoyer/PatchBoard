package updater

import (
	"context"
	"errors"
	"fmt"
	"time"
)

const updateJobModeAll = "all"
const updateJobModeSelected = "selected"

var errUpdateJobRunning = errors.New("an update job is already running")
var errNoUpdateCandidates = errors.New("no updatable packages found")

var errUpdateJobStoreScanRefreshTimeout = errors.New("timed out waiting for Microsoft Store scan to finish")

// updateJobDeps are the steps of an update job that tests replace. A nil
// field means the real step.
type updateJobDeps struct {
	runPackage     func(ctx context.Context, pkg Package) CommandResult
	retryInventory func(ctx context.Context, app *App, reason string) Inventory
	refreshBefore  func(ctx context.Context, app *App, packages []Package) error
	refreshAfter   func(ctx context.Context, app *App, packages []Package) error
	// batchEligible nil means: decide from this machine's elevation support.
	batchEligible func(Package) bool
	runBatch      elevatedPackageUpdateBatchRunnerFunc
}

func (app *App) updateJobSteps() updateJobDeps {
	steps := app.updateJob
	if steps.runPackage == nil {
		steps.runPackage = updatePackageWithMetadataContext
	}
	if steps.retryInventory == nil {
		steps.retryInventory = func(ctx context.Context, app *App, reason string) Inventory {
			return app.refreshInventorySyncContext(ctx, reason)
		}
	}
	if steps.refreshBefore == nil {
		steps.refreshBefore = refreshInventoryBeforeUpdateJob
	}
	if steps.refreshAfter == nil {
		steps.refreshAfter = refreshInventoryAfterUpdateJob
	}
	if steps.runBatch == nil {
		steps.runBatch = runElevatedPackageUpdateBatch
	}
	return steps
}

func refreshInventoryBeforeUpdateJob(ctx context.Context, app *App, updatedPackages []Package) error {
	if updateJobIncludesStorePackage(updatedPackages) {
		return nil
	}
	app.refreshInventorySyncContext(ctx, "update job preflight")
	return ctx.Err()
}

func refreshInventoryAfterUpdateJob(ctx context.Context, app *App, updatedPackages []Package) error {
	app.refreshInventorySyncContext(ctx, "update job")
	if !updateJobIncludesStorePackage(updatedPackages) {
		return nil
	}
	if !app.waitForStoreScanIdle(ctx, 12*time.Minute) {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		appLog("Microsoft Store scan did not finish before update job refresh timeout.")
		return errUpdateJobStoreScanRefreshTimeout
	}
	return nil
}

type UpdateJobStatus = OperationJobStatus

type UpdateOptions struct {
	AllowUnknownVersion bool
	AllowPinned         bool
}

func (app *App) startUpdateJob(packageKeys []string) (UpdateJobStatus, error) {
	return app.startBulkUpdateJob(packageKeys, UpdateOptions{})
}

func (app *App) startUpdateJobWithOptions(packageKeys []string, options UpdateOptions) (UpdateJobStatus, error) {
	return app.startBulkUpdateJob(packageKeys, options)
}

func cloneUpdateJobStatus(status UpdateJobStatus) UpdateJobStatus {
	return cloneOperationJobStatus(status)
}

func updateJobIncludesStorePackage(updatedPackages []Package) bool {
	for _, updatedPackage := range updatedPackages {
		if updatedPackage.Manager == managerStore {
			return true
		}
	}
	return false
}

func updateRefreshNotice(refreshErr error) string {
	if refreshErr == nil {
		return ""
	}
	if errors.Is(refreshErr, errUpdateJobStoreScanRefreshTimeout) {
		return "Update accepted, but Microsoft Store status is still refreshing. See Session Log for diagnostics."
	}
	return fmt.Sprintf("Update refresh did not complete: %s", refreshErr)
}

func (app *App) cancelUpdateJob() UpdateJobStatus {
	latestStatus := app.latestOperationJobStatus(jobTypeUpdateAll, jobTypeUpdate)
	if latestStatus.JobID == "" {
		return UpdateJobStatus{}
	}
	cancelledStatus, ok := app.cancelOperationJob(latestStatus.JobID)
	if !ok {
		return UpdateJobStatus{}
	}
	return cancelledStatus
}

func (app *App) updateJobStatus() UpdateJobStatus {
	return app.latestOperationJobStatus(jobTypeUpdateAll, jobTypeUpdate)
}
