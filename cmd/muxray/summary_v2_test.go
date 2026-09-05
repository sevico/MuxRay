package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/sevico/muxray/internal/report"
)

func TestRunSummaryV2VersionIsolationAndExitCodes(t *testing.T) {
	for _, test := range []struct {
		name    string
		payload []byte
		exit    int
		code    string
	}{
		{"indexed", []byte{0x2f, 0xff, 0xfb, 0x90, 0x64}, exitOK, ""},
		{"free", []byte{0x2f, 0xff, 0xfb, 0x00, 0x64}, exitOK, "MP3_FREE_FORMAT_UNRESOLVED"},
		{"truncated", []byte{0x2f, 0xff}, exitDiagnostics, "MP3_FRAME_HEADER_TRUNCATED"},
		{"malformed", []byte{0x2f, 0, 0xfb, 0x90, 0x64}, exitDiagnostics, "MP3_SYNC_INVALID"},
		{"reserved format", []byte{0xef, 0xff, 0xe3, 0x08, 0xc4}, exitDiagnostics, "MEDIA_AUDIO_CODEC_UNSUPPORTED"},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "MP3 汇总 😀.flv")
			if err := os.WriteFile(path, makeSingleTagFLV(8, test.payload), 0o600); err != nil {
				t.Fatal(err)
			}
			var stdout, stderr, legacy, explicitLegacy bytes.Buffer
			if code := run([]string{"summary", path}, &legacy, &stderr); code != exitDiagnostics {
				t.Fatalf("default v1 MP3 exit changed: %d", code)
			}
			if code := run([]string{"summary", "--schema-version", "1", path}, &explicitLegacy, &stderr); code != exitDiagnostics || !bytes.Equal(legacy.Bytes(), explicitLegacy.Bytes()) {
				t.Fatalf("explicit v1 output differs: %d", code)
			}
			if bytes.Contains(legacy.Bytes(), []byte("mp3\":")) || !bytes.Contains(legacy.Bytes(), []byte("MEDIA_AUDIO_CODEC_UNSUPPORTED")) {
				t.Fatalf("v2 leaked into default v1: %s", legacy.String())
			}
			if code := run([]string{"summary", "--schema-version", "2", "--pretty", path}, &stdout, &stderr); code != test.exit {
				t.Fatalf("v2 exit=%d, expected=%d, stderr=%s", code, test.exit, stderr.String())
			}
			var decoded report.MediaSummaryDocumentV2
			if err := json.Unmarshal(stdout.Bytes(), &decoded); err != nil || decoded.SchemaVersion != 2 || decoded.DocumentType != "flv-media-summary" {
				t.Fatalf("invalid v2 output: %s, %v", stdout.String(), err)
			}
			if test.code != "" && !bytes.Contains(stdout.Bytes(), []byte(test.code)) {
				t.Fatalf("missing diagnostic %s: %s", test.code, stdout.String())
			}
			if bytes.Contains(stdout.Bytes(), []byte(path)) {
				t.Fatal("summary leaked local path")
			}
		})
	}
}

func TestRunSummaryV2UsageLimitsAndFailures(t *testing.T) {
	for _, flags := range [][]string{
		{"--schema-version", "0"}, {"--schema-version", "3"}, {"--schema-version", "-1"},
		{"--max-mp3-header-events", "0"}, {"--schema-version", "1", "--max-mp3-header-events", "1"},
		{"--schema-version", "2", "--max-mp3-header-events", "65537"},
		{"--schema-version", "2", "--max-mp3-header-events", "4294967296"},
	} {
		var stdout, stderr bytes.Buffer
		args := append(append([]string{"summary"}, flags...), "missing.flv")
		if exit := run(args, &stdout, &stderr); exit != exitUsage || stdout.Len() != 0 || bytes.Contains(stderr.Bytes(), []byte("open input")) {
			t.Fatalf("options not rejected before open: args=%v exit=%d stderr=%s", args, exit, stderr.String())
		}
	}
	path := filepath.Join(t.TempDir(), "sample.flv")
	data := makeSingleTagFLV(8, []byte{0x2f, 0xff, 0xfb, 0x90, 0x64})
	data = append(data, data[13:]...)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if exit := run([]string{"summary", "--schema-version", "2", "--max-mp3-header-events", "1", path}, &stdout, &stderr); exit != exitOK {
		t.Fatalf("retention-only v2 returned %d: %s", exit, stderr.String())
	}
	var document report.MediaSummaryDocumentV2
	if err := json.Unmarshal(stdout.Bytes(), &document); err != nil || document.Summary.MP3.Headers.EventCount != 2 ||
		len(document.Summary.MP3.Headers.Events) != 1 || !document.Summary.MP3.Headers.Truncated {
		t.Fatalf("retention flag ineffective: %s, %v", stdout.String(), err)
	}
	writer := &summaryV2FailWriter{err: errors.New("write failed")}
	if exit := run([]string{"summary", "--schema-version", "2", path}, writer, &stderr); exit != exitFailure || !bytes.Contains(stderr.Bytes(), []byte("write media summary")) {
		t.Fatalf("writer failure not propagated: %d, %s", exit, stderr.String())
	}
	if err := os.WriteFile(path, []byte("FLV"), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	if exit := run([]string{"summary", "--schema-version", "2", path}, &stdout, &stderr); exit != exitDiagnostics || !json.Valid(stdout.Bytes()) || !bytes.Contains(stdout.Bytes(), []byte("FLV_HEADER_TRUNCATED")) {
		t.Fatalf("partial scan v2 output: %d, %s", exit, stdout.String())
	}
}

type summaryV2FailWriter struct{ err error }

func (w *summaryV2FailWriter) Write([]byte) (int, error) { return 0, w.err }
