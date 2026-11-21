package updater

import (
	"context"
	"fmt"
	"strings"
)

func wingetSourceArg(packageManager string) string {
	if packageManager == managerStore {
		return sourceMSStore
	}
	return sourceWinget
}

var wingetInstallUpgradeFlags = []string{
	"--accept-package-agreements",
	"--accept-source-agreements",
	"--disable-interactivity",
	"--silent",
}

func wingetPackageCommand(action, source, packageID string, extraArgs ...string) []string {
	args := []string{action}
	if packageID != "" && isSafePackageID(packageID) {
		args = append(args, "--id", packageID, "--exact")
	} else {
		args = append(args, packageID)
	}
	args = append(args, "--source", source)
	args = append(args, extraArgs...)
	args = append(args, wingetInstallUpgradeFlags...)
	return managerCommand(managerWinget, args...)
}

func wingetPackageNameCommand(action, source, packageName string, extraArgs ...string) []string {
	args := []string{action, "--name", packageName, "--exact", "--source", source}
	args = append(args, extraArgs...)
	args = append(args, wingetInstallUpgradeFlags...)
	return managerCommand(managerWinget, args...)
}

func wingetInstallForceArgs(force bool) []string {
	if force {
		return []string{"--force"}
	}
	return nil
}

func wingetInstallCommand(packageManager, packageID string, force bool) []string {
	return wingetPackageCommand("install", wingetSourceArg(packageManager), packageID, wingetInstallForceArgs(force)...)
}

func wingetUpgradeCommand(packageManager, packageID string, extraArgs ...string) []string {
	return wingetPackageCommand("upgrade", wingetSourceArg(packageManager), packageID, extraArgs...)
}

func wingetMSStoreProductIDUpgradeCommand(productID string) []string {
	return wingetPackageCommand("upgrade", sourceMSStore, productID)
}

func wingetMSStoreProductIDUpgradeAvailableCommand(productID string) []string {
	return managerCommand(managerWinget, "list", "--upgrade-available", "--id", productID, "--exact", "--source", sourceMSStore, "--accept-source-agreements", "--disable-interactivity")
}

func wingetUpgradeNameCommand(packageManager, packageName string, extraArgs ...string) []string {
	return wingetPackageNameCommand("upgrade", wingetSourceArg(packageManager), packageName, extraArgs...)
}

func wingetInstallNameCommand(packageManager, packageName string, force bool) []string {
	return wingetPackageNameCommand("install", wingetSourceArg(packageManager), packageName, wingetInstallForceArgs(force)...)
}

func runWingetUpgradePackageWithInstallFallbackContext(ctx context.Context, packageManager string, pkg Package) CommandResult {
	// "Update not applicable" is final per attempt. "No installed package found"
	// is only final once every ID target and the exact-name fallback missed,
	// because WinGet reports a wrong identifier the same way. The last raw
	// attempt is tracked so earlier misses in the merged output cannot
	// reclassify a different final failure as a stale offer.
	var finalAttempt CommandResult
	targetResult := runPackageUpdateCandidates(ctx, wingetUpdateTargetCandidates(pkg), "winget target", func(target string) CommandResult {
		finalAttempt = skipWingetUpdateWhen(wingetUpdateNotApplicable, runWingetUpgradeTargetWithInstallFallbackContext(ctx, packageManager, target, pkg.AllowUnknownVersionUpdate, pkg.AllowPinnedUpdate))
		return finalAttempt
	})
	if targetResult.OK || commandResultSkipped(targetResult) || ctx.Err() != nil {
		return targetResult
	}
	fallbackName := ""
	if shouldTryAlternatePackageTarget(targetResult) {
		fallbackName = wingetNameFallbackTarget(pkg)
	}
	if fallbackName == "" {
		return skipWingetUpdateIfInstalledPackageMissing(targetResult, finalAttempt)
	}
	appLog("Winget update targets for %s missed; trying exact package name %q.", updateJobPackageName(pkg), fallbackName)
	nameResult := skipWingetUpdateWhen(wingetUpdateNotApplicable, runWingetUpgradeNameWithInstallFallbackContext(ctx, packageManager, fallbackName, pkg.AllowUnknownVersionUpdate, pkg.AllowPinnedUpdate))
	mergedResult := mergeCommandAttemptsWithFinalResult(targetResult, nameResult, "winget name fallback")
	if ctx.Err() != nil {
		return mergedResult
	}
	return skipWingetUpdateIfInstalledPackageMissing(mergedResult, nameResult)
}

func skipWingetUpdateWhen(noLongerActionable func(CommandResult) bool, result CommandResult) CommandResult {
	if !noLongerActionable(result) {
		return result
	}
	return markWingetUpdateNoLongerActionable(result)
}

func skipWingetUpdateIfInstalledPackageMissing(result, finalAttempt CommandResult) CommandResult {
	if result.OK || !wingetInstalledPackageMissing(finalAttempt) {
		return result
	}
	return markWingetUpdateNoLongerActionable(result)
}

func markWingetUpdateNoLongerActionable(result CommandResult) CommandResult {
	originalCode := result.Code
	result.OK = false
	result.Code = commandSkippedCode
	diagnostic := fmt.Sprintf("%s (original code %d).", rejectedWingetOfferDiagnostic, originalCode)
	if strings.TrimSpace(result.Stdout) == "" {
		result.Stdout = diagnostic
	} else {
		result.Stdout = strings.TrimRight(result.Stdout, "\r\n") + "\n" + diagnostic
	}
	return result
}

func runWingetUpgradeTargetWithInstallFallbackContext(ctx context.Context, packageManager, packageID string, allowUnknownVersionRetry bool, allowPinnedRetry bool) CommandResult {
	return runWingetUpgradePlanWithFallbacks(ctx, wingetUpgradePlan{
		TargetDescription:        fmt.Sprintf("%s:%s", packageManager, packageID),
		AllowUnknownVersionRetry: allowUnknownVersionRetry,
		AllowPinnedRetry:         allowPinnedRetry,
		BuildUpgradeCommand: func(extraArgs ...string) []string {
			return wingetUpgradeCommand(packageManager, packageID, extraArgs...)
		},
		BuildForcedInstallCommand:     func() []string { return wingetInstallCommand(packageManager, packageID, true) },
		UnknownVersionRetryMergeLabel: "winget include-unknown retry",
		PinnedRetryMergeLabel:         "winget include-pinned retry",
	})
}

func runWingetUpgradeNameWithInstallFallbackContext(ctx context.Context, packageManager, packageName string, retryAllowances ...bool) CommandResult {
	allowUnknownVersionRetry := len(retryAllowances) > 0 && retryAllowances[0]
	allowPinnedRetry := len(retryAllowances) > 1 && retryAllowances[1]
	return runWingetUpgradePlanWithFallbacks(ctx, wingetUpgradePlan{
		TargetDescription:        fmt.Sprintf("%s name %q", packageManager, packageName),
		AllowUnknownVersionRetry: allowUnknownVersionRetry,
		AllowPinnedRetry:         allowPinnedRetry,
		BuildUpgradeCommand: func(extraArgs ...string) []string {
			return wingetUpgradeNameCommand(packageManager, packageName, extraArgs...)
		},
		BuildForcedInstallCommand:     func() []string { return wingetInstallNameCommand(packageManager, packageName, true) },
		UnknownVersionRetryMergeLabel: "winget include-unknown name retry",
		PinnedRetryMergeLabel:         "winget include-pinned name retry",
	})
}

type wingetUpgradePlan struct {
	TargetDescription             string
	AllowUnknownVersionRetry      bool
	AllowPinnedRetry              bool
	BuildUpgradeCommand           func(extraArgs ...string) []string
	BuildForcedInstallCommand     func() []string
	UnknownVersionRetryMergeLabel string
	PinnedRetryMergeLabel         string
}

type wingetUpgradeRetryPlan struct {
	CommandFlag                string
	RetryLogMessage            string
	MergeLabel                 string
	AllowedByPolicy            bool
	ShouldRetry                func(CommandResult) bool
	BuildConsentRequiredResult func(CommandResult) CommandResult
}

func runWingetUpgradePlanWithFallbacks(ctx context.Context, plan wingetUpgradePlan) CommandResult {
	result := runPackageActionCommand(ctx, managerWinget, packageActionTimeout, plan.BuildUpgradeCommand()...)
	activeRetryFlags := []string{}

	for _, retryPlan := range []wingetUpgradeRetryPlan{
		{
			CommandFlag:                "--include-unknown",
			RetryLogMessage:            "Winget upgrade for %s reported an unknown installed version; retrying with --include-unknown.",
			MergeLabel:                 plan.UnknownVersionRetryMergeLabel,
			AllowedByPolicy:            plan.AllowUnknownVersionRetry,
			ShouldRetry:                shouldRetryWingetIncludeUnknown,
			BuildConsentRequiredResult: requireExplicitUnknownVersionUpdate,
		},
		{
			CommandFlag:                "--include-pinned",
			RetryLogMessage:            "Winget upgrade for %s reported a pinned package; retrying with --include-pinned.",
			MergeLabel:                 plan.PinnedRetryMergeLabel,
			AllowedByPolicy:            plan.AllowPinnedRetry,
			ShouldRetry:                shouldRetryWingetIncludePinned,
			BuildConsentRequiredResult: requireExplicitPinnedUpdate,
		},
	} {
		var stopAfterRetry bool
		result, activeRetryFlags, stopAfterRetry = runWingetUpgradeRetryIfNeeded(ctx, plan, result, retryPlan, activeRetryFlags)
		if stopAfterRetry {
			return result
		}
	}

	if forceRepairReason := wingetForceRepairReason(result); forceRepairReason != "" {
		if ctx.Err() != nil {
			return result
		}
		appLog("Winget upgrade for %s reported %s; retrying with --force.", plan.TargetDescription, forceRepairReason)
		forceArgs := append(append([]string{}, activeRetryFlags...), "--force")
		forceUpgradeResult := runPackageActionCommand(ctx, managerWinget, packageActionTimeout, plan.BuildUpgradeCommand(forceArgs...)...)
		result = mergeCommandAttemptsWithFinalResult(result, forceUpgradeResult, "winget force upgrade retry")
		if forceUpgradeResult.OK || ctx.Err() != nil {
			return result
		}
		if forceRepairReason := wingetForceRepairReason(forceUpgradeResult); forceRepairReason != "" && plan.BuildForcedInstallCommand != nil {
			appLog("Winget forced upgrade for %s still reported %s; retrying with exact forced install.", plan.TargetDescription, forceRepairReason)
			forcedInstallResult := runPackageActionCommand(ctx, managerWinget, packageActionTimeout, plan.BuildForcedInstallCommand()...)
			result = mergeCommandAttemptsWithFinalResult(result, forcedInstallResult, "winget forced install fallback")
			if forcedInstallResult.OK || ctx.Err() != nil {
				return result
			}
		}
		return requireExplicitWingetRepair(result)
	}
	return result
}

func wingetForceRepairReason(result CommandResult) string {
	if result.OK {
		return ""
	}
	normalizedOutput := normalizedCommandOutput(result)
	if wingetOutputReportsInstallerTechnologyMismatch(normalizedOutput) {
		return "an installer technology mismatch"
	}
	return ""
}

func runWingetUpgradeRetryIfNeeded(ctx context.Context, plan wingetUpgradePlan, currentResult CommandResult, retryPlan wingetUpgradeRetryPlan, activeRetryFlags []string) (CommandResult, []string, bool) {
	if !retryPlan.ShouldRetry(currentResult) {
		return currentResult, activeRetryFlags, false
	}
	if ctx.Err() != nil {
		return currentResult, activeRetryFlags, true
	}
	if !retryPlan.AllowedByPolicy {
		return retryPlan.BuildConsentRequiredResult(currentResult), activeRetryFlags, true
	}
	appLog(retryPlan.RetryLogMessage, plan.TargetDescription)
	retryFlags := append(append([]string{}, activeRetryFlags...), retryPlan.CommandFlag)
	retryResult := runPackageActionCommand(ctx, managerWinget, packageActionTimeout, plan.BuildUpgradeCommand(retryFlags...)...)
	currentResult = mergeCommandAttemptsWithFinalResult(currentResult, retryResult, retryPlan.MergeLabel)
	return currentResult, retryFlags, retryResult.OK || ctx.Err() != nil
}
