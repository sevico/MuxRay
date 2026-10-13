package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestDetailV6TimingContextAndVersionIsolation(t *testing.T) {
	data := appendFLVTag(makeSingleTagFLV(9, cliV3Config(t)), 9, cliV3Slice(6, 0, 1, 0xc0, 1, 0, 0x80))
	path := filepath.Join(t.TempDir(), "timing.flv")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	for _, version := range []string{"1", "2", "3", "4", "5", "6"} {
		var out, stderr bytes.Buffer
		if code := run([]string{"detail", "--schema-version", version, "--tag", "1", path}, &out, &stderr); code != exitOK {
			t.Fatalf("v%s code %d: %s %s", version, code, stderr.String(), out.String())
		}
		var doc map[string]any
		if err := json.Unmarshal(out.Bytes(), &doc); err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(out.Bytes(), []byte(`"pictureTiming":`)) != (version == "6") {
			t.Fatalf("v%s timing opt-in: %s", version, out.String())
		}
		if version == "6" && (!bytes.Contains(out.Bytes(), []byte(`"state":"resolved"`)) || !bytes.Contains(out.Bytes(), []byte(`"spsSpan":`))) {
			t.Fatal("missing timing provenance")
		}
	}
}

func TestDetailV6MissingTimingContextAndNonAVC(t *testing.T) {
	for _, tc := range []struct {
		name string
		data []byte
		code int
	}{
		{"missing", makeSingleTagFLV(9, cliV3Slice(6, 1, 0, 0x80)), exitDiagnostics},
		{"mp3", makeSingleTagFLV(8, []byte{0x2f, 0xff, 0xfb, 0x90, 0xc0}), exitUsage},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "sample.flv")
			if err := os.WriteFile(path, tc.data, 0600); err != nil {
				t.Fatal(err)
			}
			var out, stderr bytes.Buffer
			if code := run([]string{"detail", "--schema-version", "6", "--avc-nal-length-size", "4", "--tag", "0", path}, &out, &stderr); code != tc.code {
				t.Fatalf("code %d: %s %s", code, stderr.String(), out.String())
			}
			if tc.name == "missing" && !bytes.Contains(out.Bytes(), []byte("AVC_SEI_TIMING_CONTEXT_MISSING")) {
				t.Fatal("missing explicit unknown context")
			}
			if tc.name == "mp3" && out.Len() != 0 {
				t.Fatal("unsupported codec produced v6 output")
			}
		})
	}
}
