param(
    [Parameter(Mandatory)][string]$RunDirectory,
    [ValidateRange(1, 60)][int]$TimeoutSeconds = 60
)
. (Join-Path $PSScriptRoot 'common.ps1')
$run = Read-Run $RunDirectory
if ($run.kind -ne 'native') { throw 'Exit observation requires a native build run.' }
Assert-GateScenario $run 'Exit'
$entered = Read-Receipt $run 'entered.json'
Assert-GateReceipt $run $entered
$process = Get-NativeProcess $run $entered
$started = [DateTime]::UtcNow.ToString('o')
try {
    Write-NewJSON (Join-Path $run.runDirectory 'exit-watch-armed.json') ([ordered]@{
        runId = $run.runId; pid = $entered.pid; processToken = $entered.processToken
        timeUTC = $started; timeoutSeconds = $TimeoutSeconds; gateMode = $run.gateMode
    })
    Write-Host "Watching process $($entered.pid) for up to $TimeoutSeconds seconds. Record MarkExit, then close the native window without releasing the gate."
    $exited = $process.WaitForExit($TimeoutSeconds * 1000)
    $receipt = [ordered]@{
        runId = $run.runId; pid = $entered.pid; processToken = $entered.processToken
        timeUTC = [DateTime]::UtcNow.ToString('o'); watchStartedUTC = $started; exited = $exited; gateMode = $run.gateMode
    }
    if ($exited) {
        $receipt.exitUTC = $process.ExitTime.ToUniversalTime().ToString('o')
        $receipt.exitCode = $process.ExitCode
    }
    Write-NewJSON (Join-Path $run.runDirectory 'exit-observed.json') $receipt
    $receipt | ConvertTo-Json -Depth 4
    if (-not $exited) { throw 'Process did not exit within the observation window. It was not terminated by this tool.' }
} finally { $process.Dispose() }
