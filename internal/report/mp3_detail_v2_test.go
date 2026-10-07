package report

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/sevico/muxray/internal/flv"
	"github.com/sevico/muxray/internal/mp3"
)

func TestMP3TagDetailV2Goldens(t *testing.T) {
	frame := make([]byte, 417)
	copy(frame, []byte{0xff, 0xfb, 0x90, 0x64})
	copy(frame[36:], "Xing")
	binary.BigEndian.PutUint32(frame[40:], 3)
	binary.BigEndian.PutUint32(frame[44:], 9)
	binary.BigEndian.PutUint32(frame[48:], 834)
	full := append(append([]byte{0x2f}, frame...), frame...)
	for _, tc := range []struct {
		name     string
		payload  []byte
		complete bool
	}{{"flv-mp3-detail-v2.json", full, true}, {"flv-mp3-detail-truncated-v2.json", full[:len(full)-17], false}} {
		t.Run(tc.name, func(t *testing.T) {
			data, tag, _ := parseMP3TagDetail(t, tc.payload)
			parsed, err := flv.ParseMP3Audio(bytes.NewReader(data), tag, mp3.Options{InspectFrames: true})
			if err != nil {
				t.Fatal(err)
			}
			document := NewMP3TagDetailV2(uint64(len(data)), tag, parsed)
			f := document.Detail.Frames
			if document.SchemaVersion != 2 || document.Detail.Complete != tc.complete || f == nil || f.Complete != tc.complete || len(f.Items) != 2 || f.VBR == nil || *f.VBR.DeclaredFrames != 9 || document.Detail.UnparsedDataSpan != nil {
				t.Fatalf("v2 projection: %+v", document)
			}
			if tc.complete && f.CompleteFrames != 2 || !tc.complete && (f.CompleteFrames != 1 || f.StopReason != "body_truncated") {
				t.Fatalf("frame boundaries: %+v", f)
			}
			var output bytes.Buffer
			if err := WriteParsedTagDetailJSON(&output, document, true); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join("..", "..", "testdata", "golden", tc.name)
			if os.Getenv("MUXRAY_UPDATE_GOLDEN") == "1" {
				if err := os.WriteFile(path, output.Bytes(), 0600); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(want, output.Bytes()) {
				t.Fatalf("v2 detail differs from %s", tc.name)
			}
		})
	}
}
