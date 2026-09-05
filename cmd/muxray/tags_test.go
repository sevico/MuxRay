package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sevico/muxray/internal/analysis"
	"github.com/sevico/muxray/internal/cursor"
	"github.com/sevico/muxray/internal/flv"
	"github.com/sevico/muxray/internal/report"
)

func TestRunTags(t *testing.T) {
	data := makeSingleTagFLV(18, []byte{5})
	for range 1025 {
		data = appendFLVTag(data, 18, []byte{5})
	}
	path := filepath.Join(t.TempDir(), "paged tags.flv")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		args        []string
		start, next uint64
		length      int
		complete    bool
		code        int
	}{
		{nil, 0, 256, 256, true, exitOK},
		{[]string{"--pretty", "--start", "1023", "--count", "2"}, 1023, 1025, 2, true, exitOK},
		{[]string{"--start", "1025", "--count", "4096"}, 1025, 1026, 1, true, exitOK},
		{[]string{"--start", "1026"}, 1026, 1026, 0, true, exitOK},
		{[]string{"--max-tags", "1024", "--start", "1023", "--count", "2"}, 1023, 1024, 1, false, exitDiagnostics},
	} {
		args := append([]string{"tags"}, test.args...)
		args = append(args, path)
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != test.code {
			t.Fatalf("%v exit=%d: %s", args, code, stderr.String())
		}
		var doc report.TagPageDocument
		if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil {
			t.Fatal(err)
		}
		if doc.DocumentType != report.TagPageDocumentType || doc.SchemaVersion != 1 || doc.Scan.Complete != test.complete || doc.Page.Start != test.start || doc.Page.Next != test.next || len(doc.Tags) != test.length {
			t.Fatalf("wrong page: %+v", doc)
		}
		if test.complete && doc.Page.Total != 1026 {
			t.Fatal("wrong total")
		}
		for i, tag := range doc.Tags {
			id := test.start + uint64(i)
			if tag.ID != id || tag.Offset != 13+id*16 {
				t.Fatal("IDs/spans changed across page")
			}
		}
		if !test.complete && (len(doc.Diagnostics) == 0 || doc.Page.Total != 1024) {
			t.Fatal("missing partial scan status")
		}
		if strings.Contains(stdout.String(), path) {
			t.Fatal("page leaked path")
		}
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"tags", "--start", "1027", path}, &stdout, &stderr); code != exitUsage || stdout.Len() != 0 {
		t.Fatalf("out of range: exit=%d %s", code, stderr.String())
	}
}

func TestRunTagsValidationAndFailures(t *testing.T) {
	for _, args := range [][]string{
		{"tags"}, {"tags", "--unknown"}, {"tags", "--count", "0", "missing.flv"},
		{"tags", "--count", "4097", "missing.flv"}, {"tags", "--count", "18446744073709551615", "missing.flv"},
		{"tags", "--start", "18446744073709551615", "missing.flv"}, {"tags", "--max-tags", "1000001", "missing.flv"},
		{"tags", "--max-diagnostics", "1", "missing.flv"}, {"tags", "--max-diagnostics", "65537", "missing.flv"},
	} {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != exitUsage || stdout.Len() != 0 || strings.Contains(stderr.String(), "open input") {
			t.Fatalf("validation: %v code=%d %s", args, code, stderr.String())
		}
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"tags", "--help"}, &stdout, &stderr); code != exitOK {
		t.Fatal("help failed")
	}
	stderr.Reset()
	if code := run([]string{"tags", filepath.Join(t.TempDir(), "missing.flv")}, &stdout, &stderr); code != exitFailure {
		t.Fatal("open failure lost")
	}
	path := filepath.Join(t.TempDir(), "partial.flv")
	if err := os.WriteFile(path, []byte("FLV"), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"tags", path}, &stdout, &stderr); code != exitDiagnostics || !json.Valid(stdout.Bytes()) || !bytes.Contains(stdout.Bytes(), []byte("FLV_HEADER_TRUNCATED")) {
		t.Fatalf("partial report: %d %s %s", code, stdout.String(), stderr.String())
	}
	want := errors.New("output failed")
	if code := run([]string{"tags", path}, errorOutput{err: want}, io.Discard); code != exitFailure {
		t.Fatal("write failure lost")
	}
}

func TestRunContextCancellation(t *testing.T) {
	for _, args := range [][]string{{"inspect"}, {"tags"}, {"detail", "--tag", "0"}, {"summary"}, {"summary", "--schema-version", "2"}} {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		var stdout, stderr bytes.Buffer
		if code := runContext(ctx, append(args, "missing.flv"), &stdout, &stderr); code != exitCanceled || stdout.Len() != 0 || !strings.Contains(stderr.String(), "context canceled") || strings.Contains(stderr.String(), "open input") {
			t.Fatalf("pre-canceled: %v exit=%d %s", args, code, stderr.String())
		}
	}
	path := filepath.Join(t.TempDir(), "aac.flv")
	if err := os.WriteFile(path, makeSingleTagFLV(8, []byte{0xaf, 1, 0xaa}), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"inspect"}, {"tags"}, {"detail", "--tag", "0"}, {"summary"}, {"summary", "--schema-version", "2"}} {
		ctx, cancel := context.WithCancel(context.Background())
		writer := &cancelOutput{cancel: cancel}
		var stderr bytes.Buffer
		code := runContext(ctx, append(args, path), writer, &stderr)
		cancel()
		if code != exitCanceled || writer.calls != 1 || !strings.Contains(stderr.String(), "discard incomplete stdout") {
			t.Fatalf("write cancellation: %v exit=%d calls=%d %s", args, code, writer.calls, stderr.String())
		}
	}
	if code := runContext(nil, []string{"version"}, io.Discard, io.Discard); code != exitOK {
		t.Fatal("nil context failed")
	}
}

func TestAVCContextAcrossIndexBlocksAndCancellation(t *testing.T) {
	avcc := decodeTestHex(t, "0164000cffe1001a6764000cacd941419f9f011000000300100000030320f142996001000668ebe3cb22c0fdf8f800")
	data := makeSingleTagFLV(9, append([]byte{0x17, 0, 0, 0, 0}, avcc...))
	for range 1024 {
		data = appendFLVTag(data, 18, []byte{5})
	}
	data = appendFLVTag(data, 9, []byte{0x27, 1, 0, 0, 0, 0, 0, 0, 2, 0x65, 0x88})
	scan, err := flv.ScanContext(nil, bytes.NewReader(data), uint64(len(data)), flv.Options{})
	if err != nil {
		t.Fatal(err)
	}
	length, err := resolveAVCNALLengthSize(context.Background(), bytes.NewReader(data), scan.Index(), 1025, flv.AVCVideoOptions{})
	if err != nil || length != 4 {
		t.Fatalf("cross-block AVC context: %d %v", length, err)
	}
	path := filepath.Join(t.TempDir(), "cross-block-avc.flv")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"detail", "--tag", "1025", path}, &stdout, &stderr); code != exitOK || !bytes.Contains(stdout.Bytes(), []byte(`"lengthSize":4`)) {
		t.Fatalf("cross-block detail: exit=%d %s", code, stderr.String())
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reader := &cancelDetailReader{source: bytes.NewReader(data), cancel: cancel}
	// The selected prefix cancels. Even skipped ScriptData records must observe
	// cancellation during the backwards context search, without another read.
	if _, err := resolveAVCNALLengthSize(ctx, reader, scan.Index(), 1025, flv.AVCVideoOptions{}); !errors.Is(err, context.Canceled) || reader.calls != 1 {
		t.Fatalf("context search ignored cancellation: %v calls=%d", err, reader.calls)
	}
}

func TestDetailReadsHonorCancellation(t *testing.T) {
	for _, test := range []struct {
		kind    byte
		payload []byte
	}{
		{18, []byte{2, 0, 1, 'x', 5}}, {8, []byte{0xaf, 0, 0x12, 0x10}},
		{8, []byte{0x2f, 0xff, 0xfb, 0x90, 0x64}}, {9, []byte{0x27, 1, 0, 0, 0, 0, 0, 0, 2, 0x65, 0x88}},
	} {
		data := makeSingleTagFLV(test.kind, test.payload)
		scan, err := flv.ScanContext(nil, bytes.NewReader(data), uint64(len(data)), flv.Options{})
		if err != nil {
			t.Fatal(err)
		}
		tag, _ := scan.Index().Get(0)
		ctx, cancel := context.WithCancel(context.Background())
		reader := &cancelDetailReader{source: bytes.NewReader(data), cancel: cancel}
		_, err = flv.ParseTagDetail(cursor.WithContext(ctx, reader), tag, flv.DetailOptions{})
		cancel()
		if !errors.Is(err, context.Canceled) || reader.calls != 1 {
			t.Fatalf("detail kind=%d error=%v calls=%d", test.kind, err, reader.calls)
		}
	}
}

func TestSummaryReadsHonorCancellation(t *testing.T) {
	for _, payload := range [][]byte{{0xaf, 1, 0xaa}, {0x2f, 0xff, 0xfb, 0x90, 0x64}} {
		data := makeSingleTagFLV(8, payload)
		scan, err := flv.ScanContext(nil, bytes.NewReader(data), uint64(len(data)), flv.Options{})
		if err != nil {
			t.Fatal(err)
		}
		for _, v2 := range []bool{false, true} {
			ctx, cancel := context.WithCancel(context.Background())
			reader := &cancelDetailReader{source: bytes.NewReader(data), cancel: cancel}
			source := cursor.WithContext(ctx, reader)
			var complete bool
			if v2 {
				result, analyzeErr := analysis.AnalyzeV2(ctx, source, scan, analysis.OptionsV2{})
				err, complete = analyzeErr, result.Complete
			} else {
				result, analyzeErr := analysis.Analyze(ctx, source, scan, analysis.Options{})
				err, complete = analyzeErr, result.Complete
			}
			cancel()
			if !errors.Is(err, context.Canceled) || complete || reader.calls != 1 {
				t.Fatalf("summary v2=%v cancellation: %v calls=%d", v2, err, reader.calls)
			}
		}
	}
}

type cancelOutput struct {
	cancel context.CancelFunc
	calls  int
}

func (w *cancelOutput) Write(p []byte) (int, error) { w.calls++; w.cancel(); return len(p), nil }

type errorOutput struct{ err error }

func (w errorOutput) Write([]byte) (int, error) { return 0, w.err }

type cancelDetailReader struct {
	source io.ReaderAt
	cancel context.CancelFunc
	calls  int
}

func (r *cancelDetailReader) ReadAt(p []byte, offset int64) (int, error) {
	r.calls++
	n, err := r.source.ReadAt(p, offset)
	r.cancel()
	return n, err
}
