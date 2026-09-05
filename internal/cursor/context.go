package cursor

import (
	"context"
	"io"
)

// WithContext checks cancellation around each read. It cannot interrupt a
// blocked underlying ReaderAt. A read interrupted by cancellation reports no
// usable bytes, even if the source filled the buffer before cancellation.
func WithContext(ctx context.Context, source io.ReaderAt) io.ReaderAt {
	if ctx == nil {
		ctx = context.Background()
	}
	return contextReaderAt{ctx: ctx, source: source}
}

type contextReaderAt struct {
	ctx    context.Context
	source io.ReaderAt
}

func (r contextReaderAt) ReadAt(p []byte, offset int64) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	if r.source == nil {
		return 0, ErrNilReader
	}
	n, err := r.source.ReadAt(p, offset)
	if canceled := r.ctx.Err(); canceled != nil {
		return 0, canceled
	}
	return n, err
}
