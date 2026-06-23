package updater

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type storeScanRepositoryTestCase struct {
	name string
	open func(t *testing.T) StoreScanRepository
}

func storeScanRepositoryConformanceCases() []storeScanRepositoryTestCase {
	return []storeScanRepositoryTestCase{
		{name: "files", open: func(t *testing.T) StoreScanRepository {
			repo, err := openStoreScanFileRepository(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			return repo
		}},
	}
}

func TestStoreScanRepositoryConformanceRoundTripAndGoldenProjections(t *testing.T) {
	for _, tc := range storeScanRepositoryConformanceCases() {
		t.Run(tc.name, func(t *testing.T) {
			store := tc.open(t)
			defer store.Close()
			assertStoreScanRepositoryRoundTripAndGoldenProjections(t, store)
		})
	}
}

func assertStoreScanRepositoryRoundTripAndGoldenProjections(t *testing.T, store StoreScanRepository) StoreScanSnapshot {
	t.Helper()
	userSID := "S-1-5-21-repository"
	pfn := "OpenAI.Codex_abc123"
	snapshot := testStoreScanSnapshot(userSID, pfn, "repository-round-trip", time.Date(2026, 6, 23, 10, 0, 0, 0, time.UTC), StoreUpdateAvailable)

	published, err := store.PersistCompletedScanSnapshot(context.Background(), snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if !published {
		t.Fatal("snapshot was not published")
	}

	loaded, ok, err := store.LoadLatestPublishedSnapshot(context.Background(), userSID)
	if err != nil || !ok {
		t.Fatalf("LoadLatestPublishedSnapshot ok=%t err=%v", ok, err)
	}
	if loaded.SchemaVersion != storeScanSchemaVersion || !loaded.Published {
		t.Fatalf("loaded snapshot metadata mismatch: %#v", loaded)
	}
	if loaded.Scan.ScanID != snapshot.Scan.ScanID || loaded.Scan.UserSID != userSID || loaded.Scan.CompletionStatus != StoreScanCompleted {
		t.Fatalf("loaded scan mismatch: %#v", loaded.Scan)
	}
	if len(loaded.Inventory.Families) != 1 || loaded.Inventory.Families[0].Identity.PackageFamilyName != pfn || loaded.Inventory.Families[0].DisplayName != "Codex" {
		t.Fatalf("loaded inventory mismatch: %#v", loaded.Inventory.Families)
	}
	if len(loaded.ProviderRuns) != 1 || loaded.ProviderRuns[0].Version != "v1.2.3" || len(loaded.ProviderRuns[0].Observations) != 1 || len(loaded.ProviderRuns[0].Mappings) != 1 {
		t.Fatalf("loaded provider run mismatch: %#v", loaded.ProviderRuns)
	}
	if loaded.ProviderRuns[0].Observations[0].Target == nil || loaded.ProviderRuns[0].Observations[0].Target.ProductID != "9NTESTPRODUCT" {
		t.Fatalf("loaded observation lost exact target: %#v", loaded.ProviderRuns[0].Observations[0])
	}
	if len(loaded.Assessments) != 1 || loaded.Assessments[0].State != StoreUpdateAvailable || loaded.Assessments[0].StoreProductID != "9NTESTPRODUCT" {
		t.Fatalf("loaded assessment mismatch: %#v", loaded.Assessments)
	}
	if previous := previousAssessmentsFromSnapshot(loaded); previous[loaded.Assessments[0].Identity].State != StoreUpdateAvailable {
		t.Fatalf("previous positive availability was not available from snapshot: %#v", previous)
	}

	providers := providerSummariesFromRuns(loaded.ProviderRuns)
	if len(providers) != 1 || providers[0].Name != "Repository Provider" || providers[0].Version != "v1.2.3" || providers[0].Health != string(StoreProviderHealthy) {
		t.Fatalf("provider summary mismatch: %#v", providers)
	}

	apiInventory := applyPublishedStoreAssessmentsToInventory(defaultState(), Inventory{
		PackageLookup: PackageLookup{Packages: []Package{transactionalStoreAPIPackage(pfn)}},
	}, loaded.Assessments, map[string]StorePackagedAppFamily{strings.ToLower(pfn): loaded.Inventory.Families[0]}, providers)
	if len(apiInventory.Packages) != 1 {
		t.Fatalf("API projection package count=%d", len(apiInventory.Packages))
	}
	apiPackage := apiInventory.Packages[0]
	if apiPackage.UpdateState != string(StoreUpdateAvailable) || !apiPackage.UpdateAvailable || apiPackage.StoreProductID != "9NTESTPRODUCT" || apiPackage.ScanID != loaded.Scan.ScanID {
		t.Fatalf("API projection mismatch: %#v", apiPackage)
	}

	export := StoreDiagnosticsExport{GeneratedAt: "2026-06-23T10:00:02Z", SchemaVersion: storeScanSchemaVersion, DetectorMode: "new"}
	applyStoreDiagnosticsSnapshot(&export, loaded)
	exportJSON, err := json.Marshal(export)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(exportJSON), userSID) {
		t.Fatalf("diagnostic projection leaked raw user SID: %s", string(exportJSON))
	}
	if export.Scan.ScanID != loaded.Scan.ScanID || len(export.Packages) != 1 || len(export.Providers) != 1 || len(export.Observations) != 1 || len(export.Assessments) != 1 {
		t.Fatalf("diagnostic projection mismatch: %#v", export)
	}
	if export.Observations[0].ProductID != "9NTESTPRODUCT" || !export.Observations[0].TargetVerified || export.Assessments[0].ProductID != "9NTESTPRODUCT" {
		t.Fatalf("diagnostic exact target projection mismatch: %#v %#v", export.Observations, export.Assessments)
	}
	return loaded
}

func TestStoreScanRepositoryConformancePublicationIsolationAndRejection(t *testing.T) {
	for _, tc := range storeScanRepositoryConformanceCases() {
		t.Run(tc.name, func(t *testing.T) {
			store := tc.open(t)
			defer store.Close()
			assertStoreScanRepositoryPublicationIsolationAndRejection(t, store)
		})
	}
}

func assertStoreScanRepositoryPublicationIsolationAndRejection(t *testing.T, store StoreScanRepository) {
	t.Helper()
	userSID := "S-1-5-21-repository-order"
	pfn := "OpenAI.Codex_abc123"
	base := time.Date(2026, 6, 23, 11, 0, 0, 0, time.UTC)

	newer := testStoreScanSnapshot(userSID, pfn, "repository-newer", base.Add(time.Second), StoreUpdateCurrent)
	older := testStoreScanSnapshot(userSID, pfn, "repository-older", base, StoreUpdateAvailable)
	if published, err := store.PersistCompletedScanSnapshot(context.Background(), newer); err != nil || !published {
		t.Fatalf("newer publish=%t err=%v", published, err)
	}
	if published, err := store.PersistCompletedScanSnapshot(context.Background(), older); err != nil || published {
		t.Fatalf("older publish=%t err=%v", published, err)
	}
	latest, ok, err := store.LoadLatestPublishedSnapshot(context.Background(), userSID)
	if err != nil || !ok || latest.Scan.ScanID != newer.Scan.ScanID || latest.Assessments[0].State != StoreUpdateCurrent {
		t.Fatalf("older scan replaced newer published snapshot: latest=%#v ok=%t err=%v", latest, ok, err)
	}
	previous, ok, err := store.LoadPreviousSnapshot(context.Background(), userSID, newer.Scan)
	if err != nil || !ok || previous.Scan.ScanID != older.Scan.ScanID {
		t.Fatalf("previous snapshot mismatch: previous=%#v ok=%t err=%v", previous, ok, err)
	}

	otherUser := testStoreScanSnapshot("S-1-5-21-other-repository", pfn, "repository-other-user", base.Add(2*time.Second), StoreUpdateAvailable)
	if published, err := store.PersistCompletedScanSnapshot(context.Background(), otherUser); err != nil || !published {
		t.Fatalf("other user publish=%t err=%v", published, err)
	}
	if isolated, ok, err := store.LoadLatestPublishedSnapshot(context.Background(), userSID); err != nil || !ok || isolated.Scan.ScanID != newer.Scan.ScanID {
		t.Fatalf("cross-user isolation failed: snapshot=%#v ok=%t err=%v", isolated, ok, err)
	}

	bad := testStoreScanSnapshot(userSID, pfn, "repository-bad", base.Add(3*time.Second), StoreUpdateAvailable)
	bad.ProviderRuns[0].Observations[0].ScanID = "different-generation"
	if _, err := store.PersistCompletedScanSnapshot(context.Background(), bad); err == nil {
		t.Fatal("expected cross-generation observation rejection")
	}
}

func TestStoreScanRepositoryConformanceRetention(t *testing.T) {
	for _, tc := range storeScanRepositoryConformanceCases() {
		t.Run(tc.name, func(t *testing.T) {
			store := tc.open(t)
			defer store.Close()
			assertStoreScanRepositoryRetention(t, store)
		})
	}
}

func assertStoreScanRepositoryRetention(t *testing.T, store StoreScanRepository) {
	t.Helper()
	userSID := "S-1-5-21-repository-retention"
	pfn := "OpenAI.Codex_abc123"
	base := time.Date(2026, 6, 23, 12, 0, 0, 0, time.UTC)
	for index := 0; index < storeScanRetentionRunsUser+5; index++ {
		snapshot := testStoreScanSnapshot(userSID, pfn, "repository-retention-"+fmtTimeForID(base.Add(time.Duration(index)*time.Second)), base.Add(time.Duration(index)*time.Second), StoreUpdateCurrent)
		if _, err := store.PersistCompletedScanSnapshot(context.Background(), snapshot); err != nil {
			t.Fatal(err)
		}
	}
	previous, ok, err := store.LoadPreviousSnapshot(context.Background(), userSID, StoreScanGeneration{StartedAt: base.Add(time.Duration(storeScanRetentionRunsUser+5) * time.Second)})
	if err != nil || !ok {
		t.Fatalf("expected retained previous snapshot, ok=%t err=%v", ok, err)
	}
	if previous.Scan.StartedAt.Before(base.Add(5 * time.Second)) {
		t.Fatalf("retention kept a pruned snapshot: %#v", previous.Scan)
	}
}

func TestStoreScanFileRepositoryFailureInjection(t *testing.T) {
	root := t.TempDir()
	repo, err := openStoreScanFileRepository(root)
	if err != nil {
		t.Fatal(err)
	}
	repo.retention = 3
	userSID := "S-1-5-21-file-failure"
	pfn := "OpenAI.Codex_abc123"
	base := time.Date(2026, 6, 23, 13, 0, 0, 0, time.UTC)
	if snapshot, ok, err := repo.LoadLatestPublishedSnapshot(context.Background(), userSID); err != nil || ok {
		t.Fatalf("missing directory should load no snapshot, got ok=%t err=%v snapshot=%#v", ok, err, snapshot)
	}
	userDir := repo.userDir(userSID)
	if err := os.MkdirAll(userDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(userDir, ".tmp-store-scan-residue.json"), []byte(`{"partial":`), 0o600); err != nil {
		t.Fatal(err)
	}

	first := testStoreScanSnapshot(userSID, pfn, "file-first", base, StoreUpdateAvailable)
	if published, err := repo.PersistCompletedScanSnapshot(context.Background(), first); err != nil || !published {
		t.Fatalf("first publish=%t err=%v", published, err)
	}
	reopened, err := openStoreScanFileRepository(root)
	if err != nil {
		t.Fatal(err)
	}
	if loaded, ok, err := reopened.LoadLatestPublishedSnapshot(context.Background(), userSID); err != nil || !ok || loaded.Scan.ScanID != first.Scan.ScanID {
		t.Fatalf("process restart load failed: ok=%t err=%v snapshot=%#v", ok, err, loaded)
	}

	newerCorrupt := filepath.Join(userDir, "20990101T000000.000000000Z-corrupt.json")
	if err := os.WriteFile(newerCorrupt, []byte(`{"schema_version":`), 0o600); err != nil {
		t.Fatal(err)
	}
	if loaded, ok, err := reopened.LoadLatestPublishedSnapshot(context.Background(), userSID); err != nil || !ok || loaded.Scan.ScanID != first.Scan.ScanID {
		t.Fatalf("corrupt newest generation should be skipped: ok=%t err=%v snapshot=%#v", ok, err, loaded)
	}
	if len(reopened.Diagnostics()) == 0 {
		t.Fatal("corrupt snapshot should be reported through diagnostics")
	}

	future := testStoreScanSnapshot(userSID, pfn, "file-future-schema", base.Add(time.Second), StoreUpdateAvailable)
	future.SchemaVersion = storeScanSchemaVersion + 99
	writeRawStoreSnapshotFile(t, reopened, future, mustMarshalJSON(t, future))
	if loaded, ok, err := reopened.LoadLatestPublishedSnapshot(context.Background(), userSID); err != nil || !ok || loaded.Scan.ScanID != first.Scan.ScanID {
		t.Fatalf("future schema should be skipped: ok=%t err=%v snapshot=%#v", ok, err, loaded)
	}

	wrongUser := testStoreScanSnapshot("S-1-5-21-other-file", pfn, "file-wrong-user", base.Add(2*time.Second), StoreUpdateAvailable)
	writeRawStoreSnapshotPath(t, filepath.Join(userDir, snapshotFileName(wrongUser)), mustMarshalJSON(t, wrongUser))
	if loaded, ok, err := reopened.LoadLatestPublishedSnapshot(context.Background(), userSID); err != nil || !ok || loaded.Scan.ScanID != first.Scan.ScanID {
		t.Fatalf("wrong-user snapshot should be skipped: ok=%t err=%v snapshot=%#v", ok, err, loaded)
	}

	duplicate := testStoreScanSnapshot(userSID, pfn, first.Scan.ScanID, base.Add(3*time.Second), StoreUpdateCurrent)
	if _, err := reopened.PersistCompletedScanSnapshot(context.Background(), duplicate); err == nil {
		t.Fatal("duplicate scan ID should be rejected")
	}
	newer := testStoreScanSnapshot(userSID, pfn, "file-newer", base.Add(10*time.Second), StoreUpdateCurrent)
	if published, err := reopened.PersistCompletedScanSnapshot(context.Background(), newer); err != nil || !published {
		t.Fatalf("newer publish=%t err=%v", published, err)
	}
	older := testStoreScanSnapshot(userSID, pfn, "file-older-late", base.Add(5*time.Second), StoreUpdateAvailable)
	if published, err := reopened.PersistCompletedScanSnapshot(context.Background(), older); err != nil || published {
		t.Fatalf("late older publish=%t err=%v", published, err)
	}
	if latest, ok, err := reopened.LoadLatestPublishedSnapshot(context.Background(), userSID); err != nil || !ok || latest.Scan.ScanID != newer.Scan.ScanID {
		t.Fatalf("older scan replaced newer: ok=%t err=%v latest=%#v", ok, err, latest)
	}

	for index := 0; index < 6; index++ {
		snapshot := testStoreScanSnapshot(userSID, pfn, "file-retention-"+fmtTimeForID(base.Add(time.Duration(20+index)*time.Second)), base.Add(time.Duration(20+index)*time.Second), StoreUpdateCurrent)
		if _, err := reopened.PersistCompletedScanSnapshot(context.Background(), snapshot); err != nil {
			t.Fatal(err)
		}
	}
	if count := countJSONSnapshots(t, userDir); count > reopened.retentionLimit() {
		t.Fatalf("retention kept %d snapshots, want at most %d", count, reopened.retentionLimit())
	}

	malformed := testStoreScanSnapshot(userSID, pfn, "file-malformed-nested", base.Add(2*time.Minute), StoreUpdateAvailable)
	malformed.ProviderRuns[0].Observations[0].Identity.UserSID = "S-1-5-21-wrong-nested"
	writeRawStoreSnapshotFile(t, reopened, malformed, mustMarshalJSON(t, malformed))
	if loaded, ok, err := reopened.LoadLatestPublishedSnapshot(context.Background(), userSID); err != nil || !ok || loaded.Scan.ScanID == malformed.Scan.ScanID {
		t.Fatalf("malformed nested evidence should not load: ok=%t err=%v snapshot=%#v", ok, err, loaded)
	}
}

func TestStoreScanFileRepositoryOversizedSnapshot(t *testing.T) {
	repo, err := openStoreScanFileRepository(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	repo.maxBytes = 100
	snapshot := testStoreScanSnapshot("S-1-5-21-file-oversized", "OpenAI.Codex_abc123", "file-oversized", time.Date(2026, 6, 23, 14, 0, 0, 0, time.UTC), StoreUpdateAvailable)
	if _, err := repo.PersistCompletedScanSnapshot(context.Background(), snapshot); err == nil {
		t.Fatal("oversized snapshot should be rejected")
	}
}

func TestStoreScanFileRepositoryMigratesSchemaOneSnapshot(t *testing.T) {
	repo, err := openStoreScanFileRepository(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	userSID := "S-1-5-21-file-schema-one"
	snapshot := testStoreScanSnapshot(userSID, "OpenAI.Codex_abc123", "file-schema-one", time.Date(2026, 6, 23, 13, 30, 0, 0, time.UTC), StoreUpdateAvailable)
	snapshot.SchemaVersion = 1
	writeRawStoreSnapshotFile(t, repo, snapshot, mustMarshalJSON(t, snapshot))
	loaded, ok, err := repo.LoadLatestPublishedSnapshot(context.Background(), userSID)
	if err != nil || !ok {
		t.Fatalf("schema one snapshot did not load: ok=%t err=%v", ok, err)
	}
	if loaded.SchemaVersion != storeScanSchemaVersion || loaded.Scan.ScanID != snapshot.Scan.ScanID {
		t.Fatalf("schema one snapshot was not migrated: %#v", loaded)
	}
}

func TestDefaultStoreScanRepositoryUsesFileSnapshotsAndRetiresLegacySQLiteCache(t *testing.T) {
	state := t.TempDir()
	t.Setenv("UPDATER_STATE_DIR", state)
	legacyPath := filepath.Join(state, legacyStoreScanSQLiteFileName)
	if err := os.WriteFile(legacyPath, []byte("legacy cached scan data"), 0o600); err != nil {
		t.Fatal(err)
	}
	repo, err := openDefaultStoreScanRepository()
	if err != nil {
		t.Fatal(err)
	}
	userSID := "S-1-5-21-file-default"
	snapshot := testStoreScanSnapshot(userSID, "OpenAI.Codex_abc123", "file-default", time.Date(2026, 6, 23, 15, 30, 0, 0, time.UTC), StoreUpdateAvailable)
	if published, err := repo.PersistCompletedScanSnapshot(context.Background(), snapshot); err != nil || !published {
		t.Fatalf("default dual write publish=%t err=%v", published, err)
	}
	if err := repo.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(legacyPath); !os.IsNotExist(err) {
		t.Fatalf("legacy SQLite cache was not retired: %v", err)
	}
	matches, err := filepath.Glob(legacyPath + ".legacy-cache.*")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected one retired legacy cache file, got %#v", matches)
	}
	files, err := openStoreScanFileRepository(filepath.Join(state, storeScanSnapshotDirName))
	if err != nil {
		t.Fatal(err)
	}
	loaded, ok, err := files.LoadLatestPublishedSnapshot(context.Background(), userSID)
	if err != nil || !ok || loaded.Scan.ScanID != snapshot.Scan.ScanID {
		t.Fatalf("default file snapshot missing: ok=%t err=%v snapshot=%#v", ok, err, loaded)
	}
}

func TestStoreScanPipelineUsesFileRepositoryForHysteresis(t *testing.T) {
	repo, err := openStoreScanFileRepository(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	userSID := "S-1-5-21-file-pipeline"
	pfn := "OpenAI.Codex_abc123"
	restore := replaceStoreScanSID(userSID)
	defer restore()
	firstPipeline := newTestStoreScanPipeline(repo, userSID, pfn, positiveProvider(pfn, "1.0.0", "1.1.0"))
	firstPipeline.NewScanID = func(time.Time) string { return "file-pipeline-first" }
	first, err := firstPipeline.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if first.Assessments[0].State != StoreUpdateAvailable || first.Assessments[0].Stale {
		t.Fatalf("initial file-backed pipeline positive assessment=%#v", first.Assessments[0])
	}
	secondPipeline := newTestStoreScanPipeline(repo, userSID, pfn, failingProvider("catalog timeout"))
	secondPipeline.Now = fixedPipelineTimes(time.Date(2026, 6, 21, 12, 1, 0, 0, time.UTC), time.Date(2026, 6, 21, 12, 1, 1, 0, time.UTC), time.Date(2026, 6, 21, 12, 1, 2, 0, time.UTC))
	secondPipeline.NewScanID = func(time.Time) string { return "file-pipeline-second" }
	second, err := secondPipeline.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := second.Assessments[0]; got.State != StoreUpdateAvailable || !got.Stale || got.AvailableVersion != "1.1.0" {
		t.Fatalf("file-backed pipeline hysteresis assessment=%#v", got)
	}
}

func TestStoreScanFileRepositoryPermissionFailure(t *testing.T) {
	rootFile := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(rootFile, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := openStoreScanFileRepository(rootFile); err == nil {
		t.Fatal("opening a repository on a file path should fail")
	}
}

func testStoreScanSnapshot(userSID, pfn, scanID string, started time.Time, state StoreUpdateState) StoreScanSnapshot {
	completed := started.Add(time.Second)
	scan := StoreScanGeneration{
		ScanID:           scanID,
		UserSID:          userSID,
		StartedAt:        started,
		CompletedAt:      completed,
		WindowsVersion:   "Windows 11",
		WindowsBuild:     "10.0.26200.8655",
		Architecture:     "amd64",
		CompletionStatus: StoreScanCompleted,
	}
	identity := StoreInstalledIdentity{UserSID: userSID, PackageFamilyName: pfn}
	provider := StoreProviderIdentity{ID: "repository-provider", Name: "Repository Provider", Backend: "fake"}
	target := exactStoreTarget(identity, provider)
	target.VerifiedAt = completed
	mapping := VerifiedStoreIdentityMapping{
		InstalledIdentity: identity,
		ProductID:         target.ProductID,
		Provider:          provider,
		ScanID:            scan.ScanID,
		VerifiedAt:        completed,
		Evidence:          "structured exact PFN/Product ID fixture",
	}
	observationKind := StoreObservationAuthoritativeNegative
	availableVersion := ""
	if state == StoreUpdateAvailable {
		observationKind = StoreObservationPositiveUpdateOffer
		availableVersion = "1.1.0"
	}
	observation := storeObservation(identity, scan, provider, StoreProviderHealthy, observationKind, "1.0.0", availableVersion, nil)
	if state == StoreUpdateAvailable {
		observation.Target = target
	}
	assessment := StorePublishedAssessment{
		StoreUpdateAssessment: StoreUpdateAssessment{
			State:            state,
			Identity:         identity,
			ScanID:           scan.ScanID,
			Reason:           "repository conformance fixture",
			InstalledVersion: "1.0.0",
			AvailableVersion: availableVersion,
			Evidence: []StoreEvidenceSummary{{
				Provider: provider.Key(),
				Health:   StoreProviderHealthy,
				Kind:     observationKind,
			}},
		},
		ObservedAt:     completed,
		StoreProductID: target.ProductID,
		Applicability:  "unknown",
	}
	if state == StoreUpdateAvailable {
		assessment.Target = target
		assessment.ExactActionTargetAvailable = true
		assessment.Applicability = "applicable"
	}
	return StoreScanSnapshot{
		SchemaVersion: storeScanSchemaVersion,
		Published:     true,
		Scan:          scan,
		Inventory:     testStoreInventory(scan, pfn, "1.0.0"),
		ProviderRuns: []StoreCatalogProviderRun{{
			Provider:     provider,
			Version:      "v1.2.3",
			StartedAt:    started,
			CompletedAt:  completed,
			Health:       StoreProviderHealthy,
			Observations: []StoreProviderObservation{observation},
			Mappings:     []VerifiedStoreIdentityMapping{mapping},
		}},
		Assessments: []StorePublishedAssessment{assessment},
	}
}

func mustMarshalJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func writeRawStoreSnapshotFile(t *testing.T, repo *StoreScanFileRepository, snapshot StoreScanSnapshot, data []byte) {
	t.Helper()
	path := repo.snapshotPath(snapshot)
	writeRawStoreSnapshotPath(t, path, data)
}

func writeRawStoreSnapshotPath(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func countJSONSnapshots(t *testing.T, dir string) int {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(strings.ToLower(entry.Name()), ".json") {
			count++
		}
	}
	return count
}

func storeScanAPIProjectionJSON(t *testing.T, snapshot StoreScanSnapshot) []byte {
	t.Helper()
	providers := providerSummariesFromRuns(snapshot.ProviderRuns)
	families := map[string]StorePackagedAppFamily{}
	for _, family := range snapshot.Inventory.Families {
		families[strings.ToLower(family.Identity.PackageFamilyName)] = family
	}
	inventory := applyPublishedStoreAssessmentsToInventory(defaultState(), Inventory{
		PackageLookup: PackageLookup{Packages: []Package{transactionalStoreAPIPackage("OpenAI.Codex_abc123")}},
	}, snapshot.Assessments, families, providers)
	data, err := json.Marshal(inventory.Packages)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func storeScanDiagnosticsProjectionJSON(t *testing.T, snapshot StoreScanSnapshot) []byte {
	t.Helper()
	export := StoreDiagnosticsExport{GeneratedAt: "2026-06-23T10:00:02Z", SchemaVersion: storeScanSchemaVersion, DetectorMode: "new"}
	applyStoreDiagnosticsSnapshot(&export, snapshot)
	data, err := json.Marshal(export)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
