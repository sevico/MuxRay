package flv

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/sevico/muxray/internal/mp3"
)

type mp3PageFaultSource struct {
	data    []byte
	offset  int64
	failure error
	cancel  context.CancelFunc
}

func (r mp3PageFaultSource) ReadAt(p []byte, offset int64) (int, error) {
	n, err := bytes.NewReader(r.data).ReadAt(p, offset)
	if offset == r.offset {
		if r.cancel != nil {
			r.cancel()
		}
		if r.failure != nil {
			err = r.failure
		}
	}
	return n, err
}

func TestMP3PageFLVPrefixChecksFullCountErrorAndCancellation(t *testing.T) {
	frame := make([]byte, 417)
	copy(frame, []byte{0xff, 0xfb, 0x90, 0x64})
	data, tag := scriptDataFixture(t, byte(TagTypeAudio), append([]byte{0x2f}, frame...))
	for _, offset := range []int64{int64(tag.PayloadSpan().ByteOffset), int64(tag.PayloadSpan().ByteOffset) + 1} {
		for _, cancelRead := range []bool{false, true} {
			ctx, cancel := context.WithCancel(t.Context())
			want := errors.New("full-count source failure")
			r := mp3PageFaultSource{data: data, offset: offset, failure: want}
			if cancelRead {
				r.failure = nil
				r.cancel = cancel
				want = context.Canceled
			}
			p, page, err := ParseMP3AudioPage(ctx, r, tag, nil)
			cancel()
			if !errors.Is(err, want) || p.Frames != nil || page.Next != nil {
				t.Fatalf("prefix offset%d cancel%t: %+v %v", offset, cancelRead, p, err)
			}
		}
	}
}

func TestMP3PageFLVReadCountAndBoundary(t *testing.T) {
	frame := make([]byte, 417)
	copy(frame, []byte{0xff, 0xfb, 0x90, 0x64})
	data, tag := scriptDataFixture(t, byte(TagTypeAudio), append([]byte{0x2f}, bytes.Repeat(frame, 65)...))
	reader := &mp3PageCountingSource{data: data, start: tag.PayloadSpan().ByteOffset, end: tag.PayloadSpan().ByteOffset + uint64(tag.DataSize)}
	_, first, err := ParseMP3AudioPage(t.Context(), reader, tag, nil)
	if err != nil || first.Next == nil || first.Sequence.BytesRead != reader.read || reader.read > mp3.FramePageReadBudget {
		t.Fatalf("page read %d %+v %v", reader.read, first, err)
	}
	reader.read = 0
	p, last, err := ParseMP3AudioPage(t.Context(), reader, tag, first.Next)
	if err != nil || last.Next != nil || !p.Complete || last.Sequence.BytesRead != reader.read || last.Sequence.Frames[0].Index != 64 {
		t.Fatalf("last %+v %v", last, err)
	}
}

type mp3PageCountingSource struct {
	data             []byte
	start, end, read uint64
}

func (r *mp3PageCountingSource) ReadAt(p []byte, offset int64) (int, error) {
	if offset < 0 || uint64(offset) < r.start || uint64(offset)+uint64(len(p)) > r.end {
		return 0, errors.New("crossed tag payload")
	}
	n, err := bytes.NewReader(r.data).ReadAt(p, offset)
	r.read += uint64(n)
	return n, err
}
