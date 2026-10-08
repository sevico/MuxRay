package desktop

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/sevico/muxray/internal/detailview"
	"github.com/sevico/muxray/internal/document"
	"github.com/sevico/muxray/internal/flv"
	"github.com/sevico/muxray/internal/summaryview"
)

func snapshotText(s string) *string { return &s }

func decodeSnapshot(t *testing.T, captured SnapshotCapture, failure *Fault) DiagnosticSnapshot {
	t.Helper()
	if failure != nil {
		t.Fatalf("capture: %+v", failure)
	}
	if len(captured.Data) == 0 || len(captured.Data) > MaxSnapshotBytes || captured.Data[len(captured.Data)-1] != '\n' {
		t.Fatalf("bounded JSON: %d bytes", len(captured.Data))
	}
	var result DiagnosticSnapshot
	if err := json.Unmarshal(captured.Data, &result); err != nil {
		t.Fatal(err)
	}
	if _, err := time.Parse(time.RFC3339Nano, result.CreatedAt); err != nil || !strings.HasSuffix(result.CreatedAt, "Z") {
		t.Fatalf("UTC timestamp: %q %v", result.CreatedAt, err)
	}
	return result
}

func snapshotGolden(t *testing.T, name string, report DiagnosticSnapshot) {
	t.Helper()
	report.CreatedAt = "2026-09-29T00:00:00Z"
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	path := filepath.Join("..", "..", "testdata", "golden", name)
	if os.Getenv("MUXRAY_UPDATE_SNAPSHOT_GOLDEN") == "1" {
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(want, data) {
		t.Fatalf("snapshot contract changed: %s", path)
	}
}

func TestSnapshotCaptureFullRetainedWorkflowGolden(t *testing.T) {
	path := detailFixture(t)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, data[len(data)-19:]...) // another AAC configuration at the same raw DTS
	data[len(data)-1] = 0                       // retain an actual structural diagnostic and local observation
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	app := New(nil)
	cleanupApp(t, app)
	b := app.Bindings()
	id := b.OpenFile(OpenRequest{Path: path}).Session.SessionID
	finishReal(t, app, id)
	b.StartSummary(SummaryQuery{id, "9007199254740993"})
	if got := finishSummary(t, b, id, "9007199254740993"); got.Error != nil {
		t.Fatal(got.Error)
	}
	b.StartDetail(DetailRequest{SessionID: id, RequestID: "9007199254740994", TagID: "0"})
	if got := finishDetail(t, b, id, "9007199254740994"); got.Error != nil {
		t.Fatal(got.Error)
	}
	b.StartHex(HexRequest{id, "9007199254740995", "0", 4096})
	if got := finishHex(t, b, id, "9007199254740995"); got.Error != nil {
		t.Fatal(got.Error)
	}
	request := SnapshotRequest{SessionID: id, SelectedTagID: snapshotText("0"), DetailRequestID: snapshotText("9007199254740994"), HexRequestID: snapshotText("9007199254740995"), SummaryRequestID: snapshotText("9007199254740993"), SummaryPage: &SnapshotSummaryPageRequest{"aac", "0", 100}, Trend: &SnapshotTrendRequest{"0", 512, "all", "delta", "1000"}}
	captured, failure := app.CaptureSnapshot(request)
	report := decodeSnapshot(t, captured, failure)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if captured.SourcePath != path || captured.FileName != filepath.Base(path) || !os.SameFile(captured.SourceInfo, info) || bytes.Contains(captured.Data, []byte(filepath.Dir(path))) {
		t.Fatal("source identity or path privacy")
	}
	if report.Detail == nil || report.Hex == nil || report.Summary == nil || report.SummaryPage == nil || report.Trend == nil || report.SelectedTag == nil || len(report.Omitted) != 0 {
		t.Fatalf("ready sections missing: %+v", report.Omitted)
	}
	if report.Detail.RequestID != "9007199254740994" || report.Hex.RequestID != "9007199254740995" || report.Summary.RequestID != "9007199254740993" || report.SummaryPage.Kind != "aac" || len(report.Trend.Points) != 3 || len(report.TrendIssues) != 1 {
		t.Fatal("exact identities or trend facts lost")
	}
	if got := report.TrendIssues[0]; got.SourceTagID != "2" || !reflect.DeepEqual(got.Codes, []string{"duplicate", "previous_invalid"}) {
		t.Fatalf("local observations: %+v", got)
	}
	if len(report.ScanDiagnostics.Diagnostics) == 0 {
		t.Fatal("missing scan diagnostics")
	}
	snapshotGolden(t, "desktop-diagnostic-snapshot-full-v1.json", report)
	frozen := bytes.Clone(captured.Data)
	b.CloseFile(id)
	closeReal(t, app)
	if !bytes.Equal(captured.Data, frozen) {
		t.Fatal("close mutated frozen bytes")
	}
	_, failure = app.CaptureSnapshot(request)
	checkFault(t, failure, CodeSessionClosed)
	newID := b.OpenFile(OpenRequest{Path: path}).Session.SessionID
	finishReal(t, app, newID)
	_, failure = app.CaptureSnapshot(request)
	checkFault(t, failure, CodeSessionNotFound)
}

func TestSnapshotCaptureMinimalGoldenAndPartialIndex(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.flv")
	if err := os.WriteFile(path, fixture(0), 0600); err != nil {
		t.Fatal(err)
	}
	app := New(nil)
	cleanupApp(t, app)
	id := app.Bindings().OpenFile(OpenRequest{Path: path}).Session.SessionID
	finishReal(t, app, id)
	captured, failure := app.CaptureSnapshot(SnapshotRequest{SessionID: id})
	report := decodeSnapshot(t, captured, failure)
	if len(report.Omitted) != 6 || report.ScanDiagnostics.Diagnostics == nil || report.TrendIssues == nil {
		t.Fatalf("explicit empty sections: %+v", report)
	}
	snapshotGolden(t, "desktop-diagnostic-snapshot-minimal-v1.json", report)
	app.Bindings().CloseFile(id)
	closeReal(t, app)
	if err := os.WriteFile(path, fixture(3), 0600); err != nil {
		t.Fatal(err)
	}
	id = app.Bindings().OpenFile(OpenRequest{Path: path, MaxTags: 1}).Session.SessionID
	finishReal(t, app, id)
	captured, failure = app.CaptureSnapshot(SnapshotRequest{SessionID: id, Trend: &SnapshotTrendRequest{"0", 512, "all", "size", "1"}})
	report = decodeSnapshot(t, captured, failure)
	if report.Session.Scan.Complete || report.Session.Progress.TagCount != "1" || report.Trend.Bounds.Total != "1" {
		t.Fatal("partial coverage became complete")
	}
}

func TestSnapshotCaptureValidationAndIdentityOmissions(t *testing.T) {
	path := detailFixture(t)
	app := New(nil)
	cleanupApp(t, app)
	id := app.Bindings().OpenFile(OpenRequest{Path: path}).Session.SessionID
	finishReal(t, app, id)
	app.Bindings().StartDetail(DetailRequest{SessionID: id, RequestID: "1", TagID: "0"})
	finishDetail(t, app.Bindings(), id, "1")
	q := SnapshotRequest{SessionID: id, SelectedTagID: snapshotText("1"), DetailRequestID: snapshotText("1"), HexRequestID: snapshotText("2"), SummaryRequestID: snapshotText("3"), Trend: &SnapshotTrendRequest{"18446744073709551615", 512, "all", "dts", "1"}}
	captured, failure := app.CaptureSnapshot(q)
	report := decodeSnapshot(t, captured, failure)
	reasons := map[string]string{}
	for _, omission := range report.Omitted {
		reasons[omission.Section] = omission.Reason
	}
	if report.Detail != nil || reasons["detail"] != "identity_mismatch" || reasons["hex"] != CodeHexNotFound || reasons["summary"] != CodeSummaryNotFound || report.Trend != nil || reasons["trend"] != CodeInvalidArgument {
		t.Fatalf("omissions: %+v", reasons)
	}
	for _, mutate := range []func(*SnapshotRequest){
		func(q *SnapshotRequest) { q.SelectedTagID = snapshotText("01") },
		func(q *SnapshotRequest) { q.DetailRequestID = snapshotText("0") },
		func(q *SnapshotRequest) { q.HexRequestID = snapshotText("18446744073709551616") },
		func(q *SnapshotRequest) { q.SummaryPage = &SnapshotSummaryPageRequest{"unknown", "0", 100} },
		func(q *SnapshotRequest) { q.SummaryPage = &SnapshotSummaryPageRequest{"aac", "0", 101} },
		func(q *SnapshotRequest) { q.Trend = &SnapshotTrendRequest{"0", 513, "all", "dts", "1"} },
		func(q *SnapshotRequest) { q.Trend = &SnapshotTrendRequest{"0", 1, "all", "dts", "0"} },
		func(q *SnapshotRequest) { q.Trend = &SnapshotTrendRequest{"0", 1, "all", "dts", "4294967296"} },
		func(q *SnapshotRequest) { q.Trend = &SnapshotTrendRequest{"0", 1, "all", "bitrate", "1"} },
	} {
		q := SnapshotRequest{SessionID: id}
		mutate(&q)
		captured, failure := app.CaptureSnapshot(q)
		checkFault(t, failure, CodeInvalidArgument)
		if captured.Data != nil {
			t.Fatal("invalid request yielded bytes")
		}
	}
}

type snapshotLatchScan struct {
	*fakeScan
	queries int
	latchAt int
}

func (s *snapshotLatchScan) TrendPage(start uint64, count uint32) (flv.TagTrendPage, error) {
	s.queries++
	if s.queries >= s.latchAt {
		return flv.TagTrendPage{}, document.ErrSourceChanged
	}
	return s.fakeScan.TrendPage(start, count)
}

func TestSnapshotCaptureSourceLatchAndCloseFence(t *testing.T) {
	for _, latch := range []int{1, 2} {
		scan := &snapshotLatchScan{fakeScan: newFakeScan(t, false), latchAt: latch}
		app := newApp(nil, func(context.Context, string, flv.Options) (scanSession, error) { return scan, nil })
		cleanupApp(t, app)
		id := app.Bindings().OpenFile(OpenRequest{Path: "private.flv"}).Session.SessionID
		waitOpened(t, app)
		captured, failure := app.CaptureSnapshot(SnapshotRequest{SessionID: id})
		checkFault(t, failure, CodeSourceChanged)
		if captured.Data != nil {
			t.Fatal("latched source yielded snapshot")
		}
	}
	scan := newFakeScan(t, false)
	scan.pageEntered, scan.pageRelease = make(chan struct{}), make(chan struct{})
	app := newApp(nil, func(context.Context, string, flv.Options) (scanSession, error) { return scan, nil })
	cleanupApp(t, app)
	id := app.Bindings().OpenFile(OpenRequest{Path: "source.flv"}).Session.SessionID
	waitOpened(t, app)
	done := make(chan SnapshotCapture, 1)
	go func() {
		got, err := app.CaptureSnapshot(SnapshotRequest{SessionID: id, SelectedTagID: snapshotText("0")})
		if err != nil {
			t.Errorf("capture raced close: %+v", err)
		}
		done <- got
	}()
	<-scan.pageEntered
	if app.mu.TryLock() {
		app.mu.Unlock()
		t.Fatal("capture released slot fence")
	}
	closed := make(chan struct{})
	go func() { app.Bindings().CloseFile(id); close(closed) }()
	close(scan.pageRelease)
	got := <-done
	<-closed
	if len(got.Data) == 0 || got.SourcePath != "source.flv" {
		t.Fatal("frozen result lost")
	}
}

func TestSnapshotTrendObservationBoundariesAndExactCoordinates(t *testing.T) {
	points := []TagTrendPoint{
		{TagID: "9007199254740993", TypeCode: 8, DeltaMS: snapshotText("-4294967295"), PayloadComplete: false, PreviousTagSizeState: "missing"},
		{TagID: "9007199254740994", TypeCode: 8, DeltaMS: snapshotText("0"), PayloadComplete: true, PreviousTagSizeState: "valid"},
		{TagID: "9007199254740995", TypeCode: 8, DeltaMS: snapshotText("1000"), PayloadComplete: true, PreviousTagSizeState: "valid"},
		{TagID: "9007199254740996", TypeCode: 9, DeltaMS: snapshotText("1001"), PayloadComplete: true, PreviousTagSizeState: "invalid"},
		{TagID: "9007199254740997", TypeCode: 18, PayloadComplete: true, PreviousTagSizeState: "valid"},
	}
	got := snapshotTrendIssues(points, SnapshotTrendRequest{Type: "all", GapThreshold: "1000"})
	want := []SnapshotTrendIssue{{"local_observation", points[0].TagID, []string{"regression", "payload", "previous_missing"}}, {"local_observation", points[1].TagID, []string{"duplicate"}}, {"local_observation", points[3].TagID, []string{"gap", "previous_invalid"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("issues: %+v", got)
	}
	if got := snapshotTrendIssues(points, SnapshotTrendRequest{Type: "video", GapThreshold: "1001"}); len(got) != 1 || !reflect.DeepEqual(got[0].Codes, []string{"previous_invalid"}) {
		t.Fatalf("type/strict boundary: %+v", got)
	}
	scan := newFakeScan(t, false)
	scan.result = flv.ScanResult{Tags: []flv.TagRecord{{ID: 0, Offset: 1<<53 + 1, DTS: math.MaxUint32, RawTypeFlags: 8}}}
	app := newApp(nil, func(context.Context, string, flv.Options) (scanSession, error) { return scan, nil })
	cleanupApp(t, app)
	id := app.Bindings().OpenFile(OpenRequest{Path: "x.flv"}).Session.SessionID
	waitOpened(t, app)
	captured, failure := app.CaptureSnapshot(SnapshotRequest{SessionID: id, SelectedTagID: snapshotText("0"), Trend: &SnapshotTrendRequest{"0", 512, "all", "dts", "4294967295"}})
	report := decodeSnapshot(t, captured, failure)
	if report.SelectedTag.Offset != "9007199254740993" || report.Trend.Points[0].DTS != "4294967295" {
		t.Fatal("64-bit precision lost")
	}
}

func TestSnapshotMaximumSectionsAndHardByteBudget(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "golden", "desktop-diagnostic-snapshot-full-v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var report DiagnosticSnapshot
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatal(err)
	}
	wide := strings.Repeat("x", 256)
	maximum := "18446744073709551615"
	report.Detail.Detail.Fields = make([]detailview.Field, detailview.MaxFields)
	for i := range report.Detail.Detail.Fields {
		report.Detail.Detail.Fields[i] = detailview.Field{ID: decimal(uint64(i)), Name: strings.Repeat("n", 32), Type: "string", Value: wide, Span: &detailview.Span{ByteOffset: maximum, BitOffset: 7, BitLength: math.MaxUint32}}
	}
	report.Detail.Detail.Diagnostics = make([]detailview.Diagnostic, detailview.MaxDiagnostics)
	for i := range report.Detail.Detail.Diagnostics {
		report.Detail.Detail.Diagnostics[i] = detailview.Diagnostic{Code: "A_DIAGNOSTIC", Severity: "error", Offset: maximum, Message: wide, Expected: &maximum, Actual: &maximum}
	}
	report.Summary.Overview.Metrics = make([]summaryview.Metric, 50)
	for i := range report.Summary.Overview.Metrics {
		report.Summary.Overview.Metrics[i] = summaryview.Metric{Section: wide, Name: wide, Value: &wide, Unit: wide}
	}
	report.SummaryPage.Page.Events = make([]summaryview.Event, 100)
	for i := range report.SummaryPage.Page.Events {
		report.SummaryPage.Page.Events[i] = summaryview.Event{ID: decimal(uint64(i)), TagID: &maximum, Offset: maximum, Title: wide, Message: wide, Values: []summaryview.Metric{{Section: wide, Name: wide, Value: &wide, Unit: wide}}}
	}
	report.Hex.Window.Hex = strings.Repeat("ff", 4096)
	previous, dts, delta := uint64(math.MaxUint64), int64(math.MaxUint32), -int64(math.MaxUint32)
	point := mapTrendPoint(flv.TagTrendPoint{TagID: math.MaxUint64, Offset: math.MaxUint64, DTS: math.MaxUint32, TypeCode: math.MaxUint8, StreamID: math.MaxUint32, DataSize: math.MaxUint32, AvailableDataSize: math.MaxUint32, PreviousTagSizeState: "invalid", PreviousTagID: &previous, PreviousDTS: &dts, DeltaMS: &delta, Context: "found"})
	report.Trend.Points = make([]TagTrendPoint, 512)
	for i := range report.Trend.Points {
		report.Trend.Points[i] = point
	}
	report.TrendIssues = snapshotTrendIssues(report.Trend.Points, SnapshotTrendRequest{Type: "all", GapThreshold: "1"})
	report.ScanDiagnostics.Diagnostics = make([]Diagnostic, 100)
	for i := range report.ScanDiagnostics.Diagnostics {
		report.ScanDiagnostics.Diagnostics[i] = Diagnostic{Code: strings.Repeat("C", 128), Severity: "error", Offset: maximum, Message: strings.Repeat("m", 512), Expected: &maximum, Actual: &maximum}
	}
	for _, part := range []struct {
		value any
		limit int
	}{{report.Detail.Detail, detailview.MaxJSONBytes}, {report.Summary.Overview, summaryview.MaxOverviewBytes}, {report.SummaryPage, summaryview.MaxResponseBytes}, {report.Trend, 256 << 10}} {
		encoded, err := json.Marshal(part.value)
		if err != nil || len(encoded) > part.limit {
			t.Fatalf("maximum test fixture exceeds real section budget: %d > %d, %v", len(encoded), part.limit, err)
		}
	}
	encoded, err := json.Marshal(report)
	if err != nil || len(encoded)+1 > MaxSnapshotBytes {
		t.Fatalf("maximum sections exceed whole snapshot budget: %d %v", len(encoded), err)
	}
	t.Logf("maximum-width bounded sections: %d bytes", len(encoded)+1)
	// Even a faulty provider cannot export an oversized report.
	overflow := strings.Repeat("x", MaxSnapshotBytes)
	scan := &summaryTransportScan{fakeScan: newFakeScan(t, false), result: document.SummaryResult{RequestID: 1, State: "ready", Overview: &summaryview.Overview{Metrics: []summaryview.Metric{{Value: &overflow}}}}}
	app := newApp(nil, func(context.Context, string, flv.Options) (scanSession, error) { return scan, nil })
	cleanupApp(t, app)
	id := app.Bindings().OpenFile(OpenRequest{Path: "x.flv"}).Session.SessionID
	waitOpened(t, app)
	for _, version := range []uint8{0, 2} {
		captured, failure := app.CaptureSnapshot(SnapshotRequest{SchemaVersion: version, SessionID: id, SummaryRequestID: snapshotText("1")})
		checkFault(t, failure, CodeSnapshotFailed)
		if captured.Data != nil {
			t.Fatal("oversized capture yielded bytes")
		}
	}
}

func TestSnapshotScanDiagnosticPrefixCoverage(t *testing.T) {
	scan := newFakeScan(t, false)
	scan.result.Diagnostics = make([]flv.Diagnostic, 101)
	for i := range scan.result.Diagnostics {
		scan.result.Diagnostics[i] = flv.Diagnostic{Code: "D", Severity: flv.SeverityWarning, Offset: uint64(i)}
	}
	app := newApp(nil, func(context.Context, string, flv.Options) (scanSession, error) { return scan, nil })
	cleanupApp(t, app)
	id := app.Bindings().OpenFile(OpenRequest{Path: "prefix.flv"}).Session.SessionID
	waitOpened(t, app)
	captured, failure := app.CaptureSnapshot(SnapshotRequest{SessionID: id})
	report := decodeSnapshot(t, captured, failure)
	if report.ScanDiagnostics.Bounds != (PageBounds{"0", 100, "101", "100"}) || len(report.ScanDiagnostics.Diagnostics) != 100 || report.ScanDiagnostics.Diagnostics[99].Offset != "99" {
		t.Fatalf("prefix coverage: %+v", report.ScanDiagnostics.Bounds)
	}
}
