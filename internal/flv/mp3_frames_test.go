package flv

import (
	"bytes"
	"testing"

	"github.com/sevico/muxray/internal/mp3"
)

func TestMP3SameTagInspectionIsOptIn(t *testing.T) {
	// Keep this fixture self-contained: one FLV audio tag with a header-only
	// MP3 payload. Its MPEG header is valid, but its declared frame is cut off.
	payload := []byte{0x2f, 0xff, 0xfb, 0x90, 0x64}
	source, tag := scriptDataFixture(t, byte(TagTypeAudio), payload)
	legacy, err := ParseMP3Audio(bytes.NewReader(source), tag, mp3.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !legacy.Complete || legacy.Frames != nil || len(legacy.Diagnostics) != 0 || legacy.BitsConsumed != 40 {
		t.Fatalf("legacy prefix contract changed: %+v", legacy)
	}
	deep, err := ParseMP3Audio(bytes.NewReader(source), tag, mp3.Options{InspectFrames: true})
	if err != nil {
		t.Fatal(err)
	}
	if deep.Complete || deep.Frames == nil || deep.Frames.StopReason != "body_truncated" || deep.FrameHeader.Header != legacy.FrameHeader.Header || deep.BitsConsumed != legacy.BitsConsumed {
		t.Fatalf("opt-in deep inspection: %+v", deep)
	}
}
