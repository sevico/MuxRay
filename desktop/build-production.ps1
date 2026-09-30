# Shared production entry point for local builds and .github/workflows/desktop.yml.
# Requires the reviewed Wails CLI v2.14.0, Go and Node/npm on PATH.
# Wails generates bindings and builds the locked frontend before embedding assets.
[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
Push-Location -LiteralPath $PSScriptRoot
try {
    & wails build -webview2 error -nopackage -trimpath
    if ($LASTEXITCODE -ne 0) {
        throw "Production Wails build failed with exit code $LASTEXITCODE."
    }
}
finally {
    Pop-Location
}
