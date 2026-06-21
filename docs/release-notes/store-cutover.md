# Release Notes: Microsoft Store Detector Cutover

## Changed

- The transactional Microsoft Store detector is now the default.
- Legacy Store display-name resolution, substring scoring, punctuation-stripped
  identity equivalence, Store CLI table update truth, and WinGet `msstore`
  truth merging are no longer active by default.
- Store package update status uses explicit states: `available`, `current`,
  `unknown`, `conflict`, `inapplicable`, and `pending`.
- Store update execution requires a fresh available assessment and verified
  Product ID/action target.
- Store command success is reported as accepted until post-action verification
  proves completion.
- Store auto-update preferences migrate to current-user package family identity.

## Added

- Store diagnostics export from the WebUI scan-health panel.
- Store detector migration report.
- Release-gate matrix and compliance report.
- One-release emergency rollback flag:
  `UPDATER_STORE_LEGACY_DETECTOR=1`.

## Removed From Active Detection

- Display-name Store update resolution.
- Store identity matching by normalized names or punctuation-stripped strings.
- Normal `Get-AppxPackage -AllUsers` Store inventory.
- Store update execution by display-name search fallback.
- Treating empty provider output as no update.

## Expected Behavior

Some Store packages may now show `Unknown` instead of `Current` or `Update`
until exact provider evidence is available. That is a correctness improvement:
the app no longer guesses from ambiguous Store output.
