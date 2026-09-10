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

function Get-GateSettings([string]$Mode) {
    switch ($Mode) {
        'Scan' { return [ordered]@{ gateMode = 'Scan'; gateReadCall = 23; gateOffset = 163; committedTags = 10; fixtureBytes = 493; fixtureTags = 32 } }
        'Detail' { return [ordered]@{ gateMode = 'Detail'; gateReadCall = 67; gateOffset = 24; committedTags = 32; fixtureBytes = 497; fixtureTags = 32 } }
        'AVCDetail' { return [ordered]@{ gateMode = 'AVCDetail'; gateReadCall = 67; gateOffset = 24; committedTags = 32; fixtureBytes = 504; fixtureTags = 32 } }
        default { throw 'Unsupported gate mode; use Scan, Detail or AVCDetail.' }
    }
}

function Assert-GateReceipt($Run, $Entered) {
    if ($Entered.readCall -ne $Run.gateReadCall -or $Entered.offset -ne $Run.gateOffset -or $Entered.committedTags -ne $Run.committedTags) {
        throw 'The source did not enter the gate described by its build record.'
    }
    if ($Entered.PSObject.Properties['gateMode']) {
        if ($Entered.gateMode -ne $Run.gateMode) { throw 'Gate receipt mode differs from its build record.' }
    } elseif ($Run.gateMode -ne 'Scan') { throw 'Detail gate receipts must identify their mode.' }
}

function Assert-GateScenario($Run, [string]$Scenario) {
    $modes = if ($Scenario -eq 'DetailClose') { @('Detail', 'AVCDetail') } elseif ($Scenario -in @('Cancel', 'Exit')) { @('Scan') } else { throw 'Unknown acceptance scenario.' }
    if ($Run.gateMode -notin $modes) { throw "$Scenario requires a compatible gate run: $($modes -join ', ')." }
}

function Read-Run([string]$Directory) {
    $directoryPath = Get-AbsoluteDirectory $Directory
    $record = Get-Content -LiteralPath (Join-Path $directoryPath 'build-record.json') -Raw | ConvertFrom-Json
    if ($record.format -ne 'muxray-native-acceptance-v1' -or $record.runDirectory -ne $directoryPath) {
        throw 'The build record does not identify this acceptance directory.'
    }
    if ($record.runId -notmatch '^[0-9a-f]{32}$') { throw 'Invalid run identity.' }
    # Original v1 Scan records predate the explicit mode and offset fields.
    if (-not $record.PSObject.Properties['gateMode']) {
        $record | Add-Member -NotePropertyName gateMode -NotePropertyValue 'Scan'
        $record | Add-Member -NotePropertyName gateOffset -NotePropertyValue 163
        $record | Add-Member -NotePropertyName committedTags -NotePropertyValue $record.committedTagsBeforeGate
    }
    $gate = Get-GateSettings $record.gateMode
    foreach ($key in $gate.Keys) {
        if ($record.$key -ne $gate[$key]) { throw "Invalid gate setting in build record: $key." }
    }
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
    if ($receipt.PSObject.Properties['gateMode']) {
        if ($receipt.gateMode -ne $Run.gateMode) { throw "Receipt gate mode mismatch: $Name" }
    } elseif ($Run.gateMode -ne 'Scan') { throw "Detail receipt is missing its gate mode: $Name" }
    return $receipt
}

function Convert-ReceiptTime($Value) {
    # Windows PowerShell 5.1 keeps JSON timestamps as strings; PowerShell 7
    # may deserialize them to DateTime. Do not format a DateTime back to text:
    # that loses its Kind and fractional seconds before Parse sees it.
    if ($Value -is [DateTimeOffset]) { return $Value.ToUniversalTime() }
    if ($Value -is [DateTime]) {
        if ($Value.Kind -eq [DateTimeKind]::Unspecified) { throw 'Receipt time must identify a timezone.' }
        return ([DateTimeOffset]$Value).ToUniversalTime()
    }
    if ($Value -isnot [string] -or $Value -notmatch '^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d{1,9})?(Z|[+-]\d{2}:\d{2})$') {
        throw 'Receipt time must be an RFC 3339 timestamp with a timezone.'
    }
    return [DateTimeOffset]::Parse($Value, [Globalization.CultureInfo]::InvariantCulture, [Globalization.DateTimeStyles]::None).ToUniversalTime()
}

function Get-NativeProcess($Run, $Entered) {
    $expected = Join-Path $Run.runDirectory 'MuxRay-io-acceptance.exe'
    if ($Entered.executable -ne $expected) { throw 'The entered receipt belongs to a non-native verification process.' }
    $process = Get-Process -Id $Entered.pid -ErrorAction Stop
    try {
        if ($process.Path -ne $expected -or (Convert-ReceiptTime $process.StartTime) -gt (Convert-ReceiptTime $Entered.timeUTC)) {
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
    try { if ($file.Length -ne $Run.fixtureBytes) { throw 'Fixture size changed.' } } finally { $file.Dispose() }
}

function New-Run([string]$Repository, [string]$Directory, [string]$Kind, [string]$GateMode = 'Scan') {
    if ($env:OS -ne 'Windows_NT') { throw 'These acceptance tools require Windows.' }
    $repo = Get-AbsoluteDirectory $Repository
    $run = Get-AbsoluteDirectory $Directory
    if (-not (Test-Path -LiteralPath (Join-Path $repo 'desktop\go.mod'))) { throw 'Repository is not a MuxRay checkout.' }
    if ($run.Equals($repo, [StringComparison]::OrdinalIgnoreCase) -or $run.StartsWith($repo + [IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase)) {
        throw 'Run directory must be outside the repository so generated overlay Go files cannot enter normal builds.'
    }
    if (Test-Path -LiteralPath $run) { throw 'Run directory already exists; choose a new directory to preserve evidence.' }
    $null = New-Item -ItemType Directory -Path $run
    $gateSettings = Get-GateSettings $GateMode
    $record = [ordered]@{
        format = 'muxray-native-acceptance-v1'; kind = $Kind; runId = [Guid]::NewGuid().ToString('N')
        createdUTC = [DateTime]::UtcNow.ToString('o'); repository = $repo; runDirectory = $run
    }
    foreach ($key in $gateSettings.Keys) { $record[$key] = $gateSettings[$key] }
    $record.committedTagsBeforeGate = $record.committedTags
    New-FramingFixture (Join-Path $run 'native-controlled.flv') $record.gateMode
    $sourcePath = Join-Path $repo 'internal\document\document.go'
    $original = [IO.File]::ReadAllText($sourcePath)
    $anchor = 'return start(ctx, source, info, options), nil'
    if ([regex]::Matches($original, [regex]::Escape($anchor)).Count -ne 1) { throw 'Document Open seam changed; review the overlay before rebuilding.' }
    $modified = $original.Replace($anchor, 'return start(ctx, nativeIOAcceptanceSource(path, source), info, options), nil')
    $importAnchor = 'import ('
    if ([regex]::Matches($original, [regex]::Escape($importAnchor)).Count -ne 1) { throw 'Document imports changed; review the overlay.' }
    foreach ($import in @('encoding/json', 'path/filepath', 'strings', 'time')) {
        if (-not $original.Contains('"' + $import + '"')) {
            $modified = $modified.Replace($importAnchor, $importAnchor + "`n`t" + '"' + $import + '"')
        }
    }
    $gate = [IO.File]::ReadAllText((Join-Path $PSScriptRoot 'gate.go.txt'))
    # JSON string quoting is also valid for these Windows paths in Go source.
    $gate = $gate.Replace('__RUN_DIRECTORY__', ($run | ConvertTo-Json -Compress)).Replace('__RUN_ID__', ($record.runId | ConvertTo-Json -Compress))
    $gate = $gate.Replace('__GATE_MODE__', ($record.gateMode | ConvertTo-Json -Compress)).Replace('__GATE_READ_CALL__', [string]$record.gateReadCall).Replace('__GATE_OFFSET__', [string]$record.gateOffset).Replace('__COMMITTED_TAGS__', [string]$record.committedTags)
    $overlaySource = Join-Path $run 'document_overlay.go'
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
        $testSource = Join-Path $repo 'internal\document\native_acceptance_test.go'
        if (Test-Path -LiteralPath $testSource) { throw 'The virtual native acceptance test path is already occupied.' }
        $testOverlay = Join-Path $run 'document_overlay_test.go'
        Write-NewText $testOverlay ([IO.File]::ReadAllText((Join-Path $PSScriptRoot 'verify_test.go.txt')))
        & gofmt -w $testOverlay
        if ($LASTEXITCODE -ne 0) { throw 'gofmt test overlay failed.' }
        $replace[$testSource] = $testOverlay
    }
    Write-NewJSON (Join-Path $run 'overlay.json') @{ Replace = $replace }
    return $record
}

function Complete-Run($Record) {
    if ((Get-SHA256 $Record.source.path) -ne $Record.source.sha256) { throw 'Repository document source changed during the run.' }
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

function New-FramingFixture([string]$Path, [string]$GateMode = 'Scan') {
    # Detail/AVCDetail change only Tag 0; the remaining 31 tags are empty
    # ScriptData. These fixtures exercise reads, not playable media.
    $gate = Get-GateSettings $GateMode
    $detail = $gate.gateMode -ne 'Scan'
    [byte[]]$payload = @()
    $tagType = 18
    if ($gate.gateMode -eq 'Detail') { $payload = [byte[]]@(0xaf, 0, 0x12, 0x10); $tagType = 8 }
    if ($gate.gateMode -eq 'AVCDetail') { $payload = [byte[]]@(0x17, 1, 0, 0, 0, 0, 0, 0, 2, 0x65, 0x88); $tagType = 9 }
    $bytes = [byte[]]::new($gate.fixtureBytes)
    [byte[]]$header = 70, 76, 86, 1, 0, 0, 0, 0, 9
    if ($gate.gateMode -eq 'Detail') { $header[4] = 4 }
    if ($gate.gateMode -eq 'AVCDetail') { $header[4] = 1 }
    [Array]::Copy($header, $bytes, $header.Length)
    for ($index = 0; $index -lt 32; $index++) {
        $offset = 13 + 15 * $index
        if ($detail -and $index -gt 0) { $offset += $payload.Length }
        if ($detail -and $index -eq 0) {
            $bytes[$offset] = $tagType
            $bytes[$offset + 3] = $payload.Length
            [Array]::Copy($payload, 0, $bytes, $offset + 11, $payload.Length)
            $bytes[$offset + 14 + $payload.Length] = 11 + $payload.Length
        } else {
            $bytes[$offset] = 18
            $bytes[$offset + 14] = 11
        }
    }
    $stream = [IO.File]::Open($Path, [IO.FileMode]::CreateNew, [IO.FileAccess]::Write, [IO.FileShare]::Read)
    try { $stream.Write($bytes, 0, $bytes.Length) } finally { $stream.Dispose() }
}
