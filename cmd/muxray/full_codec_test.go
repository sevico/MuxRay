package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/sevico/muxray/internal/report"
)

func TestSummaryV3AllFramesAndIndependentBudgets(t *testing.T) {
	first, second := make([]byte, 417), make([]byte, 522)
	copy(first, []byte{0xff, 0xfb, 0x90, 0xc0})
	copy(second, []byte{0xff, 0xfb, 0xa0, 0x00})
	path := filepath.Join(t.TempDir(), "frames.flv")
	if err := os.WriteFile(path, makeSingleTagFLV(8, append(append([]byte{0x2f}, first...), second...)), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name               string
		flags              []string
		frames, unresolved uint64
		exit               int
		truncated          bool
	}{
		{"all", nil, 2, 0, exitOK, false},
		{"retention", []string{"--max-mp3-frame-events", "1"}, 2, 0, exitOK, true},
		{"analysis budget", []string{"--max-mp3-frames", "1"}, 1, 522, exitDiagnostics, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out, stderr bytes.Buffer
			args := append([]string{"summary", "--schema-version", "3"}, tc.flags...)
			if code := run(append(args, path), &out, &stderr); code != tc.exit {
				t.Fatalf("exit %d: %s", code, stderr.String())
			}
			var doc report.MediaSummaryDocumentV3
			if err := json.Unmarshal(out.Bytes(), &doc); err != nil {
				t.Fatal(err)
			}
			f := doc.Summary.MP3Frames
			if doc.SchemaVersion != 3 || f.CompleteFrameCount != tc.frames || f.UnresolvedBytes != tc.unresolved || f.EventsTruncated != tc.truncated || f.SoundDataBytes != 939 || f.FrameBytes+f.UnresolvedBytes != 939 {
				t.Fatalf("frames %+v", f)
			}
			if tc.exit == exitOK && (f.ParameterChangeCount != 1 || f.BitrateChangeCount != 1 || f.DurationSeconds.Numerator != 64 || f.DurationSeconds.Denominator != 1225) {
				t.Fatalf("later frame missing: %+v", f)
			}
		})
	}
	for _, version := range []string{"1", "2"} {
		var out, stderr bytes.Buffer
		want := exitOK
		if version == "1" {
			want = exitDiagnostics
		} // v1 does not support MP3.
		if code := run([]string{"summary", "--schema-version", version, path}, &out, &stderr); code != want || bytes.Contains(out.Bytes(), []byte(`"mp3Frames"`)) {
			t.Fatalf("legacy v%s changed: %d %s", version, code, stderr.String())
		}
	}
}

func TestSummaryV3FlagsRejectedBeforeSourceOpen(t *testing.T) {
	for _, options := range [][]string{
		{"--max-mp3-frames", "1"}, {"--schema-version", "2", "--max-mp3-frame-events", "1"},
		{"--schema-version", "3", "--max-mp3-frames", "16000001"}, {"--schema-version", "3", "--max-mp3-frame-events", "65537"},
	} {
		var out, stderr bytes.Buffer
		if code := run(append(append([]string{"summary"}, options...), "absent.flv"), &out, &stderr); code != exitUsage || out.Len() != 0 || bytes.Contains(stderr.Bytes(), []byte("open input")) {
			t.Fatalf("%v: %d %s", options, code, stderr.String())
		}
	}
}

func TestDetailV4FullHeaderAndLegacyPrefixRemainDistinct(t *testing.T) {
	for _, full := range []bool{false, true} {
		nal := []byte{0x65, 0xb8, 0x40}
		if full {
			nal = append(nal, 0xa8)
		} // QP delta=0, disable_deblocking_filter_idc=1, trailing/body bits.
		data := appendFLVTag(makeSingleTagFLV(9, cliV3Config(t)), 9, cliV3Slice(nal...))
		path := filepath.Join(t.TempDir(), "slice.flv")
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		for _, version := range []string{"3", "4"} {
			var out, stderr bytes.Buffer
			want := exitOK
			if !full && version == "4" {
				want = exitDiagnostics
			}
			if code := run([]string{"detail", "--schema-version", version, "--tag", "1", path}, &out, &stderr); code != want {
				t.Fatalf("full=%v v%s exit %d: %s", full, version, code, stderr.String())
			}
			hasFull := bytes.Contains(out.Bytes(), []byte(`"sliceHeaderFull"`))
			if hasFull != (version == "4") || !bytes.Contains(out.Bytes(), []byte(`"prefixComplete":true`)) {
				t.Fatalf("prefix/full identity changed: %s", out.String())
			}
			if version == "4" && full && (!bytes.Contains(out.Bytes(), []byte(`"sliceQpDelta"`)) || !bytes.Contains(out.Bytes(), []byte(`"disableDeblockingFilterIdc"`))) {
				t.Fatal("missing header tail")
			}
		}
	}
}
