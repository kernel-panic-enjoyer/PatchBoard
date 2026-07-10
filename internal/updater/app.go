package updater

import (
	"context"
	"net/http"
	"time"
)

const gracefulShutdownTimeout = 5 * time.Second

type InventoryResponse struct {
	Inventory
	AsyncSnapshot
	// StoreLoading is true while the (slow) Microsoft Store update scan is
	// still running in the background after the fast managers (winget, choco)
	// have already been returned. The frontend keeps polling and shows a
	// per-Store loading indicator while this is true.
	StoreLoading bool `json:"store_loading,omitempty"`
}

type StatusResponse struct {
	Admin                     bool                     `json:"admin"`
	StateDir                  string                   `json:"state_dir"`
	Managers                  map[string]ManagerStatus `json:"managers"`
	StartupEnabled            bool                     `json:"startup_enabled"`
	AutoTaskEnabled           bool                     `json:"auto_task_enabled"`
	AutoTaskSupported         bool                     `json:"auto_task_supported"`
	AutoTaskUnsupportedReason string                   `json:"auto_task_unsupported_reason,omitempty"`
	Settings                  StatusSettings           `json:"settings"`
	AppUpdate                 AppUpdateStatus          `json:"app_update"`
	Application               ApplicationInfo          `json:"application"`
	ApplicationInstall        ApplicationInstallStatus `json:"application_install"`
	AsyncSnapshot
}

type StatusSettings struct {
	AutoUpdateGlobal                bool                        `json:"auto_update_global"`
	AutoUpdatePackages              map[string]bool             `json:"auto_update_packages,omitempty"`
	Theme                           string                      `json:"theme"`
	LastScanAt                      string                      `json:"last_scan_at,omitempty"`
	LastAutoUpdateAt                string                      `json:"last_auto_update_at,omitempty"`
	LastAutoUpdateResults           []UpdateResultSummary       `json:"last_auto_update_results,omitempty"`
	LastAutoUpdateSummary           *ScheduledAutoUpdateSummary `json:"last_auto_update_summary,omitempty"`
	AppUpdatePromptDismissedVersion string                      `json:"app_update_prompt_dismissed_version,omitempty"`
	AppUpdateAutoInstallEnabled     bool                        `json:"app_update_auto_install_enabled"`
	AppUpdateCheckingEnabled        bool                        `json:"app_update_checking_enabled"`
	RemoveNewDesktopShortcuts       bool                        `json:"remove_new_desktop_shortcuts"`
}

type AsyncSnapshot struct {
	Loading   bool   `json:"loading"`
	UpdatedAt string `json:"updated_at,omitempty"`
	Error     string `json:"error,omitempty"`
}

func asyncSnapshot(loading bool, updatedAt time.Time, errorText string) AsyncSnapshot {
	snapshot := AsyncSnapshot{Loading: loading, Error: errorText}
	if !updatedAt.IsZero() {
		snapshot.UpdatedAt = updatedAt.UTC().Truncate(time.Second).Format(time.RFC3339)
	}
	return snapshot
}

// App service lock order:
// Prefer releasing one service lock before taking another. If a future change
// must hold multiple locks at once, acquire them in this order to avoid
// deadlocks: lifecycle.mu -> inventoryService.mu -> statusCache.mu ->
// jobScheduler.mu. webSession's bootstrap lock is never held with another
// service lock, and lifecycle cleanup locks are private to appLifecycle.
type App struct {
	webSession       webSession
	server           *http.Server
	inventoryService inventoryService
	statusCache      appStatusCache
	appUpdateChecker appUpdateChecker
	jobScheduler     operationJobScheduler
	lifecycle        appLifecycle
}

func (app *App) isShuttingDown() bool {
	return app.lifecycle.isShuttingDown()
}

func (app *App) startBackgroundWork(workName string, runWork func(context.Context)) bool {
	return app.lifecycle.startBackgroundWork(workName, runWork)
}

func (app *App) rootContext() context.Context {
	return app.lifecycle.rootContext()
}

func (app *App) beginShutdown() {
	app.lifecycle.beginShutdown()
	app.cancelOperationJobsForShutdown()
}

func (app *App) waitForBackgroundWork(timeout time.Duration) bool {
	return app.lifecycle.waitForBackgroundWork(timeout)
}

func (app *App) addShutdownCleanup(cleanup func()) {
	app.lifecycle.addShutdownCleanup(cleanup)
}

func (app *App) runShutdownCleanups() {
	app.lifecycle.runShutdownCleanups()
}

func (app *App) requestShutdown(requestSource string) {
	app.lifecycle.runShutdownOnce(func() {
		appLog("%s quit requested.", requestSource)
		app.beginShutdown()
		if !app.waitForBackgroundWork(gracefulShutdownTimeout) {
			appLog("Shutdown timed out waiting for background work.")
		}
		app.runShutdownCleanups()
		if app.server == nil {
			return
		}
		shutdownCtx, cancel := context.WithTimeout(context.Background(), gracefulShutdownTimeout)
		defer cancel()
		if shutdownErr := app.server.Shutdown(shutdownCtx); shutdownErr != nil {
			appLog("Graceful shutdown failed: %s; forcing server close.", shutdownErr)
			_ = app.server.Close()
		}
	})
}
