package updater

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestInventoryResponseFlattensInventoryJSON(t *testing.T) {
	response := InventoryResponse{
		Inventory: Inventory{
			PackageLookup: PackageLookup{
				Packages: []Package{{Name: "Git", ID: "Git.Git", Manager: managerWinget}},
				Managers: map[string]ManagerStatus{
					managerWinget: {Available: true},
				},
				CommandResults: map[string]CommandResult{
					"winget_list": {OK: true},
				},
			},
			Scan: InventoryScanSummary{TrackedCount: 1},
		},
		AsyncSnapshot: AsyncSnapshot{Loading: true},
	}

	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &payload); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"packages", "managers", "command_results", "scan", "loading"} {
		if _, ok := payload[key]; !ok {
			t.Fatalf("missing flattened inventory response key %q in %s", key, encoded)
		}
	}
	if _, ok := payload["Inventory"]; ok {
		t.Fatalf("embedded Inventory should not be encoded as a nested field: %s", encoded)
	}
	if _, ok := payload["PackageLookup"]; ok {
		t.Fatalf("embedded PackageLookup should not be encoded as a nested field: %s", encoded)
	}
	if _, ok := payload["AsyncSnapshot"]; ok {
		t.Fatalf("embedded AsyncSnapshot should not be encoded as a nested field: %s", encoded)
	}
}

func TestStatusSnapshotPreservesStoreInventoryManagerDetails(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("UPDATER_STATE_DIR", dir)
	app := &App{
		status: StatusResponse{
			Managers: map[string]ManagerStatus{
				managerStore: {Available: true, ActionBackend: backendStoreCLI},
			},
		},
		statusFetchedAt: time.Now(),
		inventory: Inventory{
			PackageLookup: PackageLookup{
				Managers: map[string]ManagerStatus{
					managerStore: {
						Available:          true,
						ActionBackend:      backendStoreCLI,
						InventoryAvailable: true,
						InventoryBackend:   inventoryBackendAppX,
					},
				},
			},
		},
	}

	snapshot := app.statusSnapshot()
	store := snapshot.Managers[managerStore]
	if !store.InventoryAvailable || store.InventoryBackend != inventoryBackendAppX {
		t.Fatalf("expected status snapshot to keep Store inventory details, got %#v", store)
	}
	if app.status.Managers[managerStore].InventoryAvailable {
		t.Fatal("status snapshot should not mutate cached status managers in place")
	}
}

func TestRefreshStatusQueuesForcedRefreshWhileLoading(t *testing.T) {
	app := &App{statusLoading: true}

	app.refreshStatus(false)
	if app.statusQueued {
		t.Fatal("non-forced status refresh should not queue while loading")
	}

	app.refreshStatus(true)
	if !app.statusQueued {
		t.Fatal("forced status refresh should queue while loading")
	}
	if !app.statusLoading {
		t.Fatal("status should remain loading after queueing forced refresh")
	}
}

func TestStatusSettingsExposeAppUpdatePromptDismissedVersion(t *testing.T) {
	state := defaultState()
	state.AppUpdatePromptDismissedVersion = "1.2.3"

	settings := statusSettingsFromState(state)
	if settings.AppUpdatePromptDismissedVersion != "1.2.3" {
		t.Fatalf("expected dismissed app update version in status settings, got %#v", settings)
	}
}

func TestStatusSnapshotReloadsPersistedSettings(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("UPDATER_STATE_DIR", dir)
	state := defaultState()
	state.AppUpdatePromptDismissedVersion = "1.2.3"
	if err := saveState(state); err != nil {
		t.Fatal(err)
	}
	app := &App{
		status: StatusResponse{
			StateDir: dir,
			Settings: StatusSettings{
				Theme: "dark",
			},
		},
		statusFetchedAt: time.Now(),
	}

	status := app.statusSnapshot()
	if status.Settings.AppUpdatePromptDismissedVersion != "1.2.3" {
		t.Fatalf("expected status snapshot to reload persisted settings, got %#v", status.Settings)
	}
}

func TestStatusSnapshotIncludesApplicationLicenseAndRepository(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("UPDATER_STATE_DIR", dir)

	status := (&App{}).statusSnapshot()
	if status.Application.License != appLicenseID {
		t.Fatalf("expected application license %q, got %#v", appLicenseID, status.Application)
	}
	if status.Application.Repository != appRepositoryURL {
		t.Fatalf("expected application repository %q, got %#v", appRepositoryURL, status.Application)
	}
}

func TestStatusSnapshotExplainsUnsupportedDailyAutoUpdateLocation(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("UPDATER_STATE_DIR", dir)

	status := (&App{}).statusSnapshot()
	if !status.AutoTaskSupported && status.AutoTaskUnsupportedReason == "" {
		t.Fatalf("unsupported daily auto-update status should include a reason: %#v", status)
	}
}

type countingAppUpdateChecker struct {
	calls    int
	statuses []AppUpdateStatus
}

func (checker *countingAppUpdateChecker) Check(context.Context, string) (AppUpdateStatus, error) {
	checker.calls++
	if len(checker.statuses) >= checker.calls {
		return checker.statuses[checker.calls-1], nil
	}
	return AppUpdateStatus{LatestVersion: "0.0.1"}, nil
}

func TestAppUpdateStatusIsNotCached(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("UPDATER_STATE_DIR", dir)
	checker := &countingAppUpdateChecker{statuses: []AppUpdateStatus{
		{LatestVersion: "0.0.1"},
		{LatestVersion: "0.0.2"},
	}}
	app := &App{appUpdateChecker: checker}

	first := app.appUpdateStatusContext(context.Background(), false)
	second := app.appUpdateStatusContext(context.Background(), false)

	if checker.calls != 2 {
		t.Fatalf("expected every app update status call to reach checker, got %d", checker.calls)
	}
	if first.LatestVersion != "0.0.1" || second.LatestVersion != "0.0.2" {
		t.Fatalf("expected uncached app update statuses, first=%#v second=%#v", first, second)
	}
}

func TestStatusSnapshotDoesNotReuseCachedAppUpdatePayload(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("UPDATER_STATE_DIR", dir)
	checker := &countingAppUpdateChecker{statuses: []AppUpdateStatus{
		{LatestVersion: "0.0.1"},
		{LatestVersion: "0.0.2"},
	}}
	app := &App{
		appUpdateChecker: checker,
		status: StatusResponse{
			AppUpdate: AppUpdateStatus{CurrentVersion: "cached", LatestVersion: "cached"},
		},
		statusFetchedAt: time.Now(),
	}

	first := app.statusSnapshot()
	second := app.statusSnapshot()

	if checker.calls != 2 {
		t.Fatalf("expected every status snapshot to refresh app update status, got %d", checker.calls)
	}
	if first.AppUpdate.LatestVersion != "0.0.1" || second.AppUpdate.LatestVersion != "0.0.2" {
		t.Fatalf("expected status snapshots to bypass cached app update payload, first=%#v second=%#v", first.AppUpdate, second.AppUpdate)
	}
}

func TestStatusSnapshotSkipsAutomaticAppUpdateCheckWhenDisabled(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("UPDATER_STATE_DIR", dir)
	state := defaultState()
	state.AppUpdateChecksDisabled = true
	if err := saveState(state); err != nil {
		t.Fatal(err)
	}
	checker := &countingAppUpdateChecker{statuses: []AppUpdateStatus{{LatestVersion: "9.9.9", Available: true}}}
	app := &App{appUpdateChecker: checker}

	status := app.statusSnapshot()

	if checker.calls != 0 {
		t.Fatalf("disabled automatic checks should not call app update checker, got %d call(s)", checker.calls)
	}
	if status.AppUpdate.LatestVersion != "" || status.AppUpdate.Available {
		t.Fatalf("disabled automatic checks should expose only the current version, got %#v", status.AppUpdate)
	}
	if status.Settings.AppUpdateCheckingEnabled {
		t.Fatalf("settings should expose disabled automatic checks as app_update_checking_enabled=false: %#v", status.Settings)
	}

	manual := app.appUpdateStatusContext(context.Background(), true)
	if checker.calls != 1 || manual.LatestVersion != "9.9.9" {
		t.Fatalf("manual check should still call checker once, calls=%d status=%#v", checker.calls, manual)
	}
}

func TestStatusSettingsExposeApplicationPreferencesDefaults(t *testing.T) {
	settings := statusSettingsFromState(defaultState())
	if settings.AppUpdateAutoInstallEnabled {
		t.Fatal("automatic application self update should default to disabled")
	}
	if !settings.AppUpdateCheckingEnabled {
		t.Fatal("application update checks should default to enabled")
	}
	if settings.RemoveNewDesktopShortcuts {
		t.Fatal("desktop shortcut cleanup should default to disabled")
	}
}
