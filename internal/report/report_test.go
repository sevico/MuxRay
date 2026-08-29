package report

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/sevico/muxray/internal/flv"
)

func TestGoldenBasicFLV(t *testing.T) {
	data := []byte{
		'F', 'L', 'V', 1, 5, 0, 0, 0, 9,
		0, 0, 0, 0,
		0x12, 0, 0, 3, 0, 0, 2, 1, 0, 0, 0,
		0xaa, 0xbb, 0xcc,
		0, 0, 0, 14,
	}
	result, err := flv.Scan(bytes.NewReader(data), uint64(len(data)), flv.Options{})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	var output bytes.Buffer
	if err := WriteScanJSON(&output, result, true); err != nil {
		t.Fatalf("WriteScanJSON: %v", err)
	}
	goldenPath := filepath.Join("..", "..", "testdata", "golden", "flv-basic-v1.json")
	expected, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read Golden: %v", err)
	}
	if !bytes.Equal(output.Bytes(), expected) {
		t.Fatalf("Golden mismatch\n--- got ---\n%s\n--- want ---\n%s", output.Bytes(), expected)
	}
	var materialized bytes.Buffer
	if err := WriteJSON(&materialized, New(result), true); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}
	if !bytes.Equal(materialized.Bytes(), output.Bytes()) {
		t.Fatalf("streaming and materialized encoders differ\n--- streaming ---\n%s\n--- materialized ---\n%s", output.Bytes(), materialized.Bytes())
	}

	for iteration := 0; iteration < 3; iteration++ {
		var repeated bytes.Buffer
		if err := WriteScanJSON(&repeated, result, true); err != nil {
			t.Fatalf("repeat WriteScanJSON: %v", err)
		}
		if !bytes.Equal(repeated.Bytes(), output.Bytes()) {
			t.Fatalf("output changed on iteration %d", iteration)
		}
	}

	var compactStreaming, compactMaterialized bytes.Buffer
	if err := WriteScanJSON(&compactStreaming, result, false); err != nil {
		t.Fatalf("compact WriteScanJSON: %v", err)
	}
	if err := WriteJSON(&compactMaterialized, New(result), false); err != nil {
		t.Fatalf("compact WriteJSON: %v", err)
	}
	if !bytes.Equal(compactStreaming.Bytes(), compactMaterialized.Bytes()) {
		t.Fatalf("compact encoders differ\nstreaming=%s\nmaterialized=%s", compactStreaming.Bytes(), compactMaterialized.Bytes())
	}
}

func TestStreamingMatchesMaterializedForDiagnosticReport(t *testing.T) {
	data := []byte("FLV")
	result, err := flv.Scan(bytes.NewReader(data), uint64(len(data)), flv.Options{})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	for _, pretty := range []bool{false, true} {
		var streaming, materialized bytes.Buffer
		if err := WriteScanJSON(&streaming, result, pretty); err != nil {
			t.Fatalf("WriteScanJSON(pretty=%v): %v", pretty, err)
		}
		if err := WriteJSON(&materialized, New(result), pretty); err != nil {
			t.Fatalf("WriteJSON(pretty=%v): %v", pretty, err)
		}
		if !bytes.Equal(streaming.Bytes(), materialized.Bytes()) {
			t.Fatalf("encoders differ for pretty=%v\nstreaming=%s\nmaterialized=%s", pretty, streaming.Bytes(), materialized.Bytes())
		}
	}
}

func TestWriteScanJSONPropagatesWriterFailure(t *testing.T) {
	want := errors.New("output failed")
	writer := &failingWriter{remaining: 8, err: want}
	if err := WriteScanJSON(writer, flv.ScanResult{}, false); !errors.Is(err, want) {
		t.Fatalf("WriteScanJSON error = %v, want wrapped %v", err, want)
	}
}

type failingWriter struct {
	remaining int
	err       error
}

func (w *failingWriter) Write(value []byte) (int, error) {
	if w.remaining <= 0 {
		return 0, w.err
	}
	if len(value) > w.remaining {
		written := w.remaining
		w.remaining = 0
		return written, w.err
	}
	w.remaining -= len(value)
	return len(value), nil
}

func BenchmarkWriteScanJSON(b *testing.B) {
	const tagCount = 10_000
	result := flv.ScanResult{
		SourceSize:    tagCount * 15,
		HeaderPresent: true,
		Complete:      true,
		Tags:          make([]flv.TagRecord, tagCount),
	}
	for index := range result.Tags {
		result.Tags[index] = flv.TagRecord{
			ID: uint64(index), Offset: 13 + uint64(index*15), RawTypeFlags: byte(flv.TagTypeScript),
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if err := WriteScanJSON(io.Discard, result, false); err != nil {
			b.Fatal(err)
		}
	}
}
