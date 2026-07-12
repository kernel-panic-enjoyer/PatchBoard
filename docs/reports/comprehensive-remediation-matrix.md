# Comprehensive Remediation Matrix

Baseline revision: `36c6f471cb3e7f8ae465346cee04acc1bd441cc6`

Baseline captured: `2026-07-12T12:16:08+02:00`

This matrix tracks the release-blocking remediation requested for PatchBoard. A
row is complete only when the production change, a regression test, and the
relevant validation gates pass. Status values are `pending`, `in progress`,
`complete`, `blocked`, or `not applicable`.

## Baseline

- Go: `go1.26.5 windows/amd64`; root and browser modules pass unit tests.
- Node: bundled `v24.14.0`; TypeScript `7.0.2` type-checks the current assets.
- Browser: Microsoft Edge `150.0.4078.65`; tagged browser tests pass locally.
- Static analysis: `go vet`, Staticcheck `v0.7.0`, and govulncheck `v1.5.0`
  pass.
- Race detector: full root-module race suite passes with MSYS2 UCRT64 GCC.
- Build and distribution smoke tests pass; the baseline executable was built
  under `dist/`.

## Remediation Work

| ID | Severity | Area | Affected boundary | Starting behavior | Required behavior / regression | Phase | Status |
| --- | --- | --- | --- | --- | --- | --- | --- |
| SU-01 | Critical | Self-update | staged apply helper | A staged helper authorizes only itself or Program Files, so a portable original target cannot be replaced. | Carry an explicit one-use authorization for the original target; prove a staged helper can replace a portable original. | 1 | complete |
| SU-02 | Critical | Self-update | parent/helper handoff | The parent treats process start as helper readiness and may shut down before the helper validates its request. | Add an authenticated readiness handshake; a failed helper launch must neither report success nor stop the parent. | 1 | complete |
| SU-03 | Critical | Self-update | staged source | Hashing and copying can observe different path contents. | Open once, validate and copy from the same handle; reject source substitution. | 1 | complete |
| SU-04 | Critical | Self-update | replacement transaction | Replacement lacks a complete startup-health acknowledgement and durable rollback decision. | Stage, verify, replace, restart, await health acknowledgement, and restore backup on failure. | 1 | complete |
| SU-05 | Critical | Supply-chain | release authorization | Digest plus GitHub release metadata does not provide an independent signer/attestation trust root. | Bind repo, revision, semver, PE architecture, Go/build metadata, license, signature/attestation, provenance, and SBOM; fail closed where trust is absent. | 1, 9 | in progress |
| SU-06 | High | Self-update | download client | Redirect and final-origin behavior is not explicitly constrained by the release policy. | Enforce bounded redirects and approved HTTPS origins; test cross-origin and downgrade rejection. | 1 | complete |
| PR-01 | Critical | Process ownership | Windows command launch | A child can run before assignment to the kill-on-close Job Object. | Launch atomically owned with `PROC_THREAD_ATTRIBUTE_JOB_LIST` or suspended/assign/resume; prove child/grandchild cleanup. | 2 | complete |
| PR-02 | Critical | Cancellation | command runner | Cancellation and final wait paths can block indefinitely. | Bound terminate/wait/pipe-drain phases and return a typed timeout diagnostic. | 2 | complete |
| PR-03 | High | Command API | provider execution | Boolean-heavy command calls obscure mutability, elevation, timeout, output and cancellation policy. | Introduce a typed `CommandSpec` and migrate provider call sites incrementally. | 2 | complete |
| PR-04 | High | Elevation | worker lifecycle | Parent and elevated worker cancellation can diverge. | Use one cancellation protocol with acknowledgement and bounded teardown. | 2 | complete |
| PR-05 | Medium | Diagnostics | process termination | Process identifiers and exit/termination phases are not consistently correlated. | Emit bounded, redacted correlation fields for launch, cancellation, termination and exit. | 2 | complete |
| ST-01 | Critical | State recovery | primary/backup load | A missing primary can ignore a valid backup and silently recreate defaults. | Recover from a valid backup when primary is missing and report provenance/health. | 3 | complete |
| ST-02 | Critical | State recovery | corrupt files | Corrupt-primary and corrupt-backup outcomes are not sufficiently explicit to callers. | Distinguish healthy, recovered, degraded and unrecoverable state without silently losing preferences. | 3 | pending |
| ST-03 | High | Persistence | state ownership | Unrelated settings, scheduler state and historical results share one broad state document. | Split persistence domains with independent schemas/migrations while preserving compatibility. | 3 | pending |
| ST-04 | High | Persistence | bounded collections | Lexical/truncated identity eviction can remove the wrong records or collapse identities. | Bound by explicit chronology and full canonical keys; test adversarial keys and migrations. | 3 | pending |
| ST-05 | High | Windows storage | file transaction | Atomic replace, ACL and cross-process lock behavior need stronger invariant coverage. | Preserve live state, flush same-directory temp, replace atomically, retain known-good backup and private ACLs. | 3 | pending |
| IN-01 | Critical | Inventory | synchronous refresh | An older refresh can publish after a newer generation and overwrite authoritative inventory. | Add generation ownership and discard superseded results; retain previous inventory on cancellation/failure. | 4 | complete |
| IN-02 | High | Inventory | publication status | Loading/error/superseded states are distributed and can misrepresent freshness. | Model refresh outcome explicitly and publish timestamps only for authoritative success. | 4 | pending |
| JB-01 | Critical | Jobs | deduplication | Equivalent requests can use non-canonical identities and enqueue duplicate work. | Deduplicate by canonical package action identity across API, scheduler and workers. | 4 | pending |
| JB-02 | High | Jobs | scheduler indexes | Lifetime bookkeeping can grow even when visible job history is bounded. | Bound all scheduler maps/indexes; stress at least 100,000 completed jobs. | 4 | pending |
| JB-03 | High | Jobs | state machine | Job status is represented by interacting booleans and strings. | Use validated transitions that cannot leave active/running flags inconsistent after panic or cancellation. | 4 | pending |
| LC-01 | Critical | Lifecycle | shutdown | Background activities and shutdown order are not represented by one ownership model. | Stop admission, cancel root, terminate trees, wait with deadline, then release server/tray resources; repeated shutdown is safe. | 4 | pending |
| LC-02 | High | Context | application call graph | Application-path `context.Background()` calls can detach work from shutdown/request cancellation. | Keep background contexts only at true process entrypoints; audit and document remaining uses. | 4 | pending |
| PK-01 | Critical | WinGet | update target | Update execution can broaden from the exact selected package through positional/name fallback. | Use only inventory-proven exact identifiers and provider/source target; regression rejects positional broadening. | 5 | pending |
| PK-02 | Critical | Chocolatey | package variant | `.install`/`.portable` variants can be guessed rather than inventory-proven. | Update only the exact installed package identity; regression rejects variant guessing. | 5 | pending |
| PK-03 | High | Domain model | package identity | One broad package struct mixes display, inventory evidence, action target and UI projection. | Introduce typed provider-specific identity and action-target values with explicit validation. | 5 | pending |
| PK-04 | High | Versioning | provider policies | Generic version comparison can imply semantics a provider does not guarantee. | Define provider-specific comparison/verification policies and malformed/opaque handling. | 5 | pending |
| MS-01 | Critical | Microsoft Store | identity/evidence | Store safety depends on exact SID+PFN and generation/freshness rules spread across multiple layers. | Preserve exact identity; centralize authorization so unknown/stale/conflict/wrong-user evidence is never actionable. | 5 | pending |
| MS-02 | Critical | Microsoft Store | execution | A display or queue hint could be confused with an exact action target. | Require a fresh exact target and independent post-action verification at execution time. | 5 | pending |
| HT-01 | High | HTTP | content type | Content-Type handling is ad hoc and can accept malformed or unsupported media types. | Use `mime.ParseMediaType`; return 415 for unsupported media types. | 6 | pending |
| HT-02 | High | HTTP | mutation boundary | Mutation parameters can arrive through query strings and method errors omit complete protocol metadata. | Require bounded bodies for mutations, set `Allow` on 405, and reject GET/HEAD bodies including chunked requests. | 6 | pending |
| HT-03 | High | HTTP | errors | API failures are largely free-form strings, limiting safe client handling and observability. | Add versioned structured errors and request IDs while retaining redacted diagnostics. | 6 | pending |
| HT-04 | Critical | Web security | session boundary | Host/origin/session/CSRF/bootstrap protections must survive routing and DTO refactors. | Add regressions for every mutating route and preserve local-session authorization. | 6 | pending |
| HT-05 | High | API contracts | internal/domain exposure | Persistence/domain structs are reused as public JSON contracts. | Define versioned request/response DTOs and map explicitly at the boundary. | 6 | pending |
| AR-01 | High | Architecture | backend package | The updater package combines command, domain, persistence, HTTP, Windows and self-update concerns. | Extract vertical ownership boundaries incrementally with dependency direction tests/documentation. | 7 | pending |
| AR-02 | Medium | Maintainability | globals/test hooks | Package globals and mutable hooks obscure ownership and parallel-test safety. | Replace with constructor-injected ports and scoped fakes; remove obsolete compatibility paths. | 7, 10 | pending |
| FE-01 | High | Frontend | monolithic script | UI behavior, state, network calls and rendering share a large JavaScript module. | Split TypeScript modules by API, stores, controllers and views without duplicating business rules. | 8 | pending |
| FE-02 | High | Frontend | DOM contracts | Optional element lookups can hide template regressions until interaction time. | Use typed required-element helpers and fail fast during bootstrap. | 8 | pending |
| FE-03 | High | Frontend | API errors/state | Stringly errors and implicit globals create inconsistent reconnect/loading behavior. | Use typed API responses/errors and explicit stores with deterministic transitions. | 8 | pending |
| FE-04 | High | Accessibility | interactive UI | Keyboard/focus/dialog/live-region behavior lacks full browser coverage. | Add deterministic accessibility interaction tests for dialogs, settings, jobs, toasts and reconnect states. | 8 | pending |
| CI-01 | Critical | Browser CI | browser availability | Browser CI treats a missing browser as a successful skip. | Install/pin a browser and fail if it cannot launch; archive useful failure artifacts. | 9 | pending |
| CI-02 | High | Portable CI | non-Windows guard | Linux coverage is documentation-only and does not exercise portable packages. | Add real portable unit/static tests while excluding Windows-only code through build constraints. | 9 | pending |
| CI-03 | Critical | Release | revision binding | Release workflow can build `main` rather than the exact approved release revision. | Resolve and validate one immutable revision, build once, and promote the same artifacts. | 9 | pending |
| CI-04 | Critical | Supply-chain | workflow dependencies | Actions and MSYS2 package acquisition rely on mutable tags/repositories. | Pin actions by commit SHA and use a deterministic race-toolchain source with dependency automation. | 9 | pending |
| CI-05 | High | Release | artifacts | Release lacks complete SBOM, provenance and independent signing/attestation policy. | Generate and publish checksums, SBOM and provenance; enforce signature/attestation verification in update flow. | 9 | pending |
| CI-06 | High | Quality gates | fuzz/coverage/hardware | Fuzzing, coverage and hardware-gated tests are incomplete or potentially misleading. | Expand fuzz targets/fixtures, publish coverage, and make opt-in hardware/live gates explicit. | 9 | pending |
| CL-01 | Medium | Cleanup | dead/legacy code | Compatibility wrappers and stale hook paths remain after migrations. | Remove only after all consumers migrate and regression coverage proves behavior. | 10 | pending |
| CL-02 | Medium | Diagnostics | redaction/output | Secret redaction and bounded output need repository-wide consistency. | Centralize redaction/limits and test encoded, split and oversized inputs. | 10 | pending |
| CL-03 | Medium | Time/testability | clocks/backoff | Wall-clock calls make lifecycle and retry behavior harder to test deterministically. | Inject clocks/timers at ownership boundaries without introducing global test hooks. | 10 | pending |
| DOC-01 | High | Documentation | architecture/security | Existing docs do not cover the complete post-remediation trust, lifecycle and persistence model. | Update ADRs, threat model, state recovery, self-update trust, process tree, testing and release runbooks. | 10 | pending |

## Status Rules

- `in progress`: a failing regression has been reproduced or production work
  has started, but the slice is not fully validated.
- `complete`: behavior, tests, documentation and all applicable focused gates
  pass.
- `blocked`: the exact external or environmental blocker is recorded with a
  safe fallback; lack of time is not a blocker.
- `not applicable`: repository inspection proves the requested issue does not
  exist, with evidence recorded in the final report.
