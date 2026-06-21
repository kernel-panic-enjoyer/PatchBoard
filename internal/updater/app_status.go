package updater

import (
	"sync"
	"time"
)

const statusCacheTTL = 30 * time.Second

func (app *App) refreshStatus(force bool) {
	app.mu.Lock()
	stale := app.statusFetchedAt.IsZero() || time.Since(app.statusFetchedAt) > statusCacheTTL
	if app.statusLoading {
		if force {
			app.statusQueued = true
			appLog("Status refresh queued.")
		}
		app.mu.Unlock()
		return
	}
	if !force && !stale {
		app.mu.Unlock()
		return
	}
	app.statusLoading = true
	app.statusErr = ""
	app.mu.Unlock()
	appLog("Status refresh started.")

	go app.runStatusRefresh(force)
}

func (app *App) runStatusRefresh(force bool) {
	status := buildStatusResponse(force)
	app.mu.Lock()
	app.status = status
	app.statusFetchedAt = time.Now()
	app.statusErr = ""
	if app.statusQueued {
		app.statusQueued = false
		app.statusLoading = true
		app.mu.Unlock()
		appLog("Status refresh completed; running queued refresh.")
		go app.runStatusRefresh(true)
		return
	}
	app.statusLoading = false
	app.mu.Unlock()
	appLog("Status refresh completed.")
}

func buildStatusResponse(force bool) StatusResponse {
	state := loadState()
	dir, _ := stateDir()
	var startupEnabled bool
	var autoTaskEnabled bool
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		startupEnabled = taskExists(taskStartup)
	}()
	go func() {
		defer wg.Done()
		autoTaskEnabled = taskExists(taskAutoUpdate)
	}()
	var managers map[string]ManagerStatus
	if force {
		managers = detectManagersFresh()
	} else {
		managers = detectManagers()
	}
	wg.Wait()

	return StatusResponse{
		Admin:           isAdmin(),
		StateDir:        dir,
		Managers:        managers,
		StartupEnabled:  startupEnabled,
		AutoTaskEnabled: autoTaskEnabled,
		Settings:        state,
	}
}

func (app *App) refreshStatusSync(reason string) StatusResponse {
	appLog("Status refresh started for %s.", reason)
	app.mu.Lock()
	app.statusLoading = true
	app.statusQueued = false
	app.statusErr = ""
	app.mu.Unlock()

	status := buildStatusResponse(true)

	app.mu.Lock()
	app.status = status
	app.statusFetchedAt = time.Now()
	app.statusErr = ""
	if app.statusQueued {
		app.statusQueued = false
		app.statusLoading = true
		app.mu.Unlock()
		appLog("Status refresh completed; running queued refresh.")
		go app.runStatusRefresh(true)
		return status
	}
	app.statusLoading = false
	app.mu.Unlock()
	appLog("Status refresh completed for %s.", reason)
	return status
}

func (app *App) statusSnapshot() StatusResponse {
	app.mu.RLock()
	status := app.status
	loading := app.statusLoading
	fetchedAt := app.statusFetchedAt
	errText := app.statusErr
	inventoryManagers := cloneManagerStatuses(app.inventory.Managers)
	app.mu.RUnlock()

	if status.StateDir == "" {
		status.Settings = loadState()
		status.StateDir, _ = stateDir()
		status.Admin = isAdmin()
	}
	if status.Managers == nil {
		status.Managers = map[string]ManagerStatus{}
	} else {
		status.Managers = cloneManagerStatuses(status.Managers)
	}
	mergeStatusInventoryManagerDetails(&status, inventoryManagers)
	status.AsyncSnapshot = asyncSnapshot(loading, fetchedAt, errText)
	return status
}

func mergeStatusInventoryManagerDetails(status *StatusResponse, inventoryManagers map[string]ManagerStatus) {
	inventoryStore, ok := inventoryManagers[managerStore]
	if !ok || !inventoryStore.InventoryAvailable {
		return
	}
	store := status.Managers[managerStore]
	if store == (ManagerStatus{}) {
		store = inventoryStore
	} else {
		store.InventoryAvailable = true
		store.InventoryBackend = inventoryStore.InventoryBackend
		if store.ActionBackend == "" {
			store.ActionBackend = inventoryStore.ActionBackend
		}
	}
	status.Managers[managerStore] = store
}
