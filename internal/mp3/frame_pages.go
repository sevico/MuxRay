package mp3

import (
	"context"
	"errors"
	"io"

	"github.com/sevico/muxray/internal/cursor"
)

const FramePageSize uint32 = 64
const FramePageReadBudget uint64 = 64 << 10
const FramePageHistorySize = 64

var ErrFramePageInput = errors.New("mp3: invalid frame page cursor")
var ErrFramePageReadBudget = errors.New("mp3: frame page read budget exhausted")

// FrameCursor is an immutable, source-free checkpoint issued only after
// sequentially locating complete frames. Its owner must invalidate it whenever
// the source identity changes. It retains at most 64 compact physical slots.
type FrameCursor struct {
	payload cursor.Span
	offset  uint64
	index   uint32
	history mainDataHistory
	slots   []Frame
}

func (c *FrameCursor) NextFrameIndex() uint32 {
	if c == nil {
		return 0
	}
	return c.index
}

// FramePage retains original Tag-relative indices. Next is nil at end of Tag
// or an unresolved boundary. PriorFrames is a compact physical suffix, not
// decoded reservoir data; it may omit older frames even within the same Tag.
type FramePage struct {
	Sequence    FrameSequence
	StartIndex  uint32
	Next        *FrameCursor
	PriorFrames []Frame
}

// ParseFramePage advances at most 64 frames, with fresh 1 MiB coverage and
// 64 KiB actual-read budgets on each page. It never scans for a sync word or
// replays the prefix before the supplied checkpoint. Errors discard the page
// and checkpoint, including a cancellation discovered after a source read.
func ParseFramePage(ctx context.Context, source io.ReaderAt, payload cursor.Span, previous *FrameCursor) (FramePage, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return FramePage{}, err
	}
	start, end, err := payload.ByteRange()
	if err != nil || payload.BitOffset != 0 || payload.BitLength%8 != 0 || end-start > HardMaxPayloadBytes || source == nil {
		return FramePage{}, ErrFramePageInput
	}
	state := FrameCursor{payload: payload, offset: start, history: mainDataHistory{fromTagStart: true}}
	if previous != nil {
		if previous.payload != payload || previous.offset < start || previous.offset >= end || previous.index%FramePageSize != 0 {
			return FramePage{}, ErrFramePageInput
		}
		state = *previous
		state.slots = cloneSlots(previous.slots)
	}
	out := FramePage{StartIndex: state.index, PriorFrames: cloneSlots(state.slots)}
	r := &pageReader{ctx: ctx, source: source, start: start, end: end}
	out.Sequence, err = parseFrameSequence(r, payload, Options{InspectSideInfo: true, MaxFrames: FramePageSize, MaxDiagnostics: 64}, &state)
	if r.failure != nil {
		err = r.failure
	}
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		return FramePage{}, err
	}
	if out.Sequence.StopReason == "page_limit" && state.index%FramePageSize == 0 && state.offset < end {
		out.Next = &state
	}
	return out, nil
}

func (c *FrameCursor) retain(f Frame) {
	c.slots = append(c.slots, compactFrame(f))
	if len(c.slots) > FramePageHistorySize {
		c.slots = c.slots[len(c.slots)-FramePageHistorySize:]
	}
}

func cloneSlots(frames []Frame) []Frame {
	result := make([]Frame, len(frames))
	for i, f := range frames {
		result[i] = compactFrame(f)
	}
	return result
}

func compactFrame(f Frame) Frame {
	if f.SideInfo != nil {
		s := &SideInfo{Complete: f.SideInfo.Complete}
		if f.SideInfo.MainDataBegin != nil {
			v := *f.SideInfo.MainDataBegin
			s.MainDataBegin = &v
		}
		f.SideInfo = s
	}
	if f.Dependency != nil {
		d := *f.Dependency
		if d.Part23Bits != nil {
			v := *d.Part23Bits
			d.Part23Bits = &v
		}
		if d.AvailableBits != nil {
			v := *d.AvailableBits
			d.AvailableBits = &v
		}
		if d.MissingPreviousBytes != nil {
			v := *d.MissingPreviousBytes
			d.MissingPreviousBytes = &v
		}
		f.Dependency = &d
	}
	return f
}

type pageReader struct {
	ctx              context.Context
	source           io.ReaderAt
	start, end, read uint64
	failure          error
}

func (r *pageReader) ReadAt(p []byte, offset int64) (n int, err error) {
	if r.failure != nil {
		return 0, r.failure
	}
	defer func() {
		if err != nil {
			r.failure = err
		}
	}()
	if err = r.ctx.Err(); err != nil {
		return 0, err
	}
	if offset < 0 || uint64(offset) < r.start || uint64(offset) > r.end || uint64(len(p)) > r.end-uint64(offset) {
		return 0, ErrFramePageInput
	}
	if uint64(len(p)) > FramePageReadBudget-r.read {
		return 0, ErrFramePageReadBudget
	}
	n, err = r.source.ReadAt(p, offset)
	if n < 0 || n > len(p) {
		return 0, ErrFramePageInput
	}
	r.read += uint64(n)
	if canceled := r.ctx.Err(); canceled != nil {
		return n, canceled
	}
	if n == len(p) && errors.Is(err, io.EOF) {
		err = nil
	}
	if n != len(p) && err == nil {
		err = io.ErrUnexpectedEOF
	}
	return n, err
}
