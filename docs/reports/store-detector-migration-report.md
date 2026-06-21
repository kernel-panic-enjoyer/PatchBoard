# Microsoft Store Detector Migration Report

Date: 2026-06-21

## Scope

This report compares the retired legacy Store detector with the new
transactional Store assessment path using repository fixtures, automated tests,
and available Windows smoke-test evidence.

## Legacy Detector Summary

Legacy Store update truth could be produced by:

- AppX inventory from PowerShell, including an all-users attempt.
- Store CLI human-readable table parsing.
- WinGet `msstore` human-readable table parsing.
- `package_store_resolver.go` display-name searches.
- `mergeStoreAppxPackages`, `mergeStoreNativeUpdatePackages`, and
  `storeUpdateForPackage` matching by normalized names, IDs, matches, and
  stable action IDs.
- Store update execution through alternate targets and display-name search.

Automated fixture coverage in `package_appx_test.go`,
`package_store_resolver_test.go`, and `package_store_test.go` demonstrated that
these parsers could process common output, but the same tests also encoded the
unsafe behavior being retired: display-name resolution, same-version Store
pending rows, and full-name-to-action-ID collapse.

## New Detector Summary

The new default path uses:

- Current-user native packaged-app inventory through
  `StorePackagedAppInventoryProvider`.
- Transactional scan persistence in `StoreScanStore`.
- Reconciliation through `ReconcileStoreUpdate`.
- API projection through `applyPublishedStoreScanAssessments`.
- Exact Store execution through `StoreExactUpdateExecutor`.

The compatibility `Package.UpdateAvailable` boolean is true only when
`update_state == available`.

## Comparison Result

The new detector intentionally reports `Unknown` where the legacy detector would
have guessed from display names, substring-normalized IDs, Store CLI table text,
or WinGet `msstore` rows without an exact PFN/Product-ID association.

This is a correctness-preserving regression in apparent coverage: ambiguous
legacy positives are not migrated into authoritative Store updates.

## Automated Evidence

The following test groups cover the migration:

- `store_update_model_test.go`: reconciliation states, provider failure,
  stale evidence, cross-user rejection, and compatibility adapter behavior.
- `store_scan_pipeline_test.go`: transactional persistence, migration,
  concurrent scans, stale generation rejection, cross-user isolation,
  positive hysteresis, conflicts, cancellation, and fallback to previous
  published generation.
- `store_update_api_test.go`: API serialization of Store states and scan
  health.
- `store_update_execution_test.go`: exact target execution, accepted versus
  verified states, cancellation, wrong-user events, and impossible display-name
  fallback.
- `state_test.go` and `package_manager_test.go`: canonical Store
  auto-update preference migration.
- `store_diagnostics_export_test.go`: diagnostics export evidence and raw user
  SID redaction.

## Windows Smoke-Test Evidence

Smoke-test procedures exist under `docs/windows-smoke-tests/`, including native
inventory and privilege-boundary coverage. This migration report does not claim
new real-machine smoke results for this cutover run. Required release-gate
execution is listed in `docs/release-gate/store-cutover-checklist.md`.

## Cutover Decision

The new detector is default. The explicit one-release rollback flag is:

```cmd
set UPDATER_STORE_LEGACY_DETECTOR=1
```

Without that flag, legacy Store heuristics do not produce update truth. New
detector failure produces `Unknown`.
