param(
    [Parameter(Mandatory)][string]$Repository,
    [Parameter(Mandatory)][string]$RunDirectory,
    [ValidateSet('Scan', 'Detail')][string]$GateMode = 'Scan'
)
. (Join-Path $PSScriptRoot 'common.ps1')

$record = New-Run $Repository $RunDirectory 'verify' $GateMode
Invoke-Offline {
    Push-Location $record.repository
    try {
        # Windows PowerShell 5.1 wraps redirected native stderr in error records.
        # Preserve failure output, then decide success using the native exit code.
        $savedPreference = $ErrorActionPreference
        try {
            $ErrorActionPreference = 'Continue'
            $output = & go test -mod=readonly -race -count=1 -timeout=20s -run '^TestNativeAcceptanceOverlayGate$' -v -overlay (Join-Path $record.runDirectory 'overlay.json') ./internal/document 2>&1
            $resultCode = $LASTEXITCODE
        } finally { $ErrorActionPreference = $savedPreference }
        Write-NewText (Join-Path $record.runDirectory 'verify.log') (($output | ForEach-Object { "$_" }) -join "`n")
        $output | ForEach-Object { Write-Host "$_" }
        $record.exitCode = $resultCode
        $record.goVersion = (& go version) -join "`n"
        if ($resultCode -ne 0) { throw 'Controlled overlay verification failed; inspect the preserved verify.log.' }
    } finally { Pop-Location }
}
Complete-Run $record
$run = Read-Run $record.runDirectory
$entered = Read-Receipt $run 'entered.json'
foreach ($name in @('release.json', 'released.json', 'closed.json')) { Assert-SameProcess $entered (Read-Receipt $run $name) }
Assert-GateReceipt $run $entered
Assert-ExclusiveFixture $run
$scenario = if ($run.gateMode -eq 'Detail') { 'non-GUI controlled detail close' } else { 'non-GUI controlled document cancellation' }
Write-NewJSON (Join-Path $record.runDirectory 'verification.json') ([ordered]@{
    runId = $run.runId; gateMode = $run.gateMode; scenario = $scenario; passed = $true
    completedUTC = [DateTime]::UtcNow.ToString('o'); retainedTags = $run.committedTags; exclusiveSourceOpen = $true
})
Write-Host "PASS: $scenario, $($run.committedTags) committed Tags, Close and exclusive file open. $RunDirectory"
