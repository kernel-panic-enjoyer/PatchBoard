package updater

import (
	"context"
	"testing"
	"time"
)

func TestRefreshInventoryPreservesExplicitUpdateCandidateWhenManagerListIsDegraded(t *testing.T) {
	oldGetter := inventoryGetter
	defer func() { inventoryGetter = oldGetter }()

	const key = "winget:PokeMMO.PokeMMO"
	app := &App{
		inventoryService: inventoryService{cache: Inventory{PackageLookup: PackageLookup{
			Packages: []Package{{
				Key:              key,
				Manager:          managerWinget,
				ID:               "PokeMMO.PokeMMO",
				Name:             "PokeMMO",
				Version:          "Unknown",
				AvailableVersion: "1.0",
				UpdateAvailable:  true,
				UpdateSupported:  true,
				UnknownVersion:   true,
				Installed:        true,
				Source:           sourceWinget,
			}},
		}},
			fetchedAt: time.Now()},
	}

	inventoryGetter = func(ctx context.Context) Inventory {
		return Inventory{PackageLookup: PackageLookup{
			Packages: []Package{{
				Key:             key,
				Manager:         managerWinget,
				ID:              "PokeMMO.PokeMMO",
				Name:            "PokeMMO.PokeMMO",
				Version:         "Unknown",
				UpdateSupported: true,
				UnknownVersion:  true,
				Installed:       true,
				Source:          sourceWinget,
			}},
			CommandResults: map[string]CommandResult{
				"winget_list": {Command: "winget list", Code: 1, Stderr: "list failed"},
			},
		}}
	}

	refreshed := app.refreshInventorySync("test")
	if len(refreshed.Packages) != 1 {
		t.Fatalf("expected one refreshed package, got %#v", refreshed.Packages)
	}
	pkg := refreshed.Packages[0]
	if !pkg.UpdateAvailable || pkg.AvailableVersion != "1.0" || !pkg.UnknownVersion {
		t.Fatalf("degraded refresh dropped explicit unknown-version update candidate: %#v", pkg)
	}
}

func TestRefreshInventoryDoesNotPreserveNormalUpdateCandidateWhenManagerListIsDegraded(t *testing.T) {
	oldGetter := inventoryGetter
	defer func() { inventoryGetter = oldGetter }()

	const key = "winget:DenoLand.Deno"
	app := &App{
		inventoryService: inventoryService{cache: Inventory{PackageLookup: PackageLookup{
			Packages: []Package{{
				Key:              key,
				Manager:          managerWinget,
				ID:               "DenoLand.Deno",
				Name:             "Deno",
				Version:          "2.9.1",
				AvailableVersion: "2.9.2",
				UpdateAvailable:  true,
				UpdateSupported:  true,
				Installed:        true,
				Source:           sourceWinget,
			}},
		}},
			fetchedAt: time.Now()},
	}

	inventoryGetter = func(ctx context.Context) Inventory {
		return Inventory{PackageLookup: PackageLookup{
			Packages: []Package{{
				Key:             key,
				Manager:         managerWinget,
				ID:              "DenoLand.Deno",
				Name:            "Deno",
				Version:         "2.9.2",
				UpdateSupported: true,
				Installed:       true,
				Source:          sourceWinget,
			}},
			CommandResults: map[string]CommandResult{
				"winget_list": {Command: "winget list", Code: 1, Stderr: "list failed"},
			},
		}}
	}

	refreshed := app.refreshInventorySync("test")
	if len(refreshed.Packages) != 1 {
		t.Fatalf("expected one refreshed package, got %#v", refreshed.Packages)
	}
	if refreshed.Packages[0].UpdateAvailable || refreshed.Packages[0].AvailableVersion != "" {
		t.Fatalf("degraded refresh preserved normal completed update candidate: %#v", refreshed.Packages[0])
	}
}
