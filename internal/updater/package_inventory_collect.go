package updater

import (
	"context"
	"sync"
)

type packageManagerInventoryCollector struct {
	manager          string
	collectInstalled func(context.Context) ([]Package, CommandResult)
	collectUpdates   func(context.Context) (map[string]string, map[string]Package, CommandResult)
	listResultKey    string
	updateResultKey  string
}

var packageManagerInventoryCollectors = []packageManagerInventoryCollector{
	{
		manager:          managerWinget,
		collectInstalled: wingetInstalledContext,
		collectUpdates:   wingetUpdatesContext,
		listResultKey:    "winget_list",
		updateResultKey:  "winget_upgrade",
	},
	{
		manager:          managerChoco,
		collectInstalled: chocoInstalledContext,
		collectUpdates:   chocoUpdatesContext,
		listResultKey:    "choco_list",
		updateResultKey:  "choco_outdated",
	},
}

func collectManagerInventory(
	ctx context.Context,
	manager string,
	collectInstalled func(context.Context) ([]Package, CommandResult),
	collectUpdates func(context.Context) (map[string]string, map[string]Package, CommandResult),
	listResultKey string,
	updateResultKey string,
) managerInventory {
	var installedPackages []Package
	var listResult CommandResult
	var availableVersions map[string]string
	var updateDetailsByKey map[string]Package
	var updateResult CommandResult
	var wg sync.WaitGroup
	goSafeGroup(&wg, manager+" installed inventory", func() {
		installedPackages, listResult = collectInstalled(ctx)
	}, func(err error) {
		installedPackages, listResult = nil, failedBackgroundCommandResult(listResultKey, err)
	})
	goSafeGroup(&wg, manager+" update inventory", func() {
		availableVersions, updateDetailsByKey, updateResult = collectUpdates(ctx)
	}, func(err error) {
		availableVersions, updateDetailsByKey, updateResult = nil, nil, failedBackgroundCommandResult(updateResultKey, err)
	})
	wg.Wait()
	return managerInventory{
		manager:       manager,
		installed:     installedPackages,
		listResult:    listResult,
		updates:       availableVersions,
		updateDetails: updateDetailsByKey,
		updateResult:  updateResult,
		listKey:       listResultKey,
		updateKey:     updateResultKey,
	}
}

func collectInventoryInputs(ctx context.Context, managers map[string]ManagerStatus) inventoryInputs {
	inputs := inventoryInputs{}
	inventoryCh := make(chan managerInventory, len(managedPackageManagers))
	var wg sync.WaitGroup

	goSafeGroup(&wg, "Store packaged inventory", func() {
		inputs.storePackagedInventory, inputs.storePackagedResult = collectNativeStorePackagedInventoryContext(ctx)
	}, func(err error) {
		inputs.storePackagedResult = failedBackgroundCommandResult("store packaged inventory", err)
	})
	for _, collector := range packageManagerInventoryCollectors {
		if !managers[collector.manager].Available {
			continue
		}
		activeCollector := collector
		goSafeGroup(&wg, activeCollector.manager+" inventory", func() {
			inventoryCh <- collectManagerInventory(
				ctx,
				activeCollector.manager,
				activeCollector.collectInstalled,
				activeCollector.collectUpdates,
				activeCollector.listResultKey,
				activeCollector.updateResultKey,
			)
		}, func(err error) {
			inventoryCh <- managerInventory{
				manager:      activeCollector.manager,
				listResult:   failedBackgroundCommandResult(activeCollector.listResultKey, err),
				updateResult: failedBackgroundCommandResult(activeCollector.updateResultKey, err),
				listKey:      activeCollector.listResultKey,
				updateKey:    activeCollector.updateResultKey,
			}
		})
	}

	wg.Wait()
	inputs.appxResult = inputs.storePackagedResult
	if inputs.storePackagedResult.OK {
		state := loadStateContext(ctx)
		inputs.appxPackages = packagesFromNativeStorePackagedInventory(state, inputs.storePackagedInventory)
	}
	close(inventoryCh)
	for inventory := range inventoryCh {
		inputs.managerInventories = append(inputs.managerInventories, inventory)
	}
	return inputs
}

func collectNativeStorePackagedInventoryContext(ctx context.Context) (StorePackagedAppInventory, CommandResult) {
	userSID, err := currentUserSID()
	if err != nil {
		result := validationCommandResult("native Store inventory", err)
		return StorePackagedAppInventory{Partial: true, Errors: []string{err.Error()}}, result
	}
	scan := newStorePackagedAppScan(userSID)
	return storePackagedAppInventoryProvider().Inventory(ctx, scan)
}
