package desktop

import (
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func avcSearchFixture(t *testing.T) (string, []byte) {
	t.Helper()
	data := []byte{'F', 'L', 'V', 1, 1, 0, 0, 0, 9, 0, 0, 0, 0}
	for _, p := range [][]byte{{0x17, 0, 0, 0, 0, 1, 66, 0, 30, 0xff, 0xe0, 0}, {0x27, 1, 0, 0, 0, 0, 0, 0, 1, 0x65}, {0x17, 1, 0, 0, 0, 0, 0, 0, 1, 0x41}, {0x17, 1, 0, 0, 0, 0, 0, 0, 1, 0x74}} {
		h := make([]byte, 11)
		h[0] = 9
		h[3] = byte(len(p))
		data = append(data, h...)
		data = append(data, p...)
		var size [4]byte
		binary.BigEndian.PutUint32(size[:], uint32(11+len(p)))
		data = append(data, size[:]...)
	}
	path := filepath.Join(t.TempDir(), "search.flv")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return path, data
}
func finishAVCSearch(t *testing.T, b *Bindings, session, id string) AVCSearchResponse {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		r := b.GetAVCSearch(DetailQuery{session, id})
		if r.State != "pending" && r.State != "canceling" {
			return r
		}
		select {
		case <-deadline:
			t.Fatal("search pending")
		default:
			runtime.Gosched()
		}
	}
}
func TestAVCSearchAPIIdentityFiltersAndSpans(t *testing.T) {
	path, data := avcSearchFixture(t)
	app := New(nil)
	cleanupApp(t, app)
	b := app.Bindings()
	id := b.OpenFile(OpenRequest{Path: path}).Session.SessionID
	finishReal(t, app, id)
	request := AVCSearchRequest{SessionID: id, RequestID: "9007199254740993", Start: "0", Filter: "mismatches"}
	checkFault(t, b.GetAVCSearch(DetailQuery{id, request.RequestID}).Error, CodeAVCSearchNotFound)
	if started := b.StartAVCSearch(request); started.Error != nil || started.State != "pending" || started.Page != nil {
		t.Fatalf("start %+v", started)
	}
	got := finishAVCSearch(t, b, id, request.RequestID)
	if got.Error != nil || got.Page == nil || got.State != "ready" || got.Page.Start != "0" || got.Page.Next != "4" || got.Page.Total != "4" || got.Page.Scanned != 4 || !got.Page.Exhausted || !got.Page.ScanComplete || len(got.Page.Rows) != 2 {
		t.Fatalf("ready %+v", got)
	}
	for i, row := range got.Page.Rows {
		want := []string{"yes", "no"}[i]
		if row.ObservedIDR != want || row.Span == nil || row.Span.BitOffset != 3 || row.Span.BitLength != 5 || row.DeclaredFrameType == nil {
			t.Fatalf("row %+v", row)
		}
		off, valid := parseStart(row.Span.ByteOffset)
		if !valid || data[off]&31 != []byte{5, 1}[i] {
			t.Fatalf("source %+v", row)
		}
	}
	got.Page.Rows[0].ObservedIDR = "mutated"
	got.Page.Rows[0].Span.BitLength = 0
	duplicate := b.StartAVCSearch(request)
	if duplicate.State != "ready" || duplicate.Error != nil || duplicate.Page.Rows[0].ObservedIDR != "yes" || duplicate.Page.Rows[0].Span.BitLength != 5 {
		t.Fatalf("duplicate %+v", duplicate)
	}
	changed := request
	changed.Start = "1"
	checkFault(t, b.StartAVCSearch(changed).Error, CodeInvalidArgument)
	changed = request
	changed.Filter = "all"
	checkFault(t, b.StartAVCSearch(changed).Error, CodeInvalidArgument)
	encoded, err := json.Marshal(duplicate)
	if err != nil || !strings.Contains(string(encoded), `"requestId":"9007199254740993"`) || strings.Contains(string(encoded), path) || len(encoded) > 256<<10 {
		t.Fatalf("JSON %s %v", encoded, err)
	}
	if detail := b.StartDetail(DetailRequest{SessionID: id, RequestID: "9007199254740994", TagID: "1"}); detail.Error != nil {
		t.Fatal(detail.Error)
	}
	finishDetail(t, b, id, "9007199254740994")
	if retained := b.GetAVCSearch(DetailQuery{id, request.RequestID}); retained.Error != nil || retained.Page == nil || retained.Page.Rows[0].ObservedIDR != "yes" {
		t.Fatalf("retained %+v", retained)
	}
	request.RequestID = "18446744073709551615"
	request.Start = "3"
	request.Filter = "unknown"
	if r := b.StartAVCSearch(request); r.Error != nil {
		t.Fatal(r.Error)
	}
	got = finishAVCSearch(t, b, id, request.RequestID)
	if got.Error != nil || got.RequestID != request.RequestID || got.Page.Start != "3" || len(got.Page.Rows) != 1 || got.Page.Rows[0].ObservedIDR != "unknown" {
		t.Fatalf("unknown %+v", got)
	}
	b.CloseFile(id)
	checkFault(t, b.GetAVCSearch(DetailQuery{id, request.RequestID}).Error, CodeSessionClosed)
}
func TestAVCSearchAPIValidation(t *testing.T) {
	path, _ := avcSearchFixture(t)
	app := New(nil)
	cleanupApp(t, app)
	b := app.Bindings()
	id := b.OpenFile(OpenRequest{Path: path}).Session.SessionID
	finishReal(t, app, id)
	for _, s := range []string{"", "00", "01", "-1", "+1", " 1", "1.0", "18446744073709551616", strings.Repeat("1", 10000)} {
		checkFault(t, b.StartAVCSearch(AVCSearchRequest{SessionID: id, RequestID: "1", Start: s, Filter: "all"}).Error, CodeInvalidArgument)
		checkFault(t, b.GetAVCSearch(DetailQuery{id, s}).Error, CodeInvalidArgument)
	}
	checkFault(t, b.StartAVCSearch(AVCSearchRequest{SessionID: id, RequestID: "0", Start: "0", Filter: "all"}).Error, CodeInvalidArgument)
	checkFault(t, b.StartAVCSearch(AVCSearchRequest{SessionID: id, RequestID: "1", Start: "5", Filter: "all"}).Error, CodeInvalidArgument)
	for _, filter := range []string{"", "invalid", strings.Repeat("x", 10000)} {
		checkFault(t, b.StartAVCSearch(AVCSearchRequest{SessionID: id, RequestID: "1", Start: "0", Filter: filter}).Error, CodeInvalidArgument)
	}
	checkFault(t, b.CancelAVCSearch(DetailQuery{id, "1"}).Error, CodeAVCSearchNotFound)
	bad := b.StartAVCSearch(AVCSearchRequest{SessionID: strings.Repeat("s", 10000), RequestID: strings.Repeat("1", 10000), Start: "0", Filter: "all"})
	if bad.SessionID != "" || bad.RequestID != "" || bad.Page != nil {
		t.Fatalf("unbounded echo %+v", bad)
	}
	if start := b.StartAVCSearch(AVCSearchRequest{SessionID: id, RequestID: "1", Start: "4", Filter: "all"}); start.Error != nil {
		t.Fatal(start.Error)
	}
	got := finishAVCSearch(t, b, id, "1")
	if got.Page == nil || got.Page.Rows == nil || len(got.Page.Rows) != 0 || !got.Page.Exhausted {
		t.Fatalf("empty page %+v", got)
	}
}
