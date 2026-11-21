package updater

import "strings"

// shouldTryAlternatePackageTarget reports that the manager found no
// installed package for the identifier just tried, so another identifier
// for the same package may still match.
func shouldTryAlternatePackageTarget(result CommandResult) bool {
	if !isRetryablePackageActionFailure(result) {
		return false
	}
	if result.Outcome != outcomeUnclassified {
		return result.Outcome == outcomeNotInstalled
	}
	return storeCLIReportsTargetMiss(normalizedCommandOutput(result))
}

// storeCLIReportsTargetMiss reads Store CLI output, which has no documented
// exit codes. Phase 4 of the modernization brief replaces it.
func storeCLIReportsTargetMiss(normalizedOutput string) bool {
	if strings.Contains(normalizedOutput, "requires explicit user confirmation") {
		return false
	}
	return outputContainsAny(normalizedOutput, []string{
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

func wingetActionabilityCandidate(result CommandResult) bool {
	return !result.OK && !commandResultSkipped(result) && result.Code != commandCancelledCode && result.Code != commandTimeoutCode
}

// wingetUpdateNotApplicable reports that WinGet found the installed package but
// rejected the offered version. It is safe to act on per attempt because a
// different target identifier cannot make the same version applicable.
func wingetUpdateNotApplicable(result CommandResult) bool {
	return wingetActionabilityCandidate(result) && packageResultOutcome(managerWinget, result) == outcomeNotApplicable
}

// wingetInstalledPackageMissing reports that WinGet found no installed package
// for the attempted identifier. WinGet uses the same code and text for a stale
// offer (package uninstalled) and for a wrong identifier, so callers must only
// treat it as a stale offer after every alternate target has missed as well.
func wingetInstalledPackageMissing(result CommandResult) bool {
	return wingetActionabilityCandidate(result) && packageResultOutcome(managerWinget, result) == outcomeNotInstalled
}

func shouldRetryWingetIncludeUnknown(result CommandResult) bool {
	return isRetryablePackageActionFailure(result) && packageResultOutcome(managerWinget, result) == outcomeNeedsUnknownConsent
}

func shouldRetryWingetIncludePinned(result CommandResult) bool {
	return isRetryablePackageActionFailure(result) && packageResultOutcome(managerWinget, result) == outcomeNeedsPinnedConsent
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
