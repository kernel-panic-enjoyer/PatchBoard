package updater

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestInventoryAPIReadDoesNotMutateCachedBaseInventory(t *testing.T) {
	pfn := "OpenAI.Codex_abc123"
	app := effectiveInventoryTestApp(t, pfn, freshStoreAssessment)
	original := app.inventory.DeepCopy()

	response := requestPackages(t, app)
	got := findStorePackageByPFN(t, response.Packages, pfn)
	if !got.UpdateAvailable || got.StoreProductID == "" || len(got.ProviderSummaries) == 0 {
		t.Fatalf("expected API projection to include fresh Store overlay, got %#v", got)
	}

	app.mu.RLock()
	cached := app.inventory.DeepCopy()
	app.mu.RUnlock()
	if !reflect.DeepEqual(cached, original) {
		t.Fatalf("API projection mutated cached base inventory:\noriginal=%#v\ncached=%#v", original, cached)
	}
}

func TestEffectiveInventorySelectionUsesPublishedStoreOverlayWithoutAPIPoll(t *testing.T) {
	pfn := "OpenAI.Codex_abc123"
	app := effectiveInventoryTestApp(t, pfn, freshStoreAssessment)

	selected, mode, err := app.updateJobPackages([]string{packageKey(managerStore, pfn)}, UpdateOptions{})
	if err != nil {
		t.Fatalf("selected Store update should use published overlay without API polling: %v", err)
	}
	if mode != updateJobModeSelected || len(selected) != 1 || !selected[0].UpdateAvailable || selected[0].StoreProductID == "" {
		t.Fatalf("selected Store update did not include effective overlay: mode=%q packages=%#v", mode, selected)
	}

	bulk, mode, err := app.updateJobPackages(nil, UpdateOptions{})
	if err != nil {
		t.Fatalf("bulk Store update should use published overlay without API polling: %v", err)
	}
	if mode != updateJobModeAll || len(bulk) != 1 || !bulk[0].UpdateAvailable || bulk[0].StoreProductID == "" {
		t.Fatalf("bulk Store update did not include effective overlay: mode=%q packages=%#v", mode, bulk)
	}

	app.mu.RLock()
	cached := app.inventory.DeepCopy()
	app.mu.RUnlock()
	if cached.Packages[0].UpdateAvailable || cached.Packages[0].UpdateState != "" {
		t.Fatalf("update selection mutated cached base inventory: %#v", cached.Packages[0])
	}
}

func TestPackageForUpdateMatchesInventorySnapshotStoreOverlay(t *testing.T) {
	pfn := "OpenAI.Codex_abc123"
	app := effectiveInventoryTestApp(t, pfn, freshStoreAssessment)

	snapshotPackage := findStorePackageByPFN(t, app.inventorySnapshot().Packages, pfn)
	selectedPackage := app.packageForUpdate(managerStore, pfn)

	if !selectedPackage.UpdateAvailable || selectedPackage.StoreProductID == "" {
		t.Fatalf("packageForUpdate did not see Store overlay: %#v", selectedPackage)
	}
	for _, field := range []struct {
		name  string
		left  string
		right string
	}{
		{"key", selectedPackage.Key, snapshotPackage.Key},
		{"state", selectedPackage.UpdateState, snapshotPackage.UpdateState},
		{"product", selectedPackage.StoreProductID, snapshotPackage.StoreProductID},
		{"update", selectedPackage.StoreUpdateID, snapshotPackage.StoreUpdateID},
		{"offered", selectedPackage.OfferedVersion, snapshotPackage.OfferedVersion},
		{"scan", selectedPackage.ScanID, snapshotPackage.ScanID},
	} {
		if field.left != field.right {
			t.Fatalf("packageForUpdate %s=%q, inventorySnapshot=%q; selected=%#v snapshot=%#v", field.name, field.left, field.right, selectedPackage, snapshotPackage)
		}
	}
}

func TestConcurrentEffectiveInventorySnapshotsDoNotMutateCache(t *testing.T) {
	pfn := "OpenAI.Codex_abc123"
	app := effectiveInventoryTestApp(t, pfn, freshStoreAssessment)
	original := app.inventory.DeepCopy()

	var wg sync.WaitGroup
	errs := make(chan string, 80)
	for worker := 0; worker < 8; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for iteration := 0; iteration < 10; iteration++ {
				response := app.inventorySnapshot()
				packages := response.Packages
				pkg, ok := packageByPFN(packages, pfn)
				if !ok || !pkg.UpdateAvailable || pkg.StoreProductID == "" {
					errs <- fmt.Sprintf("snapshot missing Store overlay: ok=%t pkg=%#v health=%#v packages=%#v", ok, pkg, response.StoreScanHealth, packages)
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for errText := range errs {
		t.Fatal(errText)
	}

	app.mu.RLock()
	cached := app.inventory.DeepCopy()
	app.mu.RUnlock()
	if !reflect.DeepEqual(cached, original) {
		t.Fatalf("concurrent snapshots mutated cached base inventory:\noriginal=%#v\ncached=%#v", original, cached)
	}
}

func TestInventorySnapshotConcurrentWithUpdateSelection(t *testing.T) {
	pfn := "OpenAI.Codex_abc123"
	app := effectiveInventoryTestApp(t, pfn, freshStoreAssessment)

	var wg sync.WaitGroup
	errs := make(chan string, 80)
	for worker := 0; worker < 4; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for iteration := 0; iteration < 10; iteration++ {
				packages := app.inventorySnapshot().Packages
				pkg, ok := packageByPFN(packages, pfn)
				if !ok || !pkg.UpdateAvailable {
					errs <- fmt.Sprintf("snapshot Store package was not actionable: ok=%t pkg=%#v packages=%#v", ok, pkg, packages)
				}
			}
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			for iteration := 0; iteration < 10; iteration++ {
				packages, _, err := app.updateJobPackages([]string{packageKey(managerStore, pfn)}, UpdateOptions{})
				if err != nil || len(packages) != 1 || !packages[0].UpdateAvailable {
					errs <- "updateJobPackages did not select Store overlay"
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for errText := range errs {
		t.Fatal(errText)
	}
}

func TestEffectiveInventoryDoesNotAliasCacheMapsOrProviderSummaries(t *testing.T) {
	t.Setenv("UPDATER_STATE_DIR", t.TempDir())
	restoreSID := replaceStoreScanSID("S-1-5-21-no-snapshot")
	defer restoreSID()

	app := testSessionApp()
	app.inventory = Inventory{
		PackageLookup: PackageLookup{
			Packages: []Package{{
				Key:       "winget:Git.Git",
				Manager:   managerWinget,
				ID:        "Git.Git",
				Name:      "Git",
				Installed: true,
				ProviderSummaries: []StorePackageProviderSummary{{
					Name:   "base-provider",
					Health: string(StoreProviderHealthy),
					Kind:   "base",
				}},
			}},
			Managers: map[string]ManagerStatus{
				managerWinget: {Available: true, Version: "1.0.0"},
			},
			CommandResults: map[string]CommandResult{
				"winget_list": {OK: true, Command: "winget list"},
			},
		},
		StoreScanHealth: StoreScanHealthSummary{
			Counts:    map[string]int{string(StoreUpdateUnknown): 1},
			Providers: []StorePackageProviderSummary{{Name: "health-provider", Health: string(StoreProviderHealthy), Kind: "base"}},
		},
	}
	original := app.inventory.DeepCopy()

	got, err := app.effectiveInventorySnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got.Packages[0].ProviderSummaries[0].Name = "mutated-package-provider"
	got.Managers[managerWinget] = ManagerStatus{Available: false, Error: "mutated"}
	got.CommandResults["winget_list"] = CommandResult{Command: "mutated"}
	cacheCopy := app.cacheInventorySnapshot().inventory
	cacheCopy.StoreScanHealth.Counts[string(StoreUpdateUnknown)] = 99
	cacheCopy.StoreScanHealth.Providers[0].Name = "mutated-health-provider"

	app.mu.RLock()
	cached := app.inventory.DeepCopy()
	app.mu.RUnlock()
	if !reflect.DeepEqual(cached, original) {
		t.Fatalf("effective inventory aliased cached mutable fields:\noriginal=%#v\ncached=%#v", original, cached)
	}
}

func TestStaleStoreAssessmentIsNonActionableEverywhere(t *testing.T) {
	pfn := "OpenAI.Codex_abc123"
	app := effectiveInventoryTestApp(t, pfn, staleStoreAssessment)
	assertStoreAssessmentNonActionableEverywhere(t, app, pfn)
}

func TestOldStoreAssessmentIsNonActionableEverywhere(t *testing.T) {
	pfn := "OpenAI.Codex_abc123"
	app := effectiveInventoryTestApp(t, pfn, oldStoreAssessment)
	assertStoreAssessmentNonActionableEverywhere(t, app, pfn)
}

func TestRecoveredStoreAssessmentIsNonActionableEverywhere(t *testing.T) {
	pfn := "OpenAI.Codex_abc123"
	app := effectiveInventoryTestApp(t, pfn, recoveredStoreAssessment)
	assertStoreAssessmentNonActionableEverywhere(t, app, pfn)
}

func TestInstalledVersionMismatchStoreAssessmentIsNonActionableEverywhere(t *testing.T) {
	pfn := "OpenAI.Codex_abc123"
	app := effectiveInventoryTestApp(t, pfn, versionMismatchStoreAssessment)
	assertStoreAssessmentNonActionableEverywhere(t, app, pfn)
}

func assertStoreAssessmentNonActionableEverywhere(t *testing.T, app *App, pfn string) {
	t.Helper()
	apiPackage := findStorePackageByPFN(t, app.inventorySnapshot().Packages, pfn)
	if apiPackage.UpdateAvailable || apiPackage.ExactActionTargetAvailable || packageAllowedInBulkUpdate(apiPackage, UpdateOptions{}) || packageHasFreshStoreAvailableAssessment(apiPackage) {
		t.Fatalf("stale API package should be non-actionable: %#v", apiPackage)
	}

	if packages, _, err := app.updateJobPackages(nil, UpdateOptions{}); err == nil || len(packages) != 0 {
		t.Fatalf("bulk stale Store package should not be selectable: packages=%#v err=%v", packages, err)
	}
	if packages, _, err := app.updateJobPackages([]string{packageKey(managerStore, pfn)}, UpdateOptions{}); err == nil || len(packages) != 0 {
		t.Fatalf("selected stale Store package should be rejected: packages=%#v err=%v", packages, err)
	}
	selectedPackage := app.packageForUpdate(managerStore, pfn)
	if selectedPackage.UpdateAvailable || packageHasFreshStoreAvailableAssessment(selectedPackage) {
		t.Fatalf("packageForUpdate should expose stale evidence as non-actionable: %#v", selectedPackage)
	}
}

func TestFreshExactVP9FixtureIsActionableEverywhere(t *testing.T) {
	pfn := "Microsoft.VP9VideoExtensions_8wekyb3d8bbwe"
	app := effectiveInventoryTestApp(t, pfn, freshStoreAssessment)

	apiPackage := findStorePackageByPFN(t, app.inventorySnapshot().Packages, pfn)
	if !storePackageActionable(apiPackage) {
		t.Fatalf("VP9 API projection should be actionable: %#v", apiPackage)
	}
	selectedPackage := app.packageForUpdate(managerStore, pfn)
	if !storePackageActionable(selectedPackage) {
		t.Fatalf("VP9 packageForUpdate should be actionable: %#v", selectedPackage)
	}
	selected, _, err := app.updateJobPackages([]string{packageKey(managerStore, pfn)}, UpdateOptions{})
	if err != nil || len(selected) != 1 || !storePackageActionable(selected[0]) {
		t.Fatalf("VP9 selected update should be actionable: packages=%#v err=%v", selected, err)
	}
	bulk, _, err := app.updateJobPackages(nil, UpdateOptions{})
	if err != nil || len(bulk) != 1 || !storePackageActionable(bulk[0]) {
		t.Fatalf("VP9 bulk update should be actionable: packages=%#v err=%v", bulk, err)
	}
}

type storeAssessmentFixtureKind int

const (
	freshStoreAssessment storeAssessmentFixtureKind = iota
	staleStoreAssessment
	oldStoreAssessment
	recoveredStoreAssessment
	versionMismatchStoreAssessment
)

func effectiveInventoryTestApp(t *testing.T, pfn string, kind storeAssessmentFixtureKind) *App {
	t.Helper()
	t.Setenv("UPDATER_STATE_DIR", t.TempDir())
	userSID := "S-1-5-21-effective-inventory"
	restoreSID := replaceStoreScanSID(userSID)
	t.Cleanup(restoreSID)
	store, err := openDefaultStoreScanRepository()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	now := time.Date(2026, 6, 24, 12, 10, 0, 0, time.UTC)
	restoreNow := replaceStoreScanNow(now)
	t.Cleanup(restoreNow)
	persistStoreAssessmentFixture(t, store, userSID, pfn, now, kind)

	basePackage := baseStoreInventoryPackage(pfn)
	if kind == versionMismatchStoreAssessment {
		basePackage.Version = "2.0.0"
		basePackage.Match = pfn + "_2.0.0_x64__abc123"
	}

	app := testSessionApp()
	app.inventory = Inventory{
		PackageLookup: PackageLookup{
			Packages: []Package{basePackage},
			Managers: map[string]ManagerStatus{
				managerStore: {Available: true, Version: "fixture", InventoryAvailable: true, InventoryBackend: inventoryBackendAppX, ActionBackend: backendStoreCLI},
			},
			CommandResults: map[string]CommandResult{
				"native_store_inventory": {OK: true, Command: "fixture native inventory"},
			},
		},
		Scan: InventoryScanSummary{StoreCount: 1, TrackedCount: 1},
	}
	app.inventoryFetchedAt = time.Now()
	return app
}

func baseStoreInventoryPackage(pfn string) Package {
	return Package{
		Key:                        packageKey(managerStore, pfn),
		Manager:                    managerStore,
		ID:                         pfn,
		Name:                       "Store Fixture",
		Version:                    "1.0.0",
		Installed:                  true,
		Source:                     sourceNativeAppX,
		Match:                      pfn + "_1.0.0_x64__abc123",
		ActionBackend:              backendAppXInventory,
		UpdateSupported:            false,
		InstalledPackageFamilyName: pfn,
		ExactIdentityAvailable:     true,
		ProviderSummaries: []StorePackageProviderSummary{{
			Name:   "base-native-inventory",
			Health: string(StoreProviderHealthy),
			Kind:   "base",
		}},
	}
}

func persistStoreAssessmentFixture(t *testing.T, store StoreScanRepository, userSID, pfn string, now time.Time, kind storeAssessmentFixtureKind) {
	t.Helper()
	started := now.Add(-2 * time.Second)
	if kind == oldStoreAssessment {
		started = now.Add(-storeAssessmentFreshnessWindow - time.Minute)
	}
	completed := started.Add(time.Second)
	scan := StoreScanGeneration{
		ScanID:           "scan-" + strings.ToLower(strings.ReplaceAll(pfn, ".", "-")),
		UserSID:          userSID,
		StartedAt:        started,
		CompletedAt:      completed,
		CompletionStatus: StoreScanCompleted,
	}
	identity := StoreInstalledIdentity{UserSID: userSID, PackageFamilyName: pfn}
	provider := StoreProviderIdentity{ID: "fixture-provider", Name: "Fixture Provider", Backend: "fixture"}
	target := &ExactStoreUpdateTarget{
		Identity:   identity,
		Provider:   provider,
		ProductID:  "9NVP9FIXTURE",
		UpdateID:   pfn,
		Verified:   true,
		VerifiedBy: provider.Key(),
		VerifiedAt: completed,
	}
	assessment := StorePublishedAssessment{
		StoreUpdateAssessment: StoreUpdateAssessment{
			State:            StoreUpdateAvailable,
			Identity:         identity,
			ScanID:           scan.ScanID,
			Reason:           "fresh exact fixture",
			InstalledVersion: "1.0.0",
			AvailableVersion: "1.1.0",
			Target:           target,
			Evidence: []StoreEvidenceSummary{{
				Provider: provider.Key(),
				Health:   StoreProviderHealthy,
				Kind:     StoreObservationPositiveUpdateOffer,
			}},
		},
		ObservedAt:                 completed,
		StoreProductID:             target.ProductID,
		UpdateID:                   target.UpdateID,
		ExactActionTargetAvailable: true,
		Applicability:              "applicable",
	}
	if kind == staleStoreAssessment {
		assessment.Stale = true
		assessment.Reason = "retained stale positive fixture"
	}
	snapshot := StoreScanSnapshot{
		SchemaVersion: storeScanSchemaVersion,
		Published:     true,
		Scan:          scan,
		Inventory:     testStoreInventory(scan, pfn, "1.0.0"),
		Assessments:   []StorePublishedAssessment{assessment},
	}
	if kind == recoveredStoreAssessment {
		snapshot.RecoveredFromFallback = true
	}
	if published, err := store.PersistCompletedScanSnapshot(context.Background(), snapshot); err != nil || !published {
		t.Fatalf("persist fixture published=%t err=%v", published, err)
	}
}

func storePackageActionable(pkg Package) bool {
	return pkg.UpdateAvailable &&
		pkg.UpdateSupported &&
		packageHasExactStoreUpdateTarget(pkg) &&
		packageHasFreshStoreAvailableAssessment(pkg) &&
		pkg.StoreProductID != "" &&
		pkg.StoreUpdateID != ""
}

func packageByPFN(packages []Package, pfn string) (Package, bool) {
	for _, pkg := range packages {
		if pkg.Manager == managerStore && strings.EqualFold(pkg.InstalledPackageFamilyName, pfn) {
			return pkg, true
		}
	}
	return Package{}, false
}
