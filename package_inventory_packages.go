package main

import "strings"

func packagesFromManagerInventory(state State, managers map[string]ManagerStatus, inventory managerInventory) []Package {
	packages := make([]Package, 0, len(inventory.installed))
	for _, pkg := range inventory.installed {
		packages = append(packages, packageFromManagerInventory(state, managers, inventory, pkg))
	}
	return packages
}

func packageFromManagerInventory(state State, managers map[string]ManagerStatus, inventory managerInventory, pkg Package) Package {
	displayManager := inventory.manager
	if inventory.manager == managerWinget {
		displayManager = wingetSourceManager(pkg.Source)
	}
	available := inventory.updates[packageKey(displayManager, strings.ToLower(pkg.ID))]
	if available == "" && inventory.manager == managerWinget {
		available = pkg.AvailableVersion
	}
	pkg.Key = packageKey(displayManager, pkg.ID)
	pkg.Manager = displayManager
	pkg.AvailableVersion = available
	pkg.UpdateAvailable = available != ""
	pkg.UpdateSupported = true
	pkg.Installed = true
	pkg.UnknownVersion = pkg.UnknownVersion || isUnknownPackageVersion(pkg.Version)
	pkg.AutoUpdate = packageAutoUpdateEnabled(state, pkg)
	if pkg.ActionBackend == "" {
		pkg.ActionBackend = displayManager
	}
	if displayManager == managerStore && managers[managerStore].Available {
		pkg.ActionBackend = backendStoreCLI
	} else if displayManager == managerStore {
		pkg.ActionBackend = backendWingetMSStoreFallback
	}
	return pkg
}
