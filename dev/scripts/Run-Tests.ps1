<#
.SYNOPSIS
    Runs the PatchBoard test suites independently.

.DESCRIPTION
    Runs each suite as its own step and reports each result separately:

      1. Root module unit/integration tests:   go test ./...
      2. Browser-level UI tests (separate tests/browser module):
                                                 go test -tags uitestsupport ./...
         These run only when a Chromium/Edge browser is available; otherwise the
         step is skipped (the tests also self-skip when no browser is found).

    With -Live, additionally runs the destructive/live Microsoft Store tests
    (build tag "storelive"). Those remain gated by their own
    UPDATER_RUN_STORE_LIVE_* / UPDATER_APPLY_STORE_LIVE_UPDATE environment
    variables and self-skip unless those gates are set.

    With -SelfUpdateIntegration, additionally compiles and runs the staged
    self-update replacement test. The test is excluded from default test
    binaries because its temporary self-replacing executable can trigger
    endpoint-security heuristics.

    With -ProcessIntegration, additionally runs the Windows Job Object tests.
    Those tests start hidden child processes and terminate their process trees,
    so they are excluded from default test binaries to avoid endpoint-security
    false positives on developer workstations.

    With -CrashIntegration, additionally runs the crash-log capture test. It
    relaunches the test binary as a hidden child that deliberately panics, so
    it is excluded from default test binaries for the same endpoint-security
    reason.

    Requires Go >= 1.26.11 on PATH (the module targets go 1.26.11).

.EXAMPLE
    .\dev\scripts\Run-Tests.ps1

.EXAMPLE
    .\dev\scripts\Run-Tests.ps1 -SkipBrowser

.EXAMPLE
    $env:UPDATER_RUN_STORE_LIVE_TESTS = '1'; .\dev\scripts\Run-Tests.ps1 -Live

.EXAMPLE
    .\dev\scripts\Run-Tests.ps1 -SelfUpdateIntegration

.EXAMPLE
    .\dev\scripts\Run-Tests.ps1 -ProcessIntegration
#>
[CmdletBinding()]
param(
    [switch]$CrashIntegration,
    [switch]$Live,
    [switch]$ProcessIntegration,
    [switch]$SelfUpdateIntegration,
    [switch]$SkipBrowser
)

$ErrorActionPreference = 'Stop'
$root = (Resolve-Path (Join-Path $PSScriptRoot '..\..')).Path
$results = [ordered]@{}

function Test-BrowserAvailable {
    if ($env:CHROME_PATH -and (Test-Path -LiteralPath $env:CHROME_PATH)) { return $true }
    $candidates = @(
        (Join-Path $env:ProgramFiles 'Microsoft\Edge\Application\msedge.exe'),
        (Join-Path ${env:ProgramFiles(x86)} 'Microsoft\Edge\Application\msedge.exe'),
        (Join-Path $env:LocalAppData 'Microsoft\Edge\Application\msedge.exe'),
        (Join-Path $env:ProgramFiles 'Google\Chrome\Application\chrome.exe'),
        (Join-Path ${env:ProgramFiles(x86)} 'Google\Chrome\Application\chrome.exe'),
        (Join-Path $env:LocalAppData 'Google\Chrome\Application\chrome.exe')
    )
    foreach ($candidate in $candidates) {
        if ($candidate -and (Test-Path -LiteralPath $candidate)) { return $true }
    }
    foreach ($name in @('msedge.exe', 'chrome.exe', 'chromium.exe')) {
        if (Get-Command $name -ErrorAction SilentlyContinue) { return $true }
    }
    return $false
}

# 1. Root module unit/integration tests.
Write-Host '== Root module tests: go test ./... ==' -ForegroundColor Cyan
Push-Location $root
try {
    & go test ./... -count=1
    $results['root'] = $LASTEXITCODE
} finally {
    Pop-Location
}

# 2. Browser-level UI tests (separate module, build tag required).
if ($SkipBrowser) {
    Write-Host '== Browser module tests: skipped (-SkipBrowser) ==' -ForegroundColor Yellow
    $results['browser'] = 'skipped'
} elseif (-not (Test-BrowserAvailable)) {
    Write-Host '== Browser module tests: skipped (no Chromium/Edge found) ==' -ForegroundColor Yellow
    $results['browser'] = 'skipped (no browser)'
} else {
    Write-Host '== Browser module tests: go test -tags uitestsupport ./... (tests/browser) ==' -ForegroundColor Cyan
    Push-Location (Join-Path $root 'tests\browser')
    try {
        & go test -tags uitestsupport ./... -count=1
        $results['browser'] = $LASTEXITCODE
    } finally {
        Pop-Location
    }
}

# 3. Optional destructive/live Microsoft Store tests.
if ($Live) {
    Write-Host '== Live Store tests: go test -tags storelive ./internal/updater/ -run TestLive ==' -ForegroundColor Cyan
    $liveGated = $env:UPDATER_RUN_STORE_LIVE_TESTS -or
        $env:UPDATER_RUN_STORE_LIVE_API_TESTS -or
        $env:UPDATER_RUN_STORE_LIVE_TARGET_TESTS -or
        $env:UPDATER_RUN_STORE_LIVE_EXECUTION_TESTS
    if (-not $liveGated) {
        Write-Host '  No UPDATER_RUN_STORE_LIVE_* gate set; the live tests will self-skip.' -ForegroundColor Yellow
    }
    Push-Location $root
    try {
        & go test -tags storelive ./internal/updater/ -run TestLive -count=1 -v
        $results['live'] = $LASTEXITCODE
    } finally {
        Pop-Location
    }
}

if ($ProcessIntegration) {
    Write-Host '== Windows process-tree integration tests ==' -ForegroundColor Cyan
    Push-Location $root
    try {
        & go test -tags processintegration ./internal/updater -run '^Test(NamedCommandProcessOwnerLetsWorkerJoinItself|CommandProcessOwnerTerminatesGrandchildProcessTree|CommandProcessOwnerAssignsBeforeResumingMutableCommand|RunCommandContextCancellationTerminatesOwnedPackageProcessTree)$' -count=1
        $results['process'] = $LASTEXITCODE
    } finally {
        Pop-Location
    }
}

if ($CrashIntegration) {
    Write-Host '== Crash log capture integration test ==' -ForegroundColor Cyan
    Push-Location $root
    try {
        & go test -tags crashintegration ./internal/updater -run '^TestCrashOutputFileCapturesUnrecoveredPanic$' -count=1
        $results['crash'] = $LASTEXITCODE
    } finally {
        Pop-Location
    }
}

if ($SelfUpdateIntegration) {
    Write-Host '== Self-update replacement integration test ==' -ForegroundColor Cyan
    $previousGate = $env:PATCHBOARD_RUN_SELF_UPDATE_INTEGRATION
    Push-Location $root
    try {
        $env:PATCHBOARD_RUN_SELF_UPDATE_INTEGRATION = '1'
        & go test -tags selfupdateintegration ./internal/updater -run '^Test(SelfUpdateHelperMustAcknowledgeReadiness|StagedSelfUpdateHelperReplacesPortableOriginal)$' -count=1
        $results['self-update'] = $LASTEXITCODE
    } finally {
        if ($null -eq $previousGate) {
            Remove-Item Env:PATCHBOARD_RUN_SELF_UPDATE_INTEGRATION -ErrorAction SilentlyContinue
        } else {
            $env:PATCHBOARD_RUN_SELF_UPDATE_INTEGRATION = $previousGate
        }
        Pop-Location
    }
}

# Summary: report each suite independently.
Write-Host ''
Write-Host '== Summary ==' -ForegroundColor Cyan
$failed = $false
foreach ($key in $results.Keys) {
    $value = $results[$key]
    if ($value -is [int]) {
        if ($value -eq 0) {
            Write-Host ("  {0,-8} PASS" -f $key) -ForegroundColor Green
        } else {
            Write-Host ("  {0,-8} FAIL (exit {1})" -f $key, $value) -ForegroundColor Red
            $failed = $true
        }
    } else {
        Write-Host ("  {0,-8} {1}" -f $key, $value) -ForegroundColor Yellow
    }
}

if ($failed) { exit 1 } else { exit 0 }
