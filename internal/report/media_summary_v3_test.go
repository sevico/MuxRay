package report

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/sevico/muxray/internal/analysis"
	"github.com/sevico/muxray/internal/flv"
)

func summaryV3Frame(header [4]byte, size int) []byte {
	data := make([]byte, size)
	copy(data, header[:])
	return data
}
func summaryV3Fixture(name string) ([]byte, analysis.OptionsV3) {
	a := summaryV3Frame([4]byte{0xff, 0xfb, 0x90, 0x64}, 417)
	b := summaryV3Frame([4]byte{0xff, 0xfb, 0xa4, 0x64}, 480)
	c := summaryV3Frame([4]byte{0xff, 0xf3, 0x80, 0xc4}, 208)
	options := analysis.OptionsV3{}
	var tags []mediaSummaryFixtureTag
	switch name {
	case "flv-media-summary-v3.json":
		tags = []mediaSummaryFixtureTag{{8, 0, append([]byte{0x2f}, bytes.Join([][]byte{a, b, c}, nil)...)}, {8, 80, append([]byte{0x2f}, c...)}}
	case "flv-media-summary-partial-v3.json":
		tags = []mediaSummaryFixtureTag{
			{8, 0, append([]byte{0x2f}, append(a, 0xff)...)},
			{8, 26, []byte{0x2f, 0xff, 0xfb, 0x00, 0x64}},
			{8, 52, append([]byte{0x2f}, b[:20]...)},
			{8, 78, []byte{0x2f, 0xff, 0xfd, 0x90, 0x64}},
			{8, 104, []byte{0x2f, 0, 0, 0, 0}},
			{8, 130, append([]byte{0x2f}, c...)},
		}
	case "flv-media-summary-budget-v3.json":
		tags = []mediaSummaryFixtureTag{{8, 0, append([]byte{0x2f}, bytes.Join([][]byte{a, b, c}, nil)...)}, {8, 80, append([]byte{0x2f}, c...)}}
		options.MaxMP3Frames = 2
		options.MaxMP3FrameEvents = 1
	default:
		panic("unknown summary v3 fixture")
	}
	return mediaSummaryV2FixtureBytes(tags), options
}
func TestMediaSummaryV3Goldens(t *testing.T) {
	for _, name := range []string{"flv-media-summary-v3.json", "flv-media-summary-partial-v3.json", "flv-media-summary-budget-v3.json"} {
		t.Run(name, func(t *testing.T) {
			data, options := summaryV3Fixture(name)
			scan, err := flv.Scan(bytes.NewReader(data), uint64(len(data)), flv.Options{})
			if err != nil || scan.HasErrors() {
				t.Fatalf("fixture: %v", err)
			}
			result, err := analysis.AnalyzeV3(nil, bytes.NewReader(data), scan, options)
			if err != nil {
				t.Fatal(err)
			}
			doc := NewMediaSummaryV3(scan, result)
			if doc.SchemaVersion != 3 || doc.DocumentType != "flv-media-summary" {
				t.Fatal("v3 identity")
			}
			if !reflect.DeepEqual(doc.Summary.MediaSummaryV2, NewMediaSummaryV2(scan, result.ResultV2).Summary) {
				t.Fatal("legacy facts remapped")
			}
			s := doc.Summary.MP3Frames
			switch name {
			case "flv-media-summary-v3.json":
				if !s.Complete || s.CompleteFrameCount != 4 || s.FrameBytes != 1313 || s.ParameterChangeCount != 2 || s.Events[1].FrameIndex != 1 || s.Events[1].ParametersChangedFromPrevious == nil || !*s.Events[1].ParametersChangedFromPrevious {
					t.Fatalf("full: %+v", s)
				}
			case "flv-media-summary-partial-v3.json":
				if s.Complete || s.CompleteFrameCount != 2 || s.PartialTagCount != 5 || s.FreeFormatCount != 1 || s.TruncatedHeaderCount != 1 || s.TruncatedFrameCount != 1 || s.UnsupportedLayerCount != 1 || s.MalformedHeaderCount != 1 {
					t.Fatalf("partial: %+v", s)
				}
				if s.Events[2].Parameters == nil || s.Events[2].BitrateKbps != nil || s.Events[4].Parameters != nil || s.Events[6].ParametersChangedFromPrevious != nil {
					t.Fatal("unsupported facts leaked")
				}
			case "flv-media-summary-budget-v3.json":
				if s.Complete || s.CompleteFrameCount != 2 || s.BudgetStoppedTagCount != 2 || !s.EventsTruncated || len(s.Events) != 1 {
					t.Fatalf("budget: %+v", s)
				}
			}
			for _, pretty := range []bool{false, true} {
				var output, repeated, roundTrip bytes.Buffer
				if err := WriteMediaSummaryV3JSON(&output, doc, pretty); err != nil {
					t.Fatal(err)
				}
				if err := WriteMediaSummaryV3JSON(&repeated, NewMediaSummaryV3(scan, result), pretty); err != nil || !bytes.Equal(output.Bytes(), repeated.Bytes()) {
					t.Fatal("unstable output")
				}
				var decoded MediaSummaryDocumentV3
				if err := json.Unmarshal(output.Bytes(), &decoded); err != nil {
					t.Fatal(err)
				}
				if err := WriteMediaSummaryV3JSON(&roundTrip, decoded, pretty); err != nil || !bytes.Equal(output.Bytes(), roundTrip.Bytes()) {
					t.Fatal("typed roundtrip changed output")
				}
				if pretty {
					path := filepath.Join("..", "..", "testdata", "golden", name)
					if os.Getenv("MUXRAY_UPDATE_GOLDEN") == "1" {
						if err := os.WriteFile(path, output.Bytes(), 0644); err != nil {
							t.Fatal(err)
						}
					}
					golden, err := os.ReadFile(path)
					if err != nil {
						t.Fatal(err)
					}
					if !bytes.Equal(golden, output.Bytes()) {
						t.Fatalf("golden mismatch %s", name)
					}
				}
			}
		})
	}
}
func TestMediaSummaryV3EmptyAndWriteFailure(t *testing.T) {
	data := mediaSummaryV2FixtureBytes(nil)
	scan, err := flv.Scan(bytes.NewReader(data), uint64(len(data)), flv.Options{})
	if err != nil {
		t.Fatal(err)
	}
	result, err := analysis.AnalyzeV3(nil, bytes.NewReader(data), scan, analysis.OptionsV3{})
	if err != nil {
		t.Fatal(err)
	}
	doc := NewMediaSummaryV3(scan, result)
	if doc.Summary.MP3Frames.Events == nil || doc.Summary.MP3Frames.Rates == nil || doc.Summary.MP3Frames.DurationSeconds != (MediaMP3Duration{Denominator: 1}) {
		t.Fatal("empty summary")
	}
	want := errors.New("v3 write failed")
	if err := WriteMediaSummaryV3JSON(&failingWriter{remaining: 8, err: want}, doc, true); !errors.Is(err, want) {
		t.Fatal(err)
	}
}
