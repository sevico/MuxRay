package document

import (
	"context"

	"github.com/sevico/muxray/internal/avcsearch"
	"github.com/sevico/muxray/internal/session"
)

type AVCSearchResult struct {
	RequestID uint64
	State     string
	Page      *avcsearch.Page
	Err       error
}

func (d *Document) StartAVCSearch(id, start uint64, filter string) (AVCSearchResult, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed || d.ctx.Err() != nil {
		return AVCSearchResult{}, session.ErrClosed
	}
	if id == 0 || !avcsearch.ValidFilter(filter) {
		return AVCSearchResult{}, ErrInvalidRequest
	}
	if id < d.request.id {
		return AVCSearchResult{}, ErrStaleRequest
	}
	if id == d.request.id {
		if d.request.kind != avcSearchWork || d.request.offset != start || d.request.filter != filter {
			return AVCSearchResult{}, ErrInvalidRequest
		}
		return cloneAVCSearch(d.avcSearch), nil
	}
	snapshot := d.scan.Snapshot()
	if !snapshot.State.Terminal() {
		return AVCSearchResult{}, session.ErrNotReady
	}
	if start > snapshot.Progress.TagCount {
		return AVCSearchResult{}, ErrInvalidRequest
	}
	d.admitLocked(work{id: id, kind: avcSearchWork, offset: start, filter: filter})
	return cloneAVCSearch(d.avcSearch), nil
}
func (d *Document) avcSearchIDLocked(id uint64) error {
	if d.closed || d.ctx.Err() != nil {
		return session.ErrClosed
	}
	if id == 0 {
		return ErrInvalidRequest
	}
	if id == d.avcSearch.RequestID {
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
func (d *Document) GetAVCSearch(id uint64) (AVCSearchResult, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.avcSearchIDLocked(id); err != nil {
		return AVCSearchResult{}, err
	}
	return cloneAVCSearch(d.avcSearch), nil
}
func (d *Document) CancelAVCSearch(id uint64) (AVCSearchResult, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.avcSearchIDLocked(id); err != nil {
		return AVCSearchResult{}, err
	}
	d.cancelAVCSearchLocked()
	return cloneAVCSearch(d.avcSearch), nil
}
func (d *Document) cancelAVCSearchLocked() {
	if d.avcSearch.State != "pending" {
		return
	}
	d.avcSearch.Page = nil
	if d.active != nil && d.active.kind == avcSearchWork && d.active.id == d.avcSearch.RequestID {
		d.avcSearch.State = "canceling"
		if d.activeCancel != nil {
			d.activeCancel()
		}
		return
	}
	if d.pending != nil && d.pending.kind == avcSearchWork && d.pending.id == d.avcSearch.RequestID {
		d.pending = nil
	}
	d.avcSearch.State, d.avcSearch.Err = "failed", context.Canceled
}
func (d *Document) invalidateAVCSearchLocked(err error) {
	d.avcSearch.Page = nil
	if d.avcSearch.RequestID != 0 {
		d.avcSearch.State, d.avcSearch.Err = "failed", err
	}
}
func (d *Document) readAVCSearch(ctx context.Context, start uint64, filter string) (*avcsearch.Page, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := d.sourceUnchanged(); err != nil {
		return nil, err
	}
	snapshot := d.scan.Snapshot()
	page, err := avcsearch.Search(ctx, d.source, d.scan.Tag, d.avcIndex, start, snapshot.Progress.TagCount, snapshot.Complete, filter)
	if changed := d.sourceUnchanged(); changed != nil {
		return nil, changed
	}
	if canceled := ctx.Err(); canceled != nil {
		return nil, canceled
	}
	return page, err
}
func cloneAVCSearch(result AVCSearchResult) AVCSearchResult {
	if result.Page != nil {
		page := *result.Page
		page.Rows = append([]avcsearch.Row{}, page.Rows...)
		for i := range page.Rows {
			row := &page.Rows[i]
			if row.DeclaredFrameType != nil {
				value := *row.DeclaredFrameType
				row.DeclaredFrameType = &value
			}
			if row.Span != nil {
				span := *row.Span
				row.Span = &span
			}
		}
		result.Page = &page
	}
	return result
}
