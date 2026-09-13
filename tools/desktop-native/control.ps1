param(
    [Parameter(Mandatory)][string]$RunDirectory,
    [Parameter(Mandatory)][ValidateSet('Status', 'Release', 'MarkCancel', 'MarkExit', 'MarkClose')][string]$Action
)
. (Join-Path $PSScriptRoot 'common.ps1')
$run = Read-Run $RunDirectory
if ($run.kind -ne 'native') { throw 'Control commands require a native build run.' }
if ($Action -eq 'Status') {
    $status = [ordered]@{ runId = $run.runId; runDirectory = $run.runDirectory; gateMode = $run.gateMode }
    foreach ($name in @('entered', 'cancel-command', 'close-command', 'exit-watch-armed', 'exit-command', 'release', 'released', 'closed', 'exit-observed', 'acceptance')) {
        $path = Join-Path $run.runDirectory ($name + '.json')
        $status[$name] = if (Test-Path -LiteralPath $path) { Get-Content -LiteralPath $path -Raw | ConvertFrom-Json } else { $null }
    }
    $status | ConvertTo-Json -Depth 12
    exit
}
switch ($Action) {
    'MarkCancel' { Assert-GateScenario $run 'Cancel' }
    'MarkExit' { Assert-GateScenario $run 'Exit' }
    'MarkClose' { Assert-GateScenario $run $(if ($run.gateMode -eq 'Summary') { 'SummaryClose' } elseif ($run.gateMode -eq 'Hex') { 'HexClose' } else { 'DetailClose' }) }
}
$entered = Read-Receipt $run 'entered.json'
Assert-GateReceipt $run $entered
$process = Get-NativeProcess $run $entered
try {
    $name = switch ($Action) { 'Release' { 'release.json' }; 'MarkCancel' { 'cancel-command.json' }; 'MarkExit' { 'exit-command.json' }; 'MarkClose' { 'close-command.json' } }
    if (Test-Path -LiteralPath (Join-Path $run.runDirectory 'closed.json')) { throw 'The gated source already closed.' }
    if ($Action -ne 'Release' -and (Test-Path -LiteralPath (Join-Path $run.runDirectory 'release.json'))) { throw 'Record GUI action before releasing the gate.' }
    Write-NewJSON (Join-Path $run.runDirectory $name) ([ordered]@{
        runId = $run.runId; pid = $entered.pid; processToken = $entered.processToken
        executable = $entered.executable; timeUTC = [DateTime]::UtcNow.ToString('o'); gateMode = $run.gateMode
        action = $Action; evidence = 'Operator command marker; GUI action and resulting UI state require separate observation.'
    })
    Write-Host "$Action recorded for process $($entered.pid)."
} finally { $process.Dispose() }
