# Run unchanged in Windows PowerShell 5.1 and PowerShell 7. No GUI or run files.
. (Join-Path $PSScriptRoot 'common.ps1')

function Assert-UTC($Value, [DateTimeOffset]$Expected, [string]$Context) {
    $actual = Convert-ReceiptTime $Value
    if ($actual.Offset -ne [TimeSpan]::Zero -or $actual.Ticks -ne $Expected.Ticks) {
        throw "$Context lost timezone or fractional seconds: $($actual.ToString('o'))"
    }
}

$instant = [DateTimeOffset]::new([DateTime]::new(2026, 9, 10, 0, 36, 9, [DateTimeKind]::Utc).AddTicks(9931234))
$json = '{"timeUTC":"2026-09-10T00:36:09.9931234Z","exitUTC":"2026-09-10T00:36:12.9932484Z"}'
$receipt = $json | ConvertFrom-Json
Assert-UTC $receipt.timeUTC $instant 'ConvertFrom-Json UTC value'
Assert-UTC '2026-09-10T00:36:09.9931234Z' $instant 'String UTC value'
Assert-UTC '2026-09-10T00:36:09.993123400Z' $instant 'Go RFC3339Nano string'
Assert-UTC $instant.UtcDateTime $instant 'DateTime UTC value'
Assert-UTC $instant.LocalDateTime $instant 'DateTime local value'
Assert-UTC $instant.ToOffset([TimeSpan]::FromHours(8)) $instant 'DateTimeOffset value'
$offsetReceipt = '{"timeUTC":"2026-09-10T08:36:09.9931234+08:00"}' | ConvertFrom-Json
Assert-UTC $offsetReceipt.timeUTC $instant 'ConvertFrom-Json offset value'

# Preserve sub-millisecond timing across both real JSON deserialization modes.
$elapsed = ((Convert-ReceiptTime $receipt.exitUTC) - (Convert-ReceiptTime $receipt.timeUTC)).Ticks
if ($elapsed -ne 30001250) { throw "Receipt interval lost precision: $elapsed ticks." }
$earlier = $instant.AddTicks(-1)
if ((Convert-ReceiptTime $earlier.UtcDateTime) -ge (Convert-ReceiptTime $receipt.timeUTC)) { throw 'Subsecond receipt ordering was lost.' }
$started = [DateTime]::new(2026, 9, 10, 0, 35, 3, [DateTimeKind]::Utc).ToLocalTime()
if ((Convert-ReceiptTime $started) -gt (Convert-ReceiptTime $receipt.timeUTC)) { throw 'UTC receipt incorrectly predates process start.' }

foreach ($invalid in @('2026-09-10T00:36:09.9931234', [DateTime]::SpecifyKind($instant.UtcDateTime, [DateTimeKind]::Unspecified))) {
    $rejected = $false
    try { $null = Convert-ReceiptTime $invalid } catch { $rejected = $true }
    if (-not $rejected) { throw 'Ambiguous timezone was accepted.' }
}

$fixtureHashes = @{
    Scan = '9758dc3635a5e05a780ffe4cd40659b243d2d23a4a9e704762add90fcd4c34fb'
    Detail = '16346b21c3e84be9a1d398f330a0023b5ba3b5df5a5c74d8756b5cda8f1e4437'
    AVCDetail = '8b9d282048f76e6cc7fa6821a83ce8c02726a2dd91592b8da493b421ba4cdda9'
    MP3Detail = 'd6414212c5fb29d116fdf198ae7a1ab66ab688956d6fbfbfee7a16e2cfb10543'
    Hex = '9758dc3635a5e05a780ffe4cd40659b243d2d23a4a9e704762add90fcd4c34fb'
    Summary = '16346b21c3e84be9a1d398f330a0023b5ba3b5df5a5c74d8756b5cda8f1e4437'
}
foreach ($mode in @('Scan', 'Detail', 'AVCDetail', 'MP3Detail', 'Hex', 'Summary')) {
    $gate = [pscustomobject](Get-GateSettings $mode)
    [byte[]]$fixture = Get-FramingFixtureBytes $mode
    $sha = [Security.Cryptography.SHA256]::Create()
    try { $hash = [BitConverter]::ToString($sha.ComputeHash($fixture)).Replace('-', '').ToLowerInvariant() } finally { $sha.Dispose() }
    if ($fixture.Length -ne $gate.fixtureBytes -or $hash -ne $fixtureHashes[$mode]) { throw "Stable $mode fixture bytes changed." }
    $entered = [pscustomobject]@{ gateMode = $mode; readCall = $gate.gateReadCall; offset = $gate.gateOffset; committedTags = $gate.committedTags }
    Assert-GateReceipt $gate $entered
    foreach ($scenario in @('Cancel', 'Exit', 'DetailClose', 'HexClose', 'SummaryClose')) {
        $shouldPass = (($mode -eq 'Scan') -and ($scenario -in @('Cancel', 'Exit'))) -or
            (($mode -eq 'Hex') -and ($scenario -eq 'HexClose')) -or
            (($mode -eq 'Summary') -and ($scenario -eq 'SummaryClose')) -or
            (($mode -in @('Detail', 'AVCDetail', 'MP3Detail')) -and ($scenario -eq 'DetailClose'))
        $accepted = $true
        try { Assert-GateScenario $gate $scenario } catch { $accepted = $false }
        if ($accepted -ne $shouldPass) { throw "Wrong mode accepted/rejected for $mode / $scenario." }
    }
    $entered.offset++
    $rejected = $false
    try { Assert-GateReceipt $gate $entered } catch { $rejected = $true }
    if (-not $rejected) { throw 'Mismatched gate offset was accepted.' }
    $entered.offset--
    $entered.gateMode = 'Other'
    $rejected = $false
    try { Assert-GateReceipt $gate $entered } catch { $rejected = $true }
    if (-not $rejected) { throw 'Mismatched receipt mode was accepted.' }
    $entered.PSObject.Properties.Remove('gateMode')
    $accepted = $true
    try { Assert-GateReceipt $gate $entered } catch { $accepted = $false }
    if ($accepted -ne ($mode -eq 'Scan')) { throw 'Only legacy Scan receipts may omit their mode.' }
}
$scanGate = Get-GateSettings 'Scan'
$detailGate = Get-GateSettings 'Detail'
if ($scanGate.fixtureBytes -ne 493 -or $scanGate.gateReadCall -ne 23 -or $scanGate.gateOffset -ne 163 -or $scanGate.committedTags -ne 10 -or $detailGate.fixtureBytes -ne 497 -or $detailGate.gateReadCall -ne 67 -or $detailGate.gateOffset -ne 24 -or $detailGate.committedTags -ne 32) {
    throw 'Stable fixture/gate contract changed.'
}
Write-Host "PASS: receipt UTC identity, JSON string/DateTime conversion, 100 ns ordering and 3000.125 ms interval (PowerShell $($PSVersionTable.PSVersion))."
$avcGate = Get-GateSettings 'AVCDetail'
if ($avcGate.fixtureBytes -ne 504 -or $avcGate.gateReadCall -ne 67 -or $avcGate.gateOffset -ne 24 -or $avcGate.committedTags -ne 32) { throw 'AVC detail gate contract changed.' }
$mp3Gate = Get-GateSettings 'MP3Detail'
if ($mp3Gate.fixtureBytes -ne 498 -or $mp3Gate.gateReadCall -ne 67 -or $mp3Gate.gateOffset -ne 24 -or $mp3Gate.committedTags -ne 32) { throw 'MP3 detail gate contract changed.' }
$hexGate = Get-GateSettings 'Hex'
if ($hexGate.fixtureBytes -ne 493 -or $hexGate.gateReadCall -ne 67 -or $hexGate.gateOffset -ne 13 -or $hexGate.committedTags -ne 32) { throw 'Hex gate contract changed.' }
$summaryGate = Get-GateSettings 'Summary'
if ($summaryGate.fixtureBytes -ne 497 -or $summaryGate.gateReadCall -ne 67 -or $summaryGate.gateOffset -ne 24 -or $summaryGate.committedTags -ne 32) { throw 'Summary gate contract changed.' }
Write-Host 'PASS: Scan/Detail/AVCDetail/MP3Detail/Hex/Summary fixture SHA-256, gate identity, legacy receipts and scenario mismatch rejection.'
