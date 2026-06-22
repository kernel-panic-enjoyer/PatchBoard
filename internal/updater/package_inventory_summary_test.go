package updater

import "testing"

func TestSortInventoryPackagesPushesAppxInventoryRowsLast(t *testing.T) {
	packages := []Package{
		{
			Name:          "Current-AppX",
			Manager:       managerStore,
			ActionBackend: backendAppXInventory,
			UpdateState:   string(StoreUpdateCurrent),
		},
		{
			Name:          "App-Resolver",
			Manager:       managerStore,
			ActionBackend: backendAppXInventory,
			UpdateState:   string(StoreUpdateUnknown),
		},
		{
			Name:          "chocolatey",
			Manager:       managerChoco,
			ActionBackend: managerChoco,
		},
		{
			Name:          "Codex",
			Manager:       managerStore,
			ActionBackend: backendStoreCLI,
			UpdateState:   string(StoreUpdateCurrent),
		},
		{
			Name:          "GitHub CLI",
			Manager:       managerWinget,
			ActionBackend: managerWinget,
		},
	}

	sortInventoryPackages(packages)

	if packages[0].Name != "chocolatey" || packages[1].Name != "GitHub CLI" || packages[2].Name != "Codex" || packages[3].Name != "App-Resolver" || packages[4].Name != "Current-AppX" {
		t.Fatalf("unexpected package order: %#v", packages)
	}
}
