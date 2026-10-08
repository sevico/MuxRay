package desktop

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMP3FrameDetailBindingSelectionAndErrors(t *testing.T) {
	frame := make([]byte, 417)
	copy(frame, []byte{0xff, 0xfb, 0x90, 0})
	payload := append([]byte{0x2f}, bytes.Repeat(frame, 3)...)
	data := []byte{'F', 'L', 'V', 1, 4, 0, 0, 0, 9, 0, 0, 0, 0}
	header := make([]byte, 11)
	header[0], header[2], header[3] = 8, byte(len(payload)>>8), byte(len(payload))
	data = append(data, header...)
	data = append(data, payload...)
	var size [4]byte
	binary.BigEndian.PutUint32(size[:], uint32(11+len(payload)))
	data = append(data, size[:]...)
	path := filepath.Join(t.TempDir(), "mp3-frames.flv")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	app := New(nil)
	cleanupApp(t, app)
	b := app.Bindings()
	id := b.OpenFile(OpenRequest{Path: path}).Session.SessionID
	finishReal(t, app, id)
	index := uint32(2)
	request := DetailRequest{SessionID: id, RequestID: "1", TagID: "0", MP3FrameIndex: &index}
	if started := b.StartDetail(request); started.Error != nil {
		t.Fatalf("start %+v", started)
	}
	ready := finishDetail(t, b, id, "1")
	if ready.Error != nil || ready.Detail == nil {
		t.Fatalf("ready %+v", ready)
	}
	found := false
	for _, f := range ready.Detail.Fields {
		if f.Name == "selectedFrameIndex" && f.Value == "2" {
			found = true
		}
	}
	if !found || b.StartDetail(request).State != "ready" {
		t.Fatal("binding lost selected identity or same-ID replay")
	}
	checkFault(t, b.StartDetail(DetailRequest{SessionID: id, RequestID: "1", TagID: "0"}).Error, CodeInvalidArgument)
	index = 64
	request.RequestID = "2"
	checkFault(t, b.StartDetail(request).Error, CodeInvalidArgument)
	if prior := b.GetDetail(DetailQuery{id, "1"}); prior.Error != nil || prior.Detail == nil {
		t.Fatalf("rejected selection replaced prior request: %+v", prior)
	}
	index = 3
	if started := b.StartDetail(request); started.Error != nil {
		t.Fatalf("bounded async validation admission: %+v", started)
	}
	failed := finishDetail(t, b, id, "2")
	checkFault(t, failed.Error, CodeInvalidArgument)
	if failed.Detail != nil {
		t.Fatal("failed selection published stale detail")
	}
	encoded, err := json.Marshal(ready)
	if err != nil || strings.Contains(string(encoded), "mp3FrameIndex") {
		t.Fatalf("DetailResponse envelope changed: %s %v", encoded, err)
	}
	request.MP3FrameIndex = nil
	encoded, err = json.Marshal(request)
	if err != nil || strings.Contains(string(encoded), "mp3FrameIndex") {
		t.Fatalf("nil selector changed old request JSON: %s %v", encoded, err)
	}
}
