#!/usr/bin/env bash
# Linux production build using GTK 3 and WebKitGTK 4.1 development packages.
# Wails generates bindings and builds the locked frontend before embedding assets.
set -euo pipefail

cd -- "$(dirname -- "${BASH_SOURCE[0]}")"
if ! command -v pkg-config >/dev/null || ! pkg-config --exists gtk+-3.0 webkit2gtk-4.1; then
    printf '%s\n' 'Missing Linux build dependencies: install pkg-config, libgtk-3-dev and libwebkit2gtk-4.1-dev.' >&2
    exit 1
fi

GOTOOLCHAIN="${GOTOOLCHAIN:-go1.27.0}" go run github.com/wailsapp/wails/v2/cmd/wails@v2.14.0 build -tags webkit2_41 -nopackage -trimpath
# Wails v2.14.0 writes generated bindings with Unix executable bits.
find frontend/wailsjs/go -type f -exec chmod 0644 {} +
