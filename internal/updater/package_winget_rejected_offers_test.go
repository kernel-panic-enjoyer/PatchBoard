package updater

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestWingetListAvailableColumnDoesNotCreateActionableOffer(t *testing.T) {
	installed := []Package{
		{ID: "Mozilla.Thunderbird.ESR.de", Version: "153.2.0", AvailableVersion: "153.3.1", Source: sourceWinget},
		{ID: "TeamSpeakSystems.TeamSpeakClient.Beta.6", Version: "5.0.0", AvailableVersion: "6.0.0-beta4.1", Source: sourceWinget},
		{ID: "ZedIndustries.Zed", Version: "1.19.2", AvailableVersion: "1.20.2", Source: sourceWinget},
	}
	inventory := managerInventory{manager: managerWinget, installed: installed, updates: map[string]string{}}
	for _, pkg := range packagesFromManagerInventory(defaultState(), inventory) {
		if pkg.UpdateAvailable || pkg.AvailableVersion != "" {
			t.Fatalf("winget list alone must not authorize an update: %#v", pkg)
		}
	}
}

func TestRejectedWingetOfferIsSuppressedOnlyWhileFingerprintIsUnchanged(t *testing.T) {
	pkg := updatableTestPackage(managerWinget, "ZedIndustries.Zed", "Zed")
	pkg.Version = "1.19.2"
	pkg.AvailableVersion = "1.20.2"
	pkg.Source = sourceWinget
	state := defaultState()
	state.RejectedWingetOffers = map[string]RejectedWingetOffer{
		rejectedWingetOfferKey(pkg): {
			InstalledVersion: pkg.Version,
			AvailableVersion: pkg.AvailableVersion,
			RejectedAt:       utcNow(),
		},
	}

	packages := packagesFromManagerInventory(state, managerInventory{
		manager:   managerWinget,
		installed: []Package{pkg},
		updates:   map[string]string{rejectedWingetOfferKey(pkg): pkg.AvailableVersion},
	})
	if len(packages) != 1 || packages[0].UpdateAvailable || packages[0].AvailableVersion != "" {
		t.Fatalf("rejected unchanged offer should be hidden, got %#v", packages)
	}

	for _, changed := range []Package{
		func() Package { next := pkg; next.Version = "1.20.0"; return next }(),
		func() Package { next := pkg; next.AvailableVersion = "1.21.0"; return next }(),
	} {
		candidate := changed
		candidate.UpdateAvailable = true
		suppressRejectedWingetOffer(state, &candidate)
		if !candidate.UpdateAvailable {
			t.Fatalf("changed offer was incorrectly suppressed: %#v", candidate)
		}
	}

	state.RejectedWingetOffers[rejectedWingetOfferKey(pkg)] = RejectedWingetOffer{
		InstalledVersion: pkg.Version,
		AvailableVersion: pkg.AvailableVersion,
		RejectedAt:       time.Now().UTC().Add(-rejectedWingetOfferLifetime - time.Minute).Format(time.RFC3339),
	}
	candidate := pkg
	suppressRejectedWingetOffer(state, &candidate)
	if !candidate.UpdateAvailable {
		t.Fatal("expired rejection must not suppress a newly checked offer")
	}
}

func TestRecordRejectedWingetOffersIgnoresUnrelatedSkips(t *testing.T) {
	pkg := updatableTestPackage(managerWinget, "ZedIndustries.Zed", "Zed")
	store := newMemoryStateStore(defaultState())
	results := []UpdateResult{
		{Key: pkg.Key, Result: CommandResult{Code: commandSkippedCode, Stdout: "Skipped because the batch was cancelled."}},
	}
	if err := recordRejectedWingetOffersWithStore(context.Background(), store, []Package{pkg}, results); err != nil {
		t.Fatal(err)
	}
	state, err := store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(state.RejectedWingetOffers) != 0 {
		t.Fatalf("unrelated skips must not suppress offers: %#v", state.RejectedWingetOffers)
	}

	results[0].Result.Stdout = rejectedWingetOfferDiagnostic + " (original code 2316632107)."
	if err := recordRejectedWingetOffersWithStore(context.Background(), store, []Package{pkg}, results); err != nil {
		t.Fatal(err)
	}
	state, err = store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	offer, ok := state.RejectedWingetOffers[rejectedWingetOfferKey(pkg)]
	if !ok || offer.InstalledVersion != pkg.Version || offer.AvailableVersion != pkg.AvailableVersion {
		t.Fatalf("exact rejected offer was not recorded: %#v", state.RejectedWingetOffers)
	}
	if !strings.HasPrefix(results[0].Result.Stdout, rejectedWingetOfferDiagnostic) {
		t.Fatal("test result no longer matches WinGet rejection diagnostic")
	}
}
