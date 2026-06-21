package updater

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

type StorePackagedInventoryComparison struct {
	MissingNativePFNs   []string `json:"missing_native_pfns,omitempty"`
	MissingLegacyPFNs   []string `json:"missing_legacy_pfns,omitempty"`
	VersionDifferences  []string `json:"version_differences,omitempty"`
	ScopeDifferences    []string `json:"scope_differences,omitempty"`
	ClassificationNotes []string `json:"classification_notes,omitempty"`
	NativeErrors        []string `json:"native_errors,omitempty"`
}

func compareStorePackagedInventory(native StorePackagedAppInventory, legacy []Package, nativeResult CommandResult) StorePackagedInventoryComparison {
	comparison := StorePackagedInventoryComparison{}
	if !nativeResult.OK {
		comparison.NativeErrors = append(comparison.NativeErrors, strings.TrimSpace(nativeResult.Stderr))
	}
	comparison.NativeErrors = append(comparison.NativeErrors, native.Errors...)
	nativeByPFN := map[string]StorePackagedAppFamily{}
	for _, family := range native.Families {
		nativeByPFN[family.Identity.PackageFamilyName] = family
	}
	legacyByPFN := map[string]Package{}
	for _, pkg := range legacy {
		pfn := strings.TrimSpace(pkg.Match)
		if pfn == "" {
			continue
		}
		legacyByPFN[pfn] = pkg
	}
	for pfn, legacyPkg := range legacyByPFN {
		family, ok := nativeByPFN[pfn]
		if !ok {
			comparison.MissingNativePFNs = append(comparison.MissingNativePFNs, pfn)
			comparison.ScopeDifferences = append(comparison.ScopeDifferences, pfn+" was present in legacy AppX inventory but not current-user native inventory")
			continue
		}
		if legacyPkg.Version != "" && family.Primary.Version.String() != legacyPkg.Version {
			comparison.VersionDifferences = append(comparison.VersionDifferences, fmt.Sprintf("%s native=%s legacy=%s", pfn, family.Primary.Version.String(), legacyPkg.Version))
		}
		if !family.ProductLike {
			comparison.ClassificationNotes = append(comparison.ClassificationNotes, pfn+" is not product-like in native inventory")
		}
	}
	for pfn := range nativeByPFN {
		if _, ok := legacyByPFN[pfn]; !ok {
			comparison.MissingLegacyPFNs = append(comparison.MissingLegacyPFNs, pfn)
		}
	}
	sort.Strings(comparison.MissingNativePFNs)
	sort.Strings(comparison.MissingLegacyPFNs)
	sort.Strings(comparison.VersionDifferences)
	sort.Strings(comparison.ScopeDifferences)
	sort.Strings(comparison.ClassificationNotes)
	sort.Strings(comparison.NativeErrors)
	return comparison
}

func storePackagedInventoryComparisonResult(comparison StorePackagedInventoryComparison) CommandResult {
	data, err := json.Marshal(comparison)
	if err != nil {
		return validationCommandResult("native Store inventory comparison", err)
	}
	return CommandResult{
		OK:      len(comparison.NativeErrors) == 0,
		Code:    comparisonCode(comparison),
		Command: "native Store inventory comparison",
		Stdout:  string(data),
	}
}

func comparisonCode(comparison StorePackagedInventoryComparison) int {
	if len(comparison.NativeErrors) > 0 {
		return 1
	}
	return 0
}
