package updater

import (
	"context"
	"net/http"
	"sync"
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

// App lock order:
// Prefer releasing one App lock before taking another. If a future change must
// hold multiple locks at once, acquire them in this order to avoid deadlocks:
// lifecycle.mu -> mu -> jobsMu. Lifecycle cleanup locking is private to the
// lifecycle service and is never held while application callbacks run.
type App struct {
	token         string
	sessionToken  string
	listenHost    string
	listenPort    int
	bootstrapUsed bool
	server        *http.Server
	mu            sync.RWMutex
	// inventory is the immutable manager/native cache. Published Store
	// assessments are overlaid only onto deep-copied effective snapshots.
	inventory          Inventory
	inventoryLoading   bool
	inventoryQueued    bool
	inventoryRefreshID int64
	inventoryFetchedAt time.Time
	inventoryErr       string
	// Microsoft Store update scan runs in the background so it never blocks the
	// fast managers. storeScanLoading reports an in-flight background scan;
	// scan timestamps are split so successful publications use the normal
	// cooldown while failed/unpublished scans use a shorter retry backoff.
	// storeBackgroundScanEnabled is set only on the production App so unit tests
	// (which stub inventoryGetter) never spawn real Store scans.
	storeScanLoading           bool
	storeScanQueued            bool
	storeScanLastAttemptAt     time.Time
	storeScanLastPublishedAt   time.Time
	storeScanLastFailureAt     time.Time
	storeBackgroundScanEnabled bool
	status                     StatusResponse
	statusLoading              bool
	statusQueued               bool
	statusFetchedAt            time.Time
	statusErr                  string
	appUpdateChecker           appUpdateChecker
	jobsMu                     sync.Mutex
	jobs                       map[string]*OperationJob
	jobsRevision               int64
	jobSeq                     int64
	jobQueue                   []string
	jobActive                  bool
	lifecycle                  appLifecycle
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
