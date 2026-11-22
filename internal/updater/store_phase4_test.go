package updater

import "testing"

// An incomplete scan keeps a previous Available, and nothing else. Before,
// a previous Current or Unknown was republished as Available, which put
// every Store app in the update list after an incomplete scan.
func TestIncompleteScanRetainsOnlyPreviousAvailable(t *testing.T) {
	identity := StoreInstalledIdentity{UserSID: "S-1-5-21-test-1001", PackageFamilyName: "Example.App_abc123"}
	provider := StoreProviderIdentity{ID: storeCLIUpdatesProviderID, Name: "Store CLI aggregate updates", Backend: backendStoreCLI}
	scan := completedStoreScan("scan-2", identity.UserSID, provider)
	scan.CompletionStatus = StoreScanIncomplete
	families := []StorePackagedAppFamily{{Identity: identity, ProductLike: true}}
	runs := []StoreCatalogProviderRun{{Provider: provider, Health: StoreProviderIncomplete, Observations: []StoreProviderObservation{
		storeObservation(identity, scan, provider, StoreProviderIncomplete, StoreObservationIncompleteResult, "", "", nil),
	}}}
	for previousState, wantState := range map[StoreUpdateState]StoreUpdateState{
		StoreUpdateCurrent:   StoreUpdateUnknown,
		StoreUpdateUnknown:   StoreUpdateUnknown,
		StoreUpdateAvailable: StoreUpdateAvailable,
	} {
		previous := map[StoreInstalledIdentity]StorePublishedAssessment{identity: {StoreUpdateAssessment: StoreUpdateAssessment{State: previousState, Identity: identity}}}
		got := reconcileStoreScanAssessments(scan, families, runs, previous)
		if len(got) != 1 || got[0].State != wantState {
			t.Fatalf("previous %s: got %#v", previousState, got)
		}
		if wantState == StoreUpdateAvailable && !got[0].Stale {
			t.Fatalf("retained positive must be stale: %#v", got[0])
		}
	}
}

// RULE-278: an exact offer is not trusted when the provider that made it
// also failed for the same package. A failure from another provider does
// not hide it (TestReconcileStoreUpdate covers that case).
func TestExactOfferFromProviderThatAlsoFailedIsUnknown(t *testing.T) {
	identity := StoreInstalledIdentity{UserSID: "S-1-5-21-test-1001", PackageFamilyName: "Example.App_abc123"}
	provider := StoreProviderIdentity{ID: "store-winrt", Name: "Store WinRT", Backend: "winrt"}
	scan := completedStoreScan("scan-1", identity.UserSID, provider)
	got := ReconcileStoreUpdate(StoreReconciliationInput{
		Identity:          identity,
		Scan:              scan,
		RequiredProviders: []StoreProviderIdentity{provider},
		Observations: []StoreProviderObservation{
			storeObservation(identity, scan, provider, StoreProviderHealthy, StoreObservationPositiveUpdateOffer, "1.0.0", "1.1.0", exactStoreTarget(identity, provider)),
			storeObservation(identity, scan, provider, StoreProviderIncomplete, StoreObservationIncompleteResult, "", "", nil),
		},
	})
	if got.State != StoreUpdateUnknown {
		t.Fatalf("got %s (%s)", got.State, got.Reason)
	}
}

// RULE-122: an identity conflict blocks a Store update like any other.
func TestStoreUpdateNowBlockedByIdentityConflict(t *testing.T) {
	pkg := Package{
		Key:                        packageKey(managerStore, "OpenAI.Codex_abc123"),
		Manager:                    managerStore,
		ID:                         "OpenAI.Codex_abc123",
		UpdateAvailable:            true,
		UpdateSupported:            true,
		Installed:                  true,
		Source:                     sourceNativeAppX,
		InstalledPackageFamilyName: "OpenAI.Codex_abc123",
		UpdateState:                string(StoreUpdateAvailable),
		ExactActionTargetAvailable: true,
		StoreUpdateID:              "OpenAI.Codex_abc123",
	}
	if policy := packageUpdatePolicy(pkg, UpdateOptions{}); !policy.CanUpdateNow {
		t.Fatalf("fixture should be actionable: %#v", policy)
	}
	pkg.IdentityConflict = true
	if policy := packageUpdatePolicy(pkg, UpdateOptions{}); policy.CanUpdateNow || policy.CannotUpdateReason == "" {
		t.Fatalf("identity conflict not blocked: %#v", policy)
	}
}
