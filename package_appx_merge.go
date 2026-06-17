package main

import "strings"

func mergeStoreAppxPackages(packages, appxPackages []Package) []Package {
	seen := map[string]int{}
	markSeen := func(index int, value string) {
		normalized := normalizePackageIdentity(value)
		if normalized != "" {
			seen[normalized] = index
		}
	}
	for i, pkg := range packages {
		if pkg.Manager != managerStore {
			continue
		}
		markSeen(i, pkg.ID)
		markSeen(i, pkg.Name)
		markSeen(i, pkg.Match)
	}
	findDuplicate := func(pkg Package) (int, bool) {
		for _, value := range []string{pkg.ID, pkg.Name, pkg.Match} {
			normalized := normalizePackageIdentity(value)
			if normalized == "" {
				continue
			}
			if index, ok := seen[normalized]; ok {
				return index, true
			}
		}
		return -1, false
	}
	for _, pkg := range appxPackages {
		if index, ok := findDuplicate(pkg); ok {
			packages[index] = mergeStoreDuplicatePackage(packages[index], pkg)
			markSeen(index, packages[index].ID)
			markSeen(index, packages[index].Name)
			markSeen(index, packages[index].Match)
			continue
		}
		index := len(packages)
		packages = append(packages, pkg)
		markSeen(index, pkg.ID)
		markSeen(index, pkg.Name)
		markSeen(index, pkg.Match)
	}
	return packages
}

func mergeStoreDuplicatePackage(existing, appx Package) Package {
	if appx.Name != "" && (existing.Name == "" || existing.Name == existing.ID || appx.ActionBackend == backendStoreCLIResolved) {
		existing.Name = appx.Name
	}
	if appx.Version != "" {
		existing.Version = appx.Version
	}
	if appx.Match != "" {
		existing.Match = appx.Match
	}
	if appx.ActionBackend == backendStoreCLIResolved {
		existing.ID = appx.ID
		existing.Key = appx.Key
		existing.Source = appx.Source
		existing.ActionBackend = appx.ActionBackend
		existing.UpdateSupported = true
	}
	if appx.UpdateAvailable {
		existing.AvailableVersion = appx.AvailableVersion
		existing.UpdateAvailable = true
	}
	if appx.AutoUpdate {
		existing.AutoUpdate = true
	}
	existing.Installed = existing.Installed || appx.Installed
	return existing
}

func storeUpdateVersionForPackage(pkg Package, updates map[string]string) string {
	available, _ := storeUpdateForPackage(pkg, updates)
	return available
}

func storeUpdateForPackage(pkg Package, updates map[string]string) (string, string) {
	if pkg.Manager != managerStore || len(updates) == 0 {
		return "", ""
	}
	candidates := []string{pkg.Name, pkg.ID, stableStoreActionID(pkg.ID), pkg.Match, stableStoreActionID(pkg.Match)}
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if available := updates[packageKey(managerStore, strings.ToLower(candidate))]; available != "" {
			return available, candidate
		}
	}
	return "", ""
}

func applyStoreUpdateVersion(pkg Package, updates map[string]string, storeAvailable bool) Package {
	available, target := storeUpdateForPackage(pkg, updates)
	if available == "" {
		return pkg
	}
	if !versionGreater(available, pkg.Version) {
		pkg.AvailableVersion = ""
		pkg.UpdateAvailable = false
		return pkg
	}
	target = strings.TrimSpace(target)
	if target == "" {
		target = stableStoreActionID(pkg.ID)
	}
	if target != "" && target != pkg.ID {
		pkg.ID = target
		pkg.Key = packageKey(managerStore, target)
	}
	pkg.AvailableVersion = available
	pkg.UpdateAvailable = true
	pkg.UpdateSupported = true
	if pkg.ActionBackend == "" || pkg.ActionBackend == backendAppXInventory {
		if storeAvailable {
			pkg.ActionBackend = backendStoreCLIResolved
		} else {
			pkg.ActionBackend = backendWingetMSStoreFallback
		}
	}
	return pkg
}
