param(
    [Parameter(Mandatory)][string]$RunDirectory,
    [Parameter(Mandatory)][ValidateSet('Status', 'Release', 'MarkCancel', 'MarkExit')][string]$Action
)
. (Join-Path $PSScriptRoot 'common.ps1')
$run = Read-Run $RunDirectory
if ($run.kind -ne 'native') { throw 'Control commands require a native build run.' }
if ($Action -eq 'Status') {
    $status = [ordered]@{ runId = $run.runId; runDirectory = $run.runDirectory }
    foreach ($name in @('entered', 'cancel-command', 'exit-watch-armed', 'exit-command', 'release', 'released', 'closed', 'exit-observed', 'acceptance')) {
        $path = Join-Path $run.runDirectory ($name + '.json')
        $status[$name] = if (Test-Path -LiteralPath $path) { Get-Content -LiteralPath $path -Raw | ConvertFrom-Json } else { $null }
    }
    $status | ConvertTo-Json -Depth 12
    exit
}
$entered = Read-Receipt $run 'entered.json'
$process = Get-NativeProcess $run $entered
try {
    $name = switch ($Action) { 'Release' { 'release.json' }; 'MarkCancel' { 'cancel-command.json' }; 'MarkExit' { 'exit-command.json' } }
    if (Test-Path -LiteralPath (Join-Path $run.runDirectory 'closed.json')) { throw 'The gated source already closed.' }
    if ($Action -ne 'Release' -and (Test-Path -LiteralPath (Join-Path $run.runDirectory 'release.json'))) { throw 'Record GUI action before releasing the gate.' }
    Write-NewJSON (Join-Path $run.runDirectory $name) ([ordered]@{
        runId = $run.runId; pid = $entered.pid; processToken = $entered.processToken
        executable = $entered.executable; timeUTC = [DateTime]::UtcNow.ToString('o')
        action = $Action; evidence = 'Operator command marker; GUI action and resulting UI state require separate observation.'
    })
    Write-Host "$Action recorded for process $($entered.pid)."
} finally { $process.Dispose() }
