# Shared production entry point for local builds and .github/workflows/desktop.yml.
# Requires the reviewed Wails CLI v2.14.0, Go and Node/npm on PATH.
# Wails generates bindings and builds the locked frontend before embedding assets.
[CmdletBinding()]
param(
    [ValidateLength(1, 64)]
    [ValidatePattern('\A(?-i:dev|(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)(?:-(?:0|[1-9][0-9]*|[0-9A-Za-z-]*[A-Za-z-][0-9A-Za-z-]*)(?:\.(?:0|[1-9][0-9]*|[0-9A-Za-z-]*[A-Za-z-][0-9A-Za-z-]*))*)?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?)\z')]
    [string]$Version = '0.1.0-dev'
)

$ErrorActionPreference = 'Stop'
Push-Location -LiteralPath $PSScriptRoot
try {
    & wails build -webview2 error -nopackage -trimpath -ldflags "-X github.com/sevico/muxray/internal/buildinfo.Version=$Version"
    if ($LASTEXITCODE -ne 0) {
        throw "Production Wails build failed with exit code $LASTEXITCODE."
    }
}
finally {
    Pop-Location
}
