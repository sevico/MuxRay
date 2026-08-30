package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunSummary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "媒体 摘要😀.flv")
	if err := os.WriteFile(path, makeSingleTagFLV(0x08, []byte{0xaf, 0x01, 0xaa}), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	var stdout, stderr bytes.Buffer
	if exit := run([]string{"summary", "--pretty", path}, &stdout, &stderr); exit != exitOK {
		t.Fatalf("exit = %d, stderr=%s", exit, stderr.String())
	}
	var document map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &document); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, stdout.String())
	}
	if document["schemaVersion"] != float64(1) || document["documentType"] != "flv-media-summary" {
		t.Fatalf("unexpected document identity: %+v", document)
	}
	summary := document["summary"].(map[string]any)
	audio := summary["audio"].(map[string]any)
	if summary["complete"] != true || audio["mediaTagCount"] != float64(1) {
		t.Fatalf("unexpected media summary: %+v", summary)
	}
	if strings.Contains(stdout.String(), path) {
		t.Fatal("summary report leaked the absolute input path")
	}
}

func TestRunSummaryPartialAndUnsupportedStillWriteJSON(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		code string
	}{
		{name: "broken container", data: []byte("FLV"), code: "FLV_HEADER_TRUNCATED"},
		{name: "unsupported audio", data: makeSingleTagFLV(0x08, []byte{0x2f, 0x01}), code: "MEDIA_AUDIO_CODEC_UNSUPPORTED"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "input.flv")
			if err := os.WriteFile(path, test.data, 0o600); err != nil {
				t.Fatalf("WriteFile: %v", err)
			}
			var stdout, stderr bytes.Buffer
			if exit := run([]string{"summary", path}, &stdout, &stderr); exit != exitDiagnostics {
				t.Fatalf("exit = %d, want %d; stderr=%s", exit, exitDiagnostics, stderr.String())
			}
			if !json.Valid(stdout.Bytes()) || !bytes.Contains(stdout.Bytes(), []byte(test.code)) {
				t.Fatalf("missing JSON diagnostic %q: %s", test.code, stdout.String())
			}
			var document map[string]any
			if err := json.Unmarshal(stdout.Bytes(), &document); err != nil {
				t.Fatalf("decode partial JSON: %v", err)
			}
			summary, ok := document["summary"].(map[string]any)
			if !ok || summary["complete"] != false {
				t.Fatalf("partial output claimed completeness: %+v", document)
			}
			if _, present := summary["avSync"]; present {
				t.Fatalf("partial output retained A/V sync: %+v", summary["avSync"])
			}
		})
	}
}

func TestRunSummaryRejectsUnsafeLimitsBeforeOpen(t *testing.T) {
	tests := [][]string{
		{"summary", "--max-tags", "1000001", "missing.flv"},
		{"summary", "--max-diagnostics", "1", "missing.flv"},
		{"summary", "--max-keyframes", "65537", "missing.flv"},
		{"summary", "--max-configuration-events", "65537", "missing.flv"},
		{"summary", "--max-configuration-bytes", "4294967297", "missing.flv"},
		{"summary", "--max-analysis-diagnostics", "1", "missing.flv"},
	}
	for _, args := range tests {
		var stdout, stderr bytes.Buffer
		if exit := run(args, &stdout, &stderr); exit != exitUsage {
			t.Fatalf("args=%v exit=%d, want %d; stderr=%s", args, exit, exitUsage, stderr.String())
		}
		if stdout.Len() != 0 {
			t.Fatalf("args=%v unexpectedly wrote stdout: %s", args, stdout.String())
		}
	}
}

func TestRunSummaryOperationalFailures(t *testing.T) {
	t.Run("open", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		exit := run([]string{"summary", filepath.Join(t.TempDir(), "missing.flv")}, &stdout, &stderr)
		if exit != exitFailure || stdout.Len() != 0 || !strings.Contains(stderr.String(), "open input") {
			t.Fatalf("exit=%d stdout=%q stderr=%q", exit, stdout.String(), stderr.String())
		}
	})

	t.Run("write", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "aac.flv")
		if err := os.WriteFile(path, makeSingleTagFLV(0x08, []byte{0xaf, 0x01}), 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		writerErr := errors.New("summary writer failed")
		var stderr bytes.Buffer
		exit := run([]string{"summary", path}, cliFailingWriter{err: writerErr}, &stderr)
		if exit != exitFailure || !strings.Contains(stderr.String(), "write media summary") {
			t.Fatalf("exit=%d stderr=%q", exit, stderr.String())
		}
	})
}
