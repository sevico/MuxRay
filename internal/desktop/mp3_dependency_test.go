package desktop

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sevico/muxray/internal/detailview"
)

func TestMP3DependencyBindingIsExplicitAndDefaultsToFrameZero(t *testing.T) {
	frame := make([]byte, 417)
	copy(frame, []byte{0xff, 0xfb, 0x90, 0})
	data := []byte{'F', 'L', 'V', 1, 4, 0, 0, 0, 9, 0, 0, 0, 0}
	for i := 0; i < 2; i++ {
		frame[4] = byte(i * 5) // second frame has main_data_begin=10.
		payload := append([]byte{0x2f}, frame...)
		header := make([]byte, 11)
		header[0], header[2], header[3] = 8, byte(len(payload)>>8), byte(len(payload))
		data = append(data, header...)
		data = append(data, payload...)
		var size [4]byte
		binary.BigEndian.PutUint32(size[:], uint32(11+len(payload)))
		data = append(data, size[:]...)
	}
	path := filepath.Join(t.TempDir(), "mp3-dependency.flv")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	app := New(nil)
	cleanupApp(t, app)
	b := app.Bindings()
	id := b.OpenFile(OpenRequest{Path: path}).Session.SessionID
	finishReal(t, app, id)
	request := DetailRequest{SessionID: id, RequestID: "1", TagID: "1", MP3Dependency: true}
	if started := b.StartDetail(request); started.Error != nil {
		t.Fatalf("start %+v", started)
	}
	ready := finishDetail(t, b, id, "1")
	value := func(d *detailview.Detail, name string) string {
		t.Helper()
		if d == nil {
			t.Fatal("nil detail")
		}
		for _, f := range d.Fields {
			if f.Name == name {
				return f.Value
			}
		}
		return ""
	}
	if ready.Error != nil || value(ready.Detail, "dependencyState") != "resolved" || value(ready.Detail, "selectedFrameIndex") != "0" || value(ready.Detail, "targetFrameIndex") != "0" || value(ready.Detail, "sourceCount") != "2" {
		t.Fatalf("default identity or physical source query: %+v", ready)
	}
	zero := uint32(0)
	request.MP3FrameIndex = &zero
	if duplicate := b.StartDetail(request); duplicate.Error != nil || duplicate.State != "ready" {
		t.Fatalf("normalized duplicate: %+v", duplicate)
	}
	request.MP3Dependency = false
	checkFault(t, b.StartDetail(request).Error, CodeInvalidArgument)
	request.RequestID, request.MP3FrameIndex = "2", nil
	if started := b.StartDetail(request); started.Error != nil {
		t.Fatal(started.Error)
	}
	preview := finishDetail(t, b, id, "2")
	if preview.Error != nil || value(preview.Detail, "selectedFrameIndex") != "preview" || value(preview.Detail, "MP3 dependency sources") != "" {
		t.Fatalf("query leaked into preview: %+v", preview)
	}
	encoded, err := json.Marshal(request)
	if err != nil || strings.Contains(string(encoded), "mp3Dependency") || strings.Contains(string(encoded), "mp3FrameIndex") {
		t.Fatalf("old request envelope changed: %s %v", encoded, err)
	}
	request.MP3Dependency = true
	encoded, err = json.Marshal(request)
	if err != nil || !bytes.Contains(encoded, []byte(`"mp3Dependency":true`)) {
		t.Fatalf("explicit request missing: %s %v", encoded, err)
	}
	request.RequestID = "3"
	bad := uint32(64)
	request.MP3FrameIndex = &bad
	checkFault(t, b.StartDetail(request).Error, CodeInvalidArgument)
	if old := b.GetDetail(DetailQuery{id, "2"}); old.Error != nil || old.State != "ready" {
		t.Fatalf("invalid admission replaced prior detail: %+v", old)
	}
}
