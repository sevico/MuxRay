param(
    [Parameter(Mandatory)][string]$RunDirectory,
    [Parameter(Mandatory)][ValidateSet('Cancel', 'Exit')][string]$Scenario,
    [ValidateSet('canceled')][string]$ObservedState,
    [int]$ObservedTagCount = -1,
    [ValidateRange(1000, 15000)][int]$MaxExitMilliseconds = 6000
)
. (Join-Path $PSScriptRoot 'common.ps1')
$run = Read-Run $RunDirectory
if ($run.kind -ne 'native') { throw 'Native acceptance requires a native build run.' }
$entered = Read-Receipt $run 'entered.json'
if ($entered.executable -ne (Join-Path $run.runDirectory 'MuxRay-io-acceptance.exe') -or $entered.readCall -ne 23 -or $entered.offset -ne 163 -or $entered.committedTags -ne 10) {
    throw 'The native source did not enter the expected ten-Tag gate.'
}
$result = [ordered]@{ runId = $run.runId; scenario = $Scenario; completedUTC = [DateTime]::UtcNow.ToString('o') }
if ($Scenario -eq 'Cancel') {
    if ($ObservedState -ne 'canceled' -or $ObservedTagCount -ne 10) { throw 'Supply the actual native UI observation: -ObservedState canceled -ObservedTagCount 10.' }
    $command = Read-Receipt $run 'cancel-command.json'
    $release = Read-Receipt $run 'release.json'
    $released = Read-Receipt $run 'released.json'
    $closed = Read-Receipt $run 'closed.json'
    foreach ($item in @($command, $release, $released, $closed)) { Assert-SameProcess $entered $item }
    if ([DateTime]::Parse($command.timeUTC) -gt [DateTime]::Parse($release.timeUTC) -or [DateTime]::Parse($release.timeUTC) -gt [DateTime]::Parse($released.timeUTC) -or [DateTime]::Parse($released.timeUTC) -gt [DateTime]::Parse($closed.timeUTC)) { throw 'Cancellation receipt order is invalid.' }
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
    if ([DateTime]::Parse($observed.watchStartedUTC) -gt [DateTime]::Parse($command.timeUTC)) { throw 'Start the exit watcher before recording the exit action.' }
    $elapsed = ([DateTimeOffset]::Parse($observed.exitUTC) - [DateTimeOffset]::Parse($command.timeUTC)).TotalMilliseconds
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
