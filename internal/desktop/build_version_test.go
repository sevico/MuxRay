package desktop

import (
	"testing"

	"github.com/sevico/muxray/internal/buildinfo"
)

func TestSnapshotBuildVersion(t *testing.T) {
	path := detailFixture(t)
	app := New(nil)
	cleanupApp(t, app)
	id := app.Bindings().OpenFile(OpenRequest{Path: path}).Session.SessionID
	finishReal(t, app, id)
	captured, failure := app.CaptureSnapshot(SnapshotRequest{SessionID: id})
	report := decodeSnapshot(t, captured, failure)
	if report.Producer.Name != "MuxRay" || report.Producer.Version != buildinfo.Version {
		t.Fatalf("snapshot producer does not identify this build: %+v", report.Producer)
	}
}
