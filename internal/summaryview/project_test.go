package summaryview

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/sevico/muxray/internal/analysis"
	"github.com/sevico/muxray/internal/flv"
	"github.com/sevico/muxray/internal/report"
)

func mediaFixture(payloads [][]byte) []byte {
	data := []byte{'F', 'L', 'V', 1, 4, 0, 0, 0, 9, 0, 0, 0, 0}
	for i, payload := range payloads {
		n := len(payload)
		dts := i * 26
		data = append(data, 8, byte(n>>16), byte(n>>8), byte(n), byte(dts>>16), byte(dts>>8), byte(dts), byte(dts>>24), 0, 0, 0)
		data = append(data, payload...)
		size := 11 + n
		data = append(data, byte(size>>24), byte(size>>16), byte(size>>8), byte(size))
	}
	return data
}
func metricValue(t *testing.T, overview Overview, section, name string) *string {
	t.Helper()
	for _, m := range overview.Metrics {
		if m.Section == section && m.Name == name {
			return m.Value
		}
	}
	t.Fatalf("missing metric %s/%s", section, name)
	return nil
}
func TestProjectionMatchesRealV2IntervalAndCodecFacts(t *testing.T) {
	data := mediaFixture([][]byte{{0x2f, 0xff, 0xfb, 0x90, 0xc0}, {0x2f, 0xff, 0xfb, 0x90, 0xc0}, {0xaf, 0, 0x12, 0x10}, {0xaf, 1, 0}, {0xaf, 1, 0}})
	scan, err := flv.ScanContext(context.Background(), bytes.NewReader(data), uint64(len(data)), flv.Options{})
	if err != nil {
		t.Fatal(err)
	}
	r, err := analysis.AnalyzeV2(context.Background(), bytes.NewReader(data), scan, DesktopOptions())
	if err != nil {
		t.Fatal(err)
	}
	p, err := Project(scan, r)
	if err != nil {
		t.Fatal(err)
	}
	cli := report.NewMediaSummaryV2(scan, r)
	if cli.Summary.MP3.Track.Bitrate == nil {
		t.Fatal("missing CLI bitrate")
	}
	want := strconv.FormatUint(cli.Summary.MP3.Track.Bitrate.AverageBitsPerSecond, 10)
	if got := metricValue(t, p.Overview, "mp3Bitrate", "averageBitsPerSecond"); got == nil || *got != want || *got != "1538" {
		t.Fatalf("interval bitrate %v / %s", got, want)
	}
	if got := metricValue(t, p.Overview, "audioCodecs", "switchCount"); got == nil || *got != "1" {
		t.Fatalf("switch count %v", got)
	}
	if metricValue(t, p.Overview, "avSync", "startExtremaSkewMs") != nil {
		t.Fatal("invented AV skew without video")
	}
	if len(p.Events["mp3"]) != 2 || len(p.Events["aac"]) != 1 || *p.Events["mp3"][1].TagID != "1" || *p.Events["mp3"][1].DTS != "26" {
		t.Fatalf("events %+v", p.Events)
	}
	if len(p.Overview.Metrics) > MaxMetrics || !p.Overview.ScanComplete || p.Overview.IndexedTags != "5" {
		t.Fatalf("overview %+v", p.Overview)
	}
	// Only independent strings/scalars survive projection, never references to
	// the source, shared index, core pointer fields or mutable event slices.
	r.MP3.Headers.Events[1].TagID = 999
	if *p.Events["mp3"][1].TagID != "1" {
		t.Fatal("projection aliases core event")
	}
}

func TestRetentionCountsAndUnknownDiagnosticTotal(t *testing.T) {
	payloads := make([][]byte, 300)
	for i := range payloads {
		payloads[i] = []byte{0x2f, 0xff, 0xfb, 0, 0xc0}
	}
	data := mediaFixture(payloads)
	scan, err := flv.ScanContext(context.Background(), bytes.NewReader(data), uint64(len(data)), flv.Options{})
	if err != nil {
		t.Fatal(err)
	}
	r, err := analysis.AnalyzeV2(context.Background(), bytes.NewReader(data), scan, DesktopOptions())
	if err != nil {
		t.Fatal(err)
	}
	p, err := Project(scan, r)
	if err != nil {
		t.Fatal(err)
	}
	var mp3List, diagnostics ListInfo
	for _, l := range p.Overview.Lists {
		if l.Kind == "mp3" {
			mp3List = l
		}
		if l.Kind == "diagnostics" {
			diagnostics = l
		}
	}
	if mp3List.Observed == nil || *mp3List.Observed != "300" || mp3List.Retained != "256" || !mp3List.Truncated {
		t.Fatalf("MP3 observed/retained %+v", mp3List)
	}
	if diagnostics.Observed != nil || diagnostics.Retained != "256" || !diagnostics.Truncated {
		t.Fatalf("diagnostic unknown total %+v", diagnostics)
	}
	if len(p.Events["mp3"]) != 256 || len(p.Events["diagnostics"]) != 256 {
		t.Fatal("retention budget")
	}
	// A canceled scan's committed index can be completely analyzed, but its
	// coverage remains explicitly partial rather than implying a full file.
	scan.Complete = false
	r, err = analysis.AnalyzeV2(context.Background(), bytes.NewReader(data), scan, DesktopOptions())
	if err != nil {
		t.Fatal(err)
	}
	p, err = Project(scan, r)
	if err != nil {
		t.Fatal(err)
	}
	if p.Overview.ScanComplete || p.Overview.Complete || !p.Overview.ScanHasErrors {
		t.Fatal("partial scan coverage hidden")
	}
}

func TestExactExtremesAndProjectionBudgets(t *testing.T) {
	scan := flv.ScanResult{SourceSize: math.MaxUint64, Complete: true}
	r := analysis.ResultV2{Result: analysis.Result{Complete: true, Tags: analysis.TagCounts{DataSizeBytes: math.MaxUint64}, VideoPTS: analysis.Timeline{Present: true, FirstMS: math.MinInt64, LastMS: math.MaxInt64}}}
	p, err := Project(scan, r)
	if err != nil {
		t.Fatal(err)
	}
	if *metricValue(t, p.Overview, "tags", "dataSizeBytes") != "18446744073709551615" || *metricValue(t, p.Overview, "videoPts", "firstMs") != "-9223372036854775808" {
		t.Fatal("lost exact coordinate")
	}
	data, err := json.Marshal(p.Overview)
	if err != nil || !bytes.Contains(data, []byte(`"value":"18446744073709551615"`)) {
		t.Fatalf("wire integers %s %v", data, err)
	}
	r.Diagnostics = []analysis.Diagnostic{{Message: strings.Repeat("x", MaxTextBytes+1)}}
	if _, err = Project(scan, r); err != ErrInvalidProjection {
		t.Fatalf("oversized message %v", err)
	}
	r.Diagnostics = nil
	r.Keyframes.Entries = make([]analysis.Keyframe, 1025)
	if _, err = Project(scan, r); err != ErrInvalidProjection {
		t.Fatalf("oversized retained events %v", err)
	}
}
