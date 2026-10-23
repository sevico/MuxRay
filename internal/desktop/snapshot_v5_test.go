package desktop

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/sevico/muxray/internal/document"
	"github.com/sevico/muxray/internal/flv"
	"github.com/sevico/muxray/internal/session"
	"github.com/sevico/muxray/internal/timesearch"
)

func TestSnapshotV5RealPagesCodecCoexistenceNoIOAndLegacy(t *testing.T) {
	path, _ := codecSearchFixture(t, "mp3")
	app := New(nil)
	cleanupApp(t, app)
	b := app.Bindings()
	id := b.OpenFile(OpenRequest{Path: path}).Session.SessionID
	finishReal(t, app, id)
	q := SnapshotRequest{SchemaVersion: 5, SessionID: id}
	capture, failure := app.CaptureSnapshot(q)
	report := decodeSnapshot(t, capture, failure)
	if len(report.Omitted) != 10 || snapshotOmissions(report)["timeSearchPage"] != "not_requested" || !bytes.Contains(capture.Data, []byte(`"timeSearchPage":null`)) {
		t.Fatalf("minimal %s", capture.Data)
	}
	snapshotGolden(t, "desktop-diagnostic-snapshot-minimal-v5.json", report)
	cursor := "0"
	for _, req := range []string{"1", "2", "3"} {
		b.StartCodecSearch(CodecSearchRequest{id, req, cursor, "mp3", "all"})
		got := finishCodecSearch(t, b, id, req)
		if got.Error != nil || got.Page == nil {
			t.Fatal(got)
		}
		q.CodecSearch = &SnapshotCodecSearchRequest{req, cursor, "mp3", "all"}
		cursor = got.Page.NextCursor
	}
	b.StartTimeSearch(TimeSearchRequest{id, "4", "0", "all", "1000"})
	want := finishTimeSearch(t, b, id, "4")
	if want.Error != nil || want.Page == nil || len(want.Page.Rows) != 1 {
		t.Fatal(want)
	}
	q.TimeSearch = &SnapshotTimeSearchRequest{"4", "0", "all", "1000"}
	b.StartHex(HexRequest{id, "5", "0", 64})
	if got := finishHex(t, b, id, "5"); got.Error != nil {
		t.Fatal(got)
	}
	q.HexRequestID = snapshotText("5")
	capture, failure = app.CaptureSnapshot(q)
	report = decodeSnapshot(t, capture, failure)
	if report.CodecSearchPage == nil || report.Hex == nil || report.TimeSearchPage == nil || !reflect.DeepEqual(report.TimeSearchPage.Page.Rows, want.Page.Rows) || report.TimeSearchPage.Page.InspectedStart != "0" || report.TimeSearchPage.Page.InspectedEnd != want.Page.Next {
		t.Fatal(report.Omitted)
	}
	snapshotGolden(t, "desktop-diagnostic-snapshot-full-v5.json", report)
	// An undiscovered source mutation must not trigger Stat/ReadAt or rebuild.
	if err := os.WriteFile(path, []byte("changed, not discovered"), 0600); err != nil {
		t.Fatal(err)
	}
	second, secondFailure := app.CaptureSnapshot(q)
	again := decodeSnapshot(t, second, secondFailure)
	if !reflect.DeepEqual(again.TimeSearchPage, report.TimeSearchPage) {
		t.Fatal("capture reread/replaced evidence")
	}
	for _, version := range []uint8{0, 1, 2, 3, 4} {
		old := q
		old.SchemaVersion, old.CodecSearch = version, nil
		_, failure := app.CaptureSnapshot(old)
		checkFault(t, failure, CodeInvalidArgument)
		old.TimeSearch = nil
		legacy, failure := app.CaptureSnapshot(old)
		decodeSnapshot(t, legacy, failure)
		if bytes.Contains(legacy.Data, []byte("timeSearch")) {
			t.Fatal("legacy gained v5 keys")
		}
	}
	frozen := bytes.Clone(capture.Data)
	b.StartHex(HexRequest{id, "6", "0", 4})
	if got := finishHex(t, b, id, "6"); got.Error == nil || got.Error.Code != CodeSourceChanged {
		t.Fatal(got)
	}
	_, failure = app.CaptureSnapshot(q)
	checkFault(t, failure, CodeSourceChanged)
	b.CloseFile(id)
	_, failure = app.CaptureSnapshot(q)
	checkFault(t, failure, CodeSessionClosed)
	if !bytes.Equal(capture.Data, frozen) {
		t.Fatal("frozen capture changed")
	}
}

func timeSnapshotFixture(t *testing.T, count int, distinct bool) string {
	t.Helper()
	data := fixture(count)
	if distinct {
		for i := range count {
			o := 13 + 15*i
			data[o+8], data[o+9], data[o+10] = byte(i>>16), byte(i>>8), byte(i)
		}
	}
	path := filepath.Join(t.TempDir(), "timestamp.flv")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSnapshotV5ContinuationPartialEmptyAndUnknown(t *testing.T) {
	for _, tc := range []struct {
		name, filter string
		count        int
		maxTags      uint32
		distinct     bool
	}{
		{"continuation", "duplicates", 300, 0, false},
		{"empty_batch", "regressions", 5000, 0, false},
		{"empty_source", "all", 0, 0, false},
		{"partial", "all", 300, 100, false},
		{"unknown", "regressions", 600, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := timeSnapshotFixture(t, tc.count, tc.distinct)
			app := New(nil)
			cleanupApp(t, app)
			b := app.Bindings()
			id := b.OpenFile(OpenRequest{Path: path, MaxTags: tc.maxTags}).Session.SessionID
			finishReal(t, app, id)
			q := SnapshotRequest{SchemaVersion: 5, SessionID: id}
			cursor := "0"
			for n := 1; ; n++ {
				req := decimal(uint64(n))
				b.StartTimeSearch(TimeSearchRequest{id, req, cursor, tc.filter, "1000"})
				want := finishTimeSearch(t, b, id, req)
				if want.Error != nil || want.Page == nil {
					t.Fatal(want)
				}
				if q.TimeSearch != nil {
					old, failure := app.CaptureSnapshot(q)
					if got := decodeSnapshot(t, old, failure); got.TimeSearchPage != nil || snapshotOmissions(got)["timeSearchPage"] != CodeStaleRequest {
						t.Fatal("superseded page exported")
					}
				}
				q.TimeSearch = &SnapshotTimeSearchRequest{req, want.Page.Start, tc.filter, "1000"}
				capture, failure := app.CaptureSnapshot(q)
				got := decodeSnapshot(t, capture, failure)
				if got.TimeSearchPage == nil || !reflect.DeepEqual(got.TimeSearchPage.Page.Rows, want.Page.Rows) || got.TimeSearchPage.Page.InspectedEnd != want.Page.Next || got.TimeSearchPage.Page.InspectedStart != "0" || bytes.Contains(capture.Data, []byte("nextCursor")) || bytes.Contains(capture.Data, []byte(`"cursor"`)) {
					t.Fatalf("%+v %+v", got.TimeSearchPage, got.Omitted)
				}
				if got.TimeSearchPage.Page.ScanComplete != (tc.maxTags == 0) {
					t.Fatal("partial prefix overstated")
				}
				if tc.distinct && got.TimeSearchPage.Page.UnknownPredecessors == 0 {
					t.Fatal("missing unknown predecessor coverage")
				}
				if want.Page.Exhausted {
					break
				}
				cursor = want.Page.NextCursor
			}
		})
	}
}

func TestSnapshotV5SignedTailRegressionAcrossBatches(t *testing.T) {
	data := fixture(9000)
	for i := range 9000 {
		stamp := uint32(i)
		if i == 8191 {
			stamp = ^uint32(0)
		}
		o := 13 + 15*i
		data[o+4], data[o+5], data[o+6], data[o+7] = byte(stamp>>16), byte(stamp>>8), byte(stamp), byte(stamp>>24)
	}
	path := filepath.Join(t.TempDir(), "tail.flv")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	app := New(nil)
	cleanupApp(t, app)
	b := app.Bindings()
	id := b.OpenFile(OpenRequest{Path: path}).Session.SessionID
	finishReal(t, app, id)
	cursor := "0"
	for n := range 3 {
		req := decimal(uint64(9007199254740993 + n))
		b.StartTimeSearch(TimeSearchRequest{id, req, cursor, "regressions", "1000"})
		r := finishTimeSearch(t, b, id, req)
		if r.Error != nil || r.Page == nil {
			t.Fatal(r)
		}
		q := SnapshotRequest{SchemaVersion: 5, SessionID: id, TimeSearch: &SnapshotTimeSearchRequest{req, r.Page.Start, "regressions", "1000"}}
		capture, failure := app.CaptureSnapshot(q)
		got := decodeSnapshot(t, capture, failure)
		if got.TimeSearchPage == nil {
			t.Fatal(got.Omitted)
		}
		if n == 2 {
			p := got.TimeSearchPage.Page
			if p.Start != "8192" || p.InspectedStart != "0" || p.InspectedEnd != "9000" || !p.Exhausted || len(p.Rows) != 1 {
				t.Fatalf("%+v", p)
			}
			point := p.Rows[0].Point
			if point.TagID != "8192" || *point.PreviousTagID != "8191" || *point.PreviousDTS != "4294967295" || *point.DeltaMS != "-4294959103" {
				t.Fatalf("%+v", point)
			}
		}
		cursor = r.Page.NextCursor
	}
}

func TestSnapshotV5UnknownAndThresholdValidation(t *testing.T) {
	point := flv.TagTrendPoint{TagID: 512, Offset: 13, DTS: 1001, TypeCode: 8, DataSize: 0, AvailableDataSize: 0, PayloadComplete: true, PreviousTagSizeState: "valid", Context: "budget_exhausted"}
	p := document.TimeSearchPage{Page: timesearch.Page{Start: 0, Next: 513, Total: 513, Scanned: 513, Exhausted: true, UnknownPredecessors: 1, Rows: []timesearch.Row{{Kind: "unknown", Point: point}}}, Filter: "regressions", GapThreshold: 1000}
	if !validSnapshotTimePage(&p, 1, 24) {
		t.Fatal("valid unknown rejected")
	}
	for _, mutate := range []func(*document.TimeSearchPage){
		func(p *document.TimeSearchPage) { p.Rows[0].Point.Context = "found" },
		func(p *document.TimeSearchPage) { p.Rows[0].Point.TagID = 511 },
		func(p *document.TimeSearchPage) { v := int64(0); p.Rows[0].Point.DeltaMS = &v },
		func(p *document.TimeSearchPage) { p.UnknownPredecessors = 0 },
	} {
		copy := p
		copy.Rows = append([]timesearch.Row{}, p.Rows...)
		mutate(&copy)
		if validSnapshotTimePage(&copy, 1, 24) {
			t.Fatal("invalid unknown accepted")
		}
	}
	id, prior, delta := uint64(511), int64(0), int64(1001)
	p.Rows[0].Kind = "gap"
	p.Rows[0].Point.Context = "found"
	p.Rows[0].Point.PreviousTagID = &id
	p.Rows[0].Point.PreviousDTS = &prior
	p.Rows[0].Point.DeltaMS = &delta
	p.UnknownPredecessors = 0
	p.Filter = "gaps"
	if !validSnapshotTimePage(&p, 1, 24) {
		t.Fatal("strict gap rejected")
	}
	p.GapThreshold = 1001
	if validSnapshotTimePage(&p, 1, 24) {
		t.Fatal("equal threshold became gap")
	}
	p.GapThreshold = 1000
	p.Filter = "duplicates"
	if validSnapshotTimePage(&p, 1, 24) {
		t.Fatal("wrong filter accepted")
	}
}

type snapshotTimeOverride struct {
	scanSession
	result  document.TimeSearchResult
	err     error
	lookups int
}

func (s *snapshotTimeOverride) SnapshotTimeSearch(uint64) (document.TimeSearchResult, error) {
	return s.result, s.err
}
func (s *snapshotTimeOverride) Page(start uint64, count uint32) (flv.TagPage, error) {
	s.lookups++
	return s.scanSession.Page(start, count)
}

func TestSnapshotV5RejectsMismatchedRetainedPages(t *testing.T) {
	path := timeSnapshotFixture(t, 100, false)
	app := New(nil)
	cleanupApp(t, app)
	b := app.Bindings()
	id := b.OpenFile(OpenRequest{Path: path}).Session.SessionID
	finishReal(t, app, id)
	b.StartTimeSearch(TimeSearchRequest{id, "1", "0", "duplicates", "1000"})
	if r := finishTimeSearch(t, b, id, "1"); r.Error != nil {
		t.Fatal(r)
	}
	app.mu.Lock()
	e := app.findLocked(id)
	r, err := e.scan.(snapshotTimeSearchSession).SnapshotTimeSearch(1)
	wrapper := &snapshotTimeOverride{scanSession: e.scan, result: r}
	e.scan = wrapper
	app.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	q := SnapshotRequest{SchemaVersion: 5, SessionID: id, TimeSearch: &SnapshotTimeSearchRequest{"1", "0", "duplicates", "1000"}}
	for _, tc := range []struct {
		err  error
		code string
	}{{document.ErrStaleRequest, CodeStaleRequest}, {document.ErrSourceChanged, CodeSourceChanged}, {document.ErrUnknownRequest, CodeTimeSearchNotFound}, {session.ErrNotReady, CodeNotReady}} {
		wrapper.err = tc.err
		capture, failure := app.CaptureSnapshot(q)
		got := decodeSnapshot(t, capture, failure)
		if got.TimeSearchPage != nil || snapshotOmissions(got)["timeSearchPage"] != tc.code {
			t.Fatal(got.Omitted)
		}
	}
	wrapper.err = nil
	for _, state := range []string{"pending", "canceling", "failed"} {
		wrapper.result.State = state
		capture, failure := app.CaptureSnapshot(q)
		got := decodeSnapshot(t, capture, failure)
		if got.TimeSearchPage != nil || snapshotOmissions(got)["timeSearchPage"] != CodeNotReady {
			t.Fatal("unfinished page exported")
		}
	}
	for name, mutate := range map[string]func(*document.TimeSearchResult){
		"request":       func(r *document.TimeSearchResult) { r.RequestID++ },
		"start":         func(r *document.TimeSearchResult) { r.Page.Start++ },
		"filter":        func(r *document.TimeSearchResult) { r.Page.Filter = "all" },
		"threshold":     func(r *document.TimeSearchResult) { r.Page.GapThreshold++ },
		"total":         func(r *document.TimeSearchResult) { r.Page.Total++ },
		"complete":      func(r *document.TimeSearchResult) { r.Page.ScanComplete = false },
		"scanned":       func(r *document.TimeSearchResult) { r.Page.Scanned = 4097 },
		"progress":      func(r *document.TimeSearchResult) { r.Page.Next++ },
		"cursor":        func(r *document.TimeSearchResult) { r.Page.Cursor = 1 },
		"next_cursor":   func(r *document.TimeSearchResult) { r.Page.NextCursor = 0 },
		"exhausted":     func(r *document.TimeSearchResult) { r.Page.Exhausted = true },
		"unknown_count": func(r *document.TimeSearchResult) { r.Page.UnknownPredecessors = 1 },
		"rows":          func(r *document.TimeSearchResult) { r.Page.Rows = append(r.Page.Rows, r.Page.Rows[0]) },
		"order":         func(r *document.TimeSearchResult) { r.Page.Rows[1] = r.Page.Rows[0] },
		"offset":        func(r *document.TimeSearchResult) { r.Page.Rows[0].Point.Offset = ^uint64(0) },
		"size":          func(r *document.TimeSearchResult) { r.Page.Rows[0].Point.DataSize = 1 << 24 },
		"dts":           func(r *document.TimeSearchResult) { r.Page.Rows[0].Point.DTS = 1 << 32 },
		"delta":         func(r *document.TimeSearchResult) { *r.Page.Rows[0].Point.DeltaMS = 1 },
		"kind":          func(r *document.TimeSearchResult) { r.Page.Rows[0].Kind = "gap" },
		"nil_prior":     func(r *document.TimeSearchResult) { r.Page.Rows[0].Point.PreviousTagID = nil },
		"prior_order":   func(r *document.TimeSearchResult) { *r.Page.Rows[0].Point.PreviousTagID = 1 },
		"type":          func(r *document.TimeSearchResult) { r.Page.Rows[0].Point.TypeCode = 8 },
		"stream":        func(r *document.TimeSearchResult) { r.Page.Rows[0].Point.StreamID = 1 },
		"filtered":      func(r *document.TimeSearchResult) { r.Page.Rows[0].Point.Filtered = true },
		"framing":       func(r *document.TimeSearchResult) { r.Page.Rows[0].Point.PreviousTagSizeState = "missing" },
	} {
		t.Run(name, func(t *testing.T) {
			encoded, _ := json.Marshal(r)
			var cloned document.TimeSearchResult
			if err := json.Unmarshal(encoded, &cloned); err != nil {
				t.Fatal(err)
			}
			wrapper.result = cloned
			wrapper.lookups = 0
			mutate(&wrapper.result)
			capture, failure := app.CaptureSnapshot(q)
			got := decodeSnapshot(t, capture, failure)
			if got.TimeSearchPage != nil || snapshotOmissions(got)["timeSearchPage"] != "identity_mismatch" {
				t.Fatal("bad page escaped", got.Omitted)
			}
			if wrapper.lookups > 128 {
				t.Fatal("unbounded page validation", wrapper.lookups)
			}
		})
	}
	for _, selection := range []SnapshotTimeSearchRequest{{"0", "0", "all", "1"}, {"01", "0", "all", "1"}, {"1", "01", "all", "1"}, {"1", "0", "regression", "1"}, {"1", "0", "all", "0"}, {"1", "0", "all", "4294967296"}, {"1", "0", "all", "01"}} {
		q.TimeSearch = &selection
		_, failure := app.CaptureSnapshot(q)
		checkFault(t, failure, CodeInvalidArgument)
	}
}
