package mp3

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
)

func TestFramePagesBeyondMegabytePreserveIndicesAndHistory(t *testing.T) {
	f := mpegFrame(stereoHeader)
	f[4], f[5] = 0xff, 0x80 // main_data_begin=511 physical bytes.
	data := bytes.Repeat(f, 3000)
	var previous *FrameCursor
	var seen uint32
	for {
		p, err := ParseFramePage(t.Context(), bytes.NewReader(data), byteSpan(0, uint64(len(data))), previous)
		if err != nil {
			t.Fatal(err)
		}
		if p.StartIndex != seen || len(p.Sequence.Frames) > 64 || p.Sequence.BytesRead > FramePageReadBudget || p.Sequence.BytesCovered > MaxFrameScanBytes {
			t.Fatalf("page %+v", p)
		}
		for _, frame := range p.Sequence.Frames {
			if frame.Index != seen || frame.Span.ByteOffset != uint64(seen)*417 {
				t.Fatalf("wrong original identity: %+v", frame)
			}
			if seen >= 2 && (frame.Dependency.State != "within_tag" || frame.Dependency.KnownPreviousBytes != seen*381) {
				t.Fatalf("lost physical history at %d: %+v", seen, frame.Dependency)
			}
			seen++
		}
		if len(p.PriorFrames) > 64 {
			t.Fatal("unbounded history")
		}
		if p.Next == nil {
			if !p.Sequence.Complete || seen != 3000 {
				t.Fatalf("end count=%d complete=%t", seen, p.Sequence.Complete)
			}
			break
		}
		previous = p.Next
	}
}

func TestFramePagesStopAndCheckpointIsolation(t *testing.T) {
	f := mpegFrame(stereoHeader)
	prefix := bytes.Repeat(f, 64)
	for _, test := range []struct {
		name   string
		tail   []byte
		reason string
	}{
		{"free", []byte{0xff, 0xfb, 0, 0x64}, "free_format"}, {"invalid", []byte{0, 0, 0, 0}, "invalid_header"},
		{"header", []byte{0xff}, "header_truncated"}, {"body", f[:100], "body_truncated"},
	} {
		t.Run(test.name, func(t *testing.T) {
			data := append(append([]byte{}, prefix...), test.tail...)
			span := byteSpan(0, uint64(len(data)))
			first, err := ParseFramePage(t.Context(), bytes.NewReader(data), span, nil)
			if err != nil || first.Next == nil {
				t.Fatalf("first %v", err)
			}
			p, err := ParseFramePage(t.Context(), bytes.NewReader(data), span, first.Next)
			if err != nil || p.Next != nil || p.Sequence.StopReason != test.reason || p.StartIndex != 64 {
				t.Fatalf("stop %+v %v", p, err)
			}
			if first.Next.NextFrameIndex() != 64 {
				t.Fatal("parser mutated caller checkpoint")
			}
			if _, err := ParseFramePage(t.Context(), bytes.NewReader(data), byteSpan(1, uint64(len(data)-1)), first.Next); !errors.Is(err, ErrFramePageInput) {
				t.Fatalf("cross-source-span checkpoint %v", err)
			}
		})
	}
}

type pageFaultSource struct {
	data    []byte
	cancel  context.CancelFunc
	failure error
	calls   int
}

func (s *pageFaultSource) ReadAt(p []byte, off int64) (int, error) {
	s.calls++
	n, e := bytes.NewReader(s.data).ReadAt(p, off)
	if s.cancel != nil {
		s.cancel()
	}
	if s.failure != nil {
		e = s.failure
	}
	return n, e
}

func TestFramePageErrorsDiscardCheckpointAndBoundReads(t *testing.T) {
	data := bytes.Repeat(mpegFrame(stereoHeader), 65)
	for _, kind := range []string{"cancel", "source"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			s := &pageFaultSource{data: data}
			want := context.Canceled
			if kind == "cancel" {
				s.cancel = cancel
			} else {
				want = errors.New("source failure")
				s.failure = want
			}
			p, err := ParseFramePage(ctx, s, byteSpan(0, uint64(len(data))), nil)
			if !errors.Is(err, want) || p.Next != nil || p.Sequence.Frames != nil || s.calls != 1 {
				t.Fatalf("discard %+v %v calls%d", p, err, s.calls)
			}
		})
	}
	r := &pageReader{ctx: t.Context(), source: bytes.NewReader(data), end: uint64(len(data)), read: FramePageReadBudget - 3}
	if _, err := r.ReadAt(make([]byte, 4), 0); !errors.Is(err, ErrFramePageReadBudget) || r.read != FramePageReadBudget-3 {
		t.Fatalf("budget %v", err)
	}
	short := &pageFaultSource{data: data[:2], failure: io.ErrUnexpectedEOF}
	if p, err := ParseFramePage(t.Context(), short, byteSpan(0, uint64(len(data))), nil); err == nil || p.Next != nil {
		t.Fatalf("source tail %+v %v", p, err)
	}
}

func FuzzFramePages(f *testing.F) {
	f.Add(bytes.Repeat(mpegFrame(stereoHeader), 130))
	f.Add([]byte{0xff, 0xfb, 0, 0x64})
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 2<<20 {
			t.Skip()
		}
		var checkpoint *FrameCursor
		var expected uint32
		for round := 0; round < 4; round++ {
			p, err := ParseFramePage(t.Context(), bytes.NewReader(data), byteSpan(0, uint64(len(data))), checkpoint)
			if err != nil {
				t.Fatal(err)
			}
			if p.StartIndex != expected || len(p.Sequence.Frames) > int(FramePageSize) || len(p.PriorFrames) > FramePageHistorySize || p.Sequence.BytesRead > FramePageReadBudget || p.Sequence.BytesCovered > MaxFrameScanBytes {
				t.Fatalf("page budgets %+v", p)
			}
			for _, frame := range p.Sequence.Frames {
				start, end, err := frame.Span.ByteRange()
				if err != nil || end > uint64(len(data)) || start > end {
					t.Fatalf("frame span %+v", frame.Span)
				}
			}
			if p.Next == nil {
				break
			}
			if p.Next.NextFrameIndex() != expected+FramePageSize {
				t.Fatal("non-sequential cursor")
			}
			expected = p.Next.NextFrameIndex()
			checkpoint = p.Next
		}
	})
}
