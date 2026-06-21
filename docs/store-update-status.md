# Microsoft Store Update Status

The Store update model separates **Unknown** from **Current** on purpose.

`Current` means the app completed a fresh Store scan in the interactive user's session, all required Store providers were healthy, the installed package identity was exact, and the providers returned authoritative negative update evidence.

`Unknown` means the app does not have enough trustworthy evidence to say the Store app is current. Common causes include provider failure, incomplete scans, stale data, unresolved package identity, missing exact Store action targets, or user-context mismatch.

An update can remain visible as stale after an incomplete rescan. That prevents a failed or partial scan from erasing a previously verified positive update offer.

Store update buttons are enabled only when an exact verified action target is available. The WebUI must not silently fall back to display-name Store searches for updates.

## Detector Cutover

The transactional Store detector is the default. Legacy Store heuristics are not
used as a silent fallback. If the new detector cannot complete, Store package
status remains `Unknown` and the scan-health panel explains which provider or
identity requirement failed.

The one-release emergency rollback is explicit:

```cmd
set UPDATER_STORE_LEGACY_DETECTOR=1
```

This flag re-enables the old Store detector path for emergency diagnosis only.
It should not be used to treat display-name or fuzzy matches as durable Store
identity.

## Exact Update Execution

For Store packages with assessment data, update execution uses only the verified
Store Product ID or provider-specific exact target. A successful command return
means the request was accepted. Final success is reported only after
post-action verification.

Current verification supports exact current-user inventory polling. Event-based
verification is isolated behind an interface until the native broker implements
`Windows.ApplicationModel.PackageCatalog.OpenForCurrentUser`; Microsoft
documents that this catalog receives package events for packages installed for
the current user:
https://learn.microsoft.com/en-us/uwp/api/windows.applicationmodel.packagecatalog.packageinstalling

Exact current-user re-enumeration remains based on the documented
`Windows.Management.Deployment.PackageManager.FindPackagesForUser` family of
APIs:
https://learn.microsoft.com/en-us/uwp/api/windows.management.deployment.packagemanager.findpackagesforuser

If the Store command is accepted but inventory/catalog verification cannot prove
completion, the job reports `accepted_not_verified` rather than success.

## Diagnostics Export

The WebUI Store scan-health panel exposes `Export Store Diagnostics`. The export
includes scan context, provider health, canonical package family names,
sanitized observations, final assessments, and Store auto-update migration
notes. It excludes raw user SIDs, tokens, credentials, account identifiers, and
personal install locations.
