package updater

import "strings"

func packagesFromManagerInventory(state State, inventory managerInventory) []Package {
	packages := make([]Package, 0, len(inventory.installed))
	for _, pkg := range inventory.installed {
		adapted, ok := packageFromManagerInventory(state, inventory, pkg)
		if ok {
			packages = append(packages, adapted)
		}
	}
	return packages
}

func packageFromManagerInventory(state State, inventory managerInventory, pkg Package) (Package, bool) {
	effectiveManager := inventory.manager
	if inventory.manager == managerWinget {
		effectiveManager = wingetSourceManager(pkg.Source)
	}
	if effectiveManager == managerStore {
		return Package{}, false
	}
	lookupKey := managerUpdateLookupKey(effectiveManager, pkg.ID)
	available := inventory.updates[lookupKey]
	updateDetail := inventory.updateDetails[lookupKey]
	pkg.Key = packageKey(effectiveManager, pkg.ID)
	pkg.Manager = effectiveManager
	pkg.AvailableVersion = available
	pkg.UpdateAvailable = available != ""
	pkg.UpdateSupported = true
	pkg.Installed = true
	pkg.UnknownVersion = pkg.UnknownVersion || isUnknownPackageVersion(pkg.Version)
	pkg.Pinned = pkg.Pinned || updateDetail.Pinned
	pkg.AutoUpdate = packageAutoUpdateEnabled(state, pkg)
	if pkg.ActionBackend == "" {
		pkg.ActionBackend = effectiveManager
	}
	if effectiveManager == managerWinget {
		suppressRejectedWingetOffer(state, &pkg)
		quarantineKnownWingetIdentityConflict(&pkg)
	}
	return pkg, true
}

// WinGet/Brave issue #57356 documents the stable Brave Origin ARP entry being
// correlated with the Nightly manifest. Keep that ambiguous identity visible
// for diagnostics, but never authorize a cross-channel mutation from it.
func quarantineKnownWingetIdentityConflict(pkg *Package) {
	if !strings.EqualFold(strings.TrimSpace(pkg.ID), "Brave.BraveOrigin.Nightly") ||
		!strings.EqualFold(strings.TrimSpace(pkg.Name), "Brave Origin") {
		return
	}
	pkg.IdentityConflict = true
	pkg.IdentityReason = "WinGet correlated the stable Brave Origin installation with Brave Origin Nightly. Automatic updates are disabled to avoid changing release channels."
	pkg.AvailableVersion = ""
	pkg.UpdateAvailable = false
	pkg.UpdateSupported = false
	pkg.AutoUpdate = false
}
