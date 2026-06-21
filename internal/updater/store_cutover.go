package updater

import "strings"

const (
	storeLegacyDetectorRollbackFlag = "UPDATER_STORE_LEGACY_DETECTOR"
	storeCutoverDisableScanFlag     = "UPDATER_STORE_DISABLE_TRANSACTIONAL_SCAN"
	storeAutoUpdateKeySeparator     = "~"
)

func storeLegacyDetectorRollbackEnabled() bool {
	return featureFlagEnabled(storeLegacyDetectorRollbackFlag)
}

func storeNewDetectorActive() bool {
	return !storeLegacyDetectorRollbackEnabled()
}

func canonicalStoreAutoUpdateKey(userSID, pfn string) string {
	userSID = strings.TrimSpace(userSID)
	pfn = strings.TrimSpace(pfn)
	if userSID == "" || pfn == "" {
		return ""
	}
	return packageKey(managerStore, strings.ToLower(userSID)+storeAutoUpdateKeySeparator+strings.ToLower(pfn))
}

func splitCanonicalStoreAutoUpdateKey(key string) (string, string, bool) {
	manager, id, err := splitPackageKey(key)
	if err != nil || manager != managerStore {
		return "", "", false
	}
	userSID, pfn, ok := strings.Cut(id, storeAutoUpdateKeySeparator)
	userSID = strings.TrimSpace(userSID)
	pfn = strings.TrimSpace(pfn)
	if !ok || userSID == "" || pfn == "" {
		return "", "", false
	}
	return userSID, pfn, true
}

func storePackageAutoUpdateKey(pkg Package) string {
	pfn := storeInstalledPackageFamilyName(pkg)
	if pfn == "" {
		return ""
	}
	userSID, err := currentUserSID()
	if err != nil {
		return ""
	}
	return canonicalStoreAutoUpdateKey(userSID, pfn)
}

func storePackagePublicKey(pkg Package) string {
	pfn := storeInstalledPackageFamilyName(pkg)
	if pfn == "" {
		return pkg.Key
	}
	return packageKey(managerStore, pfn)
}
