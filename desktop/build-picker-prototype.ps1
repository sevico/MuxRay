# Explicit opt-in Windows prototype; the production entry point stays unchanged.
[CmdletBinding()]
param()
$ErrorActionPreference = 'Stop'
if ($env:OS -ne 'Windows_NT') { throw 'The picker process prototype requires Windows.' }
Push-Location -LiteralPath $PSScriptRoot
try {
    & wails build -webview2 error -nopackage -trimpath -tags pickerprocess -o MuxRay-picker-prototype.exe
    if ($LASTEXITCODE -ne 0) { throw "Picker prototype build failed with exit code $LASTEXITCODE." }
}
finally { Pop-Location }
