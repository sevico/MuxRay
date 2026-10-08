package desktop

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/sevico/muxray/internal/document"
)

func finishHex(t *testing.T, b *Bindings, sessionID, requestID string) HexResponse {
	t.Helper()
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	for {
		r := b.GetHex(HexQuery{sessionID, requestID})
		if r.State != "pending" {
			return r
		}
		select {
		case <-deadline.C:
			t.Fatal("hex did not finish")
		default:
			runtime.Gosched()
		}
	}
}

func TestHexAPIWindowIdentityAndSharedFence(t *testing.T) {
	path := detailFixture(t)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	app := New(nil)
	cleanupApp(t, app)
	b := app.Bindings()
	id := b.OpenFile(OpenRequest{Path: path}).Session.SessionID
	finishReal(t, app, id)
	request := HexRequest{id, "9007199254740993", "13", 0}
	checkFault(t, b.GetHex(HexQuery{id, request.RequestID}).Error, CodeHexNotFound)
	if start := b.StartHex(request); start.Error != nil || start.State != "pending" || start.Count != 512 {
		t.Fatalf("start: %+v", start)
	}
	r := finishHex(t, b, id, request.RequestID)
	if r.Error != nil || r.Window == nil || r.Window.Hex != hex.EncodeToString(data[13:]) || !r.Window.EOF || r.Window.Offset != "13" || r.Window.SourceSize != decimal(uint64(len(data))) {
		t.Fatalf("window: %+v", r)
	}
	r.Window.Hex = "mutated"
	request.Count = 512
	if retry := b.StartHex(request); retry.Error != nil || retry.State != "ready" || retry.Window.Hex == "mutated" {
		t.Fatalf("retry: %+v", retry)
	}
	checkFault(t, b.StartHex(HexRequest{id, request.RequestID, "14", 512}).Error, CodeInvalidArgument)
	checkFault(t, b.StartHex(HexRequest{id, request.RequestID, "13", 1}).Error, CodeInvalidArgument)
	checkFault(t, b.StartDetail(DetailRequest{SessionID: id, RequestID: request.RequestID, TagID: "0"}).Error, CodeInvalidArgument)
	if d := b.StartDetail(DetailRequest{SessionID: id, RequestID: "9007199254740994", TagID: "1"}); d.Error != nil {
		t.Fatal(d.Error)
	}
	if d := finishDetail(t, b, id, "9007199254740994"); d.Error != nil || d.Detail.Kind != "aacAudio" {
		t.Fatalf("detail after hex: %+v", d)
	}
	checkFault(t, b.GetHex(HexQuery{id, request.RequestID}).Error, CodeStaleRequest)
	eof := decimal(uint64(len(data)))
	if r := b.StartHex(HexRequest{id, "18446744073709551615", eof, 4096}); r.Error != nil {
		t.Fatal(r.Error)
	}
	r = finishHex(t, b, id, "18446744073709551615")
	if r.Window == nil || r.Window.Hex != "" || !r.Window.EOF || r.Count != 4096 {
		t.Fatalf("EOF: %+v", r)
	}
	checkFault(t, b.GetDetail(DetailQuery{id, "9007199254740994"}).Error, CodeStaleRequest)
	b.CloseFile(id)
	checkFault(t, b.GetHex(HexQuery{id, r.RequestID}).Error, CodeSessionClosed)
	closeReal(t, app)
	next := b.OpenFile(OpenRequest{Path: path}).Session.SessionID
	finishReal(t, app, next)
	checkFault(t, b.StartHex(HexRequest{id, "1", "0", 1}).Error, CodeSessionNotFound)
	if r := b.StartHex(HexRequest{next, "1", "0", 3}); r.Error != nil {
		t.Fatal(r.Error)
	}
	if r := finishHex(t, b, next, "1"); r.Window == nil || r.Window.Hex != "464c56" {
		t.Fatalf("reopen: %+v", r)
	}
}

func TestHexAPIValidationAndSourceChange(t *testing.T) {
	path := detailFixture(t)
	app := New(nil)
	cleanupApp(t, app)
	b := app.Bindings()
	id := b.OpenFile(OpenRequest{Path: path}).Session.SessionID
	finishReal(t, app, id)
	for _, bad := range []string{"", "0", "01", "-1", "+1", " 1", "1.0", "18446744073709551616", strings.Repeat("9", 10000)} {
		checkFault(t, b.StartHex(HexRequest{id, bad, "0", 1}).Error, CodeInvalidArgument)
		checkFault(t, b.GetHex(HexQuery{id, bad}).Error, CodeInvalidArgument)
	}
	for _, bad := range []string{"", "00", "-1", "1.0", "18446744073709551616", "9223372036854775808", "18446744073709551615"} {
		checkFault(t, b.StartHex(HexRequest{id, "1", bad, 1}).Error, CodeInvalidArgument)
	}
	checkFault(t, b.StartHex(HexRequest{id, "1", "0", 4097}).Error, CodeInvalidArgument)
	checkFault(t, b.StartHex(HexRequest{"unknown", "1", "0", 1}).Error, CodeSessionNotFound)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, info.ModTime(), info.ModTime().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if r := b.StartHex(HexRequest{id, "1", "0", 1}); r.Error != nil {
		t.Fatal(r.Error)
	}
	r := finishHex(t, b, id, "1")
	checkFault(t, r.Error, CodeSourceChanged)
	if r.Window != nil {
		t.Fatal("changed bytes published")
	}
	if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	b.StartDetail(DetailRequest{SessionID: id, RequestID: "2", TagID: "0"})
	checkFault(t, finishDetail(t, b, id, "2").Error, CodeSourceChanged)
}

func TestHexTransportExactCoordinatesAndBoundedErrors(t *testing.T) {
	r := mapHexResult(hexResponse("session", "1", ""), document.HexResult{
		RequestID: 18446744073709551615, Offset: 9007199254740993, Count: 4, State: "ready",
		Window: &document.HexWindow{Offset: 9007199254740993, SourceSize: 9223372036854775807, Bytes: []byte{0, 0x7f, 0x80, 0xff}},
	}, nil)
	encoded, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	for _, exact := range []string{`"requestId":"18446744073709551615"`, `"offset":"9007199254740993"`, `"sourceSize":"9223372036854775807"`, `"hex":"007f80ff"`, `"error":null`} {
		if !strings.Contains(string(encoded), exact) {
			t.Fatalf("missing %s: %s", exact, encoded)
		}
	}
	for _, tc := range []struct {
		err  error
		code string
	}{
		{document.ErrUnknownRequest, CodeHexNotFound}, {context.Canceled, CodeCanceled}, {context.DeadlineExceeded, CodeDeadline}, {errors.New("private filename and OS error"), CodeHexFailed},
	} {
		failure := hexFailure(tc.err)
		checkFault(t, failure, tc.code)
		if strings.Contains(failure.Message, "private") {
			t.Fatal("raw error leaked")
		}
	}
	oversized := strings.Repeat("x", 10000)
	invalid := New(nil).Bindings().StartHex(HexRequest{oversized, oversized, oversized, 1})
	encoded, err = json.Marshal(invalid)
	if err != nil || len(encoded) > 512 || invalid.Window != nil || invalid.State != "failed" {
		t.Fatalf("unbounded error: %d %v", len(encoded), err)
	}
}
