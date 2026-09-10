# Shared helpers for the opt-in Windows native acceptance tools.
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

function Get-AbsoluteDirectory([string]$Path) {
    if (-not [IO.Path]::IsPathRooted($Path)) { throw 'Use an absolute directory path.' }
    $full = [IO.Path]::GetFullPath($Path).TrimEnd('\', '/')
    if ($full.Length -le [IO.Path]::GetPathRoot($full).Length -or $full -match '[\x00-\x1f]') {
        throw 'A filesystem root or control character is not an acceptance directory.'
    }
    # Refuse redirection through an existing junction or symbolic link.
    $ancestor = $full
    while ($ancestor) {
        if (Test-Path -LiteralPath $ancestor) {
            $item = Get-Item -LiteralPath $ancestor -Force
            if ($item.Attributes -band [IO.FileAttributes]::ReparsePoint) { throw "Reparse point is not supported: $ancestor" }
        }
        $ancestor = [IO.Path]::GetDirectoryName($ancestor)
    }
    return $full
}

function Write-NewText([string]$Path, [string]$Text) {
    $bytes = [Text.UTF8Encoding]::new($false).GetBytes($Text)
    $temporary = $Path + '.' + [Guid]::NewGuid().ToString('N') + '.tmp'
    $stream = [IO.File]::Open($temporary, [IO.FileMode]::CreateNew, [IO.FileAccess]::Write, [IO.FileShare]::None)
    try { $stream.Write($bytes, 0, $bytes.Length) } finally { $stream.Dispose() }
    # Same-directory rename publishes complete bytes and refuses an existing target.
    # A failed publish preserves the temporary file for inspection.
    [IO.File]::Move($temporary, $Path)
}

function Write-NewJSON([string]$Path, $Value) {
    Write-NewText $Path (($Value | ConvertTo-Json -Depth 12) + "`n")
}

function Get-SHA256([string]$Path) { return (Get-FileHash -LiteralPath $Path -Algorithm SHA256).Hash.ToLowerInvariant() }

function Read-Run([string]$Directory) {
    $directoryPath = Get-AbsoluteDirectory $Directory
    $record = Get-Content -LiteralPath (Join-Path $directoryPath 'build-record.json') -Raw | ConvertFrom-Json
    if ($record.format -ne 'muxray-native-acceptance-v1' -or $record.runDirectory -ne $directoryPath) {
        throw 'The build record does not identify this acceptance directory.'
    }
    if ($record.runId -notmatch '^[0-9a-f]{32}$') { throw 'Invalid run identity.' }
    foreach ($entry in @($record.artifacts)) {
        if ($entry.name -ne [IO.Path]::GetFileName($entry.name)) { throw 'Invalid artifact name in build record.' }
        if ((Get-SHA256 (Join-Path $directoryPath $entry.name)) -ne $entry.sha256) {
            throw "Artifact identity changed: $($entry.name)"
        }
    }
    return $record
}

function Read-Receipt($Run, [string]$Name) {
    $receipt = Get-Content -LiteralPath (Join-Path $Run.runDirectory $Name) -Raw | ConvertFrom-Json
    if ($receipt.runId -ne $Run.runId -or $receipt.pid -le 0 -or -not $receipt.processToken) {
        throw "Receipt identity mismatch: $Name"
    }
    return $receipt
}

function Get-NativeProcess($Run, $Entered) {
    $expected = Join-Path $Run.runDirectory 'MuxRay-io-acceptance.exe'
    if ($Entered.executable -ne $expected) { throw 'The entered receipt belongs to a non-native verification process.' }
    $process = Get-Process -Id $Entered.pid -ErrorAction Stop
    try {
        if ($process.Path -ne $expected -or $process.StartTime.ToUniversalTime() -gt [DateTime]::Parse($Entered.timeUTC).ToUniversalTime()) {
            throw 'The PID was reused or belongs to a different process.'
        }
        # Open and retain the handle before observing exit; subsequent PID reuse
        # cannot make this Process object wait for or act on a replacement.
        $null = $process.Handle
        return $process
    } catch { $process.Dispose(); throw }
}

function Assert-SameProcess($First, $Second) {
    if ($First.pid -ne $Second.pid -or $First.processToken -ne $Second.processToken) {
        throw 'Receipts belong to different process instances.'
    }
}

function Assert-ExclusiveFixture($Run) {
    $file = [IO.File]::Open((Join-Path $Run.runDirectory 'native-controlled.flv'), [IO.FileMode]::Open, [IO.FileAccess]::Read, [IO.FileShare]::None)
    try { if ($file.Length -ne 493) { throw 'Fixture size changed.' } } finally { $file.Dispose() }
}

function New-Run([string]$Repository, [string]$Directory, [string]$Kind) {
    if ($env:OS -ne 'Windows_NT') { throw 'These acceptance tools require Windows.' }
    $repo = Get-AbsoluteDirectory $Repository
    $run = Get-AbsoluteDirectory $Directory
    if (-not (Test-Path -LiteralPath (Join-Path $repo 'desktop\go.mod'))) { throw 'Repository is not a MuxRay checkout.' }
    if ($run.Equals($repo, [StringComparison]::OrdinalIgnoreCase) -or $run.StartsWith($repo + [IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase)) {
        throw 'Run directory must be outside the repository so generated overlay Go files cannot enter normal builds.'
    }
    if (Test-Path -LiteralPath $run) { throw 'Run directory already exists; choose a new directory to preserve evidence.' }
    $null = New-Item -ItemType Directory -Path $run
    $record = [ordered]@{
        format = 'muxray-native-acceptance-v1'; kind = $Kind; runId = [Guid]::NewGuid().ToString('N')
        createdUTC = [DateTime]::UtcNow.ToString('o'); repository = $repo; runDirectory = $run
        gateReadCall = 23; committedTagsBeforeGate = 10; fixtureBytes = 493; fixtureTags = 32
    }
    New-FramingFixture (Join-Path $run 'native-controlled.flv')
    $sourcePath = Join-Path $repo 'internal\session\session.go'
    $original = [IO.File]::ReadAllText($sourcePath)
    $anchor = 'return start(ctx, source, uint64(info.Size()), options), nil'
    if ([regex]::Matches($original, [regex]::Escape($anchor)).Count -ne 1) { throw 'Session Open seam changed; review the overlay before rebuilding.' }
    $modified = $original.Replace($anchor, 'return start(ctx, nativeIOAcceptanceSource(path, source), uint64(info.Size()), options), nil')
    $importAnchor = 'import ('
    if ([regex]::Matches($original, [regex]::Escape($importAnchor)).Count -ne 1) { throw 'Session imports changed; review the overlay.' }
    foreach ($import in @('encoding/json', 'path/filepath', 'strings', 'time')) {
        if ($original.Contains('"' + $import + '"')) { throw "Overlay import already present: $import. Review the seam." }
    }
    $modified = $modified.Replace($importAnchor, $importAnchor + "`n`t" + '"encoding/json"' + "`n`t" + '"path/filepath"' + "`n`t" + '"strings"' + "`n`t" + '"time"')
    $gate = [IO.File]::ReadAllText((Join-Path $PSScriptRoot 'gate.go.txt'))
    # JSON string quoting is also valid for these Windows paths in Go source.
    $gate = $gate.Replace('__RUN_DIRECTORY__', ($run | ConvertTo-Json -Compress)).Replace('__RUN_ID__', ($record.runId | ConvertTo-Json -Compress))
    $overlaySource = Join-Path $run 'session_overlay.go'
    Write-NewText $overlaySource ($modified + "`n" + $gate)
    & gofmt -w $overlaySource
    if ($LASTEXITCODE -ne 0) { throw 'gofmt overlay failed.' }
    $replace = @{}; $replace[$sourcePath] = $overlaySource
    $record.source = [ordered]@{ path = $sourcePath; sha256 = Get-SHA256 $sourcePath }
    $record.templates = @(Get-ChildItem -LiteralPath $PSScriptRoot -File | Where-Object { $_.Extension -in '.ps1', '.txt' } | Sort-Object Name | ForEach-Object {
        [ordered]@{ name = $_.Name; sha256 = Get-SHA256 $_.FullName }
    })
    $record.goInputs = @(Get-GoInputs $repo)
    if ($Kind -eq 'verify') {
        $testSource = Join-Path $repo 'internal\session\session_test.go'
        $testOverlay = Join-Path $run 'session_overlay_test.go'
        Write-NewText $testOverlay ([IO.File]::ReadAllText($testSource) + "`n" + [IO.File]::ReadAllText((Join-Path $PSScriptRoot 'verify_test.go.txt')))
        & gofmt -w $testOverlay
        if ($LASTEXITCODE -ne 0) { throw 'gofmt test overlay failed.' }
        $replace[$testSource] = $testOverlay
        $record.testSource = [ordered]@{ path = $testSource; sha256 = Get-SHA256 $testSource }
    }
    Write-NewJSON (Join-Path $run 'overlay.json') @{ Replace = $replace }
    return $record
}

function Complete-Run($Record) {
    if ((Get-SHA256 $Record.source.path) -ne $Record.source.sha256) { throw 'Repository session source changed during the run.' }
    if ($Record.Contains('testSource') -and (Get-SHA256 $Record.testSource.path) -ne $Record.testSource.sha256) { throw 'Repository test source changed during the run.' }
    if (($Record.goInputs | ConvertTo-Json -Depth 4 -Compress) -ne (@(Get-GoInputs $Record.repository) | ConvertTo-Json -Depth 4 -Compress)) { throw 'Go inputs changed during the run.' }
    if ($Record.Contains('frontendInputs') -and ($Record.frontendInputs | ConvertTo-Json -Depth 4 -Compress) -ne (@(Get-FrontendInputs $Record.repository) | ConvertTo-Json -Depth 4 -Compress)) { throw 'Frontend inputs changed during the run.' }
    $Record.artifacts = @(Get-ChildItem -LiteralPath $Record.runDirectory -File | Where-Object { $_.Name -notmatch '^(entered|release|released|closed)\.json$' } | Sort-Object Name | ForEach-Object {
        [ordered]@{ name = $_.Name; sha256 = Get-SHA256 $_.FullName }
    })
    Write-NewJSON (Join-Path $Record.runDirectory 'build-record.json') $Record
}

function Invoke-Offline([scriptblock]$Body) {
    $keys = @('GOPROXY', 'GOSUMDB', 'GOTOOLCHAIN', 'GOOS', 'GOARCH', 'CGO_ENABLED', 'npm_config_offline')
    $saved = @{}; foreach ($key in $keys) { $saved[$key] = [Environment]::GetEnvironmentVariable($key, 'Process') }
    try {
        $env:GOPROXY = 'off'; $env:GOSUMDB = 'off'; $env:GOTOOLCHAIN = 'local'
        $env:GOOS = 'windows'; $env:GOARCH = 'amd64'; $env:CGO_ENABLED = '1'; $env:npm_config_offline = 'true'
        & $Body
    } finally { foreach ($key in $keys) { [Environment]::SetEnvironmentVariable($key, $saved[$key], 'Process') } }
}

function Get-FrontendInputs([string]$Repository) {
    $front = Join-Path $Repository 'desktop\frontend'
    $files = @('package.json', 'package-lock.json', 'index.html', 'tsconfig.json', 'vite.config.ts') | ForEach-Object { Get-Item -LiteralPath (Join-Path $front $_) }
    foreach ($name in @('src', 'wailsjs')) { $files += Get-ChildItem -LiteralPath (Join-Path $front $name) -File -Recurse }
    return @($files | Sort-Object FullName | ForEach-Object {
        [ordered]@{ name = $_.FullName.Substring($front.Length + 1).Replace('\', '/'); sha256 = Get-SHA256 $_.FullName }
    })
}

function Get-GoInputs([string]$Repository) {
    $files = @('go.mod', 'go.sum', 'desktop\go.mod', 'desktop\go.sum') | ForEach-Object {
        $path = Join-Path $Repository $_
        if (Test-Path -LiteralPath $path) { Get-Item -LiteralPath $path }
    }
    $files += Get-ChildItem -LiteralPath (Join-Path $Repository 'internal') -Filter '*.go' -File -Recurse
    $files += Get-ChildItem -LiteralPath (Join-Path $Repository 'desktop') -File | Where-Object { $_.Extension -in '.go', '.syso' }
    return @($files | Sort-Object FullName | ForEach-Object {
        [ordered]@{ name = $_.FullName.Substring($Repository.Length + 1).Replace('\', '/'); sha256 = Get-SHA256 $_.FullName }
    })
}

function New-FramingFixture([string]$Path) {
    # 32 empty script tags: a structural FLV, not playable AMF/audio/video.
    $bytes = [byte[]]::new(13 + 15 * 32)
    [byte[]]$header = 70, 76, 86, 1, 0, 0, 0, 0, 9
    [Array]::Copy($header, $bytes, $header.Length)
    for ($index = 0; $index -lt 32; $index++) {
        $offset = 13 + 15 * $index
        $bytes[$offset] = 18
        $bytes[$offset + 14] = 11
    }
    $stream = [IO.File]::Open($Path, [IO.FileMode]::CreateNew, [IO.FileAccess]::Write, [IO.FileShare]::Read)
    try { $stream.Write($bytes, 0, $bytes.Length) } finally { $stream.Dispose() }
}
