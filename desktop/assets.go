//go:build !bindings

package main

import "embed"

// Build the frontend before building/testing the native host. Keeping this out
// of binding generation permits a clean checkout with no compiled assets.
//
//go:embed all:frontend/dist
var assets embed.FS
