package report

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/sevico/muxray/internal/analysis"
	"github.com/sevico/muxray/internal/flv"
)

func tagPageFixture() []byte {
	data := []byte{'F', 'L', 'V', 1, 5, 0, 0, 0, 9, 0, 0, 0, 0,
		18, 0, 0, 3, 0, 0, 2, 1, 0, 0, 0, 0xaa, 0xbb, 0xcc, 0, 0, 0, 14}
	return append(data, data[13:]...)
}

func TestTagPageGoldens(t *testing.T) {
	for _, test := range []struct {
		name  string
		start uint64
		size  int
	}{
		{"flv-tag-page-v1.json", 1, 49},
		{"flv-tag-page-empty-v1.json", 2, 49},
		{"flv-tag-page-partial-v1.json", 1, 43},
	} {
		t.Run(test.name, func(t *testing.T) {
			data := tagPageFixture()[:test.size]
			scan, err := flv.ScanContext(nil, bytes.NewReader(data), uint64(len(data)), flv.Options{})
			if err != nil {
				t.Fatal(err)
			}
			document, err := NewTagPage(scan, test.start, 1)
			if err != nil {
				t.Fatal(err)
			}
			golden, err := os.ReadFile(filepath.Join("..", "..", "testdata", "golden", test.name))
			if err != nil {
				t.Fatal(err)
			}
			for _, pretty := range []bool{false, true} {
				var output bytes.Buffer
				if err := WriteTagPageJSON(&output, document, pretty); err != nil {
					t.Fatal(err)
				}
				want := golden
				if !pretty {
					var compact bytes.Buffer
					if err := json.Compact(&compact, golden); err != nil {
						t.Fatal(err)
					}
					want = append(compact.Bytes(), '\n')
				}
				if !bytes.Equal(output.Bytes(), want) {
					t.Fatalf("golden mismatch:\n%s", output.Bytes())
				}
				var decoded TagPageDocument
				if err := json.Unmarshal(output.Bytes(), &decoded); err != nil || !reflect.DeepEqual(document, decoded) {
					t.Fatalf("round trip: %v", err)
				}
			}
			if len(document.Tags) > 0 {
				if document.Tags[0].ID != 1 || document.Tags[0].Offset != 31 {
					t.Fatal("page-local IDs or spans")
				}
				document.Tags[0].ID = 999
				original, _ := scan.Index().Get(1)
				if original.ID != 1 {
					t.Fatal("report mutated index")
				}
			}
		})
	}
}

func TestTagPageErrors(t *testing.T) {
	if _, err := NewTagPage(flv.ScanResult{}, 0, 0); err == nil {
		t.Fatal("zero limit accepted")
	}
	if _, err := NewTagPage(flv.ScanResult{}, 1, 1); err == nil {
		t.Fatal("invalid start accepted")
	}
	want := errors.New("output failed")
	for _, pretty := range []bool{false, true} {
		if err := WriteTagPageJSON(&failingWriter{remaining: 8, err: want}, TagPageDocument{}, pretty); !errors.Is(err, want) {
			t.Fatal(err)
		}
	}
}

func TestChunkedReportAndAnalysisEquivalence(t *testing.T) {
	mixed := mediaSummaryV2Fixture("flv-media-summary-v2.json")
	large := append([]byte{}, mixed[:13]...)
	for range 128 {
		large = append(large, mixed[13:]...)
	}
	for _, data := range [][]byte{
		tagPageFixture()[:31], tagPageFixture()[:43], []byte("FLV"), mixed, large,
		mediaSummaryV2Fixture("flv-media-summary-mp3-free-v2.json"),
		mediaSummaryV2Fixture("flv-media-summary-mp3-malformed-v2.json"),
	} {
		legacy, err := flv.Scan(bytes.NewReader(data), uint64(len(data)), flv.Options{})
		if err != nil {
			t.Fatal(err)
		}
		chunked, err := flv.ScanContext(nil, bytes.NewReader(data), uint64(len(data)), flv.Options{})
		if err != nil || chunked.Tags != nil {
			t.Fatalf("chunked scan: %v", err)
		}
		if !reflect.DeepEqual(New(legacy), New(chunked)) {
			t.Fatal("materialized reports differ")
		}
		for _, pretty := range []bool{false, true} {
			var flat, blocks bytes.Buffer
			if err := WriteScanJSON(&flat, legacy, pretty); err != nil {
				t.Fatal(err)
			}
			if err := WriteScanJSON(&blocks, chunked, pretty); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(flat.Bytes(), blocks.Bytes()) {
				t.Fatal("streaming reports differ")
			}
		}
		for _, v2 := range []bool{false, true} {
			var documents []any
			for _, scan := range []flv.ScanResult{legacy, chunked} {
				if v2 {
					result, err := analysis.AnalyzeV2(context.Background(), bytes.NewReader(data), scan, analysis.OptionsV2{})
					if err != nil {
						t.Fatal(err)
					}
					documents = append(documents, NewMediaSummaryV2(scan, result))
				} else {
					result, err := analysis.Analyze(context.Background(), bytes.NewReader(data), scan, analysis.Options{})
					if err != nil {
						t.Fatal(err)
					}
					documents = append(documents, NewMediaSummary(scan, result))
				}
			}
			if !reflect.DeepEqual(documents[0], documents[1]) {
				t.Fatalf("summary v2=%v differs", v2)
			}
		}
	}
}
