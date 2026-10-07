package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// These bytes test syntax boundaries, not compressed-picture decoding.
const cliV3ConfigHex = "0142001effe100086742001ef40a0fc801000468ce3c80"

func cliV3Config(t *testing.T) []byte {
	t.Helper()
	data, err := hex.DecodeString(cliV3ConfigHex)
	if err != nil {
		t.Fatal(err)
	}
	return append([]byte{0x17, 0, 0, 0, 0}, data...)
}
func cliV3Slice(nal ...byte) []byte {
	return append([]byte{0x27, 1, 0, 0, 0, 0, 0, 0, byte(len(nal))}, nal...)
}

func TestDetailV3PPSAndSlicePrefixes(t *testing.T) {
	config := makeSingleTagFLV(9, cliV3Config(t))
	for _, tc := range []struct {
		name   string
		data   []byte
		tag    int
		code   int
		marker string
	}{
		{"configuration", config, 0, exitOK, `"numRefIdxL0DefaultActiveMinus1"`},
		{"slice", appendFLVTag(append([]byte{}, config...), 9, cliV3Slice(0x65, 0xb8, 0x40)), 1, exitOK, `"frameNum"`},
		{"missing", makeSingleTagFLV(9, cliV3Slice(0x65, 0xb8, 0x40)), 0, exitDiagnostics, `AVC_SLICE_PARAMETER_SET_MISSING`},
		{"truncated", appendFLVTag(append([]byte{}, config...), 9, cliV3Slice(0x65, 0xb8)), 1, exitDiagnostics, `AVC_RBSP_SYNTAX_TRUNCATED`},
		{"eos barrier", appendFLVTag(appendFLVTag(append([]byte{}, config...), 9, []byte{0x17, 2, 0, 0, 0}), 9, cliV3Slice(0x65, 0xb8, 0x40)), 2, exitDiagnostics, `AVC_SLICE_PARAMETER_SET_MISSING`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "source.flv")
			if err := os.WriteFile(path, tc.data, 0600); err != nil {
				t.Fatal(err)
			}
			var out, stderr bytes.Buffer
			args := []string{"detail", "--schema-version", "3", "--avc-nal-length-size", "4", "--tag", strconv.Itoa(tc.tag), path}
			if code := run(args, &out, &stderr); code != tc.code {
				t.Fatalf("code %d want %d: %s\n%s", code, tc.code, stderr.String(), out.String())
			}
			var doc map[string]any
			if err := json.Unmarshal(out.Bytes(), &doc); err != nil || doc["schemaVersion"] != float64(3) || !strings.Contains(out.String(), tc.marker) {
				t.Fatalf("missing marker %s: %v %s", tc.marker, err, out.String())
			}
			if strings.Contains(out.String(), path) {
				t.Fatal("absolute path in public detail")
			}
			if tc.name == "missing" && strings.Contains(out.String(), `"frameNum"`) {
				t.Fatal("guessed frame_num without PPS/SPS")
			}
			for _, version := range []string{"1", "2"} {
				var legacy, errout bytes.Buffer
				if code := run([]string{"detail", "--schema-version", version, "--avc-nal-length-size", "4", "--tag", strconv.Itoa(tc.tag), path}, &legacy, &errout); code != exitOK {
					t.Fatalf("legacy %s code %d: %s", version, code, errout.String())
				}
				for _, key := range []string{`"prefix"`, `"sliceHeaderPrefix"`, `"parameterContext"`} {
					if strings.Contains(legacy.String(), key) {
						t.Fatalf("v3 %s leaked into v%s", key, version)
					}
				}
			}
		})
	}
}

func TestDetailV3AutomaticParameterReplay(t *testing.T) {
	data := appendFLVTag(makeSingleTagFLV(9, cliV3Config(t)), 9, cliV3Slice(0x65, 0xb8, 0x40))
	path := filepath.Join(t.TempDir(), "replay.flv")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	var output, stderr bytes.Buffer
	if code := run([]string{"detail", "--schema-version", "3", "--tag", "1", path}, &output, &stderr); code != exitOK {
		t.Fatalf("automatic replay %d %s", code, stderr.String())
	}
	var doc struct {
		Detail struct {
			ParameterContext struct {
				State                                           string
				ScannedTags, Configurations, SPSCount, PPSCount uint64
			}
		}
	}
	if err := json.Unmarshal(output.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	p := doc.Detail.ParameterContext
	if p.State != "found" || p.ScannedTags != 1 || p.Configurations != 1 || p.SPSCount != 1 || p.PPSCount != 1 {
		t.Fatalf("context %+v", p)
	}
}

func TestDetailV3UnsupportedCodecOrVersionHasNoJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audio.flv")
	if err := os.WriteFile(path, makeSingleTagFLV(8, []byte{0xaf, 0, 0x12, 0x10}), 0600); err != nil {
		t.Fatal(err)
	}
	for _, version := range []string{"3", "4"} {
		var out, stderr bytes.Buffer
		if code := run([]string{"detail", "--schema-version", version, "--tag", "0", path}, &out, &stderr); code != exitUsage || out.Len() != 0 {
			t.Fatalf("version %s code %d: %s", version, code, stderr.String())
		}
		if version == "3" && !strings.Contains(stderr.String(), "only AVC") {
			t.Fatal("unsupported kind did not explain scope")
		}
	}
}
