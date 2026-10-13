package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestDetailV5SEIOptInAndLegacyVersionsStayFrozen(t *testing.T) {
	for _, truncated := range []bool{false, true} {
		nal := []byte{6, 6, 1, 0xc4, 0x80}
		if truncated {
			nal = []byte{6, 5, 16, 1, 2, 3}
		}
		path := filepath.Join(t.TempDir(), "sei.flv")
		if err := os.WriteFile(path, makeSingleTagFLV(9, cliV3Slice(nal...)), 0600); err != nil {
			t.Fatal(err)
		}
		for _, version := range []string{"1", "2", "3", "4", "5"} {
			var out, stderr bytes.Buffer
			code := run([]string{"detail", "--schema-version", version, "--avc-nal-length-size", "4", "--tag", "0", path}, &out, &stderr)
			want := exitOK
			if version == "5" && truncated {
				want = exitDiagnostics
			}
			if code != want {
				t.Fatalf("truncated=%t v%s code%d: %s", truncated, version, code, stderr.String())
			}
			if bytes.Contains(out.Bytes(), []byte(`"sei":`)) != (version == "5") {
				t.Fatalf("v%s SEI opt-in failed: %s", version, out.String())
			}
			if version == "5" && !truncated && !bytes.Contains(out.Bytes(), []byte(`"recoveryFrameCnt"`)) {
				t.Fatal("missing recovery declaration")
			}
		}
	}
}

func TestDetailV5RejectsNonAVC(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mp3.flv")
	if err := os.WriteFile(path, makeSingleTagFLV(8, []byte{0x2f, 0xff, 0xfb, 0x90, 0xc0}), 0600); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	if code := run([]string{"detail", "--schema-version", "5", "--tag", "0", path}, &out, &stderr); code != exitUsage || out.Len() != 0 {
		t.Fatalf("code%d: %s", code, stderr.String())
	}
}
