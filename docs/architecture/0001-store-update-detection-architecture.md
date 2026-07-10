# ADR 0001: Microsoft Store Update Detection

Date: 2026-06-21

Status: Active

## Decision

Use a medium-integrity Go WebUI/coordinator in the interactive user's session. Keep Microsoft Store inventory, Store catalog checks, and ordinary Store actions in that user context. Delegate only admin-required winget, Chocolatey, scheduler, and system actions to the typed elevated worker.

Use direct in-process Go WinRT/AppModel calls for current-user packaged-app
inventory. The coordinator calls
`Windows.Management.Deployment.PackageManager.FindPackagesForUser("")` through
the Windows Runtime ABI while remaining in the interactive user's session.

## Identity Rules

- Installed Store identity is `(user SID, package family name)`.
- Package full name is a versioned installed instance.
- Store Product ID is catalog/action identity.
- Provider-specific update IDs are action targets, not installed identity.
- Display names, localized names, fuzzy matches, normalized punctuation-free strings, and search result rank are never Store identity.

## Update State Rules

- `Current` requires a fresh complete scan in the correct user context with required providers healthy and authoritative negative evidence.
- Provider failure, parser rejection, stale evidence, unresolved identity, incomplete coverage, or user mismatch produces `Unknown`.
- Unknown never becomes Current because a provider failed, a parser rejected data, or coverage was incomplete. Absence of fresh authoritative evidence is diagnostic-only.
- Stale or incomplete negative evidence cannot clear a fresh positive update observation.
- Store update execution requires a fresh `Available` assessment and exact verified action target.
- Store command success means accepted. Final success requires post-action verification.

## Providers

- Native current-user packaged inventory enumerates installed Store package families through Go WinRT/AppModel calls.
- Store CLI exact provider verifies PFN/Product ID with `store show <PFN>` and checks exact update state with Store CLI commands.
- Store CLI aggregate provider uses `store updates --apply false` only when output contains explicit exact PFN/Product ID evidence or explicit no-update coverage.
- WinGet msstore evidence is accepted only when it can be associated to the installed PFN exactly.
- Legacy display-name Store resolution is removed from the default build. Store search remains available only for user-initiated install search, not installed identity or update truth.

## Persistence

Store scan state is persisted transactionally. Published scan generations are immutable. Failed or incomplete scans do not overwrite the last completed generation. Mappings may be persisted only from exact structured evidence or verified provider associations.

Detection, API projection, update execution validation, and diagnostics consume
Store scan state through a domain repository boundary. The repository operates
on complete `StoreScanSnapshot` generations containing scan context, current-user
inventory, provider runs, provider observations, verified mappings, and final
assessments. The production adapter is a standard-library immutable JSON file
repository; SQLite and `modernc.org/sqlite` are not part of the default module or
binary.

The repository writes complete immutable snapshot files under
`store-scans/<user-scope>/`, where `<user-scope>` is a stable hash of the user
SID and file names are derived from scan start time plus a scan-ID hash. Writes
encode, flush, close, and rename a temporary file to the final generation path so
partially written final files are not exposed. Loading validates schema version,
user SID, scan generation, and nested evidence before selecting the latest
published generation by `StartedAt` and scan ID rather than file modification
time. Corrupt, oversized, wrong-user, or unsupported future-schema snapshots are
skipped and logged as diagnostics; valid older snapshots remain available for
hysteresis.

Legacy `store-scans.sqlite` files are cached evidence, not durable user
preferences. On startup after the cutover, the coordinator renames a legacy cache
file to a timestamped `.legacy-cache.*` filename and starts from file snapshots
only. Durable preferences, including Store auto-update keys, remain in
`state.json`. Old cache rows are never imported as fresh Store truth, so the UI
reports Unknown until a fresh current-user scan publishes a new snapshot.

## Inventory Ownership and Projection

The cached base inventory is manager/native inventory only. Published Store
assessments are overlays applied to a deep-copied inventory snapshot when the API,
update selection, or automation needs the effective view. Read-only projection
must not write through shared package slices, provider summaries, command-result
maps, or Store health structures.

Recovered, stale, or incomplete evidence is retained for diagnostics so the UI can
explain why Store status is not authoritative. That evidence is never actionable:
it cannot set `UpdateAvailable`, cannot provide an exact target, and cannot
authorize execution. A fresh targeted or full Store scan must replace stale or
recovered evidence before an update can run.

## Worker Isolation

WinRT inventory and Store update discovery may block inside synchronous
COM/WinRT calls. Those operations run through a hidden same-binary worker launched
as the interactive current user in the same session, not through an elevated
alternate administrator account. The parent owns the worker lifetime with a
kill-on-close job object, bounded request/response pipes, strict schema
validation, scan-ID checks, user-SID checks, PFN/full-name validation, duplicate
rejection, and response-size limits.

The worker protocol accepts only the specific Store enumeration or discovery
operation. It never accepts arbitrary executable paths, package operations, file
writes, or generic COM activation. Wrong-user, malformed, oversized, duplicate,
or protocol-mismatched responses make the Store evidence incomplete and
non-actionable.

## Execution and Verification

Store execution is deliberately stricter than winget or Chocolatey because Store
identity, offer discovery, installation, and verification are split across
multiple Windows APIs and command surfaces. Before running a Store update, the
coordinator re-enumerates the exact current-user PFN and rejects the assessment
when the installed version no longer matches the version that was assessed.

PackageCatalog events do not prove that an update was offered and do not prove
that an update succeeded. They are only an acceleration signal that triggers
immediate exact inventory and catalog rechecks. Polling remains the fallback, and
post-action verification remains authoritative.

Successful verification requires a strict installed version increase, respecting
the offered version when it is known, or an authoritative exact catalog result
showing that the offer disappeared while the package remains installed and did
not downgrade. A package-full-name-only change is not sufficient without the
authoritative exact catalog result.

## Distribution

The normal build produces one Go executable. No C# inventory sidecar is compiled,
embedded, extracted, or launched for Microsoft Store inventory.

## Known Gaps

- PackageCatalog events are used only as an acceleration signal after exact
  Store actions are accepted. They wake immediate exact inventory and targeted
  catalog checks; events never prove offer existence or update success on their
  own.
- Broader Windows matrix validation remains release-gate work.
- Store CLI behavior varies by version. Product ID is attempted first through WinGet msstore when available, with verified Store CLI exact targets used as fallback.
