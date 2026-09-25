# Run in PowerShell 5.1 and 7. Tests metadata/order only; no GUI proof is claimed.
. (Join-Path $PSScriptRoot 'common.ps1')
. (Join-Path $PSScriptRoot 'summary-recovery-common.ps1')
$run = [pscustomobject]@{ runId = 'test-run'; gateMode = 'Summary'; committedTags = 32 }
function New-TestReceipt([int]$Second) {
    return [pscustomobject]@{ pid = 123; processToken = '123-test'; timeUTC = ('2026-09-25T01:00:{0:00}Z' -f $Second) }
}
function New-TestObservation {
    $items = @(2, 5, 6, 8 | ForEach-Object { $r = New-TestReceipt $_; $r | Add-Member -NotePropertyName evidenceFiles -NotePropertyValue @('C:\evidence\native.png'); $r })
    for ($i = 0; $i -lt 4; $i++) { $items[$i] | Add-Member -NotePropertyName state -NotePropertyValue @('canceling', 'canceled', 'ready', 'empty')[$i] }
    foreach ($name in @('processed', 'total', 'indexedTags')) { $items[2] | Add-Member -NotePropertyName $name -NotePropertyValue 32 }
    return [pscustomobject]@{ format = 'muxray-summary-recovery-observation-v1'; runId = 'test-run'; pid = 123; processToken = '123-test'; observations = $items }
}
$entered = New-TestReceipt 0
$command = New-TestReceipt 1
$command | Add-Member -NotePropertyName action -NotePropertyValue 'MarkSummaryCancel'
$release = New-TestReceipt 3
$released = New-TestReceipt 4
$closed = New-TestReceipt 7
$good = New-TestObservation
Assert-SummaryRecoveryOrder $run $entered $command $release $released $closed $good
$cases = @('wrong-run', 'wrong-process', 'canceled-before-release', 'ready-after-close', 'wrong-total', 'missing-evidence', 'missing-state', 'wrong-state')
foreach ($case in $cases) {
    $bad = New-TestObservation
    switch ($case) {
        'wrong-run' { $bad.runId = 'another' }
        'wrong-process' { $bad.pid = 456 }
        'canceled-before-release' { $bad.observations[1].timeUTC = $command.timeUTC }
        'ready-after-close' { $bad.observations[2].timeUTC = $bad.observations[3].timeUTC }
        'wrong-total' { $bad.observations[2].total = 31 }
        'missing-evidence' { $bad.observations[0].evidenceFiles = @() }
        'missing-state' { $bad.observations = $bad.observations[0..2] }
        'wrong-state' { $bad.observations[1].state = 'ready' }
    }
    $rejected = $false
    try { Assert-SummaryRecoveryOrder $run $entered $command $release $released $closed $bad } catch { $rejected = $true }
    if (-not $rejected) { throw "Accepted invalid observation: $case" }
}
$run.gateMode = 'Scan'
$rejected = $false
try { Assert-SummaryRecoveryOrder $run $entered $command $release $released $closed $good } catch { $rejected = $true }
if (-not $rejected) { throw 'Accepted Scan gate as Summary recovery.' }
Write-Host "PASS: SummaryRecovery valid sequence and nine invalid identity/order/state cases (PowerShell $($PSVersionTable.PSVersion))."
