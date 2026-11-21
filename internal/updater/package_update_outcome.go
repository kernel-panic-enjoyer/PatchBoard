package updater

// packageAttemptOutcome is what one package-manager command attempt meant.
// It is decided once, from the raw attempt, by classifyPackageAttempt. Every
// retry, fallback and skip decision reads it instead of searching command
// text, so the output of an earlier attempt can never change what a later
// one means. docs/architecture/package-update-outcomes.md is the decision
// table.
type packageAttemptOutcome string

const (
	outcomeUnclassified packageAttemptOutcome = ""
	// outcomeUpdated: the command succeeded, possibly with a restart pending.
	outcomeUpdated packageAttemptOutcome = "updated"
	// outcomeNotApplicable: the package is installed but the manager has no
	// update it can apply. Retrying another identifier cannot change that.
	outcomeNotApplicable packageAttemptOutcome = "not_applicable"
	// outcomeNotInstalled: the manager found no installed package for this
	// identifier. Another identifier for the same package may still match.
	outcomeNotInstalled packageAttemptOutcome = "not_installed"
	// outcomeNeedsUnknownConsent: the installed version is unknown and the
	// upgrade needs --include-unknown, which the user must allow.
	outcomeNeedsUnknownConsent packageAttemptOutcome = "needs_unknown_version_consent"
	// outcomeNeedsPinnedConsent: the package is pinned and the upgrade needs
	// --include-pinned, which the user must allow.
	outcomeNeedsPinnedConsent packageAttemptOutcome = "needs_pinned_consent"
	// outcomeTechnologyMismatch: the update uses another installer technology.
	outcomeTechnologyMismatch packageAttemptOutcome = "installer_technology_mismatch"
	// outcomeSourceFailure: the package source data is missing, stale or
	// broken; refreshing the source may help.
	outcomeSourceFailure packageAttemptOutcome = "source_failure"
	// outcomeTransient: another install was running, a file was locked or
	// the network failed; the same command may succeed shortly after.
	outcomeTransient packageAttemptOutcome = "transient"
	// outcomeInstallerCancelled: the installer or its UAC prompt was cancelled.
	outcomeInstallerCancelled packageAttemptOutcome = "installer_cancelled"
	// outcomeCancelled and outcomeTimedOut: PatchBoard stopped the command.
	outcomeCancelled packageAttemptOutcome = "cancelled"
	outcomeTimedOut  packageAttemptOutcome = "timed_out"
	// outcomeFailed: any other failure. Nothing is retried.
	outcomeFailed packageAttemptOutcome = "failed"
)

// WinGet exit codes, from the official list in microsoft/winget-cli
// doc/windows/package-manager/winget/returnCodes.md (checked 2026-11-21,
// last changed 2026-10-01).
const (
	wingetErrorInternal                    = 0x8A150001
	wingetErrorDownloadFailed              = 0x8A150008
	wingetErrorSourcesInvalid              = 0x8A15000B
	wingetErrorSourceDataMissing           = 0x8A15000F
	wingetErrorInstallerHashMismatch       = 0x8A150011
	wingetErrorNoApplicationsFound         = 0x8A150014
	wingetErrorNoSourcesDefined            = 0x8A150015
	wingetErrorUpdateNotApplicable         = 0x8A15002B
	wingetErrorSourceDataIntegrityFailure  = 0x8A15003F
	wingetErrorSourceOpenFailed            = 0x8A150045
	wingetErrorSourceAgreementsNotAccepted = 0x8A150046
	wingetErrorFailedToOpenAllSources      = 0x8A15004B
	wingetErrorUpgradeVersionNotNewer      = 0x8A15004F
	wingetErrorUpgradeVersionUnknown       = 0x8A150050
	wingetErrorPackageIsPinned             = 0x8A150068
	wingetErrorServiceUnavailable          = 0x8A15006D
	wingetErrorInstallerZeroByteFile       = 0x8A150086
	wingetErrorInstallTechnologyMismatch   = 0x8A15008E
	wingetErrorInstallInProgress           = 0x8A150102
	wingetErrorInstallNoNetwork            = 0x8A150107
	wingetErrorInstallRebootRequired       = 0x8A150109
	wingetErrorInstallRebootInitiated      = 0x8A15010B
	wingetErrorInstallCancelledByUser      = 0x8A15010C

	windowsErrorCancelled         = 1223       // ERROR_CANCELLED
	windowsErrorCancelledHResult  = 0x800704C7 // HRESULT_FROM_WIN32(ERROR_CANCELLED)
	windowsErrorInstallInProgress = 1618       // ERROR_INSTALL_ALREADY_RUNNING
)

func exitCodeIs(exitCode int, expected uint32) bool {
	return uint32(exitCode) == expected
}

// classifyPackageAttempt decides what one raw command attempt meant. Exit
// codes decide first; output text is only read when the code is not one the
// manager documents for that situation (older clients, localized output).
func classifyPackageAttempt(packageManager string, result CommandResult) packageAttemptOutcome {
	if result.OK {
		return outcomeUpdated
	}
	switch result.Code {
	case commandCancelledCode:
		return outcomeCancelled
	case commandTimeoutCode:
		return outcomeTimedOut
	}
	switch packageManager {
	case managerWinget:
		return classifyWingetAttempt(result)
	case managerChoco:
		return classifyChocoAttempt(result)
	case managerStore:
		return classifyStoreCLIAttempt(result)
	}
	return outcomeFailed
}

// classifyStoreCLIAttempt only separates transient failures. Store CLI
// target misses and option fallbacks stay with the Store decision code,
// which Phase 4 of the modernization brief replaces.
func classifyStoreCLIAttempt(result CommandResult) packageAttemptOutcome {
	output := normalizedCommandOutput(result)
	if exitCodeIs(result.Code, wingetErrorInternal) || result.Code == windowsErrorInstallInProgress ||
		outputContainsAny(output, installerBusyText) || outputContainsAny(output, networkTransientText) {
		return outcomeTransient
	}
	return outcomeUnclassified
}

func classifyWingetAttempt(result CommandResult) packageAttemptOutcome {
	switch uint32(result.Code) {
	case wingetErrorNoApplicationsFound:
		return outcomeNotInstalled
	case wingetErrorUpdateNotApplicable, wingetErrorUpgradeVersionNotNewer:
		return outcomeNotApplicable
	case wingetErrorUpgradeVersionUnknown:
		return outcomeNeedsUnknownConsent
	case wingetErrorPackageIsPinned:
		return outcomeNeedsPinnedConsent
	case wingetErrorInstallTechnologyMismatch:
		return outcomeTechnologyMismatch
	case wingetErrorSourcesInvalid, wingetErrorSourceDataMissing, wingetErrorNoSourcesDefined,
		wingetErrorSourceDataIntegrityFailure, wingetErrorSourceOpenFailed,
		wingetErrorSourceAgreementsNotAccepted, wingetErrorFailedToOpenAllSources,
		// A hash mismatch usually means the vendor replaced the installer and
		// the local manifest is stale. WinGet still verifies the new hash.
		wingetErrorInstallerHashMismatch:
		return outcomeSourceFailure
	case wingetErrorInternal, wingetErrorDownloadFailed, wingetErrorServiceUnavailable,
		wingetErrorInstallerZeroByteFile, wingetErrorInstallInProgress, wingetErrorInstallNoNetwork,
		windowsErrorInstallInProgress:
		return outcomeTransient
	case wingetErrorInstallCancelledByUser, windowsErrorCancelled, windowsErrorCancelledHResult:
		return outcomeInstallerCancelled
	}
	output := normalizedCommandOutput(result)
	switch {
	case outputReportsInstallerCancelled(output):
		return outcomeInstallerCancelled
	case outputContainsAny(output, wingetSourceFailureText):
		return outcomeSourceFailure
	// The consent hints come before "not found" and "not applicable":
	// WinGet words an unknown-version or pinned refusal as "no applicable
	// upgrade ... use --include-unknown".
	case outputReportsUnknownVersion(output):
		return outcomeNeedsUnknownConsent
	case outputContainsAny(output, wingetPinnedText):
		return outcomeNeedsPinnedConsent
	case outputContainsAny(output, wingetTechnologyMismatchText):
		return outcomeTechnologyMismatch
	case outputContainsAny(output, wingetNotInstalledText):
		return outcomeNotInstalled
	case outputContainsAny(output, wingetNotApplicableText):
		return outcomeNotApplicable
	case outputContainsAny(output, installerBusyText) || outputContainsAny(output, networkTransientText):
		return outcomeTransient
	}
	return outcomeFailed
}

func classifyChocoAttempt(result CommandResult) packageAttemptOutcome {
	if result.Code == windowsErrorInstallInProgress {
		return outcomeTransient
	}
	if exitCodeIs(result.Code, windowsErrorCancelled) || exitCodeIs(result.Code, windowsErrorCancelledHResult) {
		return outcomeInstallerCancelled
	}
	output := normalizedCommandOutput(result)
	switch {
	case outputReportsInstallerCancelled(output):
		return outcomeInstallerCancelled
	case outputContainsAny(output, chocoNotInstalledText):
		return outcomeNotInstalled
	case outputContainsAny(output, installerBusyText) || outputContainsAny(output, networkTransientText) || outputContainsAny(output, chocoBusyText):
		return outcomeTransient
	}
	return outcomeFailed
}

// packageResultOutcome returns the outcome recorded on a result, or
// classifies it when it came from a path that does not record one.
func packageResultOutcome(packageManager string, result CommandResult) packageAttemptOutcome {
	if result.Outcome != outcomeUnclassified {
		return result.Outcome
	}
	return classifyPackageAttempt(packageManager, result)
}

// withPackageOutcome records the outcome of a raw attempt on its result.
func withPackageOutcome(packageManager string, result CommandResult) CommandResult {
	result.Outcome = classifyPackageAttempt(packageManager, result)
	return result
}

var wingetSourceFailureText = []string{
	"failed when opening source",
	"failed when searching source",
	"failed in attempting to update the source",
	"failed to update source",
	"failed to open source",
	"failed to get source",
	"failed to query source",
	"source data is corrupted",
	"source is not configured",
	"no sources are configured",
	"data required by the source is missing",
	"the source agreements were not agreed to",
}

var wingetNotInstalledText = []string{
	"no installed package found",
	"no installed package matching",
	"kein installiertes paket gefunden",
	"es wurde kein installiertes paket gefunden",
}

var wingetNotApplicableText = []string{
	"no applicable upgrade",
	"no available upgrade",
	"no upgrade available",
	"kein anwendbares upgrade",
	"kein verfügbares upgrade",
	"kein upgrade verfügbar",
	"keine neueren paketversionen",
}

var wingetPinnedText = []string{
	"--include-pinned",
	"include-pinned",
	"package has a pin",
	"package is pinned",
	"paket ist angeheftet",
}

var wingetTechnologyMismatchText = []string{
	"installation technology",
	"install technology",
	"installer technology",
	"installationstechnologie",
}

func outputReportsUnknownVersion(output string) bool {
	if outputContainsAny(output, []string{"--include-unknown", "include-unknown", "include unknown"}) {
		return true
	}
	return outputContainsAny(output, []string{"version is unknown", "version cannot be determined", "unknown version", "unbekannte version", "version ist unbekannt"})
}

func outputReportsInstallerCancelled(output string) bool {
	return outputContainsAny(output, []string{
		"exit code: 1223",
		"exitcode: 1223",
		"canceled by the user",
		"cancelled by the user",
		"canceled the installation",
		"cancelled the installation",
		"durch den benutzer abgebrochen",
		"sie haben die installation abgebrochen",
	})
}

var chocoNotInstalledText = []string{
	"unable to find package",
	"was not found with the source",
	"is not installed. cannot upgrade", // --fail-on-not-installed
	"no package found",
}

var chocoBusyText = []string{
	"the process cannot access the file",
	"chocolatey is already running",
	"existing chocolatey process",
}

var installerBusyText = []string{
	"another installation is already in progress",
	"another install is already in progress",
	"another transaction",
	"installation is in progress",
	"installer is running",
	"being used by another process",
	"locked by another process",
	"unable to acquire lock",
	"could not acquire lock",
	"exit code 1618",
	"error 1618",
}

var networkTransientText = []string{
	"connection timed out",
	"operation timed out",
	"request timed out",
	"timeout was reached",
	"timed out while",
	"connection reset",
	"connection was reset",
	"connection aborted",
	"connection closed",
	"connection refused",
	"temporarily unavailable",
	"temporary failure",
	"remote name could not be resolved",
	"name resolution failure",
	"could not resolve host",
	"unable to resolve",
	"unable to connect to the remote server",
	"underlying connection was closed",
	"ssl connection could not be established",
	"tls connection could not be established",
	"transport connection",
	"too many requests",
	"rate limit",
	" http 429",
	"status 429",
	"response status code does not indicate success: 429",
	" http 502",
	"status 502",
	"bad gateway",
	" http 503",
	"status 503",
	"service unavailable",
	" http 504",
	"status 504",
	"gateway timeout",
}
