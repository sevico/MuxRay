package main

import (
	"context"
	"strings"
	"testing"

	"github.com/sevico/muxray/internal/buildinfo"
)

func TestDesktopTitleBuildVersion(t *testing.T) {
	// appOptions only constructs configuration; this test never starts the GUI.
	host := newHost(nil)
	defer host.shutdown(context.Background())
	title := appOptions(host).Title
	if !strings.Contains(title, "MuxRay "+buildinfo.Version+" ") {
		t.Fatalf("desktop title does not identify this build: %q", title)
	}
}
