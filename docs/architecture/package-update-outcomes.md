# Package update outcomes

Status: in use since 2026-11-21.

Every WinGet, Chocolatey and Store CLI attempt is classified once, right
after it runs, into one outcome (`CommandResult.Outcome`,
`internal/updater/package_update_outcome.go`). Retries, fallbacks and the
queue all decide on that outcome. A result merged from several attempts
keeps the outcome of the last attempt, so earlier attempts' output never
drives a later decision.

Exit codes come first. Text is matched only when the code says nothing
specific. WinGet codes are from Microsoft's published list,
`doc/windows/package-manager/winget/returnCodes.md` in microsoft/winget-cli
(checked 2026-11-21).

## Decision table

| Outcome | Recognised by | What PatchBoard does |
|---|---|---|
| `updated` | exit 0; 3010 or 1641; WinGet `0x8A150109` or `0x8A15010B` (restart needed or started) | Success. A restart note is added for the restart codes. |
| `not_installed` | WinGet `0x8A150014`; "No installed package found"; Chocolatey "is not installed" or "Unable to find package" | Try the package's next identifier or variant. If every target misses, the package is skipped as no longer installed. |
| `not_applicable` | WinGet `0x8A15002B`, `0x8A15004F`; "No applicable upgrade found" | Skipped: the offered version cannot be installed over the installed one. |
| `needs_unknown_version_consent` | WinGet `0x8A150050`; "--include-unknown" hint | Retry with `--include-unknown` if the user allowed it, otherwise ask. |
| `needs_pinned_consent` | WinGet `0x8A150068`; "--include-pinned" hint | Retry with `--include-pinned` if the user allowed it, otherwise ask. |
| `installer_technology_mismatch` | WinGet `0x8A15008E`; technology-mismatch text | `upgrade --force`, then forced install. If both fail, the user is told to repair the package; nothing is uninstalled automatically. |
| `source_failure` | WinGet `0x8A15000F`, `0x8A150045`, `0x8A150011` and the other source codes; "source data is corrupted" | `winget source update`, then `source reset`, then one retry. |
| `transient` | 1618; WinGet `0x8A150102`, `0x8A150001`, `0x8A150008`; "already running", network text | Up to 3 attempts. Retrying stops as soon as an attempt fails for another reason. |
| `installer_cancelled` | 1223, `0x800704C7`, WinGet `0x8A15010C`; "cancelled the installation" | The update queue stops; the remaining packages are reported as skipped. |
| `cancelled` / `timed_out` | PatchBoard cancelled or timed out the command | The queue stops with "the update batch was cancelled" or "the previous package update timed out". |
| `failed` | anything else | Reported as failed; the queue moves on to the next package. |
| unclassified (`""`) | Store CLI output that is not transient | Handled by the Store code's own checks (Phase 4 replaces them). |

## Chocolatey

`choco upgrade` installs a package that is missing. PatchBoard always passes
`--fail-on-not-installed`, so trying a variant such as `foo.install` never
adds a package the user did not have.

Updates from `choco outdated` are stored under the same key the inventory
looks them up by (`managerUpdateLookupKey`). Before 2026-11-21 they were
stored under the bare ID and never matched, so Chocolatey updates were not
shown or applied.

## Behaviour changes on 2026-11-21

- WinGet `0x8A150014` (no packages found) no longer triggers a source repair.
- WinGet `0x8A150010` (no applicable installer) is a failure, not a source problem.
- Transient retries stop when a retry fails for a non-transient reason.
- WinGet's own reboot codes count as success.
- Chocolatey never installs a missing package during an upgrade.
- Chocolatey updates are found again (lookup key fix).
- The update job and the elevated worker batch share one queue-stop rule (`packageQueueStop`).

Tests: `internal/updater/package_update_outcome_test.go`.
