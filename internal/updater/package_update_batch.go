package updater

import (
	"context"
	"fmt"
	"strings"
)

type elevatedPackageUpdateBatchRunnerFunc func(ctx context.Context, packages []Package, reportProgress func(int, Package)) ([]UpdateResult, CommandResult)

var elevatedPackageUpdateBatchRunner elevatedPackageUpdateBatchRunnerFunc = runElevatedPackageUpdateBatch

// Tests may replace this hook. A nil hook selects the production capability
// detector once per batch plan so every package in one job uses one decision.
var elevatedPackageUpdateBatchEligible func(Package) bool

type elevatedPackageUpdateBatchCapability struct {
	RequiresElevation          bool
	SameUserElevationAvailable bool
}

func packageEligibleForElevatedUpdateBatch(candidate Package, capability elevatedPackageUpdateBatchCapability) bool {
	if !capability.RequiresElevation {
		return false
	}
	switch candidate.Manager {
	case managerChoco:
		return true
	case managerWinget:
		return capability.SameUserElevationAvailable
	default:
		return false
	}
}

func planElevatedPackageUpdateBatch(packages []Package, isBatchEligible func(Package) bool) ([]Package, []Package) {
	if isBatchEligible == nil {
		capability := detectElevatedPackageUpdateBatchCapability()
		if capability.RequiresElevation && !capability.SameUserElevationAvailable && packageUpdateBatchIncludesManager(packages, managerWinget) {
			appLog("Bulk WinGet elevation is unavailable because UAC would require alternate administrator credentials; WinGet updates will remain in the current user context.")
		}
		isBatchEligible = func(candidate Package) bool {
			return packageEligibleForElevatedUpdateBatch(candidate, capability)
		}
	}
	batchPackages := make([]Package, 0, len(packages))
	remainingPackages := make([]Package, 0, len(packages))
	for _, candidate := range packages {
		if isBatchEligible(candidate) {
			batchPackages = append(batchPackages, candidate)
		} else {
			remainingPackages = append(remainingPackages, candidate)
		}
	}
	// A multi-package job may contain one WinGet/Chocolatey package plus Store
	// packages. Use the one worker for that eligible package while keeping a
	// true single-package update on its existing path.
	if len(packages) < 2 || len(batchPackages) == 0 {
		return nil, append([]Package(nil), packages...)
	}
	return batchPackages, remainingPackages
}

func packageUpdateQueueStopReason(result CommandResult) string {
	switch result.Code {
	case commandTimeoutCode:
		return "the previous package update timed out"
	case commandCancelledCode:
		return "the update batch was cancelled"
	case 2147943623: // HRESULT_FROM_WIN32(ERROR_CANCELLED)
		return "the previous package installer was cancelled"
	}

	output := strings.ToLower(result.Stdout + "\n" + result.Stderr)
	for _, cancellationSignal := range []string{
		"exit code: 1223",
		"exitcode: 1223",
		"canceled by the user",
		"cancelled by the user",
		"canceled the installation",
		"cancelled the installation",
		"durch den benutzer abgebrochen",
		"sie haben die installation abgebrochen",
	} {
		if strings.Contains(output, cancellationSignal) {
			return "the previous package installer was cancelled"
		}
	}
	return ""
}

func skippedPackageUpdateResults(packages []Package, reason string) []UpdateResult {
	results := make([]UpdateResult, 0, len(packages))
	for _, pkg := range packages {
		key := normalizedJobPackageKey(pkg)
		if key == "" {
			key = packageKey(pkg.Manager, pkg.ID)
		}
		results = append(results, UpdateResult{
			Key: key,
			Result: CommandResult{
				Code:    commandSkippedCode,
				Command: fmt.Sprintf("update %s:%s", pkg.Manager, pkg.ID),
				Stdout:  "Skipped because " + strings.TrimSpace(reason) + ".",
			},
		})
	}
	return results
}

// completePartialBatchResults gives every package of a batch exactly one
// result. Packages the worker never reported are skipped when the batch
// stopped on purpose, and otherwise fail with the batch's own result, for
// example when the elevated worker died partway through.
func completePartialBatchResults(packages []Package, results []UpdateResult, batchResult CommandResult) []UpdateResult {
	if len(results) == 0 {
		return failedBatchUpdateResults(packages, batchResult)
	}
	reported := map[string]bool{}
	for _, result := range results {
		reported[result.Key] = true
	}
	var missing []Package
	for _, pkg := range packages {
		if !reported[pkg.Key] && !reported[normalizedJobPackageKey(pkg)] {
			missing = append(missing, pkg)
		}
	}
	if len(missing) == 0 {
		return results
	}
	if reason := packageUpdateQueueStopReason(batchResult); reason != "" {
		return append(results, skippedPackageUpdateResults(missing, reason)...)
	}
	return append(results, failedBatchUpdateResults(missing, batchResult)...)
}

func packageUpdateBatchIncludesManager(packages []Package, manager string) bool {
	for _, pkg := range packages {
		if pkg.Manager == manager {
			return true
		}
	}
	return false
}
