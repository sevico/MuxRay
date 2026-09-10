param(
    [Parameter(Mandatory)][string]$Repository,
    [Parameter(Mandatory)][string]$RunDirectory
)
. (Join-Path $PSScriptRoot 'common.ps1')

$record = New-Run $Repository $RunDirectory 'verify'
Invoke-Offline {
    Push-Location $record.repository
    try {
        # Windows PowerShell 5.1 wraps redirected native stderr in error records.
        # Preserve failure output, then decide success using the native exit code.
        $savedPreference = $ErrorActionPreference
        try {
            $ErrorActionPreference = 'Continue'
            $output = & go test -mod=readonly -race -count=1 -timeout=20s -run '^TestNativeAcceptanceOverlayGate$' -v -overlay (Join-Path $record.runDirectory 'overlay.json') ./internal/session 2>&1
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
if ($entered.readCall -ne 23 -or $entered.offset -ne 163 -or $entered.committedTags -ne 10) { throw 'Unexpected gate location.' }
Assert-ExclusiveFixture $run
Write-NewJSON (Join-Path $record.runDirectory 'verification.json') ([ordered]@{
    runId = $run.runId; scenario = 'non-GUI controlled session cancellation'; passed = $true
    completedUTC = [DateTime]::UtcNow.ToString('o'); retainedTags = 10; exclusiveSourceOpen = $true
})
Write-Host "PASS: controlled source gate, canceling -> canceled, ten retained Tags, Close and exclusive file open. $RunDirectory"
