package updater

import (
	"context"
	"fmt"
	"strings"
)

const (
	jobStateQueued              = "queued"
	jobStateStarting            = "starting"
	jobStateRunning             = "running"
	jobStateAccepted            = "accepted"
	jobStateVerifying           = "verifying"
	jobStateRefreshing          = "refreshing"
	jobStateSucceeded           = "succeeded"
	jobStateAcceptedNotVerified = "accepted_not_verified"
	jobStateFailed              = "failed"
	jobStateCancelled           = "cancelled"

	jobTypeInstall          = "install"
	jobTypeUpdate           = "update"
	jobTypeUpdateAll        = "update-all"
	jobTypeScan             = "scan"
	jobTypeManagerInstall   = "manager-install"
	jobTypeInventoryRefresh = "inventory-refresh"
	jobTypeSelfUpdate       = "app-self-update"
)

const operationJobRecentHistoryLimit = 25
const maxPendingOperationJobs = 25

type OperationJobStatus struct {
	JobID               string         `json:"job_id,omitempty"`
	ExistingJobID       string         `json:"existing_job_id,omitempty"`
	Revision            int64          `json:"revision,omitempty"`
	Type                string         `json:"type,omitempty"`
	Mode                string         `json:"mode,omitempty"`
	State               string         `json:"state"`
	Running             bool           `json:"running"`
	QueueDepth          int            `json:"queue_depth,omitempty"`
	CancelRequested     bool           `json:"cancel_requested"`
	CurrentPackage      string         `json:"current_package,omitempty"`
	CurrentKey          string         `json:"current_key,omitempty"`
	PackageKeys         []string       `json:"package_keys,omitempty"`
	Packages            []Package      `json:"packages,omitempty"`
	CurrentIndex        int            `json:"current_index"`
	Total               int            `json:"total"`
	Results             []UpdateResult `json:"results,omitempty"`
	Result              *CommandResult `json:"result,omitempty"`
	Scan                *ScanResult    `json:"scan,omitempty"`
	RefreshStarted      bool           `json:"refresh_started"`
	AllowUnknownVersion bool           `json:"allow_unknown_version,omitempty"`
	AllowPinned         bool           `json:"allow_pinned,omitempty"`
	StartedAt           string         `json:"started_at,omitempty"`
	FinishedAt          string         `json:"finished_at,omitempty"`
	Notice              string         `json:"notice,omitempty"`
	Error               string         `json:"error,omitempty"`
}

type OperationJob struct {
	status  OperationJobStatus
	execute func(context.Context, *OperationJob)
	cancel  context.CancelFunc
}

func (app *App) startOperationJob(jobType, mode string, total int, packageKeys []string, execute func(context.Context, *OperationJob)) OperationJobStatus {
	return app.startOperationJobWithPackageSnapshot(jobType, mode, total, packageKeys, nil, execute)
}

func (app *App) startOperationJobWithPackageSnapshot(jobType, mode string, total int, packageKeys []string, packages []Package, execute func(context.Context, *OperationJob)) OperationJobStatus {
	if app.isShuttingDown() {
		return OperationJobStatus{
			Type:        jobType,
			Mode:        mode,
			State:       jobStateFailed,
			Running:     false,
			Total:       total,
			PackageKeys: append([]string(nil), packageKeys...),
			Notice:      "Job not started because shutdown is in progress.",
			Error:       "shutdown in progress",
			FinishedAt:  utcNow(),
		}
	}
	app.jobScheduler.mu.Lock()
	if app.jobScheduler.jobs == nil {
		app.jobScheduler.jobs = map[string]*OperationJob{}
	}
	if duplicateStatus, duplicate := app.jobScheduler.duplicateStatusLocked(jobType, mode, total, packageKeys); duplicate {
		app.jobScheduler.mu.Unlock()
		return duplicateStatus
	}
	if pendingJobs := app.jobScheduler.pendingCountLocked(); pendingJobs >= maxPendingOperationJobs {
		app.jobScheduler.mu.Unlock()
		return rejectedOperationJobStatus(jobType, mode, total, packageKeys, "", "operation job queue is full")
	}
	allowsUnknownVersion, allowsPinnedVersion := packageSnapshotUpdateAllowances(packages)
	app.jobScheduler.sequence++
	operationJob := &OperationJob{
		execute: execute,
		status: OperationJobStatus{
			JobID:               fmt.Sprintf("job-%d", app.jobScheduler.sequence),
			Revision:            1,
			Type:                jobType,
			Mode:                mode,
			State:               jobStateQueued,
			QueueDepth:          len(app.jobScheduler.queue) + 1,
			Total:               total,
			PackageKeys:         append([]string(nil), packageKeys...),
			Packages:            append([]Package(nil), packages...),
			AllowUnknownVersion: allowsUnknownVersion,
			AllowPinned:         allowsPinnedVersion,
		},
	}
	app.jobScheduler.jobs[operationJob.status.JobID] = operationJob
	app.jobScheduler.queue = append(app.jobScheduler.queue, operationJob.status.JobID)
	app.jobScheduler.bumpRevisionLocked()
	queuedStatus := cloneOperationJobStatus(operationJob.status)
	appLog("Job %s queued for %s.", operationJob.status.JobID, jobType)
	shouldStartQueueRunner := !app.jobScheduler.active
	if shouldStartQueueRunner {
		app.jobScheduler.active = true
	}
	app.jobScheduler.mu.Unlock()
	if shouldStartQueueRunner {
		if !app.startBackgroundWork("operation job queue", app.runOperationJobQueue) {
			app.jobScheduler.mu.Lock()
			app.jobScheduler.active = false
			operationJob.status.CancelRequested = true
			finishQueuedOperationJobCancellation(&operationJob.status, "Job cancelled by shutdown.")
			app.jobScheduler.bumpJobRevisionLocked(operationJob)
			app.jobScheduler.mu.Unlock()
		}
	}
	return queuedStatus
}

func rejectedOperationJobStatus(jobType, mode string, total int, packageKeys []string, existingJobID, message string) OperationJobStatus {
	return OperationJobStatus{
		ExistingJobID: existingJobID,
		Type:          jobType,
		Mode:          mode,
		State:         jobStateFailed,
		Running:       false,
		Total:         total,
		PackageKeys:   append([]string(nil), packageKeys...),
		Notice:        message,
		Error:         message,
		FinishedAt:    utcNow(),
	}
}

func (scheduler *operationJobScheduler) duplicateStatusLocked(jobType, mode string, total int, packageKeys []string) (OperationJobStatus, bool) {
	for i := scheduler.sequence; i >= 1; i-- {
		jobID := fmt.Sprintf("job-%d", i)
		job := scheduler.jobs[jobID]
		if job == nil || operationJobComplete(job.status) {
			continue
		}
		if operationJobTypeDedupesByType(jobType) && job.status.Type == jobType {
			message := fmt.Sprintf("%s job is already queued or running as %s", jobType, job.status.JobID)
			return rejectedOperationJobStatus(jobType, mode, total, packageKeys, job.status.JobID, message), true
		}
		if operationJobTypeDedupesByPackage(jobType) && operationJobTypeDedupesByPackage(job.status.Type) && packageKeysOverlap(packageKeys, job.status.PackageKeys) {
			message := fmt.Sprintf("package operation is already queued or running as %s", job.status.JobID)
			return rejectedOperationJobStatus(jobType, mode, total, packageKeys, job.status.JobID, message), true
		}
	}
	return OperationJobStatus{}, false
}

func operationJobTypeDedupesByType(jobType string) bool {
	switch jobType {
	case jobTypeScan, jobTypeInventoryRefresh, jobTypeSelfUpdate:
		return true
	default:
		return false
	}
}

func operationJobTypeDedupesByPackage(jobType string) bool {
	switch jobType {
	case jobTypeInstall, jobTypeUpdate, jobTypeUpdateAll:
		return true
	default:
		return false
	}
}

func packageKeysOverlap(left, right []string) bool {
	if len(left) == 0 || len(right) == 0 {
		return false
	}
	seen := map[string]bool{}
	for _, key := range left {
		normalized := strings.TrimSpace(key)
		if normalized != "" {
			seen[normalized] = true
		}
	}
	for _, key := range right {
		if seen[strings.TrimSpace(key)] {
			return true
		}
	}
	return false
}

func (scheduler *operationJobScheduler) pendingCountLocked() int {
	pendingJobs := 0
	for _, jobID := range scheduler.queue {
		job := scheduler.jobs[jobID]
		if job == nil || operationJobComplete(job.status) {
			continue
		}
		pendingJobs++
	}
	return pendingJobs
}

func (scheduler *operationJobScheduler) bumpRevisionLocked() {
	scheduler.revision++
}

func (scheduler *operationJobScheduler) bumpJobRevisionLocked(job *OperationJob) {
	if job != nil {
		job.status.Revision++
	}
	scheduler.bumpRevisionLocked()
}

func packageSnapshotUpdateAllowances(packages []Package) (allowsUnknownVersion, allowsPinnedVersion bool) {
	for _, pkg := range packages {
		allowsUnknownVersion = allowsUnknownVersion || pkg.AllowUnknownVersionUpdate
		allowsPinnedVersion = allowsPinnedVersion || pkg.AllowPinnedUpdate
	}
	return allowsUnknownVersion, allowsPinnedVersion
}

func (app *App) runOperationJobQueue(queueCtx context.Context) {
	for {
		app.jobScheduler.mu.Lock()
		var nextJob *OperationJob
		for len(app.jobScheduler.queue) > 0 {
			queuedJobID := app.jobScheduler.queue[0]
			app.jobScheduler.queue = app.jobScheduler.queue[1:]
			queuedJob := app.jobScheduler.jobs[queuedJobID]
			if queuedJob == nil {
				continue
			}
			if queuedJob.status.CancelRequested {
				finishQueuedOperationJobCancellation(&queuedJob.status, "")
				app.jobScheduler.bumpJobRevisionLocked(queuedJob)
				continue
			}
			if queueCtx.Err() != nil {
				queuedJob.status.CancelRequested = true
				finishQueuedOperationJobCancellation(&queuedJob.status, "Job cancelled by shutdown.")
				app.jobScheduler.bumpJobRevisionLocked(queuedJob)
				continue
			}
			nextJob = queuedJob
			break
		}
		if nextJob == nil {
			app.jobScheduler.active = false
			app.jobScheduler.mu.Unlock()
			return
		}
		jobCtx, cancelJob := context.WithCancel(queueCtx)
		nextJob.cancel = cancelJob
		nextJob.status.State = jobStateRunning
		nextJob.status.Running = true
		nextJob.status.StartedAt = utcNow()
		app.jobScheduler.bumpJobRevisionLocked(nextJob)
		startedStatus := cloneOperationJobStatus(nextJob.status)
		app.jobScheduler.mu.Unlock()

		jobCtx = withLogMetadata(jobCtx, logMetadata{JobID: startedStatus.JobID, JobType: startedStatus.Type})
		appLogContext(jobCtx, "Job %s started for %s.", startedStatus.JobID, startedStatus.Type)
		panicValue := runOperationJobSafely(jobCtx, nextJob)
		cancelJob()
		if panicValue != nil {
			diagnostic := sanitizedPanicDiagnostic(panicValue)
			app.mutateOperationJob(nextJob, func(status *OperationJobStatus) {
				status.State = jobStateFailed
				status.Error = diagnostic
				status.Notice = "Job failed because an internal error occurred."
				status.Result = &CommandResult{Command: status.Type, Code: 1, Stderr: diagnostic}
			})
		}

		app.jobScheduler.mu.Lock()
		if nextJob.status.State == jobStateRunning || nextJob.status.State == jobStateRefreshing {
			if nextJob.status.CancelRequested {
				nextJob.status.State = jobStateCancelled
				nextJob.status.Notice = "Job cancelled."
			} else if nextJob.status.Error != "" || operationJobStatusHasFailures(nextJob.status) {
				nextJob.status.State = jobStateFailed
			} else {
				nextJob.status.State = jobStateSucceeded
			}
		}
		nextJob.status.Running = false
		if nextJob.status.FinishedAt == "" {
			nextJob.status.FinishedAt = utcNow()
		}
		if operationJobComplete(nextJob.status) {
			compactTerminalOperationJobStatus(&nextJob.status)
		}
		finishedStatus := cloneOperationJobStatus(nextJob.status)
		// Publish the final correlated log entry before the terminal revision can
		// be observed. Callers that see a completed job can then fetch a complete
		// job log without racing this final write.
		appLogContext(jobCtx, "Job %s finished with state %s.", finishedStatus.JobID, finishedStatus.State)
		app.jobScheduler.bumpJobRevisionLocked(nextJob)
		app.jobScheduler.pruneLocked()
		app.jobScheduler.mu.Unlock()
	}
}

func finishQueuedOperationJobCancellation(status *OperationJobStatus, notice string) {
	status.State = jobStateCancelled
	status.Running = false
	if notice != "" {
		status.Notice = notice
	}
	status.FinishedAt = utcNow()
	compactTerminalOperationJobStatus(status)
}

func runOperationJobSafely(ctx context.Context, job *OperationJob) (panicValue any) {
	defer func() {
		panicValue = recover()
	}()
	job.execute(ctx, job)
	return nil
}

func sanitizedPanicDiagnostic(panicValue any) string {
	message := strings.TrimSpace(fmt.Sprint(panicValue))
	message = strings.ReplaceAll(message, "\r", " ")
	message = strings.ReplaceAll(message, "\n", " ")
	if message == "" {
		message = "unknown panic"
	}
	if len(message) > 240 {
		message = message[:240] + "..."
	}
	return "internal job panic: " + message
}

func operationJobStatusHasFailures(status OperationJobStatus) bool {
	if status.Result != nil && !status.Result.OK {
		return true
	}
	for _, result := range status.Results {
		if !result.Result.OK {
			return true
		}
	}
	return false
}

func compactTerminalOperationJobStatus(status *OperationJobStatus) {
	if status == nil || !operationJobComplete(*status) {
		return
	}
	status.Packages = nil
	if status.Result != nil {
		result := compactCommandResult(*status.Result, terminalCommandResultStreamBytes, maxCommandResultCommandBytes)
		status.Result = &result
	}
	for i := range status.Results {
		status.Results[i].Result = compactCommandResult(status.Results[i].Result, terminalCommandResultStreamBytes, maxCommandResultCommandBytes)
	}
	if status.Scan != nil {
		status.Scan.NewApps = nil
		status.Scan.RemovedApps = nil
		if status.Scan.WingetResult != nil {
			result := compactCommandResult(*status.Scan.WingetResult, terminalCommandResultStreamBytes, maxCommandResultCommandBytes)
			status.Scan.WingetResult = &result
		}
	}
}

func cloneOperationJobStatus(status OperationJobStatus) OperationJobStatus {
	status.PackageKeys = append([]string(nil), status.PackageKeys...)
	status.Packages = append([]Package(nil), status.Packages...)
	status.Results = append([]UpdateResult(nil), status.Results...)
	if status.Result != nil {
		result := *status.Result
		status.Result = &result
	}
	if status.Scan != nil {
		scan := *status.Scan
		status.Scan = &scan
	}
	return status
}

func (app *App) mutateOperationJob(job *OperationJob, mutate func(*OperationJobStatus)) OperationJobStatus {
	app.jobScheduler.mu.Lock()
	defer app.jobScheduler.mu.Unlock()
	mutate(&job.status)
	app.jobScheduler.bumpJobRevisionLocked(job)
	return cloneOperationJobStatus(job.status)
}

func (app *App) operationJobStatus(id string) (OperationJobStatus, bool) {
	app.jobScheduler.mu.Lock()
	defer app.jobScheduler.mu.Unlock()
	job := app.jobScheduler.jobs[id]
	if job == nil {
		return OperationJobStatus{}, false
	}
	return cloneOperationJobStatus(job.status), true
}

func (app *App) latestOperationJobStatus(jobTypes ...string) OperationJobStatus {
	app.jobScheduler.mu.Lock()
	defer app.jobScheduler.mu.Unlock()
	requestedTypes := map[string]bool{}
	for _, jobType := range jobTypes {
		requestedTypes[jobType] = true
	}
	for i := app.jobScheduler.sequence; i >= 1; i-- {
		jobID := fmt.Sprintf("job-%d", i)
		job := app.jobScheduler.jobs[jobID]
		if job == nil {
			continue
		}
		if len(requestedTypes) == 0 || requestedTypes[job.status.Type] {
			return cloneOperationJobStatus(job.status)
		}
	}
	return OperationJobStatus{}
}

func (app *App) operationJobsSnapshot() []OperationJobStatus {
	statuses, _, _ := app.operationJobsSnapshotWithRevision()
	return statuses
}

func (app *App) operationJobsSnapshotWithRevision() ([]OperationJobStatus, int64, int) {
	app.jobScheduler.mu.Lock()
	defer app.jobScheduler.mu.Unlock()
	app.jobScheduler.pruneLocked()
	statuses := make([]OperationJobStatus, 0, len(app.jobScheduler.jobs))
	for i := int64(1); i <= app.jobScheduler.sequence; i++ {
		jobID := fmt.Sprintf("job-%d", i)
		job := app.jobScheduler.jobs[jobID]
		if job == nil {
			continue
		}
		statuses = append(statuses, cloneOperationJobStatus(job.status))
	}
	return statuses, app.jobScheduler.revision, app.jobScheduler.pendingCountLocked()
}

func operationJobComplete(status OperationJobStatus) bool {
	state := strings.ToLower(strings.TrimSpace(status.State))
	return !status.Running && state != jobStateQueued && state != jobStateRunning && state != jobStateRefreshing
}

func (app *App) cancelOperationJob(id string) (OperationJobStatus, bool) {
	app.jobScheduler.mu.Lock()
	defer app.jobScheduler.mu.Unlock()
	job := app.jobScheduler.jobs[id]
	if job == nil {
		return OperationJobStatus{}, false
	}
	if job.status.State == jobStateQueued || job.status.Running {
		job.status.CancelRequested = true
		job.status.Notice = "Cancelling job..."
		if job.cancel != nil {
			job.cancel()
		}
		if job.status.State == jobStateQueued {
			finishQueuedOperationJobCancellation(&job.status, "Job cancelled.")
		}
		app.jobScheduler.bumpJobRevisionLocked(job)
		appLog("Job %s cancellation requested.", job.status.JobID)
	}
	app.jobScheduler.pruneLocked()
	return cloneOperationJobStatus(job.status), true
}

func (app *App) cancelOperationJobsForShutdown() {
	app.jobScheduler.mu.Lock()
	defer app.jobScheduler.mu.Unlock()
	for _, job := range app.jobScheduler.jobs {
		if job == nil || operationJobComplete(job.status) {
			continue
		}
		job.status.CancelRequested = true
		job.status.Notice = "Job cancelled by shutdown."
		if job.cancel != nil {
			job.cancel()
		}
		if job.status.State == jobStateQueued {
			finishQueuedOperationJobCancellation(&job.status, "Job cancelled by shutdown.")
		}
		app.jobScheduler.bumpJobRevisionLocked(job)
	}
	app.jobScheduler.pruneLocked()
}

func (scheduler *operationJobScheduler) pruneLocked() {
	if len(scheduler.jobs) <= operationJobRecentHistoryLimit {
		return
	}
	retainedJobIDs := map[string]bool{}
	latestCompletedTypeRetained := map[string]bool{}
	for i := scheduler.sequence; i >= 1; i-- {
		jobID := fmt.Sprintf("job-%d", i)
		job := scheduler.jobs[jobID]
		if job == nil {
			continue
		}
		if !operationJobComplete(job.status) {
			retainedJobIDs[jobID] = true
			continue
		}
		if !latestCompletedTypeRetained[job.status.Type] {
			latestCompletedTypeRetained[job.status.Type] = true
			retainedJobIDs[jobID] = true
		}
	}
	recentCompletedRetained := 0
	for i := scheduler.sequence; i >= 1 && recentCompletedRetained < operationJobRecentHistoryLimit; i-- {
		jobID := fmt.Sprintf("job-%d", i)
		job := scheduler.jobs[jobID]
		if job == nil || !operationJobComplete(job.status) {
			continue
		}
		retainedJobIDs[jobID] = true
		recentCompletedRetained++
	}
	prunedAny := false
	for jobID, job := range scheduler.jobs {
		if retainedJobIDs[jobID] {
			continue
		}
		if job != nil && operationJobComplete(job.status) {
			delete(scheduler.jobs, jobID)
			prunedAny = true
		}
	}
	previousQueueLen := len(scheduler.queue)
	scheduler.queue = filterExistingOperationJobQueueIDs(scheduler.queue, scheduler.jobs)
	if prunedAny || len(scheduler.queue) != previousQueueLen {
		scheduler.bumpRevisionLocked()
		sessionLogs.RetainJobRings(retainedJobIDs)
	}
}

func filterExistingOperationJobQueueIDs(queuedJobIDs []string, jobsByID map[string]*OperationJob) []string {
	filteredJobIDs := queuedJobIDs[:0]
	for _, jobID := range queuedJobIDs {
		if jobsByID[jobID] != nil {
			filteredJobIDs = append(filteredJobIDs, jobID)
		}
	}
	return filteredJobIDs
}
