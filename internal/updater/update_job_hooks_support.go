package updater

import "context"

// replaceUpdateJobHooksWithRefresh swaps the update job's package runner and
// inventory refresh hooks and returns a function that restores them. It is
// shared by the unit tests and the uitestsupport build used by the browser
// tests, so it cannot live in a _test file.
func replaceUpdateJobHooksWithRefresh(runner func(context.Context, string, string) CommandResult, refresh func(context.Context, *App, []Package) error) func() {
	oldRunner := updatePackageRunner
	oldPreflightRefresh := refreshInventoryBeforeUpdateJob
	oldRefresh := refreshInventoryAfterUpdateJob
	oldEligible := elevatedPackageUpdateBatchEligible
	updatePackageRunner = func(ctx context.Context, pkg Package) CommandResult {
		return runner(ctx, pkg.Manager, pkg.ID)
	}
	refreshInventoryBeforeUpdateJob = func(ctx context.Context, app *App, packages []Package) error { return nil }
	refreshInventoryAfterUpdateJob = refresh
	elevatedPackageUpdateBatchEligible = func(Package) bool { return false }
	return func() {
		updatePackageRunner = oldRunner
		refreshInventoryBeforeUpdateJob = oldPreflightRefresh
		refreshInventoryAfterUpdateJob = oldRefresh
		elevatedPackageUpdateBatchEligible = oldEligible
	}
}
