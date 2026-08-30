package report

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/sevico/muxray/internal/analysis"
	"github.com/sevico/muxray/internal/flv"
)

func TestMediaSummaryGoldenAndDeterminism(t *testing.T) {
	data := makeMediaSummaryFixture(t)
	scan, err := flv.Scan(bytes.NewReader(data), uint64(len(data)), flv.Options{})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	summary, err := analysis.Analyze(context.Background(), bytes.NewReader(data), scan, analysis.Options{})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if scan.HasErrors() || summary.HasErrors() {
		t.Fatalf("fixture did not produce a complete summary: scan=%+v summary=%+v", scan.Diagnostics, summary.Diagnostics)
	}
	document := NewMediaSummary(scan, summary)

	var output bytes.Buffer
	if err := WriteMediaSummaryJSON(&output, document, true); err != nil {
		t.Fatalf("WriteMediaSummaryJSON: %v", err)
	}
	goldenPath := filepath.Join("..", "..", "testdata", "golden", "flv-media-summary-v1.json")
	expected, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read Golden: %v", err)
	}
	if !bytes.Equal(output.Bytes(), expected) {
		t.Fatalf("Golden mismatch\n--- got ---\n%s\n--- want ---\n%s", output.Bytes(), expected)
	}

	for _, pretty := range []bool{false, true} {
		var first, second bytes.Buffer
		if err := WriteMediaSummaryJSON(&first, document, pretty); err != nil {
			t.Fatalf("first write (pretty=%v): %v", pretty, err)
		}
		if err := WriteMediaSummaryJSON(&second, NewMediaSummary(scan, summary), pretty); err != nil {
			t.Fatalf("second write (pretty=%v): %v", pretty, err)
		}
		if !bytes.Equal(first.Bytes(), second.Bytes()) {
			t.Fatalf("media summary changed for pretty=%v", pretty)
		}
	}
}

func TestWriteMediaSummaryJSONPropagatesFailure(t *testing.T) {
	want := errors.New("summary output failed")
	writer := &failingWriter{remaining: 8, err: want}
	if err := WriteMediaSummaryJSON(writer, MediaSummaryDocument{}, false); !errors.Is(err, want) {
		t.Fatalf("error = %v, want wrapped %v", err, want)
	}
}

func makeMediaSummaryFixture(t testing.TB) []byte {
	t.Helper()
	avcc := mustDecodeHex(t, "0164000cffe1001a6764000cacd941419f9f011000000300100000030320f142996001000668ebe3cb22c0fdf8f800")
	tags := []mediaSummaryFixtureTag{
		{tagType: byte(flv.TagTypeAudio), dts: 0, payload: []byte{0xaf, 0x00, 0x12, 0x10}},
		{tagType: byte(flv.TagTypeAudio), dts: 100, payload: []byte{0xaf, 0x01, 0xaa, 0xbb}},
		{tagType: byte(flv.TagTypeAudio), dts: 1100, payload: []byte{0xaf, 0x01, 0xcc}},
		{tagType: byte(flv.TagTypeVideo), dts: 0, payload: append([]byte{0x17, 0x00, 0, 0, 0}, avcc...)},
		{tagType: byte(flv.TagTypeVideo), dts: 120, payload: []byte{0x17, 0x01, 0xff, 0xff, 0xec, 0x00}},
		{tagType: byte(flv.TagTypeVideo), dts: 500, payload: []byte{0x27, 0x01, 0, 0, 0, 0x01}},
		{tagType: byte(flv.TagTypeVideo), dts: 1120, payload: []byte{0x17, 0x01, 0, 0, 30, 0x02}},
		{tagType: byte(flv.TagTypeScript), dts: 0, payload: []byte{0x05}},
	}
	data := []byte{'F', 'L', 'V', 1, 5, 0, 0, 0, 9, 0, 0, 0, 0}
	for _, tag := range tags {
		offset := len(data)
		data = append(data, make([]byte, flv.TagHeaderSize)...)
		header := data[offset : offset+int(flv.TagHeaderSize)]
		header[0] = tag.tagType
		size := uint32(len(tag.payload))
		header[1], header[2], header[3] = byte(size>>16), byte(size>>8), byte(size)
		header[4], header[5], header[6], header[7] = byte(tag.dts>>16), byte(tag.dts>>8), byte(tag.dts), byte(tag.dts>>24)
		data = append(data, tag.payload...)
		var previous [4]byte
		binary.BigEndian.PutUint32(previous[:], uint32(flv.TagHeaderSize)+size)
		data = append(data, previous[:]...)
	}
	return data
}

type mediaSummaryFixtureTag struct {
	tagType byte
	dts     uint32
	payload []byte
}
