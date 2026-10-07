package main

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDetailV2OptInAndLegacyCompatibility(t *testing.T) {
	config, err := hex.DecodeString("0164000cffe1001a6764000cacd941419f9f011000000300100000030320f142996001000668ebe3cb22c0fdf8f800")
	if err != nil {
		t.Fatal(err)
	}
	// Structural MPEG frames: their complete boundaries are known, while their
	// zero-filled compressed bodies are intentionally not decoder test data.
	frames := make([]byte, 2*417)
	copy(frames, []byte{0xff, 0xfb, 0x90, 0x00})
	copy(frames[417:], frames[:4])
	copy(frames[36:], "Info")
	binary.BigEndian.PutUint32(frames[40:], 3)
	binary.BigEndian.PutUint32(frames[44:], 1) // declaration excludes the Info frame
	binary.BigEndian.PutUint32(frames[48:], uint32(len(frames)))
	for _, tc := range []struct {
		name   string
		kind   byte
		data   []byte
		marker string
	}{
		{"avc", 9, append([]byte{0x17, 0, 0, 0, 0}, config...), `"vui"`},
		{"mp3", 8, append([]byte{0x2f}, frames...), `"Info"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), tc.name+".flv")
			if err := os.WriteFile(path, makeSingleTagFLV(tc.kind, tc.data), 0o600); err != nil {
				t.Fatal(err)
			}
			var original, explicit, deep, stderr bytes.Buffer
			if code := run([]string{"detail", "--tag", "0", path}, &original, &stderr); code != exitOK {
				t.Fatalf("v1 exit %d: %s", code, stderr.String())
			}
			if code := run([]string{"detail", "--schema-version", "1", "--tag", "0", path}, &explicit, &stderr); code != exitOK || !bytes.Equal(original.Bytes(), explicit.Bytes()) {
				t.Fatalf("explicit v1 changed output: exit %d %s", code, stderr.String())
			}
			if code := run([]string{"detail", "--schema-version", "2", "--tag", "0", path}, &deep, &stderr); code != exitOK {
				t.Fatalf("v2 exit %d: %s\n%s", code, stderr.String(), deep.String())
			}
			var value map[string]any
			if err := json.Unmarshal(deep.Bytes(), &value); err != nil || value["schemaVersion"] != float64(2) || !strings.Contains(deep.String(), tc.marker) {
				t.Fatalf("missing v2 syntax: %v %s", err, deep.String())
			}
			if strings.Contains(original.String(), tc.marker) || strings.Contains(deep.String(), path) {
				t.Fatal("deep data entered v1 or absolute path entered public report")
			}
		})
	}
}

func TestDetailV2TruncatedFrameAndUnsupportedVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "short-frame.flv")
	if err := os.WriteFile(path, makeSingleTagFLV(8, []byte{0x2f, 0xff, 0xfb, 0x90, 0}), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		version string
		code    int
	}{{"1", exitOK}, {"2", exitDiagnostics}, {"3", exitDiagnostics}, {"0", exitUsage}} {
		var stdout, stderr bytes.Buffer
		if code := run([]string{"detail", "--schema-version", tc.version, "--tag", "0", path}, &stdout, &stderr); code != tc.code {
			t.Fatalf("version %s: exit %d, want %d: %s", tc.version, code, tc.code, stderr.String())
		}
		if tc.code == exitUsage && stdout.Len() != 0 {
			t.Fatal("invalid version emitted a partial JSON document")
		}
	}
	if err := os.WriteFile(path, makeSingleTagFLV(8, []byte{0xaf, 0, 0x12, 0x10}), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"detail", "--schema-version", "2", "--tag", "0", path}, &stdout, &stderr); code != exitUsage || stdout.Len() != 0 || !strings.Contains(stderr.String(), "only AVC and MP3") {
		t.Fatalf("unsupported codec v2: exit %d %s %s", code, stdout.String(), stderr.String())
	}
}
