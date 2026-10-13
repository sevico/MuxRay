package flv

import (
	"context"
	"errors"
	"io"
	"math"

	"github.com/sevico/muxray/internal/mp3"
)

// ParseMP3AudioPage opts into bounded continuation while leaving the legacy
// first-frame and first-64 inspection contracts unchanged. The caller owns
// source identity and checkpoint lifetime.
func ParseMP3AudioPage(ctx context.Context, source io.ReaderAt, tag TagRecord, previous *mp3.FrameCursor) (MP3Audio, mp3.FramePage, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if source == nil || tag.DataSize > 0xffffff || tag.Offset > math.MaxInt64-TagHeaderSize-uint64(tag.DataSize) {
		return MP3Audio{}, mp3.FramePage{}, mp3.ErrFramePageInput
	}
	r := &mp3PageReader{ctx: ctx, source: source, start: tag.Offset + TagHeaderSize, end: tag.Offset + TagHeaderSize + uint64(tag.DataSize)}
	p, err := ParseMP3Audio(r, tag, mp3.Options{})
	if r.failure != nil {
		err = r.failure
	}
	if err != nil {
		return MP3Audio{}, mp3.FramePage{}, err
	}
	if !p.HeaderPresent {
		return p, mp3.FramePage{}, nil
	}
	page, err := mp3.ParseFramePage(ctx, r, p.DataSpan, previous)
	if r.failure != nil {
		err = r.failure
	}
	if err != nil {
		return MP3Audio{}, mp3.FramePage{}, err
	}
	page.Sequence.BytesRead = r.read
	p.Frames, p.Diagnostics, p.Complete = &page.Sequence, page.Sequence.Diagnostics, page.Sequence.Complete
	return p, page, nil
}

// The outer guard also covers the discriminator and legacy first header.
// It preserves full-count source failures that a shallow parser may tolerate.
type mp3PageReader struct {
	ctx              context.Context
	source           io.ReaderAt
	start, end, read uint64
	failure          error
}

func (r *mp3PageReader) ReadAt(p []byte, offset int64) (n int, err error) {
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
		return 0, mp3.ErrFramePageInput
	}
	if uint64(len(p)) > mp3.FramePageReadBudget-r.read {
		return 0, mp3.ErrFramePageReadBudget
	}
	n, err = r.source.ReadAt(p, offset)
	if n < 0 || n > len(p) {
		return 0, mp3.ErrFramePageInput
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
