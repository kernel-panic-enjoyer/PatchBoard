package updater

import "strings"

func requireExplicitPinnedUpdate(commandResult CommandResult) CommandResult {
	return appendCommandStderr(commandResult, "Winget reported that this package is pinned. Updating it requires explicit user confirmation.")
}

func requireExplicitUnknownVersionUpdate(commandResult CommandResult) CommandResult {
	return appendCommandStderr(commandResult, "Winget reported an unknown installed version. Updating this package requires explicit user confirmation.")
}

// requireExplicitWingetRepair is reached only after WinGet reported an
// installer technology mismatch and the forced retries did not fix it.
func requireExplicitWingetRepair(commandResult CommandResult) CommandResult {
	return appendCommandStderr(commandResult, "Winget reported an installer technology mismatch. Automatic uninstall/reinstall is not performed; use an explicit repair action if you want to replace this package manually.")
}

func appendCommandStderr(commandResult CommandResult, appendedMessage string) CommandResult {
	stderr := strings.TrimRight(commandResult.Stderr, "\r\n")
	if strings.TrimSpace(stderr) != "" {
		commandResult.Stderr = stderr + "\n" + appendedMessage
	} else {
		commandResult.Stderr = appendedMessage
	}
	return commandResult
}

func normalizedCommandOutput(commandResult CommandResult) string {
	return strings.ToLower(commandResult.Stdout + "\n" + commandResult.Stderr)
}

func outputContainsAny(output string, substrings []string) bool {
	for _, substring := range substrings {
		if strings.Contains(output, substring) {
			return true
		}
	}
	return false
}
