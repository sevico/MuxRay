package desktop

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/sevico/muxray/internal/analysis"
	"github.com/sevico/muxray/internal/document"
	"github.com/sevico/muxray/internal/flv"
	"github.com/sevico/muxray/internal/session"
	"github.com/sevico/muxray/internal/summaryview"
)

func snapshotOmissions(report DiagnosticSnapshot) map[string]string {
	result := make(map[string]string)
	for _, omission := range report.Omitted {
		result[omission.Section] = omission.Reason
	}
	return result
}

func TestSnapshotV2ReadyCurvesRetainedFreezeAndGolden(t *testing.T) {
	data := append([]byte(nil), fixture(0)...)
	for i := range 2 {
		payload := []byte{0x2f}
		for range 3 {
			frame := make([]byte, 417)
			copy(frame, []byte{0xff, 0xfb, 0x90, 0x64})
			payload = append(payload, frame...)
		}
		header := make([]byte, 11)
		header[0], header[2], header[3], header[6] = 8, byte(len(payload)>>8), byte(len(payload)), byte(i*26)
		data = append(data, header...)
		data = append(data, payload...)
		var previous [4]byte
		binary.BigEndian.PutUint32(previous[:], uint32(11+len(payload)))
		data = append(data, previous[:]...)
	}
	path := filepath.Join(t.TempDir(), "mp3-frames.flv")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	app := New(nil)
	cleanupApp(t, app)
	b := app.Bindings()
	id := b.OpenFile(OpenRequest{Path: path}).Session.SessionID
	finishReal(t, app, id)
	b.StartSummary(SummaryQuery{id, "9007199254740993"})
	if r := finishSummary(t, b, id, "9007199254740993"); r.Error != nil {
		t.Fatal(r.Error)
	}
	index := uint32(2)
	b.StartDetail(DetailRequest{SessionID: id, RequestID: "9007199254740994", TagID: "0", MP3FrameIndex: &index})
	if r := finishDetail(t, b, id, "9007199254740994"); r.Error != nil {
		t.Fatal(r.Error)
	}
	b.StartHex(HexRequest{id, "9007199254740995", "0", 4096})
	if r := finishHex(t, b, id, "9007199254740995"); r.Error != nil {
		t.Fatal(r.Error)
	}
	want := b.GetMediaCurves(MediaCurveRequest{id, "9007199254740993", "0", 64})
	if want.Error != nil {
		t.Fatal(want.Error)
	}
	request := SnapshotRequest{SchemaVersion: 2, SessionID: id, SelectedTagID: snapshotText("0"),
		DetailRequestID: snapshotText("9007199254740994"), HexRequestID: snapshotText("9007199254740995"),
		SummaryRequestID: snapshotText("9007199254740993"), SummaryPage: &SnapshotSummaryPageRequest{"mp3", "0", 100},
		Trend: &SnapshotTrendRequest{"0", 512, "all", "delta", "1000"}, MediaCurve: &SnapshotMediaCurveRequest{"0", 64, "gopMax"}}
	// The export must use already retained values, even if the source changed
	// without a later read discovering it. Its scope explicitly says latch-only.
	if err := os.WriteFile(path, []byte("changed since the retained analysis"), 0600); err != nil {
		t.Fatal(err)
	}
	captured, failure := app.CaptureSnapshot(request)
	report := decodeSnapshot(t, captured, failure)
	if report.SchemaVersion != 2 || report.Selection.SchemaVersion != 2 || !reflect.DeepEqual(report.MediaCurvePage, &want) ||
		report.Detail == nil || report.Hex == nil || report.Trend == nil || len(report.Omitted) != 0 {
		t.Fatalf("retained v2 sections or identity differ: %+v", report.Omitted)
	}
	if report.MediaCurvePage.Page.Points[0].GOP.AverageDurationMS != nil || report.Scope.SourceValidation != "existing_change_latch_only" {
		t.Fatal("unknown duration or source validation was overstated")
	}
	selected, expanded := "", []string{}
	for _, field := range report.Detail.Detail.Fields {
		if field.Name == "selectedFrameIndex" {
			selected = field.Value
		}
		if field.Type == "Layer III side information" {
			expanded = append(expanded, field.Name)
		}
	}
	if selected != "2" || !reflect.DeepEqual(expanded, []string{"Side info Frame 2"}) || report.MediaCurvePage.Page.Points[0].MP3.BitsPerSecond == nil {
		t.Fatalf("selected MP3 analysis or known curve was not frozen: %q %+v", selected, expanded)
	}
	snapshotGolden(t, "desktop-diagnostic-snapshot-full-v2.json", report)
	frozen := bytes.Clone(captured.Data)
	want.Page.Points[0].Index = "changed client DTO"
	request.MediaCurve.Start = "1"
	b.CloseFile(id)
	closeReal(t, app)
	if !bytes.Equal(frozen, captured.Data) {
		t.Fatal("later selection/DTO/close mutated frozen bytes")
	}
}

func TestSnapshotV2MinimalAndVersionIsolation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.flv")
	if err := os.WriteFile(path, fixture(0), 0600); err != nil {
		t.Fatal(err)
	}
	app := New(nil)
	cleanupApp(t, app)
	id := app.Bindings().OpenFile(OpenRequest{Path: path}).Session.SessionID
	finishReal(t, app, id)
	captured, failure := app.CaptureSnapshot(SnapshotRequest{SchemaVersion: 2, SessionID: id})
	report := decodeSnapshot(t, captured, failure)
	if len(report.Omitted) != 7 || report.MediaCurvePage != nil || snapshotOmissions(report)["mediaCurvePage"] != "not_requested" || !bytes.Contains(captured.Data, []byte(`"mediaCurvePage":null`)) {
		t.Fatalf("v2 missing section was implicit: %s", captured.Data)
	}
	snapshotGolden(t, "desktop-diagnostic-snapshot-minimal-v2.json", report)
	for _, version := range []uint8{0, 1} {
		captured, failure = app.CaptureSnapshot(SnapshotRequest{SchemaVersion: version, SessionID: id})
		report = decodeSnapshot(t, captured, failure)
		if report.SchemaVersion != 1 || report.Selection.SchemaVersion != 0 || bytes.Contains(captured.Data, []byte(`"mediaCurve`)) {
			t.Fatal("v1 shape gained v2 members")
		}
		snapshotGolden(t, "desktop-diagnostic-snapshot-minimal-v1.json", report)
	}
	for _, q := range []SnapshotRequest{
		{SchemaVersion: 3, SessionID: id},
		{SessionID: id, MediaCurve: &SnapshotMediaCurveRequest{"0", 64, "audio"}},
		{SchemaVersion: 1, SessionID: id, MediaCurve: &SnapshotMediaCurveRequest{"0", 64, "audio"}},
		{SchemaVersion: 2, SessionID: id, MediaCurve: &SnapshotMediaCurveRequest{"01", 64, "audio"}},
		{SchemaVersion: 2, SessionID: id, MediaCurve: &SnapshotMediaCurveRequest{"18446744073709551616", 64, "audio"}},
		{SchemaVersion: 2, SessionID: id, MediaCurve: &SnapshotMediaCurveRequest{"0", 0, "audio"}},
		{SchemaVersion: 2, SessionID: id, MediaCurve: &SnapshotMediaCurveRequest{"0", 65, "audio"}},
		{SchemaVersion: 2, SessionID: id, MediaCurve: &SnapshotMediaCurveRequest{"0", 64, "gop"}},
	} {
		captured, failure = app.CaptureSnapshot(q)
		checkFault(t, failure, CodeInvalidArgument)
		if captured.Data != nil {
			t.Fatal("invalid version/curve selection yielded data")
		}
	}
}

type snapshotCurveScan struct {
	*summaryTransportScan
	curve      document.MediaCurvePage
	curveErr   error
	curveCalls int
	curveID    uint64
	curveStart uint64
	curveCount uint32
}

func (s *snapshotCurveScan) StartSummary(uint64) (document.SummaryResult, error) {
	panic("snapshot must not start analysis")
}

func (s *snapshotCurveScan) MediaCurves(id, start uint64, count uint32) (document.MediaCurvePage, error) {
	s.curveCalls++
	s.curveID, s.curveStart, s.curveCount = id, start, count
	return s.curve, s.curveErr
}

func TestSnapshotV2CurvePrecisionBoundsAndOmissions(t *testing.T) {
	const start = uint64(1<<53 + 1)
	scan := &snapshotCurveScan{summaryTransportScan: &summaryTransportScan{fakeScan: newFakeScan(t, false),
		result: document.SummaryResult{RequestID: math.MaxUint64, State: "ready", Overview: &summaryview.Overview{}}},
		curve: document.MediaCurvePage{Start: start, Next: start + 1, Total: math.MaxUint64, Limit: 64, IndexedTags: math.MaxUint64,
			Points: []analysis.MediaCurvePoint{{Index: start, StartTag: start + 1, NextTag: math.MaxUint64,
				MP3: analysis.MediaCurveRate{CoveredBytes: math.MaxUint64, CoveredIntervalMS: 26, BitsPerSecond: math.MaxUint64, Present: true, TagID: start + 2}}}}}
	app := newApp(nil, func(context.Context, string, flv.Options) (scanSession, error) { return scan, nil })
	cleanupApp(t, app)
	id := app.Bindings().OpenFile(OpenRequest{Path: "memory-only-no-such-file.flv"}).Session.SessionID
	waitOpened(t, app)
	q := SnapshotRequest{SchemaVersion: 2, SessionID: id, SummaryRequestID: snapshotText("18446744073709551615"), MediaCurve: &SnapshotMediaCurveRequest{decimal(start), 64, "mp3"}}
	captured, failure := app.CaptureSnapshot(q)
	report := decodeSnapshot(t, captured, failure)
	curve := report.MediaCurvePage
	if curve == nil || curve.RequestID != *q.SummaryRequestID || curve.Page.Points[0].Index != decimal(start) ||
		*curve.Page.Points[0].MP3.BitsPerSecond != "18446744073709551615" || *curve.Page.Points[0].MP3.TagID != decimal(start+2) ||
		curve.Page.Complete || curve.Page.ScanComplete || scan.curveCalls != 1 || scan.curveID != math.MaxUint64 || scan.curveStart != start || scan.curveCount != 64 {
		t.Fatalf("exact source identity, precision or partial coverage differs: %+v", curve)
	}
	for _, tc := range []struct {
		err  error
		code string
	}{{document.ErrStaleRequest, CodeStaleRequest}, {document.ErrSourceChanged, CodeSourceChanged}, {session.ErrNotReady, CodeNotReady}, {document.ErrInvalidRequest, CodeInvalidArgument}} {
		scan.curveErr = tc.err
		captured, failure = app.CaptureSnapshot(q)
		report = decodeSnapshot(t, captured, failure)
		if report.MediaCurvePage != nil || snapshotOmissions(report)["mediaCurvePage"] != tc.code {
			t.Fatalf("curve error %v: %+v", tc.err, report.Omitted)
		}
	}
	scan.curveErr = nil
	for _, mutate := range []func(*document.MediaCurvePage){
		func(p *document.MediaCurvePage) { p.Start++ },
		func(p *document.MediaCurvePage) { p.Limit = 128 },
		func(p *document.MediaCurvePage) { p.Next++ },
		func(p *document.MediaCurvePage) { p.Points = append(p.Points, p.Points[0]) },
		func(p *document.MediaCurvePage) { p.Points = []analysis.MediaCurvePoint{{Index: 0}} },
	} {
		original := scan.curve
		mutate(&scan.curve)
		captured, failure = app.CaptureSnapshot(q)
		report = decodeSnapshot(t, captured, failure)
		if report.MediaCurvePage != nil || snapshotOmissions(report)["mediaCurvePage"] != "identity_mismatch" {
			t.Fatal("malformed provider page was exported")
		}
		scan.curve = original
	}
	before := scan.curveCalls
	scan.result.State = "pending"
	captured, failure = app.CaptureSnapshot(q)
	report = decodeSnapshot(t, captured, failure)
	if report.MediaCurvePage != nil || snapshotOmissions(report)["mediaCurvePage"] != "summary_unavailable" || scan.curveCalls != before {
		t.Fatal("pending summary was queried or exported")
	}
}

func TestSnapshotV2FullCurvePageRemainsWithinHardBudget(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "golden", "desktop-diagnostic-snapshot-full-v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var report DiagnosticSnapshot
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatal(err)
	}
	report.SchemaVersion, report.Selection.SchemaVersion = 2, 2
	maximum := "18446744073709551615"
	rate := MediaCurveRate{maximum, maximum, &maximum, &maximum}
	gop := MediaCurveGOP{maximum, maximum, maximum, &maximum, &maximum, &maximum, &maximum}
	report.MediaCurvePage = &MediaCurveResponse{APIVersion: 1, SessionID: report.Session.SessionID, RequestID: maximum,
		Page: &MediaCurvePage{Bounds: PageBounds{"0", 64, "64", "64"}, WindowTags: 256, IndexedTags: maximum, Points: make([]MediaCurvePoint, 64)}}
	for i := range report.MediaCurvePage.Page.Points {
		report.MediaCurvePage.Page.Points[i] = MediaCurvePoint{decimal(uint64(i)), maximum, maximum, rate, rate, rate, gop, maximum}
	}
	encoded, err := json.Marshal(report)
	if err != nil || len(encoded)+1 > MaxSnapshotBytes {
		t.Fatalf("64 maximum-width curve points exceed budget: %d %v", len(encoded), err)
	}
	t.Logf("full retained sections plus 64 maximum-width curve points: %d bytes", len(encoded)+1)
}
