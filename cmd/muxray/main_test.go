package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"math"
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

func TestRunDetail(t *testing.T) {
	payload := make([]byte, 0, 64)
	payload = append(payload, 0x02, 0x00, 0x0a)
	payload = append(payload, "onMetaData"...)
	payload = append(payload, 0x08, 0x00, 0x00, 0x00, 0x01)
	payload = append(payload, 0x00, 0x08)
	payload = append(payload, "duration"...)
	payload = append(payload, 0x00)
	var number [8]byte
	binary.BigEndian.PutUint64(number[:], math.Float64bits(1.5))
	payload = append(payload, number[:]...)
	payload = append(payload, 0x00, 0x00, 0x09)

	path := filepath.Join(t.TempDir(), "脚本 详情😀.flv")
	if err := os.WriteFile(path, makeSingleTagFLV(0x12, payload), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	var stdout, stderr bytes.Buffer
	if exit := run([]string{"detail", "--pretty", "--tag", "0", path}, &stdout, &stderr); exit != exitOK {
		t.Fatalf("exit = %d, stderr=%s", exit, stderr.String())
	}
	var document map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &document); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, stdout.String())
	}
	if document["schemaVersion"] != float64(1) || document["documentType"] != "flv-tag-detail" {
		t.Fatalf("unexpected document identity: %+v", document)
	}
	detail, ok := document["detail"].(map[string]any)
	if !ok || detail["complete"] != true || detail["eventName"] != "onMetaData" {
		t.Fatalf("unexpected detail: %+v", document["detail"])
	}
	if strings.Contains(stdout.String(), path) {
		t.Fatal("detail report leaked the absolute input path")
	}
}

func TestRunDetailMalformedAMFStillWritesReport(t *testing.T) {
	path := filepath.Join(t.TempDir(), "broken-script.flv")
	if err := os.WriteFile(path, makeSingleTagFLV(0x12, []byte{0x7f, 0xaa}), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	var stdout, stderr bytes.Buffer
	if exit := run([]string{"detail", "--tag", "0", path}, &stdout, &stderr); exit != exitDiagnostics {
		t.Fatalf("exit = %d, want %d, stderr=%s", exit, exitDiagnostics, stderr.String())
	}
	var document map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &document); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, stdout.String())
	}
	diagnostics, ok := document["diagnostics"].([]any)
	if !ok || len(diagnostics) == 0 {
		t.Fatalf("missing detail diagnostics: %+v", document)
	}
	first := diagnostics[0].(map[string]any)
	if first["code"] != "AMF0_MARKER_UNKNOWN" {
		t.Fatalf("first diagnostic = %+v", first)
	}
}

func TestRunDetailRejectsSelectionAndUnsafeLimits(t *testing.T) {
	t.Run("non-script tag", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "audio.flv")
		if err := os.WriteFile(path, makeSingleTagFLV(0x08, []byte{0xaf, 0x01}), 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		var stdout, stderr bytes.Buffer
		if exit := run([]string{"detail", "--tag", "0", path}, &stdout, &stderr); exit != exitDiagnostics {
			t.Fatalf("exit = %d, want %d", exit, exitDiagnostics)
		}
		if !strings.Contains(stderr.String(), "not script data") {
			t.Fatalf("stderr = %q", stderr.String())
		}
	})

	for _, args := range [][]string{
		{"detail", "does-not-exist.flv"},
		{"detail", "--tag", "0", "--max-amf-depth", "257", "does-not-exist.flv"},
		{"detail", "--tag", "0", "--max-amf-diagnostics", "1", "does-not-exist.flv"},
	} {
		var stdout, stderr bytes.Buffer
		if exit := run(args, &stdout, &stderr); exit != exitUsage {
			t.Fatalf("args=%v exit=%d, want %d; stderr=%s", args, exit, exitUsage, stderr.String())
		}
	}
}

func TestRunDetailRequiresCompleteContainerIndex(t *testing.T) {
	scriptPayload := append([]byte{0x02, 0x00, 0x05}, []byte("event")...)
	scriptPayload = append(scriptPayload, 0x05)
	validFirstTag := makeSingleTagFLV(0x12, scriptPayload)

	tests := []struct {
		name string
		data []byte
		args []string
	}{
		{
			name: "tag limit after selected tag",
			data: appendFLVTag(validFirstTag, 0x08, []byte{0xaf, 0x01}),
			args: []string{"--max-tags", "1"},
		},
		{
			name: "truncated tag after selected tag",
			data: append(append([]byte(nil), validFirstTag...), 0x08, 0x00, 0x00),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "incomplete-index.flv")
			if err := os.WriteFile(path, test.data, 0o600); err != nil {
				t.Fatalf("WriteFile: %v", err)
			}
			args := append([]string{"detail", "--tag", "0"}, test.args...)
			args = append(args, path)
			var stdout, stderr bytes.Buffer
			if exit := run(args, &stdout, &stderr); exit != exitDiagnostics {
				t.Fatalf("exit = %d, want %d; stderr=%s", exit, exitDiagnostics, stderr.String())
			}
			if stdout.Len() != 0 || !strings.Contains(stderr.String(), "incomplete FLV index") {
				t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
			}
		})
	}
}

func makeSingleTagFLV(tagType byte, payload []byte) []byte {
	data := make([]byte, 13+11+len(payload)+4)
	copy(data, []byte{'F', 'L', 'V', 1, 0, 0, 0, 0, 9})
	tagOffset := 13
	data[tagOffset] = tagType
	data[tagOffset+1] = byte(len(payload) >> 16)
	data[tagOffset+2] = byte(len(payload) >> 8)
	data[tagOffset+3] = byte(len(payload))
	copy(data[tagOffset+11:], payload)
	binary.BigEndian.PutUint32(data[len(data)-4:], uint32(11+len(payload)))
	return data
}

func appendFLVTag(data []byte, tagType byte, payload []byte) []byte {
	tagOffset := len(data)
	data = append(data, make([]byte, 11)...)
	data[tagOffset] = tagType
	data[tagOffset+1] = byte(len(payload) >> 16)
	data[tagOffset+2] = byte(len(payload) >> 8)
	data[tagOffset+3] = byte(len(payload))
	data = append(data, payload...)
	var previous [4]byte
	binary.BigEndian.PutUint32(previous[:], uint32(11+len(payload)))
	return append(data, previous[:]...)
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
