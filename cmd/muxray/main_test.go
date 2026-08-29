package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sevico/muxray/internal/report"
)

func TestRunHelpVersionAndUsage(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantExit   int
		wantOutput string
	}{
		{name: "no args", wantExit: exitUsage, wantOutput: "Usage:"},
		{name: "help", args: []string{"help"}, wantExit: exitOK, wantOutput: "inspect"},
		{name: "version", args: []string{"version"}, wantExit: exitOK, wantOutput: "muxray dev"},
		{name: "unknown", args: []string{"wat"}, wantExit: exitUsage, wantOutput: "unknown command"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			got := run(test.args, &stdout, &stderr)
			if got != test.wantExit {
				t.Fatalf("exit = %d, want %d", got, test.wantExit)
			}
			combined := stdout.String() + stderr.String()
			if !strings.Contains(combined, test.wantOutput) {
				t.Fatalf("output %q does not contain %q", combined, test.wantOutput)
			}
		})
	}
}

func TestRunInspect(t *testing.T) {
	data := []byte{
		'F', 'L', 'V', 1, 0, 0, 0, 0, 9,
		0, 0, 0, 0,
	}
	path := filepath.Join(t.TempDir(), "含 空格😀.flv")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	var stdout, stderr bytes.Buffer
	if exit := run([]string{"inspect", "--pretty", path}, &stdout, &stderr); exit != exitOK {
		t.Fatalf("exit = %d, stderr=%s", exit, stderr.String())
	}
	var document report.Document
	if err := json.Unmarshal(stdout.Bytes(), &document); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, stdout.String())
	}
	if document.SchemaVersion != 1 || !document.Scan.Complete || document.Source.SizeBytes != uint64(len(data)) {
		t.Fatalf("unexpected document: %+v", document)
	}
	if strings.Contains(stdout.String(), path) {
		t.Fatal("report leaked the absolute input path")
	}
}

func TestRunInspectInvalidFLVStillWritesReport(t *testing.T) {
	path := filepath.Join(t.TempDir(), "broken.flv")
	if err := os.WriteFile(path, []byte("FLV"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	var stdout, stderr bytes.Buffer
	if exit := run([]string{"inspect", path}, &stdout, &stderr); exit != exitDiagnostics {
		t.Fatalf("exit = %d, want %d, stderr=%s", exit, exitDiagnostics, stderr.String())
	}
	var document report.Document
	if err := json.Unmarshal(stdout.Bytes(), &document); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(document.Diagnostics) != 1 || document.Diagnostics[0].Code != "FLV_HEADER_TRUNCATED" {
		t.Fatalf("unexpected diagnostics: %+v", document.Diagnostics)
	}
}

func TestRunInspectRejectsUnsafeLimitBeforeOpeningFile(t *testing.T) {
	for _, test := range []struct {
		args        []string
		wantMessage string
	}{
		{args: []string{"inspect", "--max-tags", "1000001", "does-not-exist.flv"}, wantMessage: "hard maximum"},
		{args: []string{"inspect", "--max-diagnostics", "1", "does-not-exist.flv"}, wantMessage: "minimum"},
	} {
		var stdout, stderr bytes.Buffer
		exit := run(test.args, &stdout, &stderr)
		if exit != exitUsage {
			t.Fatalf("args=%v exit = %d, want %d", test.args, exit, exitUsage)
		}
		if !strings.Contains(stderr.String(), test.wantMessage) {
			t.Fatalf("args=%v stderr = %q, want %q", test.args, stderr.String(), test.wantMessage)
		}
	}
}
