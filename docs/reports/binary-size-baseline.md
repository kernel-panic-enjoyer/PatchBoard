# Binary Size Report

Measured on 2026-06-23T20:02:19+02:00 from branch `debloat`, commit
`b167c35567ab3643fc840e38b4c51aa8d175a12e` plus the current working tree.

## Current Build

| Field | Value |
| --- | ---: |
| Production executable | `dist/WindowsUpdaterWebUI.exe` |
| Size | 14,665,728 bytes |
| Size | 13.986 MiB |
| Build command | `go build -ldflags='-H=windowsgui' -o dist/WindowsUpdaterWebUI.exe .` |

`dev/scripts/Build-Workspace.ps1` rebuilt the production executable and reported
the same size.

## Removed Size Driver

The previous baseline identified the `modernc.org/sqlite` stack as the largest
application-controlled size target. The Store scan backend now uses standard
library JSON snapshots instead of SQLite.

Validation from the rebuilt executable:

- `go list -deps ./...` contains no `modernc.org/*`, `bigfft`, or SQLite module
  dependency.
- `go version -m dist/WindowsUpdaterWebUI.exe` contains no `modernc.org/*`,
  `bigfft`, or SQLite module entry.
- `go tool nm dist/WindowsUpdaterWebUI.exe` contains no modernc or bigfft symbol.

The only remaining `sqlite` source/binary reference is
`retireLegacyStoreScanSQLiteCache`, which renames old `store-scans.sqlite` cache
files out of the active path. It does not link SQLite.

## Latest Size Analysis

Raw reports are under:

- `dist/size-analysis/20260623-200219-debloat-b167c35567ab/`

Top package groups from `go tool nm -size -sort size`:

| Rank | Group | Bytes | MiB |
| ---: | --- | ---: | ---: |
| 1 | `crypto/*` | 34,829,850 | 33.216 |
| 2 | `runtime` | 5,682,057 | 5.419 |
| 3 | `<unknown>` | 1,895,960 | 1.808 |
| 4 | `windows-updater-webui/internal/updater` | 642,756 | 0.613 |
| 5 | `net/http` | 325,350 | 0.310 |

`chromedp` is not linked into the production executable.

## Notes

- `go tool nm` symbol sizes are diagnostic and do not add up to exact PE file
  ownership.
- The large `crypto/internal/fips140/drbg.memory` symbol is from the Go standard
  library crypto stack, not app-specific code.
