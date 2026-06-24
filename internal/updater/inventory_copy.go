package updater

func (inventory Inventory) DeepCopy() Inventory {
	inventory.PackageLookup = inventory.PackageLookup.DeepCopy()
	inventory.StoreScanHealth = inventory.StoreScanHealth.DeepCopy()
	return inventory
}

func (lookup PackageLookup) DeepCopy() PackageLookup {
	return PackageLookup{
		Packages:       clonePackages(lookup.Packages),
		Managers:       cloneInventoryManagerStatuses(lookup.Managers),
		CommandResults: cloneCommandResults(lookup.CommandResults),
	}
}

func (summary StoreScanHealthSummary) DeepCopy() StoreScanHealthSummary {
	summary.Counts = cloneStringIntMap(summary.Counts)
	summary.Providers = cloneStorePackageProviderSummaries(summary.Providers)
	return summary
}

func clonePackages(packages []Package) []Package {
	if packages == nil {
		return nil
	}
	cloned := make([]Package, len(packages))
	for index, pkg := range packages {
		pkg.ProviderSummaries = cloneStorePackageProviderSummaries(pkg.ProviderSummaries)
		cloned[index] = pkg
	}
	return cloned
}

func cloneStorePackageProviderSummaries(summaries []StorePackageProviderSummary) []StorePackageProviderSummary {
	if summaries == nil {
		return nil
	}
	cloned := make([]StorePackageProviderSummary, len(summaries))
	copy(cloned, summaries)
	return cloned
}

func cloneInventoryManagerStatuses(managers map[string]ManagerStatus) map[string]ManagerStatus {
	if managers == nil {
		return nil
	}
	cloned := make(map[string]ManagerStatus, len(managers))
	for key, manager := range managers {
		cloned[key] = manager
	}
	return cloned
}

func cloneCommandResults(results map[string]CommandResult) map[string]CommandResult {
	if results == nil {
		return nil
	}
	cloned := make(map[string]CommandResult, len(results))
	for key, result := range results {
		cloned[key] = result
	}
	return cloned
}

func cloneStringIntMap(values map[string]int) map[string]int {
	if values == nil {
		return nil
	}
	cloned := make(map[string]int, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}
