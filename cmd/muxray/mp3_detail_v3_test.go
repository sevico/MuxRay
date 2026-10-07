package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Structural fixed frames: the side information is meaningful, while the
// compressed body is zero-filled and is not a decoder conformance fixture.
func cliMP3V3Frame(t testing.TB, back bool) []byte {
	t.Helper()
	s := "005a5064055a50221b4c0c80ab5a0443698190156d40886d303202ade8110da6"
	if back {
		s = "645a5064055a56844e5c0c80ab5ad089cb8190156d5a1139703202adeb42272e"
	}
	side, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	frame := make([]byte, 417)
	copy(frame, []byte{0xff, 0xfb, 0x90, 0})
	copy(frame[4:], side)
	return frame
}

func TestMP3DetailV3SyntaxExitCodesAndLegacyDefault(t *testing.T) {
	first, second := cliMP3V3Frame(t, false), cliMP3V3Frame(t, true)
	bad := append([]byte{}, first...)
	bad[8] = 0x90
	bad[9] |= 0x80 // first granule big_values=289 (>288).
	for _, tc := range []struct {
		name              string
		payload           []byte
		codeV2, codeV3    int
		state, diagnostic string
	}{
		{"two frames", append(append([]byte{0x2f}, first...), second...), exitOK, exitOK, "within_tag", ""},
		{"before tag", append([]byte{0x2f}, second...), exitOK, exitOK, "before_tag", ""},
		{"truncated", append([]byte{0x2f}, first[:13]...), exitDiagnostics, exitDiagnostics, "unknown", "MP3_SIDE_INFO_TRUNCATED"},
		{"invalid side info", append([]byte{0x2f}, bad...), exitOK, exitDiagnostics, "unknown", "MP3_SIDE_INFO_VALUE_INVALID"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "source.flv")
			if err := os.WriteFile(path, makeSingleTagFLV(8, tc.payload), 0600); err != nil {
				t.Fatal(err)
			}
			var def, explicit, stderr bytes.Buffer
			if code := run([]string{"detail", "--tag", "0", path}, &def, &stderr); code != exitOK {
				t.Fatalf("default v1 %d %s", code, stderr.String())
			}
			if code := run([]string{"detail", "--schema-version", "1", "--tag", "0", path}, &explicit, &stderr); code != exitOK || !bytes.Equal(def.Bytes(), explicit.Bytes()) {
				t.Fatal("default v1 changed")
			}
			for _, version := range []struct {
				text string
				exit int
			}{{"2", tc.codeV2}, {"3", tc.codeV3}} {
				var out, errout bytes.Buffer
				if code := run([]string{"detail", "--schema-version", version.text, "--tag", "0", path}, &out, &errout); code != version.exit {
					t.Fatalf("v%s code%d expected%d %s\n%s", version.text, code, version.exit, errout.String(), out.String())
				}
				var envelope map[string]any
				if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
					t.Fatal(err)
				}
				if strings.Contains(out.String(), path) {
					t.Fatal("absolute path leaked")
				}
				if version.text == "2" {
					if strings.Contains(out.String(), `"sideInfo"`) || strings.Contains(out.String(), `"dependency"`) || strings.Contains(out.String(), `MP3_SIDE_INFO_`) {
						t.Fatal("side-info data leaked into v2")
					}
				} else {
					if envelope["schemaVersion"] != float64(3) || !strings.Contains(out.String(), `"sideInfo"`) || !strings.Contains(out.String(), `"state":"`+tc.state+`"`) {
						t.Fatalf("v3 state missing %s", out.String())
					}
					if tc.diagnostic != "" && !strings.Contains(out.String(), tc.diagnostic) {
						t.Fatalf("diagnostic missing %s", out.String())
					}
				}
			}
		})
	}
}
