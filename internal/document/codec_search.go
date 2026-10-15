package document

import (
	"context"

	"github.com/sevico/muxray/internal/cursor"
	"github.com/sevico/muxray/internal/mp3search"
	"github.com/sevico/muxray/internal/seisearch"
	"github.com/sevico/muxray/internal/session"
)

// Cursor tokens identify immutable, server-owned continuation states. They
// cannot be used to inject a guessed frame boundary or parameter context.
const MaxCodecSearchCursors = 8

type CodecSearchPosition struct {
	TagID      uint64
	FrameIndex uint32
}
type CodecSearchValue struct{ Name, Value string }
type CodecSearchRow struct {
	TagID, Offset                      uint64
	DTS                                int64
	FrameIndex, NALIndex, MessageIndex *uint32
	Kind, Status, ContextState         string
	Complete                           bool
	Span, SPSSpan, EvidenceSpan        *cursor.Span
	Values                             []CodecSearchValue
}
type CodecSearchPage struct {
	Kind, Filter               string
	Cursor, NextCursor, Total  uint64
	Start, Next                CodecSearchPosition
	ScannedTags, ScannedFrames uint32
	Pending                    uint32
	BytesRead                  uint64
	Exhausted, ScanComplete    bool
	Rows                       []CodecSearchRow
}
type CodecSearchResult struct {
	RequestID uint64
	State     string
	Page      *CodecSearchPage
	Err       error
}
type codecSearchCursor struct {
	id           uint64
	kind, filter string
	mp3          mp3search.State
	sei          seisearch.State
}

func ValidCodecSearch(kind, filter string) bool {
	switch kind {
	case "mp3":
		return mp3search.ValidFilter(filter)
	case "sei":
		return seisearch.ValidFilter(filter)
	default:
		return false
	}
}

func (d *Document) StartCodecSearch(id, token uint64, kind, filter string) (CodecSearchResult, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed || d.ctx.Err() != nil {
		return CodecSearchResult{}, session.ErrClosed
	}
	if id == 0 || !ValidCodecSearch(kind, filter) {
		return CodecSearchResult{}, ErrInvalidRequest
	}
	if d.changed {
		return CodecSearchResult{}, ErrSourceChanged
	}
	if id < d.request.id {
		return CodecSearchResult{}, ErrStaleRequest
	}
	if id == d.request.id {
		if d.request.kind != codecSearchWork || d.request.offset != token || d.request.codec != kind || d.request.filter != filter {
			return CodecSearchResult{}, ErrInvalidRequest
		}
		return cloneCodecSearch(d.codecSearch), nil
	}
	if !d.scan.Snapshot().State.Terminal() {
		return CodecSearchResult{}, session.ErrNotReady
	}
	seed := codecSearchCursor{kind: kind, filter: filter}
	if token != 0 {
		found := false
		for _, saved := range d.codecCursors {
			if saved.id == token && saved.kind == kind && saved.filter == filter {
				seed, found = saved, true
				break
			}
		}
		if !found {
			return CodecSearchResult{}, ErrUnknownCursor
		}
	}
	// Each work item owns its seed. A newer request can never replace the state
	// underneath an active reader, and canceled work cannot mutate saved state.
	seed.sei = seed.sei.Clone()
	d.admitLocked(work{id: id, kind: codecSearchWork, offset: token, codec: kind, filter: filter, codecSeed: &seed})
	return cloneCodecSearch(d.codecSearch), nil
}

func (d *Document) codecSearchIDLocked(id uint64) error {
	if d.closed || d.ctx.Err() != nil {
		return session.ErrClosed
	}
	if d.changed {
		return ErrSourceChanged
	}
	if id == 0 {
		return ErrInvalidRequest
	}
	if id == d.codecSearch.RequestID {
		return nil
	}
	if id < d.request.id {
		return ErrStaleRequest
	}
	if id == d.request.id {
		return ErrInvalidRequest
	}
	return ErrUnknownRequest
}
func (d *Document) GetCodecSearch(id uint64) (CodecSearchResult, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.codecSearchIDLocked(id); err != nil {
		return CodecSearchResult{}, err
	}
	return cloneCodecSearch(d.codecSearch), nil
}
func (d *Document) CancelCodecSearch(id uint64) (CodecSearchResult, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.codecSearchIDLocked(id); err != nil {
		return CodecSearchResult{}, err
	}
	d.cancelCodecSearchLocked()
	return cloneCodecSearch(d.codecSearch), nil
}
func (d *Document) cancelCodecSearchLocked() {
	if d.codecSearch.State != "pending" {
		return
	}
	d.codecSearch.Page = nil
	if d.active != nil && d.active.kind == codecSearchWork && d.active.id == d.codecSearch.RequestID {
		d.codecSearch.State = "canceling"
		if d.activeCancel != nil {
			d.activeCancel()
		}
		return
	}
	if d.pending != nil && d.pending.kind == codecSearchWork && d.pending.id == d.codecSearch.RequestID {
		d.pending = nil
	}
	d.codecSearch.State, d.codecSearch.Err = "failed", context.Canceled
}
func (d *Document) invalidateCodecSearchLocked(err error) {
	d.codecCursors = nil
	d.codecSearch.Page = nil
	if d.codecSearch.RequestID != 0 {
		d.codecSearch.State, d.codecSearch.Err = "failed", err
	}
}

func (d *Document) readCodecSearch(ctx context.Context, task work) (*CodecSearchPage, *codecSearchCursor, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if err := d.sourceUnchanged(); err != nil {
		return nil, nil, err
	}
	snapshot := d.scan.Snapshot()
	page, next, err := d.searchCodec(ctx, task, snapshot.Progress.TagCount, snapshot.Complete)
	if changed := d.sourceUnchanged(); changed != nil {
		return nil, nil, changed
	}
	if canceled := ctx.Err(); canceled != nil {
		return nil, nil, canceled
	}
	return page, next, err
}

func (d *Document) commitCodecCursorLocked(task work, next *codecSearchCursor) {
	if task.offset == 0 {
		d.codecCursors = nil
	}
	if next == nil {
		return
	}
	if len(d.codecCursors) == MaxCodecSearchCursors {
		copy(d.codecCursors, d.codecCursors[1:])
		d.codecCursors = d.codecCursors[:MaxCodecSearchCursors-1]
	}
	d.codecCursors = append(d.codecCursors, *next)
}

// A ready page survives detail/Hex navigation; snapshot capture performs no I/O.
func (d *Document) SnapshotCodecSearch(id uint64) (CodecSearchResult, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.snapshotAvailableLocked(); err != nil {
		return CodecSearchResult{}, err
	}
	if err := d.codecSearchIDLocked(id); err != nil {
		return CodecSearchResult{}, err
	}
	return cloneCodecSearch(d.codecSearch), nil
}

func cloneCodecSearch(result CodecSearchResult) CodecSearchResult {
	if result.Page == nil {
		return result
	}
	page := *result.Page
	page.Rows = append([]CodecSearchRow{}, page.Rows...)
	for i := range page.Rows {
		row := &page.Rows[i]
		row.Values = append([]CodecSearchValue{}, row.Values...)
		for _, field := range []**uint32{&row.FrameIndex, &row.NALIndex, &row.MessageIndex} {
			if *field != nil {
				value := **field
				*field = &value
			}
		}
		for _, field := range []**cursor.Span{&row.Span, &row.SPSSpan, &row.EvidenceSpan} {
			if *field != nil {
				value := **field
				*field = &value
			}
		}
	}
	result.Page = &page
	return result
}
