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

func TestMP3PageBindingOriginalIndicesRequestIdentityAndValidation(t *testing.T) {
	frame := make([]byte, 417)
	copy(frame, []byte{0xff, 0xfb, 0x90, 0x64})
	payload := append([]byte{0x2f}, bytes.Repeat(frame, 300)...)
	data := []byte{'F', 'L', 'V', 1, 4, 0, 0, 0, 9, 0, 0, 0, 0}
	header := make([]byte, 11)
	header[0], header[1], header[2], header[3] = 8, byte(len(payload)>>16), byte(len(payload)>>8), byte(len(payload))
	data = append(append(data, header...), payload...)
	var size [4]byte
	binary.BigEndian.PutUint32(size[:], uint32(len(payload)+11))
	data = append(data, size[:]...)
	path := filepath.Join(t.TempDir(), "mp3-page.flv")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	app := New(nil)
	cleanupApp(t, app)
	b := app.Bindings()
	id := b.OpenFile(OpenRequest{Path: path}).Session.SessionID
	finishReal(t, app, id)
	page, index := uint32(192), uint32(255)
	request := DetailRequest{SessionID: id, RequestID: "1", TagID: "0", MP3FrameIndex: &index, MP3PageStart: &page}
	if r := b.StartDetail(request); r.Error != nil {
		t.Fatal(r.Error)
	}
	ready := finishDetail(t, b, id, "1")
	if ready.Error != nil {
		t.Fatal(ready.Error)
	}
	values := map[string]string{}
	for _, f := range ready.Detail.Fields {
		values[f.Name] = f.Value
	}
	if values["requestedPageStart"] != "192" || values["selectedFrameIndex"] != "255" || values["pageState"] != "ready" || values["nextPageStart"] != "256" {
		t.Fatalf("page values %+v", values)
	}
	if duplicate := b.StartDetail(request); duplicate.State != "ready" || duplicate.Error != nil {
		t.Fatalf("recovery %+v", duplicate)
	}
	request.MP3PageStart = nil
	checkFault(t, b.StartDetail(request).Error, CodeInvalidArgument)
	request.RequestID = "2"
	for _, bad := range []uint32{1, 193, 256, ^uint32(0)} {
		request.MP3PageStart = &bad
		checkFault(t, b.StartDetail(request).Error, CodeInvalidArgument)
	}
	request.MP3PageStart = &page
	request.MP3FrameIndex = nil
	request.MP3Dependency = true
	if r := b.StartDetail(request); r.Error != nil {
		t.Fatal(r.Error)
	}
	ready = finishDetail(t, b, id, "2")
	if ready.Error != nil {
		t.Fatal(ready.Error)
	}
	values = map[string]string{}
	for _, f := range ready.Detail.Fields {
		values[f.Name] = f.Value
	}
	if values["selectedFrameIndex"] != "192" || values["targetFrameIndex"] != "192" {
		t.Fatal("dependency defaulted outside requested page")
	}
	encoded, err := json.Marshal(request)
	if err != nil || !strings.Contains(string(encoded), `"mp3PageStart":192`) {
		t.Fatalf("explicit DTO %s %v", encoded, err)
	}
	request.MP3PageStart = nil
	request.MP3Dependency = false
	encoded, err = json.Marshal(request)
	if err != nil || strings.Contains(string(encoded), "mp3PageStart") {
		t.Fatalf("legacy DTO changed %s %v", encoded, err)
	}
}
