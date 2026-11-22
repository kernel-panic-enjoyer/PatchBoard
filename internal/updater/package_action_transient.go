package updater

// isTransientPackageManagerFailure reports that the same command may succeed
// if it is run again shortly.
func isTransientPackageManagerFailure(manager string, result CommandResult) bool {
	return !result.OK && packageResultOutcome(manager, result) == outcomeTransient
}

// isWingetSourceFailure reports that WinGet's source data looked missing,
// stale or broken, so refreshing the source may help.
func isWingetSourceFailure(result CommandResult) bool {
	return !result.OK && packageResultOutcome(managerWinget, result) == outcomeSourceFailure
}
