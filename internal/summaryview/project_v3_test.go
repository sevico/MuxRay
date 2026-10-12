package summaryview

import (
	"bytes"
	"testing"

	"github.com/sevico/muxray/internal/analysis"
	"github.com/sevico/muxray/internal/flv"
)

func TestFullFrameProjectionRetainsCountsAfterEventLimitAndExactSource(t *testing.T) {
	frame := make([]byte, 417)
	copy(frame, []byte{0xff, 0xfb, 0x90, 0xc0})
	data := mediaFixture([][]byte{append([]byte{0x2f}, bytes.Repeat(frame, 300)...)})
	scan, err := flv.ScanContext(t.Context(), bytes.NewReader(data), uint64(len(data)), flv.Options{})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := analysis.AnalyzeV3(t.Context(), bytes.NewReader(data), scan, DesktopOptionsV3())
	if err != nil {
		t.Fatal(err)
	}
	projected, err := ProjectV3(scan, raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(projected.Overview.Metrics) > MaxMetrics || !projected.Overview.Complete {
		t.Fatalf("overview bounds: %+v", projected.Overview)
	}
	if got := metricValue(t, projected.Overview, "mp3Frames", "completeFrameCount"); got == nil || *got != "300" {
		t.Fatalf("frame count %v", got)
	}
	if got := metricValue(t, projected.Overview, "mp3Frames", "durationSeconds"); got == nil || *got != "384/49" {
		t.Fatalf("duration %v", got)
	}
	list := projected.Overview.Lists[3]
	if list.Kind != "mp3" || *list.Observed != "300" || list.Retained != "256" || !list.Truncated {
		t.Fatalf("counts %+v", list)
	}
	last := projected.Events["mp3"][255]
	if last.Title != "mp3Frame" || *last.TagID != "0" || *last.DTS != "0" || last.Offset != number(25+255*417) || *last.Values[0].Value != "255" {
		t.Fatalf("last retained source %+v", last)
	}
	raw.MP3Frames.Events[255].SourceSpan.ByteOffset = 999
	if projected.Events["mp3"][255].Offset != last.Offset {
		t.Fatal("projection aliased source events")
	}
}

func TestFullFrameProjectionDoesNotHideUnresolvedTail(t *testing.T) {
	data := mediaFixture([][]byte{{0x2f, 0xff, 0xfb, 0x90, 0xc0}})
	scan, _ := flv.ScanContext(t.Context(), bytes.NewReader(data), uint64(len(data)), flv.Options{})
	raw, err := analysis.AnalyzeV3(t.Context(), bytes.NewReader(data), scan, DesktopOptionsV3())
	if err != nil {
		t.Fatal(err)
	}
	p, err := ProjectV3(scan, raw)
	if err != nil {
		t.Fatal(err)
	}
	if p.Overview.Complete || *metricValue(t, p.Overview, "mp3Frames", "unresolvedBytes") != "4" || *p.Events["mp3"][0].Values[1].Value != "truncatedFrame" {
		t.Fatalf("incomplete framing lost: %+v", p)
	}
}
