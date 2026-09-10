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
Write-Host "PASS: receipt UTC identity, JSON string/DateTime conversion, 100 ns ordering and 3000.125 ms interval (PowerShell $($PSVersionTable.PSVersion))."
