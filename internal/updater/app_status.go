package updater

import (
	"context"
	"sync"
	"time"
)

const statusCacheTTL = 30 * time.Second

func (app *App) refreshStatus(forceRefresh bool) {
	request := app.statusCache.beginBackgroundRefresh(forceRefresh, time.Now())
	if request.queued {
		appLog("Status refresh queued.")
		return
	}
	if !request.started {
		return
	}
	appLog("Status refresh started.")

	if !app.startBackgroundWork("status refresh", func(ctx context.Context) {
		app.runStatusRefresh(ctx, forceRefresh)
	}) {
		app.statusCache.failToStart("shutdown in progress")
	}
}

func (app *App) runStatusRefresh(ctx context.Context, forceRefresh bool) {
	refreshedStatus := app.buildStatusResponseContext(ctx, forceRefresh)
	if ctx.Err() != nil {
		app.statusCache.cancelRefresh("status refresh cancelled: " + ctx.Err().Error())
		appLog("Status refresh cancelled.")
		return
	}
	if app.statusCache.finishRefresh(refreshedStatus, time.Now()) {
		app.startQueuedStatusRefresh()
		return
	}
	appLog("Status refresh completed.")
}

func (app *App) startQueuedStatusRefresh() {
	appLog("Status refresh completed; running queued refresh.")
	if !app.startBackgroundWork("queued status refresh", func(ctx context.Context) {
		app.runStatusRefresh(ctx, true)
	}) {
		app.statusCache.failToStart("shutdown in progress")
	}
}

func (app *App) buildStatusResponseContext(ctx context.Context, forceRefresh bool) StatusResponse {
	persistedState := loadStateContext(ctx)
	return buildStatusResponseContextWithStateAndUpdate(ctx, forceRefresh, persistedState, app.appUpdateStatusForStatus(ctx, persistedState))
}

func buildStatusResponseContextWithStateAndUpdate(ctx context.Context, forceRefresh bool, persistedState State, appUpdateStatus AppUpdateStatus) StatusResponse {
	stateDirectory, _ := stateDir()
	var startupTaskEnabled bool
	var autoTaskEnabled bool
	var autoTaskSupported bool
	var autoTaskUnsupportedReason string
	var taskChecks sync.WaitGroup
	taskChecks.Add(3)
	go func() {
		defer taskChecks.Done()
		startupTaskEnabled = startupTaskEnabledContext(ctx)
	}()
	go func() {
		defer taskChecks.Done()
		autoTaskEnabled = autoUpdateTaskEnabledContext(ctx)
	}()
	go func() {
		defer taskChecks.Done()
		autoTaskSupported, autoTaskUnsupportedReason = autoUpdateTaskSupportStatus()
	}()
	var managerStatuses map[string]ManagerStatus
	if forceRefresh {
		managerStatuses = detectManagersFreshContext(ctx)
	} else {
		managerStatuses = detectManagersContext(ctx)
	}
	taskChecks.Wait()

	return StatusResponse{
		Admin:                     isAdmin(),
		StateDir:                  stateDirectory,
		Managers:                  managerStatuses,
		StartupEnabled:            startupTaskEnabled,
		AutoTaskEnabled:           autoTaskEnabled,
		AutoTaskSupported:         autoTaskSupported,
		AutoTaskUnsupportedReason: autoTaskUnsupportedReason,
		Settings:                  statusSettingsFromState(persistedState),
		AppUpdate:                 appUpdateStatus,
		Application:               currentApplicationInfo(),
		ApplicationInstall:        currentApplicationInstallStatus(),
	}
}

func (app *App) refreshStatusSyncContext(ctx context.Context, reason string) StatusResponse {
	appLog("Status refresh started for %s.", reason)
	app.statusCache.beginSynchronousRefresh()

	refreshedStatus := app.buildStatusResponseContext(ctx, true)
	if ctx.Err() != nil {
		cachedStatus := app.statusCache.cancelRefresh("status refresh cancelled: " + ctx.Err().Error())
		appLog("Status refresh cancelled for %s.", reason)
		return cachedStatus
	}

	if app.statusCache.finishRefresh(refreshedStatus, time.Now()) {
		app.startQueuedStatusRefresh()
		return refreshedStatus
	}
	appLog("Status refresh completed for %s.", reason)
	return refreshedStatus
}

func (app *App) statusSnapshot() StatusResponse {
	return app.statusSnapshotContext(context.Background())
}

func (app *App) statusSnapshotContext(ctx context.Context) StatusResponse {
	snapshot, statusLoading, fetchedAt, refreshErr := app.statusCache.snapshot()
	app.inventoryService.mu.RLock()
	inventoryManagerStatuses := cloneManagerStatuses(app.inventoryService.cache.Managers)
	app.inventoryService.mu.RUnlock()

	persistedState := loadStateContext(ctx)
	snapshot.Settings = statusSettingsFromState(persistedState)
	if snapshot.StateDir == "" {
		snapshot.StateDir, _ = stateDir()
		snapshot.Admin = isAdmin()
	}
	if snapshot.Managers == nil {
		snapshot.Managers = map[string]ManagerStatus{}
	} else {
		snapshot.Managers = cloneManagerStatuses(snapshot.Managers)
	}
	if fetchedAt.IsZero() && snapshot.AutoTaskUnsupportedReason == "" {
		snapshot.AutoTaskSupported, snapshot.AutoTaskUnsupportedReason = autoUpdateTaskSupportStatus()
	}
	snapshot.AppUpdate = app.appUpdateStatusForStatus(ctx, persistedState)
	if snapshot.Application.License == "" || snapshot.Application.Repository == "" {
		snapshot.Application = currentApplicationInfo()
	}
	snapshot.ApplicationInstall = currentApplicationInstallStatus()
	mergeStatusInventoryManagerDetails(&snapshot, inventoryManagerStatuses)
	snapshot.AsyncSnapshot = asyncSnapshot(statusLoading, fetchedAt, refreshErr)
	return snapshot
}

func (app *App) appUpdateStatusForStatus(ctx context.Context, persistedState State) AppUpdateStatus {
	if persistedState.AppUpdateChecksDisabled {
		return AppUpdateStatus{CurrentVersion: currentAppVersion()}
	}
	return app.appUpdateStatusContext(ctx, false)
}

func (app *App) appUpdateStatusContext(ctx context.Context, forceRefresh bool) AppUpdateStatus {
	currentVersion := currentAppVersion()
	if app == nil || app.appUpdateChecker == nil {
		return AppUpdateStatus{CurrentVersion: currentVersion}
	}
	checkCtx, cancel := context.WithTimeout(ctx, appUpdateCheckTimeout)
	defer cancel()
	updateStatus, err := app.appUpdateChecker.Check(checkCtx, currentVersion)
	checkedAt := time.Now()
	updateStatus.CurrentVersion = currentVersion
	updateStatus.CheckedAt = checkedAt.UTC().Truncate(time.Second).Format(time.RFC3339)
	if err != nil {
		updateStatus.Error = sanitizeProviderDiagnostic(err.Error())
	}
	return updateStatus
}

func statusSettingsFromState(persistedState State) StatusSettings {
	return StatusSettings{
		AutoUpdateGlobal:                persistedState.AutoUpdateGlobal,
		AutoUpdatePackages:              trimBoolMap(persistedState.AutoUpdatePackages, maxStateAutoUpdatePackages),
		Theme:                           persistedState.Theme,
		LastScanAt:                      persistedState.LastScanAt,
		LastAutoUpdateAt:                persistedState.LastAutoUpdateAt,
		LastAutoUpdateResults:           trimUpdateResultSummaries(persistedState.LastAutoUpdateResults),
		LastAutoUpdateSummary:           persistedState.LastAutoUpdateSummary,
		AppUpdatePromptDismissedVersion: persistedState.AppUpdatePromptDismissedVersion,
		AppUpdateAutoInstallEnabled:     persistedState.AppUpdateAutoInstallEnabled,
		AppUpdateCheckingEnabled:        !persistedState.AppUpdateChecksDisabled,
		RemoveNewDesktopShortcuts:       persistedState.RemoveNewDesktopShortcuts,
	}
}

func mergeStatusInventoryManagerDetails(statusResponse *StatusResponse, inventoryManagerStatuses map[string]ManagerStatus) {
	inventoryStoreStatus, ok := inventoryManagerStatuses[managerStore]
	if !ok || !inventoryStoreStatus.InventoryAvailable {
		return
	}
	storeStatus := statusResponse.Managers[managerStore]
	if storeStatus == (ManagerStatus{}) {
		storeStatus = inventoryStoreStatus
	} else {
		storeStatus.InventoryAvailable = true
		storeStatus.InventoryBackend = inventoryStoreStatus.InventoryBackend
		if storeStatus.ActionBackend == "" {
			storeStatus.ActionBackend = inventoryStoreStatus.ActionBackend
		}
	}
	statusResponse.Managers[managerStore] = storeStatus
}
