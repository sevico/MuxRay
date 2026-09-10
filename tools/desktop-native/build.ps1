param(
    [Parameter(Mandatory)][string]$Repository,
    [Parameter(Mandatory)][string]$RunDirectory,
    [ValidateSet('Scan', 'Detail', 'AVCDetail')][string]$GateMode = 'Scan'
)
. (Join-Path $PSScriptRoot 'common.ps1')

$record = New-Run $Repository $RunDirectory 'native' $GateMode
$record.frontendInputs = @(Get-FrontendInputs $record.repository)
$record.buildTags = 'desktop,wv2runtime.error,production'
$record.purpose = 'Controlled I/O native acceptance only; never distribute as the production executable.'
Invoke-Offline {
    Push-Location (Join-Path $record.repository 'desktop\frontend')
    try {
        & npm.cmd ci --offline --ignore-scripts --no-audit --no-fund
        if ($LASTEXITCODE -ne 0) { throw 'Offline npm ci failed. Prepare the locked dependency cache before this run.' }
        & npm.cmd run build
        if ($LASTEXITCODE -ne 0) { throw 'Frontend build failed.' }
    } finally { Pop-Location }
    $currentInputs = @(Get-FrontendInputs $record.repository)
    if (($record.frontendInputs | ConvertTo-Json -Depth 4 -Compress) -ne ($currentInputs | ConvertTo-Json -Depth 4 -Compress)) {
        throw 'Frontend sources changed during the build; use a new run after edits finish.'
    }
    $record.frontendAssets = @(Get-ChildItem -LiteralPath (Join-Path $record.repository 'desktop\frontend\dist') -File -Recurse | Sort-Object FullName | ForEach-Object {
        [ordered]@{ name = $_.FullName.Substring((Join-Path $record.repository 'desktop\frontend\dist').Length + 1).Replace('\', '/'); sha256 = Get-SHA256 $_.FullName }
    })
    Push-Location (Join-Path $record.repository 'desktop')
    try {
        & go build -mod=readonly -trimpath -tags $record.buildTags -ldflags '-H windowsgui' -overlay (Join-Path $record.runDirectory 'overlay.json') -o (Join-Path $record.runDirectory 'MuxRay-io-acceptance.exe') .
        if ($LASTEXITCODE -ne 0) { throw 'Controlled native build failed.' }
        $record.goVersion = (& go version) -join "`n"
        $record.nodeVersion = (& node --version) -join "`n"
        $record.npmVersion = (& npm.cmd --version) -join "`n"
    } finally { Pop-Location }
}
Complete-Run $record
$record | ConvertTo-Json -Depth 12
