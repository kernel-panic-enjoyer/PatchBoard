# Store CLI output fixtures

Captured Microsoft Store CLI (`store …`) output samples used by the
`internal/updater` Store parsing/provider tests, organized as:

    testdata/storecli/<store-cli-version>/<locale>/<sample>.txt

- `<store-cli-version>` — the Store CLI build the sample represents
  (e.g. `22605.1401.12.0`).
- `<locale>` — the UI language of any localized prompt text in the sample:
  `en-US`, `de-DE`, or `neutral` for samples that contain only
  language-independent records (Product ID / PFN / Available Version lines).

Load samples in tests with `loadStoreCLIFixture(t, version, locale, name)`
(see `store_fixtures_test.go`). The loader normalizes CRLF to LF and strips the
trailing newline, so the returned string matches a Store CLI capture exactly.
