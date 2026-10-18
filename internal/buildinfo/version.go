// Package buildinfo identifies the software build independently of report schemas.
package buildinfo

// Version is shared by the CLI, desktop title and diagnostic snapshot producer.
// Production builds may set it with -ldflags -X; development builds stay explicit.
var Version = "0.1.0-dev"
