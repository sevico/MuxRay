package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"os"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/sevico/muxray/internal/document"
	"github.com/sevico/muxray/internal/flv"
	"github.com/sevico/muxray/internal/session"
	"github.com/sevico/muxray/internal/summaryview"
)

func finishSummary(t *testing.T, b *Bindings, sessionID, requestID string) SummaryResponse {
	t.Helper()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for {
		response := b.GetSummary(SummaryQuery{sessionID, requestID})
		if response.State != "pending" && response.State != "canceling" {
			return response
		}
		select {
		case <-deadline.C:
			t.Fatal("summary did not finish")
		default:
			runtime.Gosched()
		}
	}
}

func TestSummaryAPIRealIdentityPagesAndReadyCache(t *testing.T) {
	path := detailFixture(t)
	app := New(nil)
	cleanupApp(t, app)
	b := app.Bindings()
	id := b.OpenFile(OpenRequest{Path: path}).Session.SessionID
	finishReal(t, app, id)
	q := SummaryQuery{id, "9007199254740993"}
	for _, reply := range []SummaryResponse{b.GetSummary(q), b.CancelSummary(q)} {
		checkFault(t, reply.Error, CodeSummaryNotFound)
		if reply.SessionID != id || reply.RequestID != q.RequestID || reply.Processed != "0" || reply.Total != "0" || reply.State != "failed" || reply.Overview != nil {
			t.Fatalf("pre-admission identity: %+v", reply)
		}
	}
	started := b.StartSummary(q)
	if started.Error != nil || started.State != "pending" || started.RequestID != q.RequestID || started.Processed != "0" || started.Total != "2" {
		t.Fatalf("start: %+v", started)
	}
	ready := finishSummary(t, b, id, q.RequestID)
	if ready.Error != nil || ready.State != "ready" || ready.Overview == nil || ready.Processed != "2" || ready.Total != "2" || ready.Overview.IndexedTags != "2" || !ready.Overview.ScanComplete {
		t.Fatalf("real summary: %+v", ready)
	}
	encoded, err := json.Marshal(ready)
	if err != nil || strings.Contains(string(encoded), path) || !strings.Contains(string(encoded), `"requestId":"9007199254740993"`) {
		t.Fatalf("exact bounded summary JSON: %s %v", encoded, err)
	}
	want := append([]byte(nil), encoded...)
	for _, duplicate := range []SummaryResponse{b.StartSummary(q), b.GetSummary(q), b.CancelSummary(q), b.CancelSummary(q)} {
		encoded, err := json.Marshal(duplicate)
		if err != nil || string(encoded) != string(want) {
			t.Fatalf("terminal operation changed summary: %s %v", encoded, err)
		}
	}
	ready.Overview.SourceSize = "mutated"
	for i := range ready.Overview.Metrics {
		if ready.Overview.Metrics[i].Value != nil {
			*ready.Overview.Metrics[i].Value = "mutated"
		}
	}
	if encoded, _ := json.Marshal(b.GetSummary(q)); string(encoded) != string(want) {
		t.Fatal("summary DTO mutation changed retained overview")
	}
	pageQuery := SummaryPageRequest{id, q.RequestID, "aac", "0", 0}
	page := b.GetSummaryEvents(pageQuery)
	if page.Error != nil || page.Page == nil || page.Kind != "aac" || page.RequestID != q.RequestID || page.Page.Bounds != (PageBounds{Start: "0", Next: "1", Total: "1", Limit: 100}) || len(page.Page.Events) != 1 || page.Page.Events[0].TagID == nil || *page.Page.Events[0].TagID != "1" {
		t.Fatalf("AAC event page: %+v", page)
	}
	pageEncoded, _ := json.Marshal(page)
	*page.Page.Events[0].TagID = "mutated"
	for i := range page.Page.Events[0].Values {
		if page.Page.Events[0].Values[i].Value != nil {
			*page.Page.Events[0].Values[i].Value = "mutated"
		}
	}
	if encoded, _ := json.Marshal(b.GetSummaryEvents(pageQuery)); string(encoded) != string(pageEncoded) {
		t.Fatal("event page exposed retained pointers")
	}
	for _, empty := range []SummaryPageRequest{{id, q.RequestID, "aac", "1", 100}, {id, q.RequestID, "mp3", "0", 0}} {
		page := b.GetSummaryEvents(empty)
		if page.Error != nil || page.Page == nil || page.Page.Events == nil || len(page.Page.Events) != 0 || page.Page.Bounds.Start != page.Page.Bounds.Next {
			t.Fatalf("empty event end: %+v", page)
		}
	}
	checkFault(t, b.GetSummaryEvents(SummaryPageRequest{id, q.RequestID, "aac", "2", 1}).Error, CodeInvalidArgument)
	checkFault(t, b.GetSummaryEvents(SummaryPageRequest{id, q.RequestID, "aac", "18446744073709551615", 1}).Error, CodeInvalidArgument)
	checkFault(t, b.StartDetail(DetailRequest{id, q.RequestID, "0"}).Error, CodeInvalidArgument)
	checkFault(t, b.StartHex(HexRequest{id, q.RequestID, "0", 1}).Error, CodeInvalidArgument)
	if r := b.StartDetail(DetailRequest{id, "9007199254740994", "1"}); r.Error != nil {
		t.Fatal(r.Error)
	}
	if r := finishDetail(t, b, id, "9007199254740994"); r.Error != nil || r.Detail == nil {
		t.Fatalf("detail: %+v", r)
	}
	if r := b.StartHex(HexRequest{id, "9007199254740995", "0", 3}); r.Error != nil {
		t.Fatal(r.Error)
	}
	if r := finishHex(t, b, id, "9007199254740995"); r.Error != nil || r.Window == nil || r.Window.Hex != "464c56" {
		t.Fatalf("Hex: %+v", r)
	}
	if encoded, _ := json.Marshal(b.GetSummary(q)); string(encoded) != string(want) {
		t.Fatal("Detail/Hex evicted ready summary")
	}
	if page := b.GetSummaryEvents(pageQuery); page.Error != nil || page.Page == nil {
		t.Fatalf("cached page after Hex: %+v", page)
	}
	checkFault(t, b.StartSummary(q).Error, CodeStaleRequest)
	checkFault(t, b.StartSummary(SummaryQuery{id, "9007199254740995"}).Error, CodeInvalidArgument)
	q.RequestID = "18446744073709551615"
	if r := b.StartSummary(q); r.Error != nil {
		t.Fatal(r.Error)
	}
	ready = finishSummary(t, b, id, q.RequestID)
	if ready.Error != nil || ready.RequestID != q.RequestID || ready.Overview == nil {
		t.Fatalf("maximum ID: %+v", ready)
	}
	checkFault(t, b.GetSummary(SummaryQuery{id, "9007199254740993"}).Error, CodeStaleRequest)
	b.CloseFile(id)
	for _, r := range []SummaryResponse{b.GetSummary(q), b.CancelSummary(q), b.StartSummary(q)} {
		checkFault(t, r.Error, CodeSessionClosed)
	}
	checkFault(t, b.GetSummaryEvents(SummaryPageRequest{id, q.RequestID, "aac", "0", 1}).Error, CodeSessionClosed)
	closeReal(t, app)
	newID := b.OpenFile(OpenRequest{Path: path}).Session.SessionID
	finishReal(t, app, newID)
	if newID == id {
		t.Fatal("new document reused identity")
	}
	for _, r := range []SummaryResponse{b.GetSummary(q), b.CancelSummary(q), b.StartSummary(q)} {
		checkFault(t, r.Error, CodeSessionNotFound)
	}
	checkFault(t, b.GetSummaryEvents(pageQuery).Error, CodeSessionNotFound)
	if r := b.StartSummary(SummaryQuery{newID, "1"}); r.Error != nil {
		t.Fatal(r.Error)
	}
	if r := finishSummary(t, b, newID, "1"); r.Error != nil || r.SessionID != newID || r.Overview == nil {
		t.Fatalf("new ledger: %+v", r)
	}
}

func TestSummaryAPIValidationAndSourceChange(t *testing.T) {
	path := detailFixture(t)
	app := New(nil)
	cleanupApp(t, app)
	b := app.Bindings()
	id := b.OpenFile(OpenRequest{Path: path}).Session.SessionID
	finishReal(t, app, id)
	for _, bad := range []string{"", "0", "01", "-1", "+1", " 1", "1.0", "18446744073709551616", strings.Repeat("9", 10000)} {
		q := SummaryQuery{id, bad}
		for _, r := range []SummaryResponse{b.StartSummary(q), b.GetSummary(q), b.CancelSummary(q)} {
			checkFault(t, r.Error, CodeInvalidArgument)
		}
		checkFault(t, b.GetSummaryEvents(SummaryPageRequest{id, bad, "aac", "0", 1}).Error, CodeInvalidArgument)
	}
	for _, bad := range []string{"", "00", "-1", "+1", "1.0", "18446744073709551616"} {
		checkFault(t, b.GetSummaryEvents(SummaryPageRequest{id, "1", "aac", bad, 1}).Error, CodeInvalidArgument)
	}
	for _, bad := range []string{"", "AAC", "unknown", strings.Repeat("x", 10000)} {
		checkFault(t, b.GetSummaryEvents(SummaryPageRequest{id, "1", bad, "0", 1}).Error, CodeInvalidArgument)
	}
	checkFault(t, b.GetSummaryEvents(SummaryPageRequest{id, "1", "aac", "0", 101}).Error, CodeInvalidArgument)
	checkFault(t, b.StartSummary(SummaryQuery{"missing", "1"}).Error, CodeSessionNotFound)
	q := SummaryQuery{id, "1"}
	if r := b.StartSummary(q); r.Error != nil {
		t.Fatal(r.Error)
	}
	if r := finishSummary(t, b, id, "1"); r.Error != nil {
		t.Fatal(r.Error)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, info.ModTime(), info.ModTime().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if r := b.StartHex(HexRequest{id, "2", "0", 1}); r.Error != nil {
		t.Fatal(r.Error)
	}
	checkFault(t, finishHex(t, b, id, "2").Error, CodeSourceChanged)
	for _, r := range []SummaryResponse{b.GetSummary(q), b.CancelSummary(q)} {
		checkFault(t, r.Error, CodeSourceChanged)
		if r.Overview != nil || r.State != "failed" {
			t.Fatalf("stale overview: %+v", r)
		}
	}
	checkFault(t, b.GetSummaryEvents(SummaryPageRequest{id, "1", "aac", "0", 1}).Error, CodeSourceChanged)
	if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	if r := b.StartSummary(SummaryQuery{id, "3"}); r.Error != nil {
		t.Fatal(r.Error)
	}
	if r := finishSummary(t, b, id, "3"); r.Overview != nil || r.Error == nil || r.Error.Code != CodeSourceChanged {
		t.Fatalf("restored mtime cleared invalidation: %+v", r)
	}
}

func TestSummaryAPIRealCancellationConfirmation(t *testing.T) {
	path := detailFixture(t)
	app := New(nil)
	cleanupApp(t, app)
	b := app.Bindings()
	id := b.OpenFile(OpenRequest{Path: path}).Session.SessionID
	finishReal(t, app, id)
	q := SummaryQuery{id, "1"}
	if r := b.StartSummary(q); r.Error != nil {
		t.Fatal(r.Error)
	}
	first := b.CancelSummary(q)
	// A real file can finish before Cancel reaches the worker. Both terminal
	// outcomes are valid; same-ID confirmation must never admit a new analysis.
	if first.State != "ready" && first.State != "failed" && first.State != "canceling" {
		t.Fatalf("cancel state: %+v", first)
	}
	terminal := finishSummary(t, b, id, "1")
	if terminal.State == "ready" {
		if terminal.Error != nil || terminal.Overview == nil {
			t.Fatalf("completion won cancel incorrectly: %+v", terminal)
		}
	} else {
		checkFault(t, terminal.Error, CodeCanceled)
		if terminal.State != "failed" || terminal.Overview != nil {
			t.Fatalf("cancel published aggregate: %+v", terminal)
		}
	}
	want, _ := json.Marshal(terminal)
	for _, r := range []SummaryResponse{b.CancelSummary(q), b.CancelSummary(q), b.StartSummary(q), b.GetSummary(q)} {
		encoded, _ := json.Marshal(r)
		if string(encoded) != string(want) {
			t.Fatalf("cancel confirmation restarted/changed outcome: %s", encoded)
		}
	}
	if page := b.GetTags(PageRequest{id, "0", 100}); page.Error != nil || page.Page == nil || page.Page.Bounds.Total != "2" {
		t.Fatalf("summary cancellation discarded index: %+v", page)
	}
	if r := b.StartSummary(SummaryQuery{id, "2"}); r.Error != nil {
		t.Fatal(r.Error)
	}
	if r := finishSummary(t, b, id, "2"); r.Error != nil || r.Overview == nil {
		t.Fatalf("explicit reanalysis: %+v", r)
	}
}

// A transport-only provider supplies large values and malformed oversized DTOs;
// lifecycle behavior above uses actual retained file documents.
type summaryTransportScan struct {
	*fakeScan
	result    document.SummaryResult
	page      document.SummaryPage
	err       error
	id, start uint64
	kind      string
	count     uint32
}

func (s *summaryTransportScan) StartSummary(id uint64) (document.SummaryResult, error) {
	s.id = id
	return s.result, s.err
}
func (s *summaryTransportScan) GetSummary(id uint64) (document.SummaryResult, error) {
	s.id = id
	return s.result, s.err
}
func (s *summaryTransportScan) CancelSummary(id uint64) (document.SummaryResult, error) {
	s.id = id
	return s.result, s.err
}
func (s *summaryTransportScan) SummaryEvents(id uint64, kind string, start uint64, count uint32) (document.SummaryPage, error) {
	s.id, s.kind, s.start, s.count = id, kind, start, count
	return s.page, s.err
}

func TestSummaryTransportExactDTOAndEnvelopeBounds(t *testing.T) {
	const exact = "9007199254740993"
	minimum := "-9223372036854775808"
	scan := &summaryTransportScan{fakeScan: newFakeScan(t, false), result: document.SummaryResult{
		RequestID: math.MaxUint64, State: "ready", Processed: 9007199254740993, Total: math.MaxUint64,
		Overview: &summaryview.Overview{SourceSize: "9223372036854775807", IndexedTags: exact, Metrics: []summaryview.Metric{{Section: "timing", Name: "dts", Value: &minimum}, {Section: "timing", Name: "unknown"}}, Lists: []summaryview.ListInfo{{Kind: "diagnostics", Retained: "0"}}},
	}, page: document.SummaryPage{Kind: "mp3", Start: 9007199254740993, Next: 9007199254740994, Total: math.MaxUint64, Limit: 1, Events: []summaryview.Event{{ID: exact, Offset: exact, DTS: &minimum, Values: []summaryview.Metric{}}}}}
	app := newApp(nil, func(context.Context, string, flv.Options) (scanSession, error) { return scan, nil })
	cleanupApp(t, app)
	b := app.Bindings()
	id := b.OpenFile(OpenRequest{Path: "transport-only"}).Session.SessionID
	waitOpened(t, app)
	q := SummaryQuery{id, "18446744073709551615"}
	for _, r := range []SummaryResponse{b.StartSummary(q), b.GetSummary(q), b.CancelSummary(q)} {
		encoded, err := json.Marshal(r)
		if err != nil || r.Error != nil || scan.id != math.MaxUint64 {
			t.Fatalf("transport: %+v %v", r, err)
		}
		for _, fragment := range []string{`"requestId":"18446744073709551615"`, `"processed":"9007199254740993"`, `"total":"18446744073709551615"`, `"value":"-9223372036854775808"`, `"value":null`, `"observed":null`} {
			if !strings.Contains(string(encoded), fragment) {
				t.Fatalf("missing %s: %s", fragment, encoded)
			}
		}
	}
	page := b.GetSummaryEvents(SummaryPageRequest{id, q.RequestID, "mp3", exact, 1})
	encoded, err := json.Marshal(page)
	if err != nil || page.Error != nil || scan.start != 9007199254740993 || scan.kind != "mp3" || scan.count != 1 {
		t.Fatalf("page transport: %+v %v", page, err)
	}
	for _, fragment := range []string{`"start":"9007199254740993"`, `"next":"9007199254740994"`, `"total":"18446744073709551615"`, `"offset":"9007199254740993"`, `"dts":"-9223372036854775808"`} {
		if !strings.Contains(string(encoded), fragment) {
			t.Fatalf("missing %s: %s", fragment, encoded)
		}
	}
	oversized := strings.Repeat("x", summaryview.MaxResponseBytes)
	scan.result.Overview.Metrics[0].Value = &oversized
	failed := b.GetSummary(q)
	checkFault(t, failed.Error, CodeSummaryFailed)
	encoded, _ = json.Marshal(failed)
	if failed.Overview != nil || failed.State != "failed" || len(encoded) > 512 {
		t.Fatalf("oversized overview escaped bound: %d", len(encoded))
	}
	scan.page.Events[0].Message = oversized
	failedPage := b.GetSummaryEvents(SummaryPageRequest{id, q.RequestID, "mp3", "0", 100})
	checkFault(t, failedPage.Error, CodeSummaryFailed)
	encoded, _ = json.Marshal(failedPage)
	if failedPage.Page != nil || len(encoded) > 512 {
		t.Fatalf("oversized page escaped bound: %d", len(encoded))
	}
}

func TestSummaryAPIErrorsStayBoundedAndPrivate(t *testing.T) {
	for _, tc := range []struct {
		err  error
		code string
	}{
		{document.ErrUnknownRequest, CodeSummaryNotFound}, {document.ErrStaleRequest, CodeStaleRequest}, {document.ErrInvalidRequest, CodeInvalidArgument},
		{document.ErrSourceChanged, CodeSourceChanged}, {session.ErrClosed, CodeSessionClosed}, {session.ErrNotReady, CodeNotReady},
		{context.Canceled, CodeCanceled}, {context.DeadlineExceeded, CodeDeadline},
		{&os.PathError{Op: "read", Path: "C:/private/secret.flv", Err: errors.New("private OS error")}, CodeSummaryFailed},
	} {
		scan := &summaryTransportScan{fakeScan: newFakeScan(t, false), err: tc.err}
		app := newApp(nil, func(context.Context, string, flv.Options) (scanSession, error) { return scan, nil })
		cleanupApp(t, app)
		b := app.Bindings()
		id := b.OpenFile(OpenRequest{Path: "transport-only"}).Session.SessionID
		waitOpened(t, app)
		q := SummaryQuery{id, "1"}
		for _, r := range []SummaryResponse{b.StartSummary(q), b.GetSummary(q), b.CancelSummary(q)} {
			checkFault(t, r.Error, tc.code)
			encoded, _ := json.Marshal(r)
			if strings.Contains(string(encoded), "private") || len(encoded) > 512 || r.Overview != nil || r.Processed != "0" || r.Total != "0" {
				t.Fatalf("unsafe failure: %s", encoded)
			}
		}
		p := b.GetSummaryEvents(SummaryPageRequest{id, "1", "aac", "0", 1})
		checkFault(t, p.Error, tc.code)
		if strings.Contains(p.Error.Message, "private") || p.Page != nil {
			t.Fatalf("unsafe page error: %+v", p)
		}
		// The task-error channel must map errors identically to access failures.
		scan.err, scan.result = nil, document.SummaryResult{RequestID: 1, State: "failed", Err: tc.err}
		checkFault(t, b.GetSummary(q).Error, tc.code)
	}
	large := strings.Repeat("private", 10000)
	b := New(nil).Bindings()
	for _, r := range []SummaryResponse{b.StartSummary(SummaryQuery{large, large}), b.GetSummary(SummaryQuery{large, large}), b.CancelSummary(SummaryQuery{large, large})} {
		checkFault(t, r.Error, CodeInvalidArgument)
		encoded, _ := json.Marshal(r)
		if len(encoded) > 512 || strings.Contains(string(encoded), "private") || r.SessionID != "" || r.RequestID != "" {
			t.Fatalf("unbounded request echo: %s", encoded)
		}
	}
	p := b.GetSummaryEvents(SummaryPageRequest{large, large, large, large, 101})
	encoded, _ := json.Marshal(p)
	if len(encoded) > 512 || strings.Contains(string(encoded), "private") || p.Kind != "" || p.Page != nil {
		t.Fatalf("unbounded page echo: %s", encoded)
	}
}

func TestSummaryAPINotReadyUnsupportedAndOpenFailure(t *testing.T) {
	for _, active := range []bool{true, false} {
		scan := newFakeScan(t, active)
		app := newApp(nil, func(context.Context, string, flv.Options) (scanSession, error) { return scan, nil })
		cleanupApp(t, app)
		if active {
			t.Cleanup(func() { scan.finish(context.Canceled) })
		}
		b := app.Bindings()
		id := b.OpenFile(OpenRequest{Path: "legacy-scan-only"}).Session.SessionID
		waitOpened(t, app)
		code := CodeSummaryUnsupported
		if active {
			code = CodeNotReady
		}
		q := SummaryQuery{id, "1"}
		for _, r := range []SummaryResponse{b.StartSummary(q), b.GetSummary(q), b.CancelSummary(q)} {
			checkFault(t, r.Error, code)
		}
		checkFault(t, b.GetSummaryEvents(SummaryPageRequest{id, "1", "mp3", "0", 1}).Error, code)
	}
	app := newApp(nil, func(context.Context, string, flv.Options) (scanSession, error) {
		return nil, errors.New("private open failure")
	})
	cleanupApp(t, app)
	b := app.Bindings()
	id := b.OpenFile(OpenRequest{Path: "private-missing"}).Session.SessionID
	waitOpened(t, app)
	q := SummaryQuery{id, "1"}
	for _, r := range []SummaryResponse{b.StartSummary(q), b.GetSummary(q), b.CancelSummary(q)} {
		checkFault(t, r.Error, CodeOpenFailed)
	}
	checkFault(t, b.GetSummaryEvents(SummaryPageRequest{id, "1", "mp3", "0", 1}).Error, CodeOpenFailed)
}

func TestSummaryDTOsExposeOnlyConcreteExactFields(t *testing.T) {
	seen := map[reflect.Type]bool{}
	var visit func(reflect.Type)
	visit = func(typ reflect.Type) {
		if seen[typ] {
			return
		}
		seen[typ] = true
		switch typ.Kind() {
		case reflect.Struct:
			for field := range typ.Fields() {
				if !field.IsExported() || field.Tag.Get("json") == "" {
					t.Errorf("unmapped DTO: %s.%s", typ, field.Name)
				}
				visit(field.Type)
			}
		case reflect.Pointer, reflect.Slice:
			visit(typ.Elem())
		case reflect.String, reflect.Bool, reflect.Uint8, reflect.Uint32:
		default:
			t.Errorf("unsafe/lossy DTO field: %s", typ)
		}
	}
	for _, typ := range []reflect.Type{reflect.TypeFor[SummaryQuery](), reflect.TypeFor[SummaryResponse](), reflect.TypeFor[SummaryPageRequest](), reflect.TypeFor[SummaryPageResponse]()} {
		visit(typ)
	}
}
