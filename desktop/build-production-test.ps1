# Validates the production entry point without building or starting an application.
# Runs in Windows PowerShell 5.1 and PowerShell 7; wails is replaced in this scope.
$ErrorActionPreference = 'Stop'
$buildScript = Join-Path $PSScriptRoot 'build-production.ps1'
$calls = [Collections.Generic.List[object]]::new()
$wailsExitCode = 0
$originalExitCode = $global:LASTEXITCODE
$originalLocation = (Get-Location).Path

function wails {
    $calls.Add(@($args))
    $global:LASTEXITCODE = $wailsExitCode
}

function Assert-BuildCall([string]$ExpectedVersion) {
    if ($calls.Count -ne 1) { throw "Expected one Wails invocation, got $($calls.Count)." }
    $expected = @('build', '-webview2', 'error', '-nopackage', '-trimpath', '-ldflags', "-X github.com/sevico/muxray/internal/buildinfo.Version=$ExpectedVersion")
    if (($calls[0] -join '|') -cne ($expected -join '|')) { throw 'Production arguments or version injection changed.' }
    if ((Get-Location).Path -ne $originalLocation) { throw 'Build did not restore the working directory.' }
    $calls.Clear()
}

try {
    & $buildScript
    Assert-BuildCall '0.1.0-dev'
    foreach ($version in @('dev', '0.0.0', '0.1.0-dev', '1.2.3-rc.1+sha.0123', ('1.0.0+' + ('a' * 58)))) {
        & $buildScript -Version $version
        Assert-BuildCall $version
    }
    foreach ($version in @($null, '', 'DEV', 'v1.2.3', '01.2.3', '1.02.3', '1.2.03', '1.2', '1.2.3-01', '1.2.3-rc..1', '1.2.3+x..y', '1.2.3 ', "1.2.3`n", '1.2.3 -X injected=value', '1.2.3"', ('1.0.0+' + ('a' * 59)))) {
        $rejected = $false
        try { & $buildScript -Version $version } catch { $rejected = $true }
        if (-not $rejected -or $calls.Count -ne 0) { throw "Invalid version reached Wails: $version" }
    }
    $wailsExitCode = 9
    $rejected = $false
    try { & $buildScript -Version '0.1.0-dev' } catch {
        if ($_.Exception.Message -notmatch 'exit code 9') { throw }
        $rejected = $true
    }
    if (-not $rejected) { throw 'Wails build failure was ignored.' }
    Assert-BuildCall '0.1.0-dev'
    Write-Output "PASS: development default, SemVer validation, bounded single-argument injection and failure cleanup (PowerShell $($PSVersionTable.PSVersion))."
}
finally {
    $global:LASTEXITCODE = $originalExitCode
}
