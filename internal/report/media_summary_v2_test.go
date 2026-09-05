package report

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/sevico/muxray/internal/analysis"
	"github.com/sevico/muxray/internal/flv"
)

func TestMediaSummaryV2Goldens(t *testing.T) {
	for _, name := range []string{"flv-media-summary-v2.json", "flv-media-summary-mp3-free-v2.json", "flv-media-summary-mp3-malformed-v2.json"} {
		t.Run(name, func(t *testing.T) {
			data := mediaSummaryV2Fixture(name)
			scan, err := flv.Scan(bytes.NewReader(data), uint64(len(data)), flv.Options{})
			if err != nil || scan.HasErrors() {
				t.Fatalf("invalid container fixture: %+v, %v", scan, err)
			}
			result, err := analysis.AnalyzeV2(context.Background(), bytes.NewReader(data), scan, analysis.OptionsV2{})
			if err != nil {
				t.Fatal(err)
			}
			document := NewMediaSummaryV2(scan, result)
			if document.SchemaVersion != 2 || document.DocumentType != "flv-media-summary" {
				t.Fatalf("wrong v2 identity: %+v", document)
			}
			headers := document.Summary.MP3.Headers
			switch name {
			case "flv-media-summary-v2.json":
				if result.HasErrors() || result.AudioCodecs.SwitchCount != 2 || headers.ParameterChangeCount != 1 || headers.BitrateChangeCount != 2 ||
					result.Audio.Bitrate.Bytes != 13 || result.Audio.Bitrate.CoveredIntervalMS != 78 || document.Summary.AVSync != nil {
					t.Fatalf("mixed fixture semantics: %+v", result)
				}
			case "flv-media-summary-mp3-free-v2.json":
				if result.HasErrors() || headers.FreeFormatCount != 2 || headers.Events[0].BitrateKbps != nil || headers.Events[0].Parameters == nil ||
					document.Summary.AVSync == nil || document.Summary.AVSync.EndExtremaSkewMS != 5 {
					t.Fatalf("free-format fixture semantics: %+v", document)
				}
			case "flv-media-summary-mp3-malformed-v2.json":
				if result.Complete || headers.MalformedCount != 2 || headers.ValidCount != 3 || headers.Events[3].ParametersChangedFromPrevious != nil ||
					result.MP3.Track.Bitrate.Bytes != 5 || result.MP3.Track.Bitrate.CoveredIntervalMS != 26 {
					t.Fatalf("malformed fixture semantics: %+v", result)
				}
				for _, index := range []int{1, 2} {
					e := headers.Events[index]
					if e.Complete || e.Parameters != nil || e.BitrateMode != "" || e.BitrateKbps != nil || e.ParametersChangedFromPrevious != nil || e.BitrateChangedFromPrevious != nil {
						t.Fatalf("malformed header leaked confident facts: %+v", e)
					}
				}
			}
			for _, pretty := range []bool{false, true} {
				var output, repeated, roundTrip bytes.Buffer
				if err := WriteMediaSummaryV2JSON(&output, document, pretty); err != nil {
					t.Fatal(err)
				}
				if err := WriteMediaSummaryV2JSON(&repeated, NewMediaSummaryV2(scan, result), pretty); err != nil || !bytes.Equal(output.Bytes(), repeated.Bytes()) {
					t.Fatalf("unstable v2 output: %v", err)
				}
				var decoded MediaSummaryDocumentV2
				if err := json.Unmarshal(output.Bytes(), &decoded); err != nil {
					t.Fatal(err)
				}
				if err := WriteMediaSummaryV2JSON(&roundTrip, decoded, pretty); err != nil || !bytes.Equal(output.Bytes(), roundTrip.Bytes()) {
					t.Fatalf("typed JSON round trip changed v2: %v", err)
				}
				if pretty {
					want, err := os.ReadFile(filepath.Join("..", "..", "testdata", "golden", name))
					if err != nil {
						t.Fatal(err)
					}
					if !bytes.Equal(want, output.Bytes()) {
						t.Fatalf("golden mismatch for %s\nactual:\n%s", name, output.Bytes())
					}
				}
			}
		})
	}
}

func TestMediaSummaryV2RetainsAACAVCAndEmptyArrays(t *testing.T) {
	for _, data := range [][]byte{makeMediaSummaryFixture(t), mediaSummaryV2FixtureBytes(nil)} {
		scan, err := flv.Scan(bytes.NewReader(data), uint64(len(data)), flv.Options{})
		if err != nil {
			t.Fatal(err)
		}
		v1, err := analysis.Analyze(nil, bytes.NewReader(data), scan, analysis.Options{})
		if err != nil {
			t.Fatal(err)
		}
		v2, err := analysis.AnalyzeV2(nil, bytes.NewReader(data), scan, analysis.OptionsV2{})
		if err != nil || !reflect.DeepEqual(v1, v2.Result) {
			t.Fatalf("unchanged AAC/AVC fixture changed in v2: %v", err)
		}
		document := NewMediaSummaryV2(scan, v2)
		if !reflect.DeepEqual(document.Summary.MediaSummary, NewMediaSummary(scan, v1).Summary) || document.Summary.MP3.Headers.Events == nil {
			t.Fatal("base summary mapping changed or MP3 empty events encoded as null")
		}
	}
}

func TestWriteMediaSummaryV2PropagatesFailure(t *testing.T) {
	want := errors.New("v2 summary output failed")
	if err := WriteMediaSummaryV2JSON(&failingWriter{remaining: 8, err: want}, MediaSummaryDocumentV2{}, true); !errors.Is(err, want) {
		t.Fatalf("writer failure: %v", err)
	}
}

func mediaSummaryV2Fixture(name string) []byte {
	var tags []mediaSummaryFixtureTag
	switch name {
	case "flv-media-summary-v2.json":
		tags = []mediaSummaryFixtureTag{
			{8, 0, []byte{0xaf, 0, 0x12, 0x10}}, {8, 0, []byte{0xaf, 1, 0xaa}},
			{8, 26, []byte{0x2f, 0xff, 0xfb, 0x90, 0x64}}, {8, 52, []byte{0x2f, 0xff, 0xfb, 0xa0, 0x64}},
			{8, 78, []byte{0x2f, 0xff, 0xf3, 0x80, 0xc4}}, {8, 104, []byte{0x2f, 0xff, 0xf3, 0x80, 0xc4}},
			{8, 130, []byte{0xaf, 0, 0x12, 0x10}}, {8, 130, []byte{0xaf, 1, 0xbb}}, {8, 156, []byte{0xaf, 1, 0xcc}},
			{9, 0, []byte{0x17, 1, 0, 0, 0}}, {9, 156, []byte{0x17, 1, 0, 0, 0}},
		}
	case "flv-media-summary-mp3-free-v2.json":
		tags = []mediaSummaryFixtureTag{
			{8, 100, []byte{0x2f, 0xff, 0xe3, 0x08, 0xc4}}, {8, 125, []byte{0x2f, 0xff, 0xe3, 0x08, 0xc4}},
			{9, 110, []byte{0x17, 1, 0xff, 0xff, 0xf6}}, {9, 130, []byte{0x27, 1, 0, 0, 0}},
		}
	case "flv-media-summary-mp3-malformed-v2.json":
		tags = []mediaSummaryFixtureTag{
			{8, 0, []byte{0x2f, 0xff, 0xfb, 0x90, 0x64}}, {8, 26, []byte{0x2f, 0xff}},
			{8, 52, []byte{0x2f, 0x00, 0xfb, 0x90, 0x64}}, {8, 78, []byte{0x2f, 0xff, 0xfb, 0x90, 0x64}},
			{8, 104, []byte{0x2f, 0xff, 0xfb, 0x90, 0x64}},
		}
	default:
		panic("unknown summary v2 fixture")
	}
	return mediaSummaryV2FixtureBytes(tags)
}

func mediaSummaryV2FixtureBytes(tags []mediaSummaryFixtureTag) []byte {
	data := []byte{'F', 'L', 'V', 1, 5, 0, 0, 0, 9, 0, 0, 0, 0}
	for _, tag := range tags {
		size := uint32(len(tag.payload))
		data = append(data, tag.tagType, byte(size>>16), byte(size>>8), byte(size), byte(tag.dts>>16), byte(tag.dts>>8), byte(tag.dts), byte(tag.dts>>24), 0, 0, 0)
		data = append(data, tag.payload...)
		var previous [4]byte
		binary.BigEndian.PutUint32(previous[:], uint32(flv.TagHeaderSize)+size)
		data = append(data, previous[:]...)
	}
	return data
}
