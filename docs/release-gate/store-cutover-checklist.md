# Store Cutover Release-Gate Checklist

Date: 2026-06-21

## Required Matrix

Run the Store cutover smoke suite on:

- Windows 10 supported floor, x64.
- Windows 11 supported floor, x64.
- Windows 11 current stable, x64.
- ARM64 where supported by the build pipeline.
- Administrator account.
- Standard-user account with separate admin elevation credentials.
- Multiple signed-in Windows users.
- Microsoft Store signed in.
- Microsoft Store signed out.
- Offline network.
- Intermittent network.
- Store disabled by policy.
- English UI.
- German UI or another non-English UI.
- Current Store CLI.
- Older Store CLI still within supported range.
- Current WinGet.
- Older WinGet still within supported range.
- Main, framework, optional, resource, and bundle package families.
- Updates performed externally by Microsoft Store while the WebUI is open.
- Provider disagreement.
- Provider outage.
- Application restart during scan and during post-update verification.

## Release Gates

- `go test -count=1 ./...` passes.
- `go vet ./...` passes.
- JavaScript syntax check passes for `internal/updater/assets/ui.js`.
- Windows GUI build succeeds.
- Store diagnostics export contains no raw user SID, credentials, tokens,
  account identifiers, or personal install paths.
- Store packages do not show `Current` when the scan is incomplete, stale,
  unresolved, failed, or unsupported.
- Store update buttons are disabled unless Product ID/action target is exact
  and verified.
- Legacy rollback requires `UPDATER_STORE_LEGACY_DETECTOR=1`.

## Measurement Plan

Record these values for every matrix run:

- Native inventory duration.
- Catalog provider duration per provider.
- Total Store scan duration.
- Provider timeout count.
- Store scan SQLite file size before and after 10 scans.
- WebUI package refresh response time while a scan is running.
- WebUI interaction latency for theme switching, table search, pagination, and
  log search while a scan is running.

Current configured production deadlines:

- Native Store inventory broker timeout: 90 seconds.
- Store catalog provider timeout: 45 seconds.
- Exact Store update verification timeout: 2 minutes.
- Exact Store update verification poll interval: 3 seconds.

## Exit Criteria

The cutover can ship only when all required matrix cells either pass or have a
documented release-blocking exception. Unknown Store status is acceptable when
provider coverage is genuinely incomplete; false Current is not acceptable.
