package updater

func mergeAppxInventoryPackages(
	state *State,
	managers map[string]ManagerStatus,
	commandResults map[string]CommandResult,
	packages []Package,
	appxPackages []Package,
	storeUpdateVersions map[string]string,
) []Package {
	markStoreInventoryAvailable(managers)
	for i := range appxPackages {
		appxPackages[i].Key = packageKey(managerStore, appxPackages[i].ID)
		appxPackages[i].Installed = true
		if appxPackages[i].UpdateSupported {
			appxPackages[i].AutoUpdate = packageAutoUpdateEnabled(*state, appxPackages[i])
		}
	}
	return append(packages, appxPackages...)
}
