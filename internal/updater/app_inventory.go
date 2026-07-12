package updater

import (
	"context"
	"strings"
	"time"
)

const inventoryCacheTTL = 90 * time.Second

func (app *App) refreshInventory(force bool) {
	app.inventoryService.mu.Lock()
	cacheExpired := app.inventoryService.fetchedAt.IsZero() || time.Since(app.inventoryService.fetchedAt) > inventoryCacheTTL
	if app.inventoryService.loading {
		if force {
			app.inventoryService.queued = true
			appLog("Inventory refresh queued.")
		}
		app.inventoryService.mu.Unlock()
		return
	}
	if !force && !cacheExpired {
		app.inventoryService.mu.Unlock()
		return
	}
	app.inventoryService.loading = true
	app.inventoryService.err = ""
	app.inventoryService.refreshGeneration++
	refreshGeneration := app.inventoryService.refreshGeneration
	app.inventoryService.mu.Unlock()
	appLog("Inventory refresh started.")

	if !app.startBackgroundWork("inventory refresh", func(ctx context.Context) {
		app.runInventoryRefresh(ctx, refreshGeneration, force)
	}) {
		app.inventoryService.mu.Lock()
		if refreshGeneration == app.inventoryService.refreshGeneration {
			app.inventoryService.loading = false
			app.inventoryService.err = "shutdown in progress"
		}
		app.inventoryService.mu.Unlock()
	}
}

func (app *App) runInventoryRefresh(ctx context.Context, refreshGeneration int64, force bool) {
	refreshedInventory := inventoryGetter(ctx)
	if ctx.Err() != nil {
		app.inventoryService.mu.Lock()
		if refreshGeneration == app.inventoryService.refreshGeneration {
			app.inventoryService.loading = false
			app.inventoryService.queued = false
			app.inventoryService.err = "inventory refresh cancelled: " + ctx.Err().Error()
		}
		app.inventoryService.mu.Unlock()
		appLog("Inventory refresh cancelled.")
		return
	}
	app.inventoryService.mu.Lock()
	if refreshGeneration != app.inventoryService.refreshGeneration {
		app.inventoryService.mu.Unlock()
		appLog("Discarded stale inventory refresh result.")
		return
	}
	refreshedInventory = preserveExplicitUpdateCandidatesFromDegradedRefresh(app.inventoryService.cache, refreshedInventory)
	app.inventoryService.cache = refreshedInventory.DeepCopy()
	app.inventoryService.fetchedAt = time.Now()
	app.inventoryService.err = ""
	if app.inventoryService.queued {
		queuedRefreshGeneration, shouldStartStoreScan := app.prepareQueuedInventoryRefreshLocked(force)
		app.inventoryService.mu.Unlock()
		appLog("Inventory refresh completed with %d package(s); running queued refresh.", len(refreshedInventory.Packages))
		if shouldStartStoreScan {
			app.startStoreScanBackground()
		}
		app.startQueuedInventoryRefresh(queuedRefreshGeneration)
		return
	}
	app.inventoryService.loading = false
	shouldStartStoreScan := app.beginStoreScanLocked(force)
	app.inventoryService.mu.Unlock()
	appLog("Inventory refresh completed with %d package(s).", len(refreshedInventory.Packages))
	if shouldStartStoreScan {
		app.startStoreScanBackground()
	}
}

func (app *App) refreshInventorySync(reason string) Inventory {
	return app.refreshInventorySyncContext(context.Background(), reason)
}

func (app *App) refreshInventorySyncContext(ctx context.Context, reason string) Inventory {
	if strings.TrimSpace(reason) == "" {
		reason = "synchronous request"
	}
	appLog("Inventory refresh started for %s.", reason)
	app.inventoryService.mu.Lock()
	app.inventoryService.refreshGeneration++
	refreshGeneration := app.inventoryService.refreshGeneration
	app.inventoryService.loading = true
	app.inventoryService.queued = false
	app.inventoryService.err = ""
	app.inventoryService.mu.Unlock()

	refreshedInventory := inventoryGetter(ctx)
	if ctx.Err() != nil {
		app.inventoryService.mu.Lock()
		cachedInventory := app.inventoryService.cache.DeepCopy()
		if refreshGeneration == app.inventoryService.refreshGeneration {
			app.inventoryService.loading = false
			app.inventoryService.queued = false
			app.inventoryService.err = "inventory refresh cancelled: " + ctx.Err().Error()
		}
		app.inventoryService.mu.Unlock()
		appLog("Inventory refresh cancelled for %s.", reason)
		return cachedInventory
	}
	app.inventoryService.mu.Lock()
	if refreshGeneration != app.inventoryService.refreshGeneration {
		authoritativeInventory := app.inventoryService.cache.DeepCopy()
		app.inventoryService.mu.Unlock()
		appLog("Discarded stale synchronous inventory refresh result for %s.", reason)
		return authoritativeInventory
	}
	refreshedInventory = preserveExplicitUpdateCandidatesFromDegradedRefresh(app.inventoryService.cache, refreshedInventory)
	app.inventoryService.cache = refreshedInventory.DeepCopy()
	app.inventoryService.fetchedAt = time.Now()
	app.inventoryService.err = ""
	if app.inventoryService.queued {
		queuedRefreshGeneration, shouldStartStoreScan := app.prepareQueuedInventoryRefreshLocked(true)
		app.inventoryService.mu.Unlock()
		appLog("Inventory refresh completed for %s with %d package(s); running queued refresh.", reason, len(refreshedInventory.Packages))
		if shouldStartStoreScan {
			app.startStoreScanBackground()
		}
		app.startQueuedInventoryRefresh(queuedRefreshGeneration)
		return refreshedInventory
	}
	app.inventoryService.loading = false
	shouldStartStoreScan := app.beginStoreScanLocked(true)
	app.inventoryService.mu.Unlock()
	appLog("Inventory refresh completed for %s with %d package(s).", reason, len(refreshedInventory.Packages))
	if shouldStartStoreScan {
		app.startStoreScanBackground()
	}
	return refreshedInventory
}

func preserveExplicitUpdateCandidatesFromDegradedRefresh(previousInventory, refreshedInventory Inventory) Inventory {
	degradedManagers := degradedInventoryManagers(refreshedInventory.CommandResults)
	if len(degradedManagers) == 0 {
		return refreshedInventory
	}
	previousByKey := map[string]Package{}
	for _, previousPackage := range previousInventory.Packages {
		if !isExplicitUpdateCandidate(previousPackage) {
			continue
		}
		if key := normalizedJobPackageKey(previousPackage); key != "" {
			previousByKey[key] = previousPackage
		}
	}
	if len(previousByKey) == 0 {
		return refreshedInventory
	}
	for packageIndex := range refreshedInventory.Packages {
		refreshedPackage := &refreshedInventory.Packages[packageIndex]
		if refreshedPackage.UpdateAvailable || !degradedManagers[refreshedPackage.Manager] {
			continue
		}
		previousPackage, ok := previousByKey[normalizedJobPackageKey(*refreshedPackage)]
		if !ok || !canPreserveExplicitUpdateCandidate(previousPackage, *refreshedPackage) {
			continue
		}
		refreshedPackage.AvailableVersion = previousPackage.AvailableVersion
		refreshedPackage.UpdateAvailable = true
		refreshedPackage.UnknownVersion = refreshedPackage.UnknownVersion || previousPackage.UnknownVersion
		refreshedPackage.Pinned = refreshedPackage.Pinned || previousPackage.Pinned
		refreshedPackage.UpdateSupported = previousPackage.UpdateSupported
		if refreshedPackage.Name == "" || refreshedPackage.Name == refreshedPackage.ID {
			refreshedPackage.Name = firstNonEmpty(previousPackage.Name, refreshedPackage.Name)
		}
		if refreshedPackage.ActionBackend == "" {
			refreshedPackage.ActionBackend = firstNonEmpty(previousPackage.ActionBackend, refreshedPackage.Manager)
		}
	}
	return refreshedInventory
}

func degradedInventoryManagers(commandResults map[string]CommandResult) map[string]bool {
	degradedManagers := map[string]bool{}
	managerListResults := map[string]string{
		managerWinget: "winget_list",
		managerChoco:  "choco_list",
	}
	for manager, resultKey := range managerListResults {
		result, ok := commandResults[resultKey]
		if ok && result.Command != "" && !result.OK {
			degradedManagers[manager] = true
		}
	}
	return degradedManagers
}

func isExplicitUpdateCandidate(pkg Package) bool {
	return pkg.Manager != managerStore &&
		pkg.UpdateAvailable &&
		pkg.UpdateSupported &&
		(pkg.UnknownVersion || pkg.Pinned)
}

func canPreserveExplicitUpdateCandidate(previousPackage, refreshedPackage Package) bool {
	if !isExplicitUpdateCandidate(previousPackage) || refreshedPackage.Manager != previousPackage.Manager || !refreshedPackage.Installed {
		return false
	}
	if !refreshedPackage.UpdateSupported || refreshedPackage.Manager == managerStore {
		return false
	}
	if refreshedPackage.Version != "" && previousPackage.Version != "" && refreshedPackage.Version != previousPackage.Version {
		return false
	}
	return previousPackage.AvailableVersion != ""
}

// prepareQueuedInventoryRefreshLocked starts the next refresh generation after
// the just-finished generation has published. It must be called with app.inventoryService.mu held.
func (app *App) prepareQueuedInventoryRefreshLocked(forceStoreScan bool) (int64, bool) {
	app.inventoryService.queued = false
	app.inventoryService.loading = true
	app.inventoryService.refreshGeneration++
	queuedRefreshGeneration := app.inventoryService.refreshGeneration
	shouldStartStoreScan := app.beginStoreScanLocked(forceStoreScan)
	return queuedRefreshGeneration, shouldStartStoreScan
}

func (app *App) startQueuedInventoryRefresh(refreshGeneration int64) {
	if app.startBackgroundWork("queued inventory refresh", func(ctx context.Context) {
		app.runInventoryRefresh(ctx, refreshGeneration, true)
	}) {
		return
	}
	app.inventoryService.mu.Lock()
	if refreshGeneration == app.inventoryService.refreshGeneration {
		app.inventoryService.loading = false
		app.inventoryService.err = "shutdown in progress"
	}
	app.inventoryService.mu.Unlock()
}

func (app *App) startStoreScanBackground() {
	if app.startBackgroundWork("Store update scan", app.runStoreScan) {
		return
	}
	app.inventoryService.mu.Lock()
	app.inventoryService.storeScanLoading = false
	app.inventoryService.storeScanQueued = false
	app.inventoryService.mu.Unlock()
}

// storeScanCooldown debounces automatic (non-forced) background Store scans so
// the heavy provider sweep does not re-run on every stale-cache refresh.
// Forced refreshes (startup, jobs, explicit user refresh) bypass the cooldown.
const storeScanCooldown = 5 * time.Minute
const storeScanFailureRetryBackoff = 45 * time.Second

// beginStoreScanLocked decides whether to start a background Store scan and, if
// so, marks one in flight. It must be called with app.inventoryService.mu held and returns true
// when the caller should launch app.runStoreScan in a goroutine.
func (app *App) beginStoreScanLocked(forceStoreScan bool) bool {
	if !app.inventoryService.storeBackgroundScanEnabled {
		return false
	}
	now := time.Now()
	// A non-forced refresh within the cooldown window does not warrant a scan.
	if !forceStoreScan {
		if !app.inventoryService.storeScanLastPublishedAt.IsZero() && now.Sub(app.inventoryService.storeScanLastPublishedAt) < storeScanCooldown {
			return false
		}
		if !app.inventoryService.storeScanLastFailureAt.IsZero() && now.Sub(app.inventoryService.storeScanLastFailureAt) < storeScanFailureRetryBackoff {
			return false
		}
	}
	if app.inventoryService.storeScanLoading {
		// A scan is already running. Queue a follow-up so this request is not
		// lost to an in-flight scan that may have started before the caller's
		// action (for example, re-scanning after applying a Store update).
		if forceStoreScan {
			app.inventoryService.storeScanQueued = true
		}
		return false
	}
	app.inventoryService.storeScanLoading = true
	return true
}

// runStoreScan runs the expensive Microsoft Store transactional scan and
// persists its published snapshot. It does not write back to app.inventoryService.cache:
// inventorySnapshot re-overlays the latest published snapshot on every read, so
// the fresh Store generation surfaces automatically on the next poll.
//
// If a fresh scan is requested while one is in flight (storeScanQueued), this
// loops and runs exactly one follow-up so a forced refresh is never dropped;
// storeScanLoading stays true across the follow-up so the frontend keeps
// polling until the latest requested scan completes.
func (app *App) runStoreScan(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			app.inventoryService.mu.Lock()
			app.inventoryService.storeScanLoading = false
			app.inventoryService.storeScanQueued = false
			app.inventoryService.mu.Unlock()
			appLog("Background Microsoft Store update scan cancelled before start.")
			return
		}
		appLog("Background Microsoft Store update scan started.")
		app.inventoryService.mu.Lock()
		app.inventoryService.storeScanLastAttemptAt = time.Now()
		app.inventoryService.mu.Unlock()
		result, err := runStoreTransactionalScanForInventory(ctx)
		switch {
		case ctx.Err() != nil:
			appLog("Background Store update scan cancelled.")
		case err != nil:
			appLog("Background Store update scan failed: %s", err)
		case !result.Published:
			appLog("Background Store update scan %s completed but was not published.", result.Scan.ScanID)
		default:
			appLog("Background Store update scan %s completed.", result.Scan.ScanID)
		}
		app.inventoryService.mu.Lock()
		if ctx.Err() == nil {
			now := time.Now()
			if err == nil && result.Published {
				app.inventoryService.storeScanLastPublishedAt = now
				app.inventoryService.storeScanLastFailureAt = time.Time{}
			} else {
				app.inventoryService.storeScanLastFailureAt = now
			}
		}
		if ctx.Err() == nil && app.inventoryService.storeScanQueued {
			app.inventoryService.storeScanQueued = false
			app.inventoryService.mu.Unlock()
			continue
		}
		app.inventoryService.storeScanLoading = false
		app.inventoryService.mu.Unlock()
		return
	}
}

func (app *App) waitForStoreScanIdle(ctx context.Context, timeout time.Duration) bool {
	if timeout <= 0 {
		timeout = 12 * time.Minute
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		app.inventoryService.mu.RLock()
		storeScanLoading := app.inventoryService.storeScanLoading
		app.inventoryService.mu.RUnlock()
		if !storeScanLoading {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-timer.C:
			return false
		case <-ticker.C:
		}
	}
}

type cachedInventorySnapshot struct {
	inventory    Inventory
	loading      bool
	fetchedAt    time.Time
	errText      string
	storeLoading bool
}

func (app *App) cacheInventorySnapshot() cachedInventorySnapshot {
	app.inventoryService.mu.RLock()
	snapshot := cachedInventorySnapshot{
		inventory:    app.inventoryService.cache.DeepCopy(),
		loading:      app.inventoryService.loading,
		fetchedAt:    app.inventoryService.fetchedAt,
		errText:      app.inventoryService.err,
		storeLoading: app.inventoryService.storeScanLoading,
	}
	app.inventoryService.mu.RUnlock()
	return snapshot
}

func (app *App) effectiveInventorySnapshot(ctx context.Context) (Inventory, error) {
	snapshot, err := app.effectiveCachedInventorySnapshot(ctx)
	return snapshot.inventory, err
}

func (app *App) effectiveCachedInventorySnapshot(ctx context.Context) (cachedInventorySnapshot, error) {
	snapshot := app.cacheInventorySnapshot()
	persistedState := loadStateContext(ctx)
	if snapshot.fetchedAt.IsZero() {
		snapshot.inventory.Scan = inventoryScanSummary(persistedState, managedScanSourceCounts(persistedState))
	}
	snapshot.inventory = effectiveInventoryFromBase(ctx, persistedState, snapshot.inventory)
	return snapshot, ctx.Err()
}

func effectiveInventoryFromBase(ctx context.Context, state State, baseInventory Inventory) Inventory {
	effectiveInventory := applyStateAndCapabilitiesToInventory(state, baseInventory.DeepCopy())
	return applyPublishedStoreScanAssessments(ctx, state, effectiveInventory)
}

func applyStateAndCapabilitiesToInventory(state State, inventory Inventory) Inventory {
	for packageIndex := range inventory.Packages {
		inventory.Packages[packageIndex].AutoUpdate = packageAutoUpdateEnabled(state, inventory.Packages[packageIndex])
		inventory.Packages[packageIndex] = applyPackageCapabilities(inventory.Packages[packageIndex])
	}
	return inventory
}

func (app *App) inventorySnapshot() InventoryResponse {
	return app.inventorySnapshotContext(context.Background())
}

func (app *App) inventorySnapshotContext(ctx context.Context) InventoryResponse {
	snapshot, _ := app.effectiveCachedInventorySnapshot(ctx)

	response := InventoryResponse{
		Inventory:     snapshot.inventory,
		AsyncSnapshot: asyncSnapshot(snapshot.loading, snapshot.fetchedAt, snapshot.errText),
		StoreLoading:  snapshot.storeLoading,
	}
	if response.Managers == nil {
		response.Managers = map[string]ManagerStatus{}
	}
	if response.CommandResults == nil {
		response.CommandResults = map[string]CommandResult{}
	}
	return response
}
