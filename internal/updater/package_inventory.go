package updater

import "context"

type managerInventory struct {
	manager       string
	installed     []Package
	listResult    CommandResult
	updates       map[string]string
	updateDetails map[string]Package
	updateResult  CommandResult
	listKey       string
	updateKey     string
}

type inventoryInputs struct {
	managerInventories         []managerInventory
	appxPackages               []Package
	legacyAppxPackages         []Package
	appxResult                 CommandResult
	storePackagedInventory     StorePackagedAppInventory
	storePackagedResult        CommandResult
	storePackagedComparison    StorePackagedInventoryComparison
	nativeStoreInstalled       []Package
	nativeStoreInstalledResult CommandResult
	nativeStoreUpdates         map[string]string
	nativeStoreUpdatePackages  []Package
	nativeStoreUpdatesResult   CommandResult
}

var inventoryGetter = getInventory

func getInventory() Inventory {
	state := loadState()
	managers := detectManagers()
	commandResults := map[string]CommandResult{}
	var packages []Package
	storeUpdateVersions := map[string]string{}

	inputs := collectInventoryInputs(managers)
	commandResults["appx_inventory"] = inputs.appxResult
	if inputs.storePackagedResult.Command != "" {
		commandResults["native_store_inventory"] = inputs.storePackagedResult
	}
	if len(inputs.storePackagedComparison.MissingNativePFNs) > 0 ||
		len(inputs.storePackagedComparison.MissingLegacyPFNs) > 0 ||
		len(inputs.storePackagedComparison.VersionDifferences) > 0 ||
		len(inputs.storePackagedComparison.ScopeDifferences) > 0 ||
		len(inputs.storePackagedComparison.ClassificationNotes) > 0 ||
		len(inputs.storePackagedComparison.NativeErrors) > 0 {
		commandResults["native_store_inventory_compare"] = storePackagedInventoryComparisonResult(inputs.storePackagedComparison)
	}
	if inputs.nativeStoreInstalledResult.Command != "" {
		commandResults["store_installed"] = inputs.nativeStoreInstalledResult
	}
	if inputs.nativeStoreUpdatesResult.Command != "" {
		commandResults["store_updates"] = inputs.nativeStoreUpdatesResult
		if storeLegacyDetectorRollbackEnabled() {
			mergeUpdateVersions(storeUpdateVersions, inputs.nativeStoreUpdates)
		}
	}

	for _, inventory := range inputs.managerInventories {
		commandResults[inventory.listKey] = inventory.listResult
		commandResults[inventory.updateKey] = inventory.updateResult
		if inventory.manager == managerWinget && storeLegacyDetectorRollbackEnabled() {
			mergeWingetStoreUpdateVersions(storeUpdateVersions, inventory.updates)
		}
		packages = append(packages, packagesFromManagerInventory(state, managers, inventory)...)
	}
	if managers[managerStore].Available && storeLegacyDetectorRollbackEnabled() {
		packages = append(packages, packagesFromNativeStoreInstalled(state, inputs.nativeStoreInstalled)...)
		packages = mergeStoreNativeUpdatePackages(packages, packagesFromNativeStoreUpdates(state, inputs.nativeStoreUpdatePackages))
	}

	if inputs.appxResult.OK || len(inputs.appxPackages) > 0 {
		packages = mergeAppxInventoryPackages(&state, managers, commandResults, packages, inputs.appxPackages, storeUpdateVersions)
	}

	sourceCounts := managedScanSourceCounts(state)
	inventory := Inventory{
		PackageLookup: PackageLookup{
			Packages:       packages,
			Managers:       managers,
			CommandResults: commandResults,
		},
		Scan: inventoryScanSummary(state, sourceCounts),
	}
	inventory = applyStoreTransactionalScanPipeline(context.Background(), state, inventory)
	var changed bool
	inventory, changed = applyStoreUpdateAssessmentProjection(&state, inventory)
	if changed {
		if err := saveAppState(state); err != nil {
			appLog("Failed to save Store update assessment cache: %s", err)
		}
	}
	sortInventoryPackages(inventory.Packages)
	return inventory
}
