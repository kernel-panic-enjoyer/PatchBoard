package updater

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestOperationJobPanicDoesNotBlockQueue(t *testing.T) {
	app := testSessionApp()
	first := app.startOperationJob(jobTypeScan, "", 1, nil, func(ctx context.Context, job *OperationJob) {
		panic("fixture panic")
	})
	second := app.startOperationJob(jobTypeInventoryRefresh, "", 1, nil, func(ctx context.Context, job *OperationJob) {
		app.mutateOperationJob(job, func(status *OperationJobStatus) {
			status.State = jobStateSucceeded
			status.Notice = "second job ran"
		})
	})

	secondStatus, ok := waitForOperationJobState(app, second.JobID, 2*time.Second)
	if !ok {
		t.Fatal("second job did not finish after first job panic")
	}
	if secondStatus.State != jobStateSucceeded {
		t.Fatalf("expected second job to succeed, got %#v", secondStatus)
	}
	firstStatus, ok := app.operationJobStatus(first.JobID)
	if !ok {
		t.Fatal("first job missing")
	}
	if firstStatus.State != jobStateFailed || firstStatus.Running || firstStatus.FinishedAt == "" {
		t.Fatalf("panic job was not finalized as failed: %#v", firstStatus)
	}
	if !strings.Contains(firstStatus.Error, "internal job panic") || !strings.Contains(firstStatus.Error, "fixture panic") {
		t.Fatalf("panic diagnostic not recorded: %#v", firstStatus)
	}
}

func TestOperationJobRetentionBoundsCompletedHistory(t *testing.T) {
	app := testSessionApp()
	var latest OperationJobStatus
	for i := 0; i < 80; i++ {
		index := i
		latest = app.startOperationJobWithPackageSnapshot(jobTypeUpdate, updateJobModeSelected, 1, []string{fmt.Sprintf("winget:App.%d", index)}, []Package{{
			Key:     fmt.Sprintf("winget:App.%d", index),
			Manager: managerWinget,
			ID:      fmt.Sprintf("App.%d", index),
			Name:    strings.Repeat("large package snapshot ", 100),
		}}, func(ctx context.Context, job *OperationJob) {
			app.mutateOperationJob(job, func(status *OperationJobStatus) {
				status.State = jobStateSucceeded
			})
		})
		if _, ok := waitForOperationJobState(app, latest.JobID, 2*time.Second); !ok {
			t.Fatalf("high-volume job %d did not finish", index)
		}
	}
	statuses := app.operationJobsSnapshot()
	if len(statuses) > operationJobRecentHistoryLimit+1 {
		t.Fatalf("completed job history is not bounded: got %d jobs", len(statuses))
	}
	if statuses[len(statuses)-1].JobID != latest.JobID {
		t.Fatalf("latest job must be retained, got last=%s latest=%s", statuses[len(statuses)-1].JobID, latest.JobID)
	}
}

func TestOperationJobListRevisionIncreasesForNewJobsAfterOlderJobMutations(t *testing.T) {
	app := testSessionApp()
	first := app.startOperationJob(jobTypeScan, "", 1, nil, func(ctx context.Context, job *OperationJob) {
		for i := 0; i < 5; i++ {
			app.mutateOperationJob(job, func(status *OperationJobStatus) {
				status.Notice = fmt.Sprintf("mutation %d", i)
			})
		}
		app.mutateOperationJob(job, func(status *OperationJobStatus) {
			status.State = jobStateSucceeded
		})
	})
	if _, ok := waitForOperationJobState(app, first.JobID, 2*time.Second); !ok {
		t.Fatal("first job did not finish")
	}
	_, beforeRevision, _ := app.operationJobsSnapshotWithRevision()

	second := app.startOperationJob(jobTypeInventoryRefresh, "", 1, nil, func(ctx context.Context, job *OperationJob) {
		app.mutateOperationJob(job, func(status *OperationJobStatus) {
			status.State = jobStateSucceeded
		})
	})
	_, afterRevision, _ := app.operationJobsSnapshotWithRevision()
	if afterRevision <= beforeRevision {
		t.Fatalf("new job did not advance list revision: before=%d after=%d second=%s", beforeRevision, afterRevision, second.JobID)
	}
}

func TestOperationJobRejectsDuplicateSingletonJob(t *testing.T) {
	app := testSessionApp()
	started := make(chan struct{})
	release := make(chan struct{})
	first := app.startOperationJob(jobTypeScan, "", 1, nil, func(ctx context.Context, job *OperationJob) {
		close(started)
		select {
		case <-release:
			app.mutateOperationJob(job, func(status *OperationJobStatus) {
				status.State = jobStateSucceeded
			})
		case <-ctx.Done():
		}
	})
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("first scan job did not start")
	}

	duplicate := app.startOperationJob(jobTypeScan, "", 1, nil, func(ctx context.Context, job *OperationJob) {
		t.Fatal("duplicate scan job should not be enqueued")
	})
	if duplicate.State != jobStateFailed || duplicate.ExistingJobID != first.JobID || !strings.Contains(duplicate.Error, "already queued or running") {
		t.Fatalf("expected duplicate scan rejection, got %#v", duplicate)
	}
	close(release)
	if _, ok := waitForOperationJobState(app, first.JobID, 2*time.Second); !ok {
		t.Fatal("first scan job did not finish")
	}
}

func TestOperationJobRejectsDuplicatePackageMutation(t *testing.T) {
	app := testSessionApp()
	started := make(chan struct{})
	release := make(chan struct{})
	first := app.startOperationJob(jobTypeUpdate, updateJobModeSelected, 1, []string{"winget:Git.Git"}, func(ctx context.Context, job *OperationJob) {
		close(started)
		select {
		case <-release:
			app.mutateOperationJob(job, func(status *OperationJobStatus) {
				status.State = jobStateSucceeded
			})
		case <-ctx.Done():
		}
	})
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("first package job did not start")
	}

	duplicate := app.startOperationJob(jobTypeUpdateAll, updateJobModeSelected, 2, []string{"winget:Git.Git", "choco:gh"}, func(ctx context.Context, job *OperationJob) {
		t.Fatal("duplicate package mutation should not be enqueued")
	})
	if duplicate.State != jobStateFailed || duplicate.ExistingJobID != first.JobID || !strings.Contains(duplicate.Error, "package operation is already queued or running") {
		t.Fatalf("expected duplicate package rejection, got %#v", duplicate)
	}
	close(release)
	if _, ok := waitForOperationJobState(app, first.JobID, 2*time.Second); !ok {
		t.Fatal("first package job did not finish")
	}
}

func TestOperationJobRejectsWhenPendingQueueIsFull(t *testing.T) {
	app := testSessionApp()
	started := make(chan struct{})
	release := make(chan struct{})
	running := app.startOperationJob(jobTypeScan, "", 1, nil, func(ctx context.Context, job *OperationJob) {
		close(started)
		select {
		case <-release:
			app.mutateOperationJob(job, func(status *OperationJobStatus) {
				status.State = jobStateSucceeded
			})
		case <-ctx.Done():
		}
	})
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("running job did not start")
	}

	for i := 0; i < maxPendingOperationJobs; i++ {
		status := app.startOperationJob(jobTypeManagerInstall, "", 1, []string{fmt.Sprintf("manager-%d", i)}, func(ctx context.Context, job *OperationJob) {
			app.mutateOperationJob(job, func(status *OperationJobStatus) {
				status.State = jobStateSucceeded
			})
		})
		if status.State != jobStateQueued {
			t.Fatalf("pending job %d was not queued: %#v", i, status)
		}
	}
	_, _, queueDepth := app.operationJobsSnapshotWithRevision()
	if queueDepth != maxPendingOperationJobs {
		t.Fatalf("expected queue depth %d, got %d", maxPendingOperationJobs, queueDepth)
	}

	overflow := app.startOperationJob(jobTypeManagerInstall, "", 1, []string{"manager-overflow"}, func(ctx context.Context, job *OperationJob) {
		t.Fatal("overflow job should not be enqueued")
	})
	if overflow.State != jobStateFailed || !strings.Contains(overflow.Error, "operation job queue is full") {
		t.Fatalf("expected queue-full rejection, got %#v", overflow)
	}
	close(release)
	if _, ok := waitForOperationJobState(app, running.JobID, 2*time.Second); !ok {
		t.Fatal("running job did not finish")
	}
}

func TestOperationJobRetentionPrunesPerJobLogRings(t *testing.T) {
	oldLogs := sessionLogs
	sessionLogs = &LogBuffer{}
	defer func() { sessionLogs = oldLogs }()

	app := testSessionApp()
	var latest OperationJobStatus
	for i := 0; i < 80; i++ {
		latest = app.startOperationJob(jobTypeUpdate, "", 1, nil, func(ctx context.Context, job *OperationJob) {
			sessionLogs.AppendContext(ctx, "app", "retained only while job status is retained", nil)
			app.mutateOperationJob(job, func(status *OperationJobStatus) {
				status.State = jobStateSucceeded
			})
		})
		if _, ok := waitForOperationJobState(app, latest.JobID, 2*time.Second); !ok {
			t.Fatalf("high-volume log job %d did not finish", i)
		}
	}
	statuses := app.operationJobsSnapshot()
	retainedJobIDs := map[string]bool{}
	for _, status := range statuses {
		retainedJobIDs[status.JobID] = true
	}

	sessionLogs.mu.Lock()
	defer sessionLogs.mu.Unlock()
	for jobID := range sessionLogs.jobRings {
		if !retainedJobIDs[jobID] {
			t.Fatalf("log ring for pruned job %s was retained; retained jobs are %#v", jobID, retainedJobIDs)
		}
	}
}

func TestShutdownCancelsRunningAndQueuedJobs(t *testing.T) {
	app := testSessionApp()
	started := make(chan struct{})
	first := app.startOperationJob(jobTypeScan, "", 1, nil, func(ctx context.Context, job *OperationJob) {
		close(started)
		<-ctx.Done()
		app.mutateOperationJob(job, func(status *OperationJobStatus) {
			status.CancelRequested = true
			status.State = jobStateCancelled
			status.Notice = "cancelled in test"
		})
	})
	second := app.startOperationJob(jobTypeInventoryRefresh, "", 1, nil, func(ctx context.Context, job *OperationJob) {
		app.mutateOperationJob(job, func(status *OperationJobStatus) {
			status.State = jobStateSucceeded
		})
	})

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("first job did not start")
	}
	app.requestShutdown("test")

	firstStatus, ok := app.operationJobStatus(first.JobID)
	if !ok || firstStatus.State != jobStateCancelled || firstStatus.Running {
		t.Fatalf("running job not cancelled by shutdown: ok=%t status=%#v", ok, firstStatus)
	}
	secondStatus, ok := app.operationJobStatus(second.JobID)
	if !ok || secondStatus.State != jobStateCancelled || secondStatus.Running {
		t.Fatalf("queued job not cancelled by shutdown: ok=%t status=%#v", ok, secondStatus)
	}
}

func TestShutdownCancelsStoreScanInProgress(t *testing.T) {
	t.Setenv("UPDATER_STATE_DIR", t.TempDir())
	oldGetter := inventoryGetter
	inventoryGetter = func(ctx context.Context) Inventory { return Inventory{} }
	defer func() { inventoryGetter = oldGetter }()

	oldScan := runStoreTransactionalScanForInventory
	started := make(chan struct{})
	var cancelled int32
	runStoreTransactionalScanForInventory = func(ctx context.Context) (StoreScanResult, error) {
		close(started)
		<-ctx.Done()
		atomic.StoreInt32(&cancelled, 1)
		return StoreScanResult{}, ctx.Err()
	}
	defer func() { runStoreTransactionalScanForInventory = oldScan }()

	app := &App{storeBackgroundScanEnabled: true}
	app.refreshInventorySync("test")
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("background Store scan did not start")
	}
	app.requestShutdown("test")
	if atomic.LoadInt32(&cancelled) != 1 {
		t.Fatal("Store scan did not observe root cancellation")
	}
	if app.inventorySnapshot().StoreLoading {
		t.Fatal("store loading flag remained set after shutdown")
	}
}

func TestShutdownCancelsUpdateJobRefreshWait(t *testing.T) {
	app := testUpdateJobApp(t)
	oldRunner := updatePackageRunner
	oldPreflightRefresh := refreshInventoryBeforeUpdateJob
	oldRefresh := refreshInventoryAfterUpdateJob
	updatePackageRunner = func(ctx context.Context, pkg Package) CommandResult {
		return CommandResult{OK: true, Command: "update " + pkg.ID}
	}
	refreshInventoryBeforeUpdateJob = func(ctx context.Context, app *App, packages []Package) error { return nil }
	refreshStarted := make(chan struct{})
	refreshInventoryAfterUpdateJob = func(ctx context.Context, app *App, packages []Package) error {
		close(refreshStarted)
		<-ctx.Done()
		return ctx.Err()
	}
	defer func() {
		updatePackageRunner = oldRunner
		refreshInventoryBeforeUpdateJob = oldPreflightRefresh
		refreshInventoryAfterUpdateJob = oldRefresh
	}()

	status, err := app.startUpdateJob([]string{"winget:Git.Git"})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-refreshStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("refresh wait did not start")
	}
	app.requestShutdown("test")
	final, ok := app.operationJobStatus(status.JobID)
	if !ok || final.State != jobStateCancelled || final.Running {
		t.Fatalf("update job refresh wait was not cancelled: ok=%t status=%#v", ok, final)
	}
	app.requestShutdown("test again")
}
