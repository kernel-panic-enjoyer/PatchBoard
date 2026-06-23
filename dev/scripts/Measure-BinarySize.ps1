param(
    [string]$Root = (Resolve-Path (Join-Path $PSScriptRoot '..\..')).Path,
    [string]$Label = '',
    [int]$Top = 50
)

$ErrorActionPreference = 'Stop'

function Assert-NativeSuccess {
    param(
        [string]$Label
    )
    if ($LASTEXITCODE -ne 0) {
        throw "$Label failed with exit code $LASTEXITCODE"
    }
}

function Format-MiB {
    param(
        [long]$Bytes
    )
    return [string]::Format([Globalization.CultureInfo]::InvariantCulture, '{0:N3}', ($Bytes / 1MB))
}

function ConvertTo-SafeFileName {
    param(
        [string]$Value
    )
    $invalid = [IO.Path]::GetInvalidFileNameChars()
    $chars = foreach ($char in $Value.ToCharArray()) {
        if ($invalid -contains $char) {
            '_'
        } else {
            $char
        }
    }
    $safe = -join $chars
    $safe = $safe.Trim()
    if ([string]::IsNullOrWhiteSpace($safe)) {
        return 'binary-size'
    }
    return $safe
}

function Get-NormalizedSymbolName {
    param(
        [string]$Name
    )
    $normalized = $Name
    foreach ($prefix in @('type:', 'go:itab.', 'go:string.', 'go:func.')) {
        if ($normalized.StartsWith($prefix, [StringComparison]::Ordinal)) {
            return $normalized.Substring($prefix.Length)
        }
    }
    return $normalized
}

function Get-SymbolGroup {
    param(
        [string]$Name
    )
    $normalized = Get-NormalizedSymbolName $Name
    $knownGroups = @(
        @{ Name = 'modernc.org/sqlite'; Prefixes = @('modernc.org/sqlite') },
        @{ Name = 'modernc.org/libc'; Prefixes = @('modernc.org/libc') },
        @{ Name = 'modernc.org/memory'; Prefixes = @('modernc.org/memory') },
        @{ Name = 'modernc.org/mathutil'; Prefixes = @('modernc.org/mathutil') },
        @{ Name = 'bigfft / generated SQLite support'; Prefixes = @('modernc.org/bigfft', 'github.com/remyoudompheng/bigfft') },
        @{ Name = 'windows-updater-webui/internal/updater'; Prefixes = @('windows-updater-webui/internal/updater') },
        @{ Name = 'golang.org/x/sys'; Prefixes = @('golang.org/x/sys') },
        @{ Name = 'net/http'; Prefixes = @('net/http') },
        @{ Name = 'crypto/*'; Prefixes = @('crypto/') },
        @{ Name = 'encoding/json'; Prefixes = @('encoding/json') },
        @{ Name = 'runtime'; Prefixes = @('runtime') },
        @{ Name = 'embedded frontend assets'; Prefixes = @(
                'windows-updater-webui/internal/updater.uiCSS',
                'windows-updater-webui/internal/updater.uiJS',
                'windows-updater-webui/internal/updater.appIconICO'
            )
        }
    )
    foreach ($group in $knownGroups) {
        foreach ($prefix in $group.Prefixes) {
            if ($Name.StartsWith($prefix, [StringComparison]::Ordinal) -or $normalized.StartsWith($prefix, [StringComparison]::Ordinal)) {
                return $group.Name
            }
        }
    }

    $slash = $normalized.LastIndexOf('/')
    if ($slash -ge 0) {
        $dotAfterSlash = $normalized.IndexOf('.', $slash + 1)
        if ($dotAfterSlash -gt 0) {
            return $normalized.Substring(0, $dotAfterSlash)
        }
    }

    $dot = $normalized.IndexOf('.')
    if ($dot -gt 0) {
        return $normalized.Substring(0, $dot)
    }

    if ($normalized.StartsWith('go:', [StringComparison]::Ordinal)) {
        return 'go metadata'
    }

    return '<unknown>'
}

function Read-NmSymbols {
    param(
        [string]$Path
    )
    $symbols = New-Object System.Collections.Generic.List[object]
    foreach ($line in Get-Content -LiteralPath $Path) {
        $trimmed = $line.Trim()
        if ([string]::IsNullOrWhiteSpace($trimmed)) {
            continue
        }
        $parts = $trimmed.Split([char[]]@(' ', "`t"), [StringSplitOptions]::RemoveEmptyEntries)
        if ($parts.Count -lt 4) {
            continue
        }
        $size = 0L
        if (-not [long]::TryParse($parts[1], [ref]$size)) {
            continue
        }
        if ($size -le 0) {
            continue
        }
        $name = [string]::Join(' ', $parts[3..($parts.Count - 1)])
        $symbols.Add([pscustomobject]@{
                Address = $parts[0]
                Bytes   = $size
                MiB     = Format-MiB $size
                Type    = $parts[2]
                Group   = Get-SymbolGroup $name
                Name    = $name
            })
    }
    return $symbols
}

function Get-EmbeddedAssetReport {
    param(
        [string]$Root
    )
    $assets = @(
        'internal/updater/assets/app.ico',
        'internal/updater/assets/ui.css',
        'internal/updater/assets/ui.js'
    )
    $rows = foreach ($relative in $assets) {
        $path = Join-Path $Root $relative
        if (Test-Path -LiteralPath $path) {
            $file = Get-Item -LiteralPath $path
            [pscustomobject]@{
                Path  = $relative
                Bytes = [long]$file.Length
                MiB   = Format-MiB ([long]$file.Length)
            }
        }
    }
    return @($rows)
}

$Root = (Resolve-Path -LiteralPath $Root).Path
Push-Location $Root
try {
    $commit = (git rev-parse HEAD).Trim()
    $branchOutput = @(git branch --show-current)
    if ($branchOutput.Count -gt 0) {
        $branch = ([string]$branchOutput[0]).Trim()
    } else {
        $branch = ''
    }
    if ([string]::IsNullOrWhiteSpace($branch)) {
        $branch = 'detached'
    }
    $goVersion = (go version).Trim()
    $goos = (go env GOOS).Trim()
    $goarch = (go env GOARCH).Trim()

    if ([string]::IsNullOrWhiteSpace($Label)) {
        $Label = "$branch-$($commit.Substring(0, 12))"
    }
    $safeLabel = ConvertTo-SafeFileName $Label
    $stamp = Get-Date -Format 'yyyyMMdd-HHmmss'
    $analysisRoot = Join-Path $Root 'dist\size-analysis'
    $runDir = Join-Path $analysisRoot "$stamp-$safeLabel"
    New-Item -ItemType Directory -Force -Path $runDir | Out-Null

    $exe = Join-Path $runDir 'WindowsUpdaterWebUI.exe'
    $buildCommandDisplay = "go build -ldflags='-H=windowsgui' -o `"$exe`" ."
    & go build '-ldflags=-H=windowsgui' -o $exe .
    Assert-NativeSuccess 'go build'

    $exeFile = Get-Item -LiteralPath $exe
    $versionReport = Join-Path $runDir 'go-version-m.txt'
    $nmReport = Join-Path $runDir 'nm-size-sort.txt'
    $metadataReport = Join-Path $runDir 'metadata.json'
    $groupReport = Join-Path $runDir 'package-groups.csv'
    $targetReport = Join-Path $runDir 'target-prefixes.csv'
    $symbolReport = Join-Path $runDir 'top-symbols.csv'
    $assetReport = Join-Path $runDir 'embedded-assets.csv'
    $summaryReport = Join-Path $runDir 'summary.md'

    & go version -m $exe *> $versionReport
    Assert-NativeSuccess 'go version -m'
    & go tool nm -size -sort size $exe *> $nmReport
    Assert-NativeSuccess 'go tool nm'

    $symbols = @(Read-NmSymbols $nmReport)
    $groups = @(
        $symbols |
            Group-Object -Property Group |
            ForEach-Object {
                $bytes = [long](($_.Group | Measure-Object -Property Bytes -Sum).Sum)
                [pscustomobject]@{
                    Group       = $_.Name
                    Bytes       = $bytes
                    MiB         = Format-MiB $bytes
                    SymbolCount = $_.Count
                }
            } |
            Sort-Object -Property Bytes -Descending
    )
    $groups | Export-Csv -NoTypeInformation -Encoding UTF8 -Path $groupReport

    $topSymbols = @($symbols | Sort-Object -Property Bytes -Descending | Select-Object -First $Top)
    $topSymbols | Export-Csv -NoTypeInformation -Encoding UTF8 -Path $symbolReport

    $targetNames = @(
        'modernc.org/sqlite',
        'modernc.org/libc',
        'modernc.org/memory',
        'modernc.org/mathutil',
        'bigfft / generated SQLite support',
        'windows-updater-webui/internal/updater',
        'golang.org/x/sys',
        'net/http',
        'crypto/*',
        'encoding/json',
        'runtime',
        'embedded frontend assets'
    )
    $targetRows = foreach ($name in $targetNames) {
        $group = $groups | Where-Object { $_.Group -eq $name } | Select-Object -First 1
        if ($group) {
            $group
        } else {
            [pscustomobject]@{
                Group       = $name
                Bytes       = 0
                MiB         = Format-MiB 0
                SymbolCount = 0
            }
        }
    }
    $targetRows | Export-Csv -NoTypeInformation -Encoding UTF8 -Path $targetReport

    $embeddedAssets = @(Get-EmbeddedAssetReport $Root)
    $embeddedAssets | Export-Csv -NoTypeInformation -Encoding UTF8 -Path $assetReport
    $embeddedAssetBytes = [long](($embeddedAssets | Measure-Object -Property Bytes -Sum).Sum)
    $appSyso = Join-Path $Root 'app.syso'
    $appSysoBytes = 0L
    if (Test-Path -LiteralPath $appSyso) {
        $appSysoBytes = [long](Get-Item -LiteralPath $appSyso).Length
    }

    $moderncBytes = [long]((
            $targetRows |
                Where-Object { $_.Group -in @('modernc.org/sqlite', 'modernc.org/libc', 'modernc.org/memory', 'modernc.org/mathutil', 'bigfft / generated SQLite support') } |
                Measure-Object -Property Bytes -Sum
        ).Sum)
    $appCodeBytes = [long]((
            $targetRows |
                Where-Object { $_.Group -eq 'windows-updater-webui/internal/updater' } |
                Measure-Object -Property Bytes -Sum
        ).Sum)
    $chromedpSymbols = @($symbols | Where-Object {
            $_.Name.StartsWith('github.com/chromedp/', [StringComparison]::Ordinal) -or
            $_.Name.Contains('/chromedp')
        })
    $chromedpLinked = $chromedpSymbols.Count -gt 0

    $metadata = [ordered]@{
        label                   = $Label
        root                    = $Root
        commit                  = $commit
        branch                  = $branch
        go_version              = $goVersion
        goos                    = $goos
        goarch                  = $goarch
        build_command           = $buildCommandDisplay
        executable              = $exe
        executable_bytes        = [long]$exeFile.Length
        executable_mib          = Format-MiB ([long]$exeFile.Length)
        raw_go_version_m_report = $versionReport
        raw_nm_report           = $nmReport
        package_group_report    = $groupReport
        target_prefix_report    = $targetReport
        top_symbol_report       = $symbolReport
        embedded_asset_report   = $assetReport
        embedded_asset_bytes    = $embeddedAssetBytes
        embedded_asset_mib      = Format-MiB $embeddedAssetBytes
        app_syso_bytes          = $appSysoBytes
        app_syso_mib            = Format-MiB $appSysoBytes
        modernc_sqlite_bytes    = $moderncBytes
        modernc_sqlite_mib      = Format-MiB $moderncBytes
        app_code_bytes          = $appCodeBytes
        app_code_mib            = Format-MiB $appCodeBytes
        chromedp_linked         = $chromedpLinked
        chromedp_symbol_count   = $chromedpSymbols.Count
    }
    $metadata | ConvertTo-Json -Depth 6 | Set-Content -Encoding UTF8 -Path $metadataReport

    $summary = New-Object System.Collections.Generic.List[string]
    $summary.Add("# Binary Size Measurement")
    $summary.Add("")
    $summary.Add("- Label: ``$Label``")
    $summary.Add("- Commit: ``$commit``")
    $summary.Add("- Branch: ``$branch``")
    $summary.Add("- Go version: ``$goVersion``")
    $summary.Add("- GOOS/GOARCH: ``$goos/$goarch``")
    $summary.Add("- Build command: ``$buildCommandDisplay``")
    $summary.Add("- Executable size: $($exeFile.Length) bytes ($(Format-MiB ([long]$exeFile.Length)) MiB)")
    $summary.Add("- Estimated modernc/SQLite symbol size: $moderncBytes bytes ($(Format-MiB $moderncBytes) MiB)")
    $summary.Add("- Application package symbol size: $appCodeBytes bytes ($(Format-MiB $appCodeBytes) MiB)")
    $summary.Add("- Embedded frontend asset file size: $embeddedAssetBytes bytes ($(Format-MiB $embeddedAssetBytes) MiB)")
    $summary.Add("- app.syso file size: $appSysoBytes bytes ($(Format-MiB $appSysoBytes) MiB)")
    $summary.Add("- chromedp linked into production: $chromedpLinked")
    $summary.Add("")
    $summary.Add("## Top package groups")
    $summary.Add("")
    $summary.Add("| Group | Bytes | MiB | Symbols |")
    $summary.Add("| --- | ---: | ---: | ---: |")
    foreach ($group in ($groups | Select-Object -First $Top)) {
        $summary.Add("| $($group.Group) | $($group.Bytes) | $($group.MiB) | $($group.SymbolCount) |")
    }
    $summary.Add("")
    $summary.Add("## Top individual symbols")
    $summary.Add("")
    $summary.Add("| Symbol | Group | Bytes | MiB | Type |")
    $summary.Add("| --- | --- | ---: | ---: | --- |")
    foreach ($symbol in $topSymbols) {
        $escapedName = $symbol.Name.Replace('|', '\|')
        $escapedGroup = $symbol.Group.Replace('|', '\|')
        $summary.Add("| `$escapedName` | $escapedGroup | $($symbol.Bytes) | $($symbol.MiB) | $($symbol.Type) |")
    }
    $summary | Set-Content -Encoding UTF8 -Path $summaryReport

    Write-Output "Binary size report: $runDir"
    Write-Output "Executable size: $($exeFile.Length) bytes ($(Format-MiB ([long]$exeFile.Length)) MiB)"
    Write-Output "Build command: $buildCommandDisplay"
    Write-Output "chromedp linked into production: $chromedpLinked"
    Write-Output ""
    Write-Output "Top package groups:"
    $groups | Select-Object -First 10 | Format-Table -AutoSize | Out-String | Write-Output
    Write-Output "Top individual symbols:"
    $topSymbols | Select-Object -First 10 Name, Group, Bytes, MiB, Type | Format-Table -AutoSize | Out-String | Write-Output

    return [pscustomobject]@{
        RunDirectory = $runDir
        Metadata     = $metadataReport
        Summary      = $summaryReport
        Executable   = $exe
        Bytes        = [long]$exeFile.Length
        MiB          = Format-MiB ([long]$exeFile.Length)
    }
}
finally {
    Pop-Location
}
