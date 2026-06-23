# Microsoft Store Troubleshooting

## Store Shows Unknown

`Unknown` means the app lacks enough exact, current-user evidence to say the
package is current. This is intentional. Common causes:

- Native current-user inventory failed.
- Store catalog provider is unavailable or unsupported.
- The package identity could not be resolved to package family name.
- The scan is stale or incomplete.
- Provider observations disagreed.
- The exact Product ID/action target is not verified.

Use **Export Store Diagnostics** in the Store scan-health panel to capture the
sanitized scan evidence.

## Store Update Button Is Disabled

Store updates require:

- Fresh `available` assessment.
- Current user SID matching the installed package identity.
- Exact package family name.
- Verified Store Product ID or exact provider action target.
- Healthy action provider.

The app does not retry by display name or Store search result rank.

## Legacy Detector Removed

The display-name Store detector has been retired. If exact Store providers fail,
the app reports `Unknown` with diagnostics instead of falling back to fuzzy
Store searches or legacy AppX merge heuristics.
