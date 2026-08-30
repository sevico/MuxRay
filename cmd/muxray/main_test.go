package main

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sevico/muxray/internal/flv"
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
	t.Run("unsupported audio tag", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "speex.flv")
		if err := os.WriteFile(path, makeSingleTagFLV(0x08, []byte{0xbf, 0x01}), 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		var stdout, stderr bytes.Buffer
		if exit := run([]string{"detail", "--tag", "0", path}, &stdout, &stderr); exit != exitDiagnostics {
			t.Fatalf("exit = %d, want %d", exit, exitDiagnostics)
		}
		if stdout.Len() != 0 || !strings.Contains(stderr.String(), "not AAC") {
			t.Fatalf("stderr = %q", stderr.String())
		}
	})

	t.Run("non-AVC video tag", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "video.flv")
		if err := os.WriteFile(path, makeSingleTagFLV(0x09, []byte{0x12, 0x01}), 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		var stdout, stderr bytes.Buffer
		if exit := run([]string{"detail", "--tag", "0", path}, &stdout, &stderr); exit != exitDiagnostics {
			t.Fatalf("exit = %d, want %d", exit, exitDiagnostics)
		}
		if stdout.Len() != 0 || !strings.Contains(stderr.String(), "not AVC") {
			t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
		}
	})

	for _, args := range [][]string{
		{"detail", "does-not-exist.flv"},
		{"detail", "--tag", "0", "--max-amf-depth", "257", "does-not-exist.flv"},
		{"detail", "--tag", "0", "--max-amf-diagnostics", "1", "does-not-exist.flv"},
		{"detail", "--tag", "0", "--max-aac-diagnostics", "1", "does-not-exist.flv"},
		{"detail", "--tag", "0", "--max-mp3-diagnostics", "1", "does-not-exist.flv"},
		{"detail", "--tag", "0", "--max-avc-diagnostics", "1", "does-not-exist.flv"},
		{"detail", "--tag", "0", "--max-avc-parameter-sets", "542", "does-not-exist.flv"},
		{"detail", "--tag", "0", "--max-avc-nal-units", "16385", "does-not-exist.flv"},
		{"detail", "--tag", "0", "--max-avc-parameter-set-bytes", "1048577", "does-not-exist.flv"},
		{"detail", "--tag", "0", "--max-avc-rbsp-mapping-entries", "65537", "does-not-exist.flv"},
		{"detail", "--tag", "0", "--avc-nal-length-size", "3", "does-not-exist.flv"},
	} {
		var stdout, stderr bytes.Buffer
		if exit := run(args, &stdout, &stderr); exit != exitUsage {
			t.Fatalf("args=%v exit=%d, want %d; stderr=%s", args, exit, exitUsage, stderr.String())
		}
	}
}

func TestRunDetailAACSequenceHeader(t *testing.T) {
	path := filepath.Join(t.TempDir(), "AAC 璇︽儏馃幍.flv")
	if err := os.WriteFile(path, makeSingleTagFLV(0x08, []byte{0xaf, 0x00, 0x12, 0x10}), 0o600); err != nil {
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
	if document["schemaVersion"] != float64(1) || document["documentType"] != "flv-aac-tag-detail" {
		t.Fatalf("unexpected document identity: %+v", document)
	}
	detail := document["detail"].(map[string]any)
	if detail["kind"] != "aacAudio" || detail["complete"] != true {
		t.Fatalf("unexpected AAC detail: %+v", detail)
	}
	config := detail["audioSpecificConfig"].(map[string]any)
	frequency := config["samplingFrequency"].(map[string]any)
	channel := config["channelConfiguration"].(map[string]any)
	if frequency["hertz"] != float64(44100) || channel["channelCount"] != float64(2) {
		t.Fatalf("unexpected ASC summary: frequency=%+v channel=%+v", frequency, channel)
	}
	if strings.Contains(stdout.String(), path) {
		t.Fatal("AAC detail report leaked the absolute input path")
	}
}

func TestRunDetailAACRawAndMalformed(t *testing.T) {
	tests := []struct {
		name      string
		payload   []byte
		wantExit  int
		wantField string
		wantCode  string
	}{
		{name: "raw", payload: []byte{0xaf, 0x01, 0xde, 0xad}, wantExit: exitOK, wantField: "rawDataSpan"},
		{name: "truncated ASC", payload: []byte{0xaf, 0x00, 0x12}, wantExit: exitDiagnostics, wantField: "audioSpecificConfig", wantCode: "AAC_SAMPLING_FREQUENCY_INDEX_TRUNCATED"},
		{name: "missing packet type", payload: []byte{0xaf}, wantExit: exitDiagnostics, wantCode: "AAC_FLV_PACKET_TYPE_TRUNCATED"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "aac.flv")
			if err := os.WriteFile(path, makeSingleTagFLV(0x08, test.payload), 0o600); err != nil {
				t.Fatalf("WriteFile: %v", err)
			}
			var stdout, stderr bytes.Buffer
			if exit := run([]string{"detail", "--tag", "0", path}, &stdout, &stderr); exit != test.wantExit {
				t.Fatalf("exit = %d, want %d; stderr=%s", exit, test.wantExit, stderr.String())
			}
			var document map[string]any
			if err := json.Unmarshal(stdout.Bytes(), &document); err != nil {
				t.Fatalf("invalid JSON: %v\n%s", err, stdout.String())
			}
			detail := document["detail"].(map[string]any)
			if test.wantField != "" {
				if _, ok := detail[test.wantField]; !ok {
					t.Fatalf("detail lacks %q: %+v", test.wantField, detail)
				}
			}
			if test.wantCode != "" {
				diagnostics := document["diagnostics"].([]any)
				found := false
				for _, item := range diagnostics {
					if item.(map[string]any)["code"] == test.wantCode {
						found = true
					}
				}
				if !found || detail["complete"] != false {
					t.Fatalf("missing fatal %q or complete state: %+v", test.wantCode, document)
				}
			}
		})
	}
}

func TestRunDetailMP3ValidFreeAndMalformed(t *testing.T) {
	tests := []struct {
		name        string
		payload     []byte
		wantExit    int
		wantCode    string
		wantMode    string
		wantBitrate float64
	}{
		{name: "indexed", payload: []byte{0x2f, 0xff, 0xfb, 0x90, 0x64}, wantExit: exitOK, wantMode: "indexed", wantBitrate: 128},
		{name: "free format", payload: []byte{0x2f, 0xff, 0xfb, 0x00, 0x64}, wantExit: exitOK, wantMode: "free", wantCode: "MP3_FREE_FORMAT_UNRESOLVED"},
		{name: "truncated header", payload: []byte{0x2f, 0xff, 0xfb}, wantExit: exitDiagnostics, wantCode: "MP3_FRAME_HEADER_TRUNCATED"},
		{name: "invalid sync", payload: []byte{0x2f, 0xfe, 0xfb, 0x90, 0x64}, wantExit: exitDiagnostics, wantCode: "MP3_SYNC_INVALID"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "mp3.flv")
			if err := os.WriteFile(path, makeSingleTagFLV(0x08, test.payload), 0o600); err != nil {
				t.Fatalf("WriteFile: %v", err)
			}
			var stdout, stderr bytes.Buffer
			if exit := run([]string{"detail", "--pretty", "--tag", "0", path}, &stdout, &stderr); exit != test.wantExit {
				t.Fatalf("exit = %d, want %d; stderr=%s", exit, test.wantExit, stderr.String())
			}
			var document map[string]any
			if err := json.Unmarshal(stdout.Bytes(), &document); err != nil {
				t.Fatalf("invalid JSON: %v\n%s", err, stdout.String())
			}
			if document["documentType"] != "flv-mp3-tag-detail" {
				t.Fatalf("unexpected document identity: %+v", document)
			}
			detail := document["detail"].(map[string]any)
			if detail["kind"] != "mp3Audio" || detail["complete"] != (test.wantExit == exitOK) {
				t.Fatalf("unexpected MP3 completion: %+v", detail)
			}
			if test.wantMode != "" {
				header := detail["firstFrameHeader"].(map[string]any)
				if header["bitrateMode"] != test.wantMode {
					t.Fatalf("unexpected bitrate mode: %+v", header)
				}
				if test.wantBitrate != 0 && header["bitrateKbps"] != test.wantBitrate {
					t.Fatalf("unexpected bitrate: %+v", header)
				}
			}
			if test.wantCode != "" {
				found := false
				for _, item := range document["diagnostics"].([]any) {
					if item.(map[string]any)["code"] == test.wantCode {
						found = true
					}
				}
				if !found {
					t.Fatalf("missing diagnostic %q: %+v", test.wantCode, document)
				}
			}
			if strings.Contains(stdout.String(), path) {
				t.Fatal("MP3 detail report leaked the input path")
			}
		})
	}
}

func TestRunDetailAVCSequenceHeader(t *testing.T) {
	avcc := decodeTestHex(t, "0164000cffe1001a6764000cacd941419f9f011000000300100000030320f142996001000668ebe3cb22c0fdf8f800")
	payload := append([]byte{0x17, 0x00, 0, 0, 0}, avcc...)
	path := filepath.Join(t.TempDir(), "AVC sequence header.flv")
	if err := os.WriteFile(path, makeSingleTagFLV(0x09, payload), 0o600); err != nil {
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
	if document["documentType"] != "flv-avc-tag-detail" {
		t.Fatalf("unexpected document identity: %+v", document)
	}
	detail := document["detail"].(map[string]any)
	config := detail["avcDecoderConfigurationRecord"].(map[string]any)
	sps := config["sequenceParameterSets"].([]any)[0].(map[string]any)["sequenceParameterSet"].(map[string]any)
	if detail["complete"] != true || config["lengthSize"] != float64(4) || sps["displayWidth"] != float64(320) || sps["displayHeight"] != float64(180) {
		t.Fatalf("unexpected AVC detail: %+v", detail)
	}
	if strings.Contains(stdout.String(), path) {
		t.Fatal("AVC detail report leaked the absolute input path")
	}
}

func TestRunDetailAVCNALUExplicitAndAutoContext(t *testing.T) {
	avcc := decodeTestHex(t, "0164000cffe1001a6764000cacd941419f9f011000000300100000030320f142996001000668ebe3cb22c0fdf8f800")
	sequenceHeader := append([]byte{0x17, 0x00, 0, 0, 0}, avcc...)
	nalu := []byte{0x27, 0x01, 0xff, 0xff, 0xfe, 0, 0, 0, 2, 0x65, 0x88}

	for _, test := range []struct {
		name string
		data []byte
		tag  string
		args []string
	}{
		{name: "explicit", data: makeSingleTagFLV(0x09, nalu), tag: "0", args: []string{"--avc-nal-length-size", "4"}},
		{name: "automatic preceding sequence header", data: appendFLVTag(makeSingleTagFLV(0x09, sequenceHeader), 0x09, nalu), tag: "1"},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "avc-nalu.flv")
			if err := os.WriteFile(path, test.data, 0o600); err != nil {
				t.Fatalf("WriteFile: %v", err)
			}
			args := []string{"detail", "--tag", test.tag}
			args = append(args, test.args...)
			args = append(args, path)
			var stdout, stderr bytes.Buffer
			if exit := run(args, &stdout, &stderr); exit != exitOK {
				t.Fatalf("exit = %d, stderr=%s", exit, stderr.String())
			}
			var document map[string]any
			if err := json.Unmarshal(stdout.Bytes(), &document); err != nil {
				t.Fatalf("invalid JSON: %v", err)
			}
			detail := document["detail"].(map[string]any)
			sequence := detail["nalUnitSequence"].(map[string]any)
			if detail["complete"] != true || detail["compositionTime"].(map[string]any)["value"] != float64(-2) || sequence["lengthSize"] != float64(4) || len(sequence["nalUnits"].([]any)) != 1 {
				t.Fatalf("unexpected NALU detail: %+v", detail)
			}
		})
	}
}

func TestRunDetailAVCNALUWithoutUsableContext(t *testing.T) {
	nalu := []byte{0x27, 0x01, 0, 0, 0, 0, 0, 0, 0, 2, 0x65, 0x88}
	truncatedConfig := []byte{0x17, 0x00, 0, 0, 0, 1, 100, 0, 12, 0xff}
	for _, test := range []struct {
		name string
		data []byte
		tag  string
	}{
		{name: "no preceding sequence header", data: makeSingleTagFLV(0x09, nalu), tag: "0"},
		{name: "truncated preceding sequence header", data: appendFLVTag(makeSingleTagFLV(0x09, truncatedConfig), 0x09, nalu), tag: "1"},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "avc-no-context.flv")
			if err := os.WriteFile(path, test.data, 0o600); err != nil {
				t.Fatalf("WriteFile: %v", err)
			}
			var stdout, stderr bytes.Buffer
			if exit := run([]string{"detail", "--tag", test.tag, path}, &stdout, &stderr); exit != exitDiagnostics {
				t.Fatalf("exit = %d, want %d; stderr=%s", exit, exitDiagnostics, stderr.String())
			}
			var document map[string]any
			if err := json.Unmarshal(stdout.Bytes(), &document); err != nil {
				t.Fatalf("invalid JSON: %v", err)
			}
			detail := document["detail"].(map[string]any)
			if detail["complete"] != false {
				t.Fatalf("detail unexpectedly complete: %+v", detail)
			}
			if _, present := detail["nalUnitSequence"]; present {
				t.Fatalf("NAL boundaries were guessed: %+v", detail)
			}
			diagnostic := document["diagnostics"].([]any)[0].(map[string]any)
			if diagnostic["code"] != "AVC_NAL_LENGTH_SIZE_UNAVAILABLE" {
				t.Fatalf("unexpected diagnostic: %+v", diagnostic)
			}
		})
	}
}

func TestReadAVCFramingPrefixAcceptsFullReadWithEOF(t *testing.T) {
	data := makeSingleTagFLV(0x09, []byte{0x27, 0x01, 0, 0, 0})
	scan, err := flv.Scan(bytes.NewReader(data), uint64(len(data)), flv.Options{})
	if err != nil || len(scan.Tags) != 1 {
		t.Fatalf("Scan: err=%v tags=%d", err, len(scan.Tags))
	}
	header, packetType, present, err := readAVCFramingPrefix(fullReadEOFReaderAt{data: data}, scan.Tags[0])
	if err != nil || !present || header != 0x27 || packetType != 1 {
		t.Fatalf("prefix=(%#x,%d,%v), err=%v", header, packetType, present, err)
	}
}

func TestRunDetailOperationalFailuresExitOne(t *testing.T) {
	t.Run("open", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		exit := run([]string{"detail", "--tag", "0", filepath.Join(t.TempDir(), "missing.flv")}, &stdout, &stderr)
		if exit != exitFailure || stdout.Len() != 0 || !strings.Contains(stderr.String(), "open input") {
			t.Fatalf("exit=%d stdout=%q stderr=%q", exit, stdout.String(), stderr.String())
		}
	})

	t.Run("write", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "aac.flv")
		if err := os.WriteFile(path, makeSingleTagFLV(0x08, []byte{0xaf, 0x01}), 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		writeErr := errors.New("CLI detail writer failed")
		stdout := cliFailingWriter{err: writeErr}
		var stderr bytes.Buffer
		exit := run([]string{"detail", "--tag", "0", path}, stdout, &stderr)
		if exit != exitFailure || !strings.Contains(stderr.String(), "write detail report") {
			t.Fatalf("exit=%d stderr=%q", exit, stderr.String())
		}
	})
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

type cliFailingWriter struct{ err error }

func (w cliFailingWriter) Write([]byte) (int, error) { return 0, w.err }

type fullReadEOFReaderAt struct{ data []byte }

func (reader fullReadEOFReaderAt) ReadAt(output []byte, offset int64) (int, error) {
	if offset < 0 || offset >= int64(len(reader.data)) {
		return 0, io.EOF
	}
	n := copy(output, reader.data[offset:])
	return n, io.EOF
}

func decodeTestHex(t testing.TB, value string) []byte {
	t.Helper()
	decoded, err := hex.DecodeString(value)
	if err != nil {
		t.Fatalf("hex.DecodeString: %v", err)
	}
	return decoded
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
