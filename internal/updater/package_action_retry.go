package updater

import "strings"

const (
	wingetErrorNoApplicationsFound = 0x8A150014
	wingetErrorUpdateNotApplicable = 0x8A15002B
)

func shouldTryAlternatePackageTarget(result CommandResult) bool {
	if !isRetryablePackageActionFailure(result) {
		return false
	}
	normalizedOutput := normalizedCommandOutput(result)
	if strings.Contains(normalizedOutput, "requires explicit user confirmation") {
		return false
	}
	if wingetInstalledPackageMissing(result) {
		return true
	}
	return outputContainsAny(normalizedOutput, []string{
		"no applicable upgrade",
		"no available upgrade",
		"kein anwendbares upgrade",
		"kein verfügbares upgrade",
		"keine neueren paketversionen",
		"no installed package found",
		"no package found",
		"no product found",
		"product not found",
		"not found",
		"unable to find package",
		"unable to find",
		"could not find installed product metadata",
		"no installed package matching",
		"not recognized",
		"does not match any installed package",
	})
}

func wingetOutputReportsNoApplicableUpgrade(normalizedOutput string) bool {
	return outputContainsAny(normalizedOutput, []string{
		"no applicable upgrade",
		"no available upgrade",
		"no upgrade available",
		"kein anwendbares upgrade",
		"kein verfügbares upgrade",
		"kein upgrade verfügbar",
	})
}

func wingetActionabilityCandidate(result CommandResult) bool {
	return !result.OK && !commandResultSkipped(result) && result.Code != commandCancelledCode && result.Code != commandTimeoutCode
}

// wingetUpdateNotApplicable reports that WinGet found the installed package but
// rejected the offered version. It is safe to act on per attempt because a
// different target identifier cannot make the same version applicable.
func wingetUpdateNotApplicable(result CommandResult) bool {
	if !wingetActionabilityCandidate(result) {
		return false
	}
	return wingetExitCodeMatches(result.Code, wingetErrorUpdateNotApplicable) || wingetOutputReportsNoApplicableUpgrade(normalizedCommandOutput(result))
}

// wingetInstalledPackageMissing reports that WinGet found no installed package
// for the attempted identifier. WinGet uses the same code and text for a stale
// offer (package uninstalled) and for a wrong identifier, so callers must only
// treat it as a stale offer after every alternate target has missed as well.
func wingetInstalledPackageMissing(result CommandResult) bool {
	if !wingetActionabilityCandidate(result) {
		return false
	}
	if wingetExitCodeMatches(result.Code, wingetErrorNoApplicationsFound) {
		return true
	}
	return outputContainsAny(normalizedCommandOutput(result), []string{
		"no installed package found",
		"no installed package matching",
		"kein installiertes paket gefunden",
		"es wurde kein installiertes paket gefunden",
	})
}

func wingetExitCodeMatches(exitCode int, expected uint32) bool {
	return uint32(exitCode) == expected
}

func wingetOutputReportsInstallerTechnologyMismatch(normalizedOutput string) bool {
	return outputContainsAny(normalizedOutput, []string{
		"installation technology",
		"install technology",
		"installer technology",
		"installationstechnologie",
		"uninstall the package",
		"deinstallieren sie das paket",
	})
}

func shouldRetryWingetIncludeUnknown(result CommandResult) bool {
	if !isRetryablePackageActionFailure(result) {
		return false
	}
	normalizedOutput := normalizedCommandOutput(result)
	if outputContainsAny(normalizedOutput, []string{"--include-unknown", "include unknown", "include-unknown"}) {
		return true
	}
	if (strings.Contains(normalizedOutput, "unknown") || strings.Contains(normalizedOutput, "unbekannt")) &&
		(strings.Contains(normalizedOutput, "version") || strings.Contains(normalizedOutput, "installed package")) {
		return true
	}
	return false
}

func shouldRetryWingetIncludePinned(result CommandResult) bool {
	if !isRetryablePackageActionFailure(result) {
		return false
	}
	normalizedOutput := normalizedCommandOutput(result)
	if outputContainsAny(normalizedOutput, []string{"--include-pinned", "include-pinned", "include pinned"}) {
		return true
	}
	return outputContainsAny(normalizedOutput, []string{"pinning", "pinned"})
}

func shouldRetryStoreUpdateWithoutApply(result CommandResult) bool {
	if !isRetryablePackageActionFailure(result) {
		return false
	}
	normalizedOutput := normalizedCommandOutput(result)
	if !strings.Contains(normalizedOutput, "apply") {
		return false
	}
	return outputContainsAny(normalizedOutput, []string{
		"unrecognized option",
		"unknown option",
		"unknown argument",
		"unrecognized argument",
		"unrecognized command or argument",
		"argument not recognized",
		"option not recognized",
		"parameter not recognized",
		"argumentname wurde",
		"nicht erkannt",
		"unbekannte option",
		"unbekanntes argument",
	})
}

func isRetryablePackageActionFailure(result CommandResult) bool {
	return !result.OK && result.Code != commandCancelledCode && result.Code != commandTimeoutCode
}
