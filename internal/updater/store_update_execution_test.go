package updater

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestStoreExactUpdateVerifiesVersionChange(t *testing.T) {
	targets := []string{}
	executor := testStoreExactExecutor(
		fakeStoreExactRunner{targets: &targets, result: CommandResult{OK: true, Command: "store update 9NCODEX", Stdout: "accepted"}},
		&fakeStoreExactInventory{snapshots: []StoreExactPackageSnapshot{
			testStoreExactSnapshot("1.0.0", "OpenAI.Codex_1.0.0_x64__abc123", true),
			testStoreExactSnapshot("1.1.0", "OpenAI.Codex_1.1.0_x64__abc123", true),
		}},
		fakeStoreExactCatalog{},
		fakeStoreEvents{},
	)
	result := executeStoreExactUpdateForTest(t, executor, testExactStorePackage())
	if !result.OK {
		t.Fatalf("expected verified exact update, got %#v", result)
	}
	if strings.Join(targets, "|") != "9NCODEX" {
		t.Fatalf("expected only exact Product ID target, got %#v", targets)
	}
}

func TestStoreExactUpdateCallbacksExposeExecutionPhases(t *testing.T) {
	executor := testStoreExactExecutor(
		fakeStoreExactRunner{result: CommandResult{OK: true, Command: "store update 9NCODEX", Stdout: "accepted"}},
		&fakeStoreExactInventory{snapshots: []StoreExactPackageSnapshot{
			testStoreExactSnapshot("1.0.0", "OpenAI.Codex_1.0.0_x64__abc123", true),
			testStoreExactSnapshot("1.1.0", "OpenAI.Codex_1.1.0_x64__abc123", true),
		}},
		fakeStoreExactCatalog{},
		fakeStoreEvents{},
	)
	restoreSID := replaceStoreScanSID("S-1-5-21-exec")
	defer restoreSID()
	preparePublishedExactStoreAssessment(t, testExactStorePackage())
	oldAvailable := packageActionManagerAvailable
	packageActionManagerAvailable = func(manager string) bool { return manager == managerStore }
	defer func() { packageActionManagerAvailable = oldAvailable }()
	var phases []string
	result := executor.ExecuteWithCallbacks(context.Background(), testExactStorePackage(), StoreExactUpdateCallbacks{
		Starting:  func(StoreExactUpdateRequest) { phases = append(phases, jobStateStarting) },
		Accepted:  func(StoreExactUpdateRequest, CommandResult) { phases = append(phases, jobStateAccepted) },
		Verifying: func(StoreExactUpdateRequest) { phases = append(phases, jobStateVerifying) },
	})
	if !result.OK {
		t.Fatalf("expected verified update, got %#v", result)
	}
	if strings.Join(phases, "|") != "starting|accepted|verifying" {
		t.Fatalf("unexpected phases: %#v", phases)
	}
}

func TestStoreExactUpdateAcceptedWithoutPackageChangeIsNotVerified(t *testing.T) {
	executor := testStoreExactExecutor(
		fakeStoreExactRunner{result: CommandResult{OK: true, Command: "store update 9NCODEX", Stdout: "accepted"}},
		&fakeStoreExactInventory{snapshots: []StoreExactPackageSnapshot{
			testStoreExactSnapshot("1.0.0", "OpenAI.Codex_1.0.0_x64__abc123", true),
			testStoreExactSnapshot("1.0.0", "OpenAI.Codex_1.0.0_x64__abc123", true),
		}},
		fakeStoreExactCatalog{},
		fakeStoreEvents{},
	)
	result := executeStoreExactUpdateForTest(t, executor, testExactStorePackage())
	if result.OK || result.Code != storeUpdateAcceptedNotVerifiedCode {
		t.Fatalf("expected accepted_not_verified, got %#v", result)
	}
}

func TestStoreExactUpdateAcceptedButTargetedRescanFails(t *testing.T) {
	executor := testStoreExactExecutor(
		fakeStoreExactRunner{result: CommandResult{OK: true, Command: "store update 9NCODEX", Stdout: "accepted"}},
		&fakeStoreExactInventory{snapshots: []StoreExactPackageSnapshot{
			testStoreExactSnapshot("1.0.0", "OpenAI.Codex_1.0.0_x64__abc123", true),
			testStoreExactSnapshot("1.0.0", "OpenAI.Codex_1.0.0_x64__abc123", true),
		}},
		fakeStoreExactCatalog{command: CommandResult{Command: "catalog query 9NCODEX", Code: 1, Stderr: "catalog unavailable"}},
		fakeStoreEvents{},
	)
	result := executeStoreExactUpdateForTest(t, executor, testExactStorePackage())
	if result.OK || result.Code != storeUpdateAcceptedNotVerifiedCode || !strings.Contains(result.Command, "catalog query") {
		t.Fatalf("expected accepted_not_verified with catalog diagnostics, got %#v", result)
	}
}

func TestStoreExactUpdatePollingVerifiesWhenEventIsLost(t *testing.T) {
	executor := testStoreExactExecutor(
		fakeStoreExactRunner{result: CommandResult{OK: true, Command: "store update 9NCODEX", Stdout: "accepted"}},
		&fakeStoreExactInventory{snapshots: []StoreExactPackageSnapshot{
			testStoreExactSnapshot("1.0.0", "OpenAI.Codex_1.0.0_x64__abc123", true),
			testStoreExactSnapshot("1.0.0", "OpenAI.Codex_1.0.0_x64__abc123", true),
			testStoreExactSnapshot("1.1.0", "OpenAI.Codex_1.1.0_x64__abc123", true),
		}},
		fakeStoreExactCatalog{},
		fakeStoreEvents{},
	)
	result := executeStoreExactUpdateForTest(t, executor, testExactStorePackage())
	if !result.OK {
		t.Fatalf("expected polling fallback to verify update, got %#v", result)
	}
}

func TestStoreExactUpdateSameVisibleVersionWithOfferRemoved(t *testing.T) {
	executor := testStoreExactExecutor(
		fakeStoreExactRunner{result: CommandResult{OK: true, Command: "store update 9NCODEX", Stdout: "accepted"}},
		&fakeStoreExactInventory{snapshots: []StoreExactPackageSnapshot{
			testStoreExactSnapshot("1.0.0", "OpenAI.Codex_1.0.0_x64__abc123", true),
			testStoreExactSnapshot("1.0.0", "OpenAI.Codex_1.0.0_x64__abc123", true),
		}},
		fakeStoreExactCatalog{result: StoreExactCatalogResult{Authoritative: true, OfferAvailable: false, InstalledHealthy: true}, command: CommandResult{OK: true, Command: "catalog query 9NCODEX", Stdout: "no offer"}},
		fakeStoreEvents{},
	)
	result := executeStoreExactUpdateForTest(t, executor, testExactStorePackage())
	if !result.OK || !strings.Contains(result.Stdout, "exact offer disappeared") {
		t.Fatalf("expected offer removal to verify same-version update, got %#v", result)
	}
}

func TestStoreExactUpdateIgnoresWrongUserAndUnrelatedEvents(t *testing.T) {
	executor := testStoreExactExecutor(
		fakeStoreExactRunner{result: CommandResult{OK: true, Command: "store update 9NCODEX", Stdout: "accepted"}},
		&fakeStoreExactInventory{snapshots: []StoreExactPackageSnapshot{
			testStoreExactSnapshot("1.0.0", "OpenAI.Codex_1.0.0_x64__abc123", true),
			testStoreExactSnapshot("1.0.0", "OpenAI.Codex_1.0.0_x64__abc123", true),
		}},
		fakeStoreExactCatalog{},
		fakeStoreEvents{events: []StorePackageChangeEvent{
			{Identity: StoreInstalledIdentity{UserSID: "S-1-5-21-other", PackageFamilyName: "OpenAI.Codex_abc123"}, Version: "1.1.0", PackageFullName: "OpenAI.Codex_1.1.0_x64__abc123", Healthy: true},
			{Identity: StoreInstalledIdentity{UserSID: "S-1-5-21-exec", PackageFamilyName: "Other.App_abc123"}, Version: "1.1.0", PackageFullName: "Other.App_1.1.0_x64__abc123", Healthy: true},
		}},
	)
	result := executeStoreExactUpdateForTest(t, executor, testExactStorePackage())
	if result.OK || result.Code != storeUpdateAcceptedNotVerifiedCode {
		t.Fatalf("wrong-user and unrelated events must not verify update, got %#v", result)
	}
}

func TestStoreExactUpdateVerificationCanResumeAfterAcceptedAction(t *testing.T) {
	request := testExactStoreRequest()
	pre := testStoreExactSnapshot("1.0.0", "OpenAI.Codex_1.0.0_x64__abc123", true)
	executor := testStoreExactExecutor(fakeStoreExactRunner{}, &fakeStoreExactInventory{snapshots: []StoreExactPackageSnapshot{
		testStoreExactSnapshot("1.1.0", "OpenAI.Codex_1.1.0_x64__abc123", true),
	}}, fakeStoreExactCatalog{}, fakeStoreEvents{})
	verification := executor.verifyAcceptedAction(context.Background(), request, pre, time.Millisecond)
	if !verification.Verified {
		t.Fatalf("expected resumed verification to succeed, got %#v", verification)
	}
}

func TestStoreExactUpdateCancellationStopsVerification(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	executor := testStoreExactExecutor(
		fakeStoreExactRunner{after: cancel, result: CommandResult{OK: true, Command: "store update 9NCODEX", Stdout: "accepted"}},
		&fakeStoreExactInventory{snapshots: []StoreExactPackageSnapshot{
			testStoreExactSnapshot("1.0.0", "OpenAI.Codex_1.0.0_x64__abc123", true),
			testStoreExactSnapshot("1.0.0", "OpenAI.Codex_1.0.0_x64__abc123", true),
		}},
		fakeStoreExactCatalog{},
		fakeStoreEvents{},
	)
	result := executeStoreExactUpdateForTestWithContext(t, ctx, executor, testExactStorePackage())
	if result.Code != commandCancelledCode {
		t.Fatalf("expected cancelled verification, got %#v", result)
	}
}

func TestStoreExactUpdateRejectedTargetFails(t *testing.T) {
	executor := testStoreExactExecutor(
		fakeStoreExactRunner{result: CommandResult{Command: "store update 9NCODEX", Code: 1, Stderr: "target rejected"}},
		&fakeStoreExactInventory{snapshots: []StoreExactPackageSnapshot{testStoreExactSnapshot("1.0.0", "OpenAI.Codex_1.0.0_x64__abc123", true)}},
		fakeStoreExactCatalog{},
		fakeStoreEvents{},
	)
	result := executeStoreExactUpdateForTest(t, executor, testExactStorePackage())
	if result.OK || result.Code != 1 || !strings.Contains(result.Stderr, "target rejected") {
		t.Fatalf("expected exact target rejection to fail, got %#v", result)
	}
}

func TestStoreExactUpdateDisplayNameFallbackIsImpossible(t *testing.T) {
	targets := []string{}
	executor := testStoreExactExecutor(
		fakeStoreExactRunner{targets: &targets, result: CommandResult{Command: "store update 9NCODEX", Code: 1, Stderr: "target rejected"}},
		&fakeStoreExactInventory{snapshots: []StoreExactPackageSnapshot{testStoreExactSnapshot("1.0.0", "OpenAI.Codex_1.0.0_x64__abc123", true)}},
		fakeStoreExactCatalog{},
		fakeStoreEvents{},
	)
	pkg := testExactStorePackage()
	pkg.Name = "Display Name Must Never Be Used"
	result := executeStoreExactUpdateForTest(t, executor, pkg)
	if result.OK {
		t.Fatalf("expected rejected exact target, got %#v", result)
	}
	if strings.Join(targets, "|") != "9NCODEX" {
		t.Fatalf("display-name fallback target was attempted: %#v", targets)
	}
}

func TestStoreExactUpdateValidationRequiresFreshAvailableAssessment(t *testing.T) {
	executor := testStoreExactExecutor(fakeStoreExactRunner{}, &fakeStoreExactInventory{}, fakeStoreExactCatalog{}, fakeStoreEvents{})
	pkg := testExactStorePackage()
	pkg.Stale = true
	result := executeStoreExactUpdateForTest(t, executor, pkg)
	if result.OK || !strings.Contains(result.Stderr, "fresh assessment") {
		t.Fatalf("expected stale assessment validation failure, got %#v", result)
	}
}

func executeStoreExactUpdateForTest(t *testing.T, executor StoreExactUpdateExecutor, pkg Package) CommandResult {
	t.Helper()
	return executeStoreExactUpdateForTestWithContext(t, context.Background(), executor, pkg)
}

func executeStoreExactUpdateForTestWithContext(t *testing.T, ctx context.Context, executor StoreExactUpdateExecutor, pkg Package) CommandResult {
	t.Helper()
	restoreSID := replaceStoreScanSID("S-1-5-21-exec")
	defer restoreSID()
	preparePublishedExactStoreAssessment(t, pkg)
	oldAvailable := packageActionManagerAvailable
	packageActionManagerAvailable = func(manager string) bool { return manager == managerStore }
	defer func() { packageActionManagerAvailable = oldAvailable }()
	return executor.Execute(ctx, pkg)
}

func preparePublishedExactStoreAssessment(t *testing.T, pkg Package) {
	t.Helper()
	t.Setenv("UPDATER_STATE_DIR", t.TempDir())
	store, err := openDefaultStoreScanStore()
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 6, 21, 12, 0, 0, 0, time.UTC)
	identity := StoreInstalledIdentity{UserSID: "S-1-5-21-exec", PackageFamilyName: pkg.InstalledPackageFamilyName}
	scan := StoreScanGeneration{
		ScanID:           pkg.ScanID,
		UserSID:          identity.UserSID,
		StartedAt:        now,
		CompletedAt:      now.Add(time.Second),
		CompletionStatus: StoreScanCompleted,
	}
	assessment := StorePublishedAssessment{
		StoreUpdateAssessment: StoreUpdateAssessment{
			State:            StoreUpdateState(pkg.UpdateState),
			Identity:         identity,
			ScanID:           pkg.ScanID,
			Reason:           pkg.UpdateReason,
			InstalledVersion: pkg.InstalledVersion,
			AvailableVersion: pkg.OfferedVersion,
			Target: &ExactStoreUpdateTarget{
				Identity:   identity,
				Provider:   StoreProviderIdentity{ID: managerStore, Name: "Store CLI", Backend: backendStoreCLI},
				ProductID:  pkg.StoreProductID,
				Verified:   true,
				VerifiedBy: "test",
				VerifiedAt: now,
			},
		},
		ObservedAt:                 now,
		StoreProductID:             pkg.StoreProductID,
		ExactActionTargetAvailable: pkg.ExactActionTargetAvailable,
		Applicability:              pkg.Applicability,
	}
	inventory := StorePackagedAppInventory{
		Scan: scan,
		Families: []StorePackagedAppFamily{{
			Identity:    identity,
			DisplayName: pkg.Name,
			ProductLike: true,
		}},
	}
	if _, err := store.PersistScan(context.Background(), storeScanPersistInput{Scan: scan, Inventory: inventory, Assessments: []StorePublishedAssessment{assessment}, Publish: true}); err != nil {
		t.Fatal(err)
	}
}

func testStoreExactExecutor(runner StoreExactUpdateActionRunner, inventory StoreExactInventoryProvider, catalog StoreExactCatalogProvider, events StorePackageEventSource) StoreExactUpdateExecutor {
	return StoreExactUpdateExecutor{
		Runner:    runner,
		Inventory: inventory,
		Catalog:   catalog,
		Events:    events,
		Timeout:   25 * time.Millisecond,
		PollEvery: time.Millisecond,
	}
}

func testExactStorePackage() Package {
	return Package{
		Key:                        packageKey(managerStore, "9NCODEX"),
		Manager:                    managerStore,
		ID:                         "9NCODEX",
		Name:                       "Codex",
		Version:                    "1.0.0",
		AvailableVersion:           "1.1.0",
		UpdateAvailable:            true,
		UpdateSupported:            true,
		Installed:                  true,
		Source:                     sourceNativeAppX,
		ActionBackend:              backendStoreCLI,
		UpdateState:                string(StoreUpdateAvailable),
		UpdateReason:               "fresh exact positive update evidence",
		ObservedAt:                 "2026-06-21T12:00:00Z",
		ScanID:                     "scan-exec",
		ExactIdentityAvailable:     true,
		ExactActionTargetAvailable: true,
		InstalledPackageFamilyName: "OpenAI.Codex_abc123",
		StoreProductID:             "9NCODEX",
		InstalledVersion:           "1.0.0",
		OfferedVersion:             "1.1.0",
		Applicability:              "applicable",
	}
}

func testExactStoreRequest() StoreExactUpdateRequest {
	return StoreExactUpdateRequest{
		Identity:         StoreInstalledIdentity{UserSID: "S-1-5-21-exec", PackageFamilyName: "OpenAI.Codex_abc123"},
		ProductID:        "9NCODEX",
		Target:           "9NCODEX",
		Provider:         StoreProviderIdentity{ID: managerStore, Name: "Store CLI", Backend: backendStoreCLI},
		ScanID:           "scan-exec",
		InstalledVersion: "1.0.0",
		OfferedVersion:   "1.1.0",
	}
}

func testStoreExactSnapshot(version, fullName string, healthy bool) StoreExactPackageSnapshot {
	return StoreExactPackageSnapshot{
		Identity:        StoreInstalledIdentity{UserSID: "S-1-5-21-exec", PackageFamilyName: "OpenAI.Codex_abc123"},
		PackageFullName: fullName,
		Version:         version,
		Healthy:         healthy,
		Exists:          true,
		ObservedAt:      time.Date(2026, 6, 21, 12, 0, 0, 0, time.UTC),
	}
}

type fakeStoreExactRunner struct {
	targets *[]string
	result  CommandResult
	after   func()
}

func (runner fakeStoreExactRunner) RunStoreUpdate(ctx context.Context, request StoreExactUpdateRequest) CommandResult {
	if runner.targets != nil {
		*runner.targets = append(*runner.targets, request.Target)
	}
	if runner.after != nil {
		runner.after()
	}
	return runner.result
}

type fakeStoreExactInventory struct {
	snapshots []StoreExactPackageSnapshot
	results   []CommandResult
	calls     int
}

func (inventory *fakeStoreExactInventory) Snapshot(ctx context.Context, identity StoreInstalledIdentity) (StoreExactPackageSnapshot, CommandResult) {
	if inventory.calls >= len(inventory.snapshots) && len(inventory.snapshots) > 0 {
		inventory.calls++
		return inventory.snapshots[len(inventory.snapshots)-1], fakeStoreInventoryResult(inventory, len(inventory.snapshots)-1)
	}
	index := inventory.calls
	inventory.calls++
	if len(inventory.snapshots) == 0 {
		return StoreExactPackageSnapshot{Identity: identity}, fakeStoreInventoryResult(inventory, index)
	}
	snapshot := inventory.snapshots[index]
	if !snapshot.Identity.Resolved() {
		snapshot.Identity = identity
	}
	return snapshot, fakeStoreInventoryResult(inventory, index)
}

func fakeStoreInventoryResult(inventory *fakeStoreExactInventory, index int) CommandResult {
	if index >= 0 && index < len(inventory.results) {
		return inventory.results[index]
	}
	return CommandResult{OK: true, Command: "fake inventory"}
}

type fakeStoreExactCatalog struct {
	result  StoreExactCatalogResult
	command CommandResult
}

func (catalog fakeStoreExactCatalog) QueryExact(context.Context, StoreExactUpdateRequest) (StoreExactCatalogResult, CommandResult) {
	command := catalog.command
	if command.Command == "" {
		command = CommandResult{Command: "fake catalog", Code: 1, Stderr: "not implemented"}
	}
	return catalog.result, command
}

type fakeStoreEvents struct {
	events []StorePackageChangeEvent
	err    error
}

func (events fakeStoreEvents) Subscribe(context.Context, StoreInstalledIdentity) (<-chan StorePackageChangeEvent, func(), error) {
	ch := make(chan StorePackageChangeEvent, len(events.events))
	for _, event := range events.events {
		ch <- event
	}
	close(ch)
	return ch, nil, events.err
}
