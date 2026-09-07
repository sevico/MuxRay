//go:build bindings

package main

import "embed"

// Binding generation only reflects types; it neither starts a window nor
// serves assets. No generated placeholder is needed in frontend/dist.
var assets embed.FS
