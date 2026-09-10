package desktop

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/sevico/muxray/internal/detailview"
	"github.com/sevico/muxray/internal/document"
	"github.com/sevico/muxray/internal/flv"
)

func detailFixture(t *testing.T) string {
	t.Helper()
	data := []byte{'F', 'L', 'V', 1, 5, 0, 0, 0, 9, 0, 0, 0, 0}
	for i, payload := range [][]byte{{2, 0, 1, 'x', 0, 0x3f, 0xf0, 0, 0, 0, 0, 0, 0}, {0xaf, 0, 0x12, 0x10}} {
		header := make([]byte, 11)
		header[0] = []byte{18, 8}[i]
		header[3] = byte(len(payload))
		data = append(data, header...)
		data = append(data, payload...)
		var size [4]byte
		binary.BigEndian.PutUint32(size[:], uint32(11+len(payload)))
		data = append(data, size[:]...)
	}
	path := filepath.Join(t.TempDir(), "详情.flv")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func finishDetail(t *testing.T, b *Bindings, sessionID, requestID string) DetailResponse {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		response := b.GetDetail(DetailQuery{sessionID, requestID})
		if response.State != "pending" {
			return response
		}
		select {
		case <-deadline:
			t.Fatal("detail did not finish")
		default:
			runtime.Gosched()
		}
	}
}

func TestDetailAPIIdentityAndExactIntegers(t *testing.T) {
	path := detailFixture(t)
	app := New(nil)
	cleanupApp(t, app)
	b := app.Bindings()
	id := b.OpenFile(OpenRequest{Path: path}).Session.SessionID
	finishReal(t, app, id)
	request := DetailRequest{SessionID: id, RequestID: "9007199254740993", TagID: "0"}
	unknown := b.GetDetail(DetailQuery{id, request.RequestID})
	checkFault(t, unknown.Error, CodeDetailNotFound)
	if unknown.SessionID != id || unknown.RequestID != request.RequestID || unknown.TagID != "" || unknown.State != "failed" {
		t.Fatalf("unknown identity: %+v", unknown)
	}
	started := b.StartDetail(request)
	if started.Error != nil || started.State != "pending" || started.TagID != "0" || started.RequestID != request.RequestID {
		t.Fatalf("start: %+v", started)
	}
	ready := finishDetail(t, b, id, request.RequestID)
	if ready.Error != nil || ready.Detail == nil || ready.Detail.Kind != "amf0ScriptData" || !ready.Detail.Complete {
		t.Fatalf("ready: %+v", ready)
	}
	ready.Detail.Fields[0].Value = "changed"
	if duplicate := b.StartDetail(request); duplicate.Error != nil || duplicate.State != "ready" || duplicate.Detail.Fields[0].Value == "changed" {
		t.Fatalf("duplicate: %+v", duplicate)
	}
	checkFault(t, b.StartDetail(DetailRequest{id, request.RequestID, "1"}).Error, CodeInvalidArgument)
	checkFault(t, b.StartDetail(DetailRequest{id, "9007199254740992", "0"}).Error, CodeStaleRequest)
	checkFault(t, b.GetDetail(DetailQuery{id, "9007199254740992"}).Error, CodeStaleRequest)
	checkFault(t, b.GetDetail(DetailQuery{id, "18446744073709551615"}).Error, CodeDetailNotFound)
	if next := b.StartDetail(DetailRequest{id, "18446744073709551615", "1"}); next.Error != nil {
		t.Fatal(next.Error)
	}
	ready = finishDetail(t, b, id, "18446744073709551615")
	if ready.Error != nil || ready.Detail.Kind != "aacAudio" || ready.TagID != "1" || ready.RequestID != "18446744073709551615" {
		t.Fatalf("AAC identity: %+v", ready)
	}
	encoded, err := json.Marshal(ready)
	if err != nil || !strings.Contains(string(encoded), `"requestId":"18446744073709551615"`) || strings.Contains(string(encoded), path) {
		t.Fatalf("JSON contract: %s %v", encoded, err)
	}
	b.CloseFile(id)
	checkFault(t, b.GetDetail(DetailQuery{id, "18446744073709551615"}).Error, CodeSessionClosed)
	closeReal(t, app)
	newID := b.OpenFile(OpenRequest{Path: path}).Session.SessionID
	finishReal(t, app, newID)
	checkFault(t, b.GetDetail(DetailQuery{id, "18446744073709551615"}).Error, CodeSessionNotFound)
	checkFault(t, b.StartDetail(DetailRequest{id, "1", "0"}).Error, CodeSessionNotFound)
	if current := b.StartDetail(DetailRequest{newID, "1", "0"}); current.Error != nil {
		t.Fatalf("new document did not reset ledger: %+v", current)
	}
	if current := finishDetail(t, b, newID, "1"); current.Error != nil || current.SessionID != newID {
		t.Fatalf("new document changed by stale selection: %+v", current)
	}
}

func TestDetailAPIValidationAndErrors(t *testing.T) {
	path := detailFixture(t)
	app := New(nil)
	cleanupApp(t, app)
	b := app.Bindings()
	id := b.OpenFile(OpenRequest{Path: path}).Session.SessionID
	finishReal(t, app, id)
	for _, invalid := range []string{"", "0", "00", "01", "-1", "+1", " 1", "1.0", "18446744073709551616", strings.Repeat("1", 10000)} {
		checkFault(t, b.StartDetail(DetailRequest{id, invalid, "0"}).Error, CodeInvalidArgument)
		checkFault(t, b.GetDetail(DetailQuery{id, invalid}).Error, CodeInvalidArgument)
	}
	for _, invalid := range []string{"", "00", "-1", "1.0", "18446744073709551616"} {
		checkFault(t, b.StartDetail(DetailRequest{id, "1", invalid}).Error, CodeInvalidArgument)
	}
	checkFault(t, b.StartDetail(DetailRequest{id, "1", "2"}).Error, CodeTagNotFound)
	checkFault(t, b.StartDetail(DetailRequest{id, "1", "18446744073709551615"}).Error, CodeTagNotFound)
	checkFault(t, b.StartDetail(DetailRequest{"stale", "1", "0"}).Error, CodeSessionNotFound)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, info.ModTime(), info.ModTime().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	b.StartDetail(DetailRequest{id, "1", "0"})
	changed := finishDetail(t, b, id, "1")
	checkFault(t, changed.Error, CodeSourceChanged)
	if changed.Detail != nil || changed.SessionID != id || changed.RequestID != "1" || changed.TagID != "0" {
		t.Fatalf("source changed: %+v", changed)
	}
	for _, tc := range []struct {
		err  error
		code string
	}{
		{detailview.ErrUnsupported, CodeDetailUnsupported}, {detailview.ErrFiltered, CodeDetailFiltered}, {detailview.ErrTruncated, CodeDetailTruncated},
		{context.Canceled, CodeCanceled}, {context.DeadlineExceeded, CodeDeadline}, {errors.New("private path and OS error"), CodeDetailFailed},
	} {
		failure := detailFailure(tc.err)
		checkFault(t, failure, tc.code)
		if strings.Contains(failure.Message, "private") {
			t.Fatal("raw source error leaked")
		}
	}
}

func TestCanceledOpeningPublishesClosedRealDocument(t *testing.T) {
	path := detailFixture(t)
	created, release := make(chan struct{}), make(chan struct{})
	app := newApp(nil, func(ctx context.Context, path string, options flv.Options) (scanSession, error) {
		doc, err := document.Open(ctx, path, options)
		close(created)
		<-release
		return doc, err
	})
	cleanupApp(t, app)
	b := app.Bindings()
	id := b.OpenFile(OpenRequest{Path: path}).Session.SessionID
	await(t, created)
	b.CancelScan(id)
	close(release)
	waitOpened(t, app)
	closed := b.GetSession(id)
	if closed.Error != nil || !closed.Session.Closed {
		t.Fatalf("late document closure lost: %+v", closed)
	}
	checkFault(t, b.StartDetail(DetailRequest{id, "1", "0"}).Error, CodeSessionClosed)
	closeReal(t, app)
	if final := b.GetSession(id); !final.Session.Terminal || !final.Session.Closed {
		t.Fatalf("late document did not release slot: %+v", final)
	}
}
