package document

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"slices"

	"github.com/sevico/muxray/internal/session"
)

const (
	DefaultHexBytes uint32 = 512
	MaxHexBytes     uint32 = 4096
)

// HexWindow contains an actual byte window of the retained source. EOF means
// the window reaches the original file end; an offset at EOF has no bytes.
type HexWindow struct {
	Offset     uint64
	SourceSize uint64
	Bytes      []byte
	EOF        bool
}

type HexResult struct {
	RequestID uint64
	Offset    uint64
	Count     uint32
	State     string
	Window    *HexWindow
	Err       error
}

// StartHex admits a bounded byte read after scanning reaches a terminal state.
// It may inspect the entire current file, including unindexed or damaged bytes.
// A zero count normalizes to DefaultHexBytes before idempotency comparison.
func (d *Document) StartHex(id, offset uint64, count uint32) (HexResult, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed || d.ctx.Err() != nil {
		return HexResult{}, session.ErrClosed
	}
	if id == 0 {
		return HexResult{}, ErrInvalidRequest
	}
	if id < d.request.id {
		return HexResult{}, ErrStaleRequest
	}
	if count == 0 {
		count = DefaultHexBytes
	}
	if id == d.request.id {
		if d.request.kind != hexWork || offset != d.request.offset || count != d.request.count {
			return HexResult{}, ErrInvalidRequest
		}
		return cloneHexResult(d.hex), nil
	}
	if count > MaxHexBytes || d.initial.Size() < 0 || offset > uint64(d.initial.Size()) || offset > math.MaxInt64 {
		return HexResult{}, ErrInvalidRequest
	}
	if !d.scan.Snapshot().State.Terminal() {
		return HexResult{}, session.ErrNotReady
	}
	d.admitLocked(work{id: id, kind: hexWork, offset: offset, count: count})
	return cloneHexResult(d.hex), nil
}

// GetHex only copies the latest bounded result. It never starts source I/O.
func (d *Document) GetHex(id uint64) (HexResult, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed || d.ctx.Err() != nil {
		return HexResult{}, session.ErrClosed
	}
	if id == 0 {
		return HexResult{}, ErrInvalidRequest
	}
	if id < d.request.id {
		return HexResult{}, ErrStaleRequest
	}
	if id != d.request.id {
		return HexResult{}, ErrUnknownRequest
	}
	if d.request.kind != hexWork {
		return HexResult{}, ErrInvalidRequest
	}
	return cloneHexResult(d.hex), nil
}

func cloneHexResult(result HexResult) HexResult {
	if result.Window != nil {
		window := *result.Window
		window.Bytes = slices.Clone(window.Bytes)
		result.Window = &window
	}
	return result
}

func (d *Document) readHex(ctx context.Context, offset uint64, count uint32) (*HexWindow, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := d.sourceUnchanged(); err != nil {
		return nil, err
	}
	size := uint64(d.initial.Size())
	length := min(uint64(count), size-offset)
	// Admission bounds offset by the original int64 file size. Clipping to its
	// remaining bytes also keeps the exclusive end and ReaderAt offset safe.
	window := &HexWindow{Offset: offset, SourceSize: size, Bytes: make([]byte, int(length)), EOF: length == size-offset}
	var readErr error
	if length != 0 {
		n, err := d.source.ReadAt(window.Bytes, int64(offset))
		switch {
		case n < 0 || n > len(window.Bytes):
			readErr = ErrInvalidRequest
		case n != len(window.Bytes):
			if err == nil || errors.Is(err, io.EOF) {
				err = io.ErrUnexpectedEOF
			}
			readErr = fmt.Errorf("document: read hex: %w", err)
		case err != nil && !errors.Is(err, io.EOF):
			readErr = fmt.Errorf("document: read hex: %w", err)
		}
	}
	if changed := d.sourceUnchanged(); changed != nil {
		return nil, changed
	}
	if canceled := ctx.Err(); canceled != nil {
		return nil, canceled
	}
	if readErr != nil {
		return nil, readErr
	}
	return window, nil
}
