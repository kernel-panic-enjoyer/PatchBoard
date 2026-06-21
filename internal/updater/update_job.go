package updater

import "errors"

const updateJobModeAll = "all"
const updateJobModeSelected = "selected"

var errUpdateJobRunning = errors.New("an update job is already running")
var errNoUpdateCandidates = errors.New("no updateable packages found")

var refreshInventoryAfterUpdateJob = func(app *App) {
	app.refreshInventorySync("update job")
}

type UpdateJobStatus = OperationJobStatus

type UpdateOptions struct {
	AllowUnknownVersion bool
	AllowPinned         bool
}

func (app *App) startUpdateJob(packageKeys []string) (UpdateJobStatus, error) {
	return app.startUpdateJobWithOptions(packageKeys, UpdateOptions{})
}

func (app *App) startUpdateJobWithOptions(packageKeys []string, options UpdateOptions) (UpdateJobStatus, error) {
	return app.startBulkUpdateJob(packageKeys, options)
}

func cloneUpdateJobStatus(status UpdateJobStatus) UpdateJobStatus {
	return cloneOperationJobStatus(status)
}

func updateJobNotice(status UpdateJobStatus) string {
	if status.CancelRequested {
		return "Update cancelled. Refreshing package status..."
	}
	if notice := updateResultsFailureNotice(status.Results); notice != "" {
		return notice
	}
	return "Update completed. Refreshing package status..."
}

func (app *App) cancelUpdateJob() UpdateJobStatus {
	status := app.latestOperationJobStatus(jobTypeUpdateAll, jobTypeUpdate)
	if status.JobID == "" {
		return UpdateJobStatus{}
	}
	cancelled, ok := app.cancelOperationJob(status.JobID)
	if !ok {
		return UpdateJobStatus{}
	}
	return cancelled
}

func (app *App) updateJobStatus() UpdateJobStatus {
	return app.latestOperationJobStatus(jobTypeUpdateAll, jobTypeUpdate)
}
