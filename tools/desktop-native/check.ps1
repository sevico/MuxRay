param(
    [Parameter(Mandatory)][string]$RunDirectory,
    [Parameter(Mandatory)][ValidateSet('Cancel', 'Exit', 'DetailClose')][string]$Scenario,
    [ValidateSet('canceled', 'closing')][string]$ObservedState,
    [int]$ObservedTagCount = -1,
    [ValidateRange(1000, 15000)][int]$MaxExitMilliseconds = 6000
)
. (Join-Path $PSScriptRoot 'common.ps1')
$run = Read-Run $RunDirectory
if ($run.kind -ne 'native') { throw 'Native acceptance requires a native build run.' }
Assert-GateScenario $run $Scenario
$entered = Read-Receipt $run 'entered.json'
Assert-GateReceipt $run $entered
if ($entered.executable -ne (Join-Path $run.runDirectory 'MuxRay-io-acceptance.exe')) {
    throw 'The gate receipt belongs to a non-native verification process.'
}
$result = [ordered]@{ runId = $run.runId; gateMode = $run.gateMode; scenario = $Scenario; completedUTC = [DateTime]::UtcNow.ToString('o') }
if ($Scenario -ne 'Exit') {
    $expectedState = if ($Scenario -eq 'DetailClose') { 'closing' } else { 'canceled' }
    if ($ObservedState -ne $expectedState -or $ObservedTagCount -ne $run.committedTags) { throw "Supply the actual native UI observation: -ObservedState $expectedState -ObservedTagCount $($run.committedTags)." }
    $commandName = if ($Scenario -eq 'DetailClose') { 'close-command.json' } else { 'cancel-command.json' }
    $command = Read-Receipt $run $commandName
    $release = Read-Receipt $run 'release.json'
    $released = Read-Receipt $run 'released.json'
    $closed = Read-Receipt $run 'closed.json'
    foreach ($item in @($command, $release, $released, $closed)) { Assert-SameProcess $entered $item }
    $commandTime = Convert-ReceiptTime $command.timeUTC
    $releaseTime = Convert-ReceiptTime $release.timeUTC
    $releasedTime = Convert-ReceiptTime $released.timeUTC
    $closedTime = Convert-ReceiptTime $closed.timeUTC
    $enteredTime = Convert-ReceiptTime $entered.timeUTC
    if ($enteredTime -gt $commandTime -or $commandTime -gt $releaseTime -or $releaseTime -gt $releasedTime -or $releasedTime -gt $closedTime) { throw 'Gate/command/release/close receipt order is invalid.' }
    if ($Scenario -eq 'DetailClose' -and ($commandTime -ge $releaseTime -or $releaseTime -ge $releasedTime -or $releasedTime -ge $closedTime)) { throw 'DetailClose requires strictly ordered close/release/released/closed receipts.' }
    if ($closed.PSObject.Properties['closeError'] -and $closed.closeError) { throw "Source Close failed: $($closed.closeError)" }
    $result.uiObservation = [ordered]@{ source = 'Operator supplied; verify against the native screenshot'; state = $ObservedState; tags = $ObservedTagCount }
    $result.orderlySourceClose = $true
} else {
    foreach ($name in @('release.json', 'released.json', 'closed.json')) {
        if (Test-Path -LiteralPath (Join-Path $run.runDirectory $name)) { throw "Blocked-exit proof requires no $name." }
    }
    $command = Read-Receipt $run 'exit-command.json'
    $observed = Read-Receipt $run 'exit-observed.json'
    Assert-SameProcess $entered $command
    Assert-SameProcess $entered $observed
    if (-not $observed.exited -or $observed.exitCode -ne 0) { throw 'The native process did not exit successfully.' }
    $commandTime = Convert-ReceiptTime $command.timeUTC
    if ((Convert-ReceiptTime $observed.watchStartedUTC) -gt $commandTime) { throw 'Start the exit watcher before recording the exit action.' }
    $elapsed = ((Convert-ReceiptTime $observed.exitUTC) - $commandTime).TotalMilliseconds
    if ($elapsed -lt 0 -or $elapsed -gt $MaxExitMilliseconds) { throw "Exit interval $elapsed ms exceeds the selected bound $MaxExitMilliseconds ms." }
    $result.commandToExitMilliseconds = [Math]::Round($elapsed, 3)
    $result.limitMilliseconds = $MaxExitMilliseconds
    $result.orderlySourceClose = $false
    $result.exitCode = $observed.exitCode
    $result.timingScope = 'From operator marker before GUI close to OS process exit; includes input dispatch delay.'
}
Assert-ExclusiveFixture $run
$result.exclusiveSourceOpen = $true
$result.passed = $true
Write-NewJSON (Join-Path $run.runDirectory 'acceptance.json') $result
$result | ConvertTo-Json -Depth 6
