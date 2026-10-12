package desktop

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"os"
	"reflect"
	"testing"

	"github.com/sevico/muxray/internal/avcsearch"
	"github.com/sevico/muxray/internal/cursor"
	"github.com/sevico/muxray/internal/document"
	"github.com/sevico/muxray/internal/flv"
	"github.com/sevico/muxray/internal/session"
)

func TestSnapshotV3RetainedPageAndVersionCompatibility(t *testing.T) {
	path, _ := avcSearchFixture(t)
	app := New(nil)
	cleanupApp(t, app)
	b := app.Bindings()
	id := b.OpenFile(OpenRequest{Path: path}).Session.SessionID
	finishReal(t, app, id)
	b.StartSummary(SummaryQuery{id, "9007199254740993"})
	if got := finishSummary(t, b, id, "9007199254740993"); got.Error != nil {
		t.Fatal(got.Error)
	}
	b.StartAVCSearch(AVCSearchRequest{id, "9007199254740994", "0", "all"})
	want := finishAVCSearch(t, b, id, "9007199254740994")
	if want.Error != nil || want.Page == nil || len(want.Page.Rows) != 3 {
		t.Fatalf("ready page: %+v", want)
	}
	b.StartDetail(DetailRequest{SessionID: id, RequestID: "9007199254740995", TagID: "1"})
	if got := finishDetail(t, b, id, "9007199254740995"); got.Error != nil {
		t.Fatal(got.Error)
	}
	b.StartHex(HexRequest{id, "9007199254740996", "0", 4096})
	if got := finishHex(t, b, id, "9007199254740996"); got.Error != nil {
		t.Fatal(got.Error)
	}
	q := SnapshotRequest{SchemaVersion: 3, SessionID: id, SelectedTagID: snapshotText("1"),
		DetailRequestID: snapshotText("9007199254740995"), HexRequestID: snapshotText("9007199254740996"),
		SummaryRequestID: snapshotText("9007199254740993"), SummaryPage: &SnapshotSummaryPageRequest{"diagnostics", "0", 100},
		Trend: &SnapshotTrendRequest{"0", 512, "all", "dts", "1000"}, MediaCurve: &SnapshotMediaCurveRequest{"0", 64, "video"},
		AVCSearch: &SnapshotAVCSearchRequest{"9007199254740994", "0", "all"}}
	// An undiscovered source change must not trigger fresh I/O during capture.
	if err := os.WriteFile(path, []byte("changed after retained reads"), 0600); err != nil {
		t.Fatal(err)
	}
	captured, failure := app.CaptureSnapshot(q)
	report := decodeSnapshot(t, captured, failure)
	if report.SchemaVersion != 3 || report.Selection.SchemaVersion != 3 || !reflect.DeepEqual(report.AVCSearchPage, &want) ||
		report.MediaCurvePage == nil || report.Detail == nil || report.Hex == nil || len(report.Omitted) != 0 ||
		report.AVCSearchPage.Page.Rows[2].ObservedIDR != "unknown" || report.AVCSearchPage.Page.Rows[2].Reason != "unclassified_header" {
		t.Fatalf("v3 retained sections changed: %+v", report)
	}
	snapshotGolden(t, "desktop-diagnostic-snapshot-full-v3.json", report)
	frozen := bytes.Clone(captured.Data)
	want.Page.Rows[0].Span.BitLength = 0
	q.AVCSearch.Start = "1"
	b.CloseFile(id)
	closeReal(t, app)
	if !bytes.Equal(frozen, captured.Data) {
		t.Fatal("returned capture was mutable")
	}
	_, failure = app.CaptureSnapshot(q)
	checkFault(t, failure, CodeSessionClosed)
}

func TestSnapshotV3MinimalEmptyAndPartialPages(t *testing.T) {
	for _, maxTags := range []uint32{0, 2} {
		path, _ := avcSearchFixture(t)
		app := New(nil)
		cleanupApp(t, app)
		b := app.Bindings()
		id := b.OpenFile(OpenRequest{Path: path, MaxTags: maxTags}).Session.SessionID
		finishReal(t, app, id)
		q := SnapshotRequest{SchemaVersion: 3, SessionID: id}
		captured, failure := app.CaptureSnapshot(q)
		report := decodeSnapshot(t, captured, failure)
		if report.AVCSearchPage != nil || snapshotOmissions(report)["avcSearchPage"] != "not_requested" || len(report.Omitted) != 8 ||
			!bytes.Contains(captured.Data, []byte(`"avcSearchPage":null`)) || !bytes.Contains(captured.Data, []byte(`"mediaCurvePage":null`)) {
			t.Fatalf("implicit absence: %s", captured.Data)
		}
		if maxTags == 0 {
			snapshotGolden(t, "desktop-diagnostic-snapshot-minimal-v3.json", report)
		}
		b.StartAVCSearch(AVCSearchRequest{id, "1", "0", "unknown"})
		want := finishAVCSearch(t, b, id, "1")
		q.AVCSearch = &SnapshotAVCSearchRequest{"1", "0", "unknown"}
		captured, failure = app.CaptureSnapshot(q)
		report = decodeSnapshot(t, captured, failure)
		if !reflect.DeepEqual(report.AVCSearchPage, &want) || report.AVCSearchPage.Page.ScanComplete != (maxTags == 0) {
			t.Fatal("partial index evidence was lost")
		}
		if maxTags == 2 && (len(report.AVCSearchPage.Page.Rows) != 0 || !report.AVCSearchPage.Page.Exhausted) {
			t.Fatal("empty partial page became missing/full coverage")
		}
		for _, version := range []uint8{0, 1, 2} {
			q.SchemaVersion = version
			_, failure := app.CaptureSnapshot(q)
			checkFault(t, failure, CodeInvalidArgument)
			q.AVCSearch = nil
			captured, failure := app.CaptureSnapshot(q)
			decodeSnapshot(t, captured, failure)
			if bytes.Contains(captured.Data, []byte(`"avcSearch`)) {
				t.Fatal("legacy snapshot gained v3 fields")
			}
			q.AVCSearch = &SnapshotAVCSearchRequest{"1", "0", "unknown"}
		}
	}
}

type snapshotSearchScan struct {
	*fakeScan
	result  document.AVCSearchResult
	err     error
	queryID uint64
}

func (s *snapshotSearchScan) SnapshotAVCSearch(id uint64) (document.AVCSearchResult, error) {
	s.queryID = id
	return s.result, s.err
}

func TestSnapshotV3PageValidationOmissionsAndPrecision(t *testing.T) {
	const start = uint64(1<<53 + 1)
	frame := uint8(2)
	p := avcsearch.Page{Start: start, Next: start + 1, Total: math.MaxUint64, Scanned: 1, BytesRead: 7, Filter: "all",
		Rows: []avcsearch.Row{{TagID: start, DTS: math.MaxUint32, Offset: math.MaxUint64, DeclaredFrameType: &frame,
			ObservedIDR: "yes", DeclarationRelation: "inter_with_idr", Reason: "complete", Span: &cursor.Span{ByteOffset: math.MaxUint64, BitOffset: 3, BitLength: 5}}}}
	scan := &snapshotSearchScan{fakeScan: newFakeScan(t, false), result: document.AVCSearchResult{RequestID: math.MaxUint64, State: "ready", Page: &p}}
	app := newApp(nil, func(context.Context, string, flv.Options) (scanSession, error) { return scan, nil })
	cleanupApp(t, app)
	id := app.Bindings().OpenFile(OpenRequest{Path: "memory-only.flv"}).Session.SessionID
	waitOpened(t, app)
	q := SnapshotRequest{SchemaVersion: 3, SessionID: id, AVCSearch: &SnapshotAVCSearchRequest{"18446744073709551615", decimal(start), "all"}}
	captured, failure := app.CaptureSnapshot(q)
	report := decodeSnapshot(t, captured, failure)
	if scan.queryID != math.MaxUint64 || report.AVCSearchPage == nil || report.AVCSearchPage.Page.Start != decimal(start) ||
		report.AVCSearchPage.Page.Rows[0].Span.ByteOffset != "18446744073709551615" || report.AVCSearchPage.Page.ScanComplete {
		t.Fatal("exact identity, precision or coverage lost")
	}
	for _, tc := range []struct {
		err  error
		code string
	}{
		{document.ErrStaleRequest, CodeStaleRequest}, {document.ErrSourceChanged, CodeSourceChanged},
		{document.ErrUnknownRequest, CodeAVCSearchNotFound}, {session.ErrNotReady, CodeNotReady},
	} {
		scan.err = tc.err
		captured, failure = app.CaptureSnapshot(q)
		report = decodeSnapshot(t, captured, failure)
		if report.AVCSearchPage != nil || snapshotOmissions(report)["avcSearchPage"] != tc.code {
			t.Fatalf("error omission: %+v", report.Omitted)
		}
	}
	scan.err = nil
	for _, state := range []string{"pending", "canceling"} {
		scan.result.State = state
		captured, failure = app.CaptureSnapshot(q)
		report = decodeSnapshot(t, captured, failure)
		if report.AVCSearchPage != nil || snapshotOmissions(report)["avcSearchPage"] != CodeNotReady {
			t.Fatal("unfinished work exported")
		}
	}
	scan.result.State = "ready"
	for _, mutate := range []func(*document.AVCSearchResult){
		func(r *document.AVCSearchResult) { r.RequestID-- },
		func(r *document.AVCSearchResult) { r.Page.Start++ },
		func(r *document.AVCSearchResult) { r.Page.Filter = "unknown" },
		func(r *document.AVCSearchResult) { r.Page.Next++ },
		func(r *document.AVCSearchResult) { r.Page.Scanned = avcsearch.MaxScanTags + 1 },
		func(r *document.AVCSearchResult) { r.Page.BytesRead = avcsearch.MaxBytes + 1 },
		func(r *document.AVCSearchResult) { r.Page.Exhausted = true },
		func(r *document.AVCSearchResult) { r.Page.Rows = make([]avcsearch.Row, 65) },
		func(r *document.AVCSearchResult) { r.Page.Rows = []avcsearch.Row{{TagID: start - 1}} },
	} {
		original, page := scan.result, *scan.result.Page
		mutate(&scan.result)
		captured, failure = app.CaptureSnapshot(q)
		report = decodeSnapshot(t, captured, failure)
		if report.AVCSearchPage != nil || snapshotOmissions(report)["avcSearchPage"] != "identity_mismatch" {
			t.Fatal("malformed or mismatched page exported")
		}
		scan.result, *original.Page = original, page
	}
	for _, selection := range []SnapshotAVCSearchRequest{{"0", "0", "all"}, {"01", "0", "all"}, {"1", "01", "all"}, {"1", "18446744073709551616", "all"}, {"1", "0", ""}} {
		q.AVCSearch = &selection
		_, failure = app.CaptureSnapshot(q)
		checkFault(t, failure, CodeInvalidArgument)
	}
}

func TestSnapshotV3MaxPageFitsCombinedBudget(t *testing.T) {
	data, err := os.ReadFile("../../testdata/golden/desktop-diagnostic-snapshot-full-v2.json")
	if err != nil {
		t.Fatal(err)
	}
	var report DiagnosticSnapshot
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatal(err)
	}
	report.SchemaVersion, report.Selection.SchemaVersion = 3, 3
	maximum := "18446744073709551615"
	report.AVCSearchPage = &AVCSearchResponse{APIVersion: 1, SessionID: report.Session.SessionID, RequestID: maximum, State: "ready",
		Page: &AVCSearchPage{Start: maximum, Next: maximum, Total: maximum, Scanned: 256, BytesRead: "1048576", Filter: "all", Rows: make([]AVCSearchRow, 64)}}
	for i := range report.AVCSearchPage.Page.Rows {
		report.AVCSearchPage.Page.Rows[i] = AVCSearchRow{TagID: maximum, DTS: "4294967295", Offset: maximum, ObservedIDR: "unknown", DeclarationRelation: "keyframe_without_idr", Reason: "missing_configuration"}
	}
	encoded, err := json.Marshal(report)
	if err != nil || len(encoded)+1 > MaxSnapshotBytes {
		t.Fatalf("64 rows exceed snapshot budget: %d %v", len(encoded), err)
	}
}
