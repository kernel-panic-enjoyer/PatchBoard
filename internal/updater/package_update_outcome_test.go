package updater

import (
	"context"
	"strings"
	"testing"
	"time"
)

// The decision table in docs/architecture/package-update-outcomes.md, as a
// test. Exit codes come from WinGet's published return codes.
func TestClassifyPackageAttempt(t *testing.T) {
	for _, tc := range []struct {
		name    string
		manager string
		result  CommandResult
		want    packageAttemptOutcome
	}{
		{"ok", managerWinget, CommandResult{OK: true}, outcomeUpdated},
		{"cancelled by PatchBoard", managerWinget, CommandResult{Code: commandCancelledCode}, outcomeCancelled},
		{"timed out", managerChoco, CommandResult{Code: commandTimeoutCode}, outcomeTimedOut},

		{"winget no packages found code", managerWinget, CommandResult{Code: int(int32(-1978335212))}, outcomeNotInstalled},
		{"winget no packages found unsigned", managerWinget, CommandResult{Code: 0x8A150014}, outcomeNotInstalled},
		{"winget update not applicable", managerWinget, CommandResult{Code: 0x8A15002B}, outcomeNotApplicable},
		{"winget version not newer", managerWinget, CommandResult{Code: 0x8A15004F}, outcomeNotApplicable},
		{"winget version unknown", managerWinget, CommandResult{Code: 0x8A150050}, outcomeNeedsUnknownConsent},
		{"winget pinned", managerWinget, CommandResult{Code: 0x8A150068}, outcomeNeedsPinnedConsent},
		{"winget technology mismatch", managerWinget, CommandResult{Code: 0x8A15008E}, outcomeTechnologyMismatch},
		{"winget source data missing", managerWinget, CommandResult{Code: 0x8A15000F}, outcomeSourceFailure},
		{"winget source open failed", managerWinget, CommandResult{Code: 0x8A150045}, outcomeSourceFailure},
		{"winget installer hash mismatch", managerWinget, CommandResult{Code: 0x8A150011}, outcomeSourceFailure},
		{"winget no applicable installer is not a source failure", managerWinget, CommandResult{Code: 0x8A150010}, outcomeFailed},
		{"winget install in progress", managerWinget, CommandResult{Code: 0x8A150102}, outcomeTransient},
		{"winget internal error", managerWinget, CommandResult{Code: 0x8A150001}, outcomeTransient},
		{"winget download failed", managerWinget, CommandResult{Code: 0x8A150008}, outcomeTransient},
		{"msi install already running", managerWinget, CommandResult{Code: 1618}, outcomeTransient},
		{"winget installer cancelled", managerWinget, CommandResult{Code: 0x8A15010C}, outcomeInstallerCancelled},
		{"uac cancelled", managerWinget, CommandResult{Code: 0x800704C7}, outcomeInstallerCancelled},
		{"installer failed", managerWinget, CommandResult{Code: 1603, Stdout: "Installer failed with exit code: 1603"}, outcomeFailed},

		{"text not installed", managerWinget, CommandResult{Code: 1, Stdout: "No installed package found matching input criteria."}, outcomeNotInstalled},
		{"text not installed german", managerWinget, CommandResult{Code: 1, Stdout: "Es wurde kein installiertes Paket gefunden, das den Eingabekriterien entspricht."}, outcomeNotInstalled},
		{"text not applicable", managerWinget, CommandResult{Code: 1, Stdout: "No applicable upgrade found."}, outcomeNotApplicable},
		{"text not applicable german", managerWinget, CommandResult{Code: 1, Stdout: "Es wurde kein anwendbares Upgrade gefunden."}, outcomeNotApplicable},
		{"text unknown version wins over not applicable", managerWinget, CommandResult{Code: 1, Stdout: "No applicable upgrade found for packages with unknown installed version. Use --include-unknown to include packages with unknown versions."}, outcomeNeedsUnknownConsent},
		{"text pinned", managerWinget, CommandResult{Code: 1, Stderr: "Package is pinned. Use --include-pinned to include pinned packages."}, outcomeNeedsPinnedConsent},
		{"text source corrupted", managerWinget, CommandResult{Code: 1, Stderr: "Failed when searching source; source data is corrupted"}, outcomeSourceFailure},
		{"text network", managerWinget, CommandResult{Code: 1, Stderr: "The underlying connection was closed."}, outcomeTransient},
		{"text installer cancelled german", managerWinget, CommandResult{Code: 1, Stdout: "Sie haben die Installation abgebrochen."}, outcomeInstallerCancelled},
		{"text access denied", managerWinget, CommandResult{Code: 5, Stderr: "Access is denied."}, outcomeFailed},
		{"an unrelated 'not found' is a failure, not a target miss", managerWinget, CommandResult{Code: 1, Stderr: "Installer file not found"}, outcomeFailed},

		{"choco not installed", managerChoco, CommandResult{Code: 1, Stdout: "example is not installed. Cannot upgrade a non-existent package."}, outcomeNotInstalled},
		{"choco unknown package", managerChoco, CommandResult{Code: 1, Stderr: "Unable to find package 'example'."}, outcomeNotInstalled},
		{"choco busy", managerChoco, CommandResult{Code: 1, Stderr: "Chocolatey is already running."}, outcomeTransient},
		{"choco msi busy", managerChoco, CommandResult{Code: 1618}, outcomeTransient},
		{"choco failure", managerChoco, CommandResult{Code: 1, Stderr: "The install of example was NOT successful."}, outcomeFailed},

		{"store cli miss is left to the Store code", managerStore, CommandResult{Code: 1, Stdout: "Could not find installed product metadata"}, outcomeUnclassified},
		{"store cli busy", managerStore, CommandResult{Code: 1618}, outcomeTransient},
	} {
		if got := classifyPackageAttempt(tc.manager, tc.result); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

// A transient failure is retried only while the latest attempt is still
// transient. Before, the earlier attempt's text in the merged output kept
// the retries going after a real failure.
func TestTransientRetryStopsWhenRetryFailsForAnotherReason(t *testing.T) {
	calls := 0
	restore := replacePackageActionHooks(
		func(ctx context.Context, timeout time.Duration, args ...string) CommandResult {
			calls++
			if calls == 1 {
				return CommandResult{Code: 1618, Command: strings.Join(args, " "), Stderr: "Another installation is already in progress."}
			}
			return CommandResult{Code: 1603, Command: strings.Join(args, " "), Stderr: "Fatal error during installation."}
		},
		func(string) bool { return true },
	)
	defer restore()

	result := runPackageActionCommand(context.Background(), managerChoco, time.Second, "choco", "upgrade", "example")
	if calls != 2 || result.OK || result.Outcome != outcomeFailed {
		t.Fatalf("calls=%d result=%#v", calls, result)
	}
}

// WinGet's "no packages found" is not a source problem: it must not trigger
// a source update and retry.
func TestWingetNoPackagesFoundDoesNotRepairSources(t *testing.T) {
	var commands []string
	restore := replacePackageActionHooks(
		func(ctx context.Context, timeout time.Duration, args ...string) CommandResult {
			commands = append(commands, strings.Join(args, " "))
			return CommandResult{Code: 0x8A150014, Command: strings.Join(args, " "), Stdout: "No installed package found matching input criteria."}
		},
		func(string) bool { return true },
	)
	defer restore()

	result := runPackageActionCommand(context.Background(), managerWinget, time.Second, "winget", "upgrade", "--id", "Example.App")
	if len(commands) != 1 || result.Outcome != outcomeNotInstalled {
		t.Fatalf("commands=%#v result=%#v", commands, result)
	}
}

func TestWingetRebootRequiredCodesCountAsSuccess(t *testing.T) {
	for _, code := range []int{0x8A150109, 0x8A15010B, 3010, 1641} {
		restore := replacePackageActionHooks(
			func(ctx context.Context, timeout time.Duration, args ...string) CommandResult {
				return CommandResult{Code: code, Command: strings.Join(args, " "), Stdout: "installer completed"}
			},
			func(string) bool { return true },
		)
		result := runPackageActionCommand(context.Background(), managerWinget, time.Second, "winget", "upgrade", "--id", "Example.App")
		restore()
		if !result.OK || result.Outcome != outcomeUpdated || !strings.Contains(result.Stdout, "restart") {
			t.Errorf("code %#x: result=%#v", code, result)
		}
	}
}

// "choco upgrade" installs packages that are missing. Variant fallbacks must
// never install a package the user did not have.
func TestChocoUpgradeNeverInstallsMissingPackages(t *testing.T) {
	var commands []string
	restore := replacePackageActionHooks(
		func(ctx context.Context, timeout time.Duration, args ...string) CommandResult {
			commands = append(commands, strings.Join(args, " "))
			return CommandResult{Code: 1, Command: strings.Join(args, " "), Stdout: packageActionTargetFromArgs(args) + " is not installed. Cannot upgrade a non-existent package."}
		},
		func(string) bool { return true },
	)
	defer restore()

	result := runChocoUpgradePackageWithFallbackContext(context.Background(), Package{Manager: managerChoco, ID: "example"})
	if result.OK || len(commands) != 3 {
		t.Fatalf("expected the base and both variants to be tried, commands=%#v", commands)
	}
	for _, command := range commands {
		if !strings.Contains(command, "--fail-on-not-installed") {
			t.Errorf("upgrade without --fail-on-not-installed: %s", command)
		}
	}
}

// The outcome of a merged result is the outcome of its last attempt.
func TestMergedResultKeepsFinalAttemptOutcome(t *testing.T) {
	first := CommandResult{Code: 1, Stdout: "No installed package found matching input criteria.", Outcome: outcomeNotInstalled}
	final := CommandResult{Code: 1603, Stdout: "Installer failed", Outcome: outcomeFailed}
	merged := mergeCommandAttemptsWithFinalResult(first, final, "retry")
	if merged.Outcome != outcomeFailed || shouldTryAlternatePackageTarget(merged) {
		t.Fatalf("merged outcome %q", merged.Outcome)
	}
}

// RULE-001: a Chocolatey update is found for its package whatever the case of
// the ID. Before, updates were stored under the bare ID and never matched.
func TestChocoUpdateIsFoundForInstalledPackage(t *testing.T) {
	updates := map[string]string{}
	for id, version := range parseChocoOutdated("Git|2.40.0|2.41.0|false\n") {
		updates[managerUpdateLookupKey(managerChoco, id)] = version
	}
	pkg, ok := packageFromManagerInventory(State{}, managerInventory{manager: managerChoco, updates: updates}, Package{ID: "git", Version: "2.40.0"})
	if !ok || !pkg.UpdateAvailable || pkg.AvailableVersion != "2.41.0" {
		t.Fatalf("update not found: %#v", pkg)
	}
}

// RULE-380: source and pin changes are mutations; listing is not.
func TestPackageManagerMutationClassification(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want bool
	}{
		{[]string{"winget", "source", "add", "--name", "x", "https://example.invalid"}, true},
		{[]string{"winget", "source", "remove", "x"}, true},
		{[]string{"winget", "source", "list"}, false},
		{[]string{"choco", "pin", "add", "-n", "git"}, true},
		{[]string{"choco", "pin", "remove", "-n", "git"}, true},
		{[]string{"choco", "pin", "list"}, false},
	} {
		if got := isPackageManagerMutationCommand(tc.args); got != tc.want {
			t.Errorf("%v: got %t, want %t", tc.args, got, tc.want)
		}
	}
	if !commandHasApplyFalse([]string{"store", "update", "--apply=false"}) || commandHasApplyFalse([]string{"store", "update", "--apply=true"}) {
		t.Fatal("--apply=value form not recognized")
	}
}

// The update job and the elevated batch stop the queue for the same reasons.
func TestPackageQueueStop(t *testing.T) {
	remaining := []Package{{Manager: managerWinget, ID: "Next.App"}}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	expired, cancelExpired := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancelExpired()
	for _, tc := range []struct {
		name   string
		ctx    context.Context
		result CommandResult
		reason string
	}{
		{"installer cancelled", context.Background(), CommandResult{Code: 1223, Outcome: outcomeInstallerCancelled}, "the previous package installer was cancelled"},
		{"batch timed out", expired, CommandResult{Code: 1}, "the previous package update timed out"},
		{"batch cancelled", cancelled, CommandResult{Code: 1}, "the update batch was cancelled"},
		{"ordinary failure continues", context.Background(), CommandResult{Code: 1603, Outcome: outcomeFailed}, ""},
	} {
		stop, reason, skipped := packageQueueStop(tc.ctx, tc.result, remaining)
		if reason != tc.reason || stop != (tc.reason != "") {
			t.Errorf("%s: stop=%t reason=%q", tc.name, stop, reason)
			continue
		}
		if stop && (len(skipped) != 1 || skipped[0].Result.Code != commandSkippedCode) {
			t.Errorf("%s: skipped=%#v", tc.name, skipped)
		}
	}
}

// RULE-121 and RULE-386: a non-Store package can be updated now only when an
// update is offered and supported, its identity is not in conflict, and
// unknown-version or pinned packages have the user's consent.
func TestNonStoreUpdateNowPolicy(t *testing.T) {
	ready := Package{Manager: managerWinget, ID: "Example.App", UpdateAvailable: true, UpdateSupported: true}
	for _, tc := range []struct {
		name    string
		mutate  func(*Package)
		options UpdateOptions
		want    bool
	}{
		{"ready", func(*Package) {}, UpdateOptions{}, true},
		{"identity conflict", func(p *Package) { p.IdentityConflict = true }, UpdateOptions{}, false},
		{"no update", func(p *Package) { p.UpdateAvailable = false }, UpdateOptions{}, false},
		{"unsupported", func(p *Package) { p.UpdateSupported = false }, UpdateOptions{}, false},
		{"unknown version without consent", func(p *Package) { p.UnknownVersion = true }, UpdateOptions{}, false},
		{"unknown version with consent", func(p *Package) { p.UnknownVersion = true }, UpdateOptions{AllowUnknownVersion: true}, true},
		{"pinned without consent", func(p *Package) { p.Pinned = true }, UpdateOptions{}, false},
		{"pinned with consent", func(p *Package) { p.Pinned = true }, UpdateOptions{AllowPinned: true}, true},
	} {
		pkg := ready
		tc.mutate(&pkg)
		policy := packageUpdatePolicy(pkg, tc.options)
		if policy.CanUpdateNow != tc.want || (!tc.want && policy.CannotUpdateReason == "") {
			t.Errorf("%s: %#v", tc.name, policy)
		}
	}
}
