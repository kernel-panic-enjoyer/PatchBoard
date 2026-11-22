package updater

import "context"

// updateJobDepsWithRefresh runs every package through runner, skips the
// preflight refresh and the elevated batch, and uses refresh after the job.
// It is shared by the unit tests and the uitestsupport build used by the
// browser tests, so it cannot live in a _test file.
func updateJobDepsWithRefresh(runner func(context.Context, string, string) CommandResult, refresh func(context.Context, *App, []Package) error) updateJobDeps {
	return updateJobDeps{
		runPackage: func(ctx context.Context, pkg Package) CommandResult {
			return runner(ctx, pkg.Manager, pkg.ID)
		},
		refreshBefore: func(ctx context.Context, app *App, packages []Package) error { return nil },
		refreshAfter:  refresh,
		batchEligible: func(Package) bool { return false },
	}
}
