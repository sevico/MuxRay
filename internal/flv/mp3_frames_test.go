package flv

import (
	"bytes"
	"errors"
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

func TestMP3SideInfoOptionNormalizesAndPropagatesSourceFailure(t *testing.T) {
	payload := make([]byte, 418)
	copy(payload, []byte{0x2f, 0xff, 0xfb, 0x90, 0})
	source, tag := scriptDataFixture(t, byte(TagTypeAudio), payload)
	parsed, err := ParseMP3Audio(bytes.NewReader(source), tag, mp3.Options{InspectSideInfo: true})
	if err != nil || !parsed.Complete || parsed.Frames == nil || parsed.Frames.Frames[0].SideInfo == nil ||
		!parsed.Frames.Frames[0].SideInfo.Complete || parsed.BitsConsumed != 40 {
		t.Fatalf("side-info-only option: %v %+v", err, parsed)
	}
	failure := errors.New("side-info source cancelled")
	reader := mp3SideFailureReader{source: source, offset: int64(tag.PayloadSpan().ByteOffset + 5), failure: failure}
	if _, err := ParseMP3Audio(reader, tag, mp3.Options{InspectSideInfo: true}); !errors.Is(err, failure) {
		t.Fatalf("source failure converted into syntax: %v", err)
	}
}

type mp3SideFailureReader struct {
	source  []byte
	offset  int64
	failure error
}

func (r mp3SideFailureReader) ReadAt(p []byte, offset int64) (int, error) {
	n, err := bytes.NewReader(r.source).ReadAt(p, offset)
	if offset == r.offset {
		return n, r.failure
	}
	return n, err
}
