# Store Architecture Compliance Report

Date: 2026-06-21

## Non-Negotiable Invariants

1. **No Store identity from display names, fuzzy strings, or rank.**
   - Production: `storeNewDetectorActive`, `package_inventory_appx.go`,
     `package_inventory.go`, and `update_retry.go` prevent legacy Store
     heuristics from producing update truth by default.
   - Tests: `TestPackageForUpdateUsesExactStoreInventoryMetadata`,
     `TestStoreExactUpdateDisplayNameFallbackIsImpossible`.

2. **Installed Store identity is `(user SID, package family name)`.**
   - Production: `StoreInstalledIdentity`, `StoreScanStore`,
     `canonicalStoreAutoUpdateKey`.
   - Tests: `TestGroupStorePackagedAppFamiliesUsesPFNIdentity`,
     `TestLoadStateNormalizesStoreAutoUpdateKeys`.

3. **Current only after fresh complete scan with healthy required providers.**
   - Production: `ReconcileStoreUpdate`, `StoreScanPipeline.Run`.
   - Tests: `TestReconcileStoreUpdate`, `TestTransactionalStoreAssessmentAPISerializesPublishedStates`.

4. **Failure, parser rejection, stale evidence, unresolved identity, incomplete
   coverage, or user mismatch yields Unknown.**
   - Production: `sanitizeCatalogProviderRun`, `scanCompletionStatus`,
     `ReconcileStoreUpdate`.
   - Tests: `TestStoreScanCrossUserEvidenceBecomesUnknown`,
     `TestStoreScanProviderPartialFailureIsUnknown`,
     `TestStoreAssessmentUnresolvedIdentityIsUnknown`.

5. **No cross-user or cross-generation evidence mixing.**
   - Production: `sanitizeCatalogProviderRun`,
     `StoreScanStore.persistScanTx`.
   - Tests: `TestStoreScanCrossUserEvidenceBecomesUnknown`,
     `TestStoreScanOlderCompletionDoesNotPublishOverNewerScan`.

6. **Stale/incomplete negative evidence cannot erase fresh positive updates.**
   - Production: `shouldRetainPreviousPositive`,
     `reconcileStoreScanAssessments`.
   - Tests: `TestStoreScanPositiveHysteresisRetainsUpdateOnIncompleteScan`,
     `TestTransactionalStoreAssessmentAPIStalePositiveAndBrowserReload`.

7. **Store update execution requires exact verified target.**
   - Production: `exactStoreUpdateRequestFromPackage`,
     `packageHasExactStoreUpdateTarget`.
   - Tests: `TestStoreExactUpdateValidationRequiresFreshAvailableAssessment`,
     `TestStoreExactUpdateRejectedTargetFails`.

8. **Command success is accepted, not final success.**
   - Production: `StoreExactUpdateExecutor.ExecuteWithCallbacks`,
     `verifyAcceptedAction`.
   - Tests: `TestStoreExactUpdateAcceptedWithoutPackageChangeIsNotVerified`,
     `TestStoreExactUpdateVerifiesVersionChange`.

9. **Winget and Chocolatey behavior remains stable unless directly required.**
   - Production: cutover checks are Store-specific.
   - Tests: existing winget/choco parser and action tests remain active.

10. **Production changes require tests, migration, and rollback.**
    - Tests: full suite under `internal/updater`.
    - Migration: `state_migrations.go`,
      `docs/reports/store-detector-migration-report.md`.
    - Rollback: `UPDATER_STORE_LEGACY_DETECTOR=1`.

## Known Gaps

- Native Store catalog update provider remains incomplete in this build. Until
  that provider emits exact positive or authoritative negative observations,
  affected Store packages are expected to show `Unknown`.
- Real `PackageCatalog.OpenForCurrentUser` event verification still requires
  Windows smoke validation.
