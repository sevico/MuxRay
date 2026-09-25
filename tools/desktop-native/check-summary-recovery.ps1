param(
    [Parameter(Mandatory)][string]$RunDirectory,
    [Parameter(Mandatory)][string]$ObservationFile
)
. (Join-Path $PSScriptRoot 'common.ps1')
. (Join-Path $PSScriptRoot 'summary-recovery-common.ps1')
$run = Read-Run $RunDirectory
if ($run.kind -ne 'native') { throw 'Summary recovery requires a native build run.' }
Assert-GateScenario $run 'SummaryClose'
$entered = Read-Receipt $run 'entered.json'
Assert-GateReceipt $run $entered
if ($entered.executable -ne (Join-Path $run.runDirectory 'MuxRay-io-acceptance.exe')) { throw 'The gate belongs to a non-native process.' }
if (-not [IO.Path]::IsPathRooted($ObservationFile)) { throw 'Use an absolute observation JSON path.' }
$observation = Get-Content -LiteralPath $ObservationFile -Raw | ConvertFrom-Json
$command = Read-Receipt $run 'cancel-command.json'
$release = Read-Receipt $run 'release.json'
$released = Read-Receipt $run 'released.json'
$closed = Read-Receipt $run 'closed.json'
Assert-SummaryRecoveryOrder $run $entered $command $release $released $closed $observation
$evidence = @()
foreach ($item in $observation.observations) {
    foreach ($path in @($item.evidenceFiles)) {
        if (-not [IO.Path]::IsPathRooted($path) -or -not (Test-Path -LiteralPath $path -PathType Leaf)) { throw "Missing absolute GUI evidence file: $path" }
        $evidence += [ordered]@{ state = $item.state; path = $path; sha256 = Get-SHA256 $path }
    }
}
Assert-ExclusiveFixture $run
$result = [ordered]@{
    runId = $run.runId; gateMode = 'Summary'; scenario = 'SummaryRecovery'; completedUTC = [DateTime]::UtcNow.ToString('o')
    passed = $true; orderlySourceClose = $true; exclusiveSourceOpen = $true
    uiObservation = [ordered]@{ source = 'Operator supplied observations; verify native screenshots/accessibility records. This script does not infer UI states from receipts.'; path = $ObservationFile; sha256 = Get-SHA256 $ObservationFile; observations = $observation.observations }
    evidence = $evidence
}
Write-NewJSON (Join-Path $run.runDirectory 'acceptance.json') $result
$result | ConvertTo-Json -Depth 10
