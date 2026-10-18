package document

import (
	"context"

	"github.com/sevico/muxray/internal/session"
	"github.com/sevico/muxray/internal/timesearch"
)

const MaxTimeSearchCursors = 8

type TimeSearchPage struct {
	timesearch.Page
	Cursor, NextCursor uint64
	Filter             string
	GapThreshold       uint32
}
type TimeSearchResult struct {
	RequestID uint64
	State     string
	Page      *TimeSearchPage
	Err       error
}
type timeSearchCursor struct {
	id           uint64
	filter       string
	gapThreshold uint32
	state        timesearch.State
}

func (d *Document) StartTimeSearch(id, token uint64, filter string, gapThreshold uint32) (TimeSearchResult, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed || d.ctx.Err() != nil {
		return TimeSearchResult{}, session.ErrClosed
	}
	if id == 0 || !timesearch.ValidFilter(filter) || gapThreshold == 0 {
		return TimeSearchResult{}, ErrInvalidRequest
	}
	if d.changed {
		return TimeSearchResult{}, ErrSourceChanged
	}
	if id < d.request.id {
		return TimeSearchResult{}, ErrStaleRequest
	}
	if id == d.request.id {
		if d.request.kind != timeSearchWork || d.request.offset != token || d.request.filter != filter || d.request.gapThreshold != gapThreshold {
			return TimeSearchResult{}, ErrInvalidRequest
		}
		return cloneTimeSearch(d.timeSearch), nil
	}
	if !d.scan.Snapshot().State.Terminal() {
		return TimeSearchResult{}, session.ErrNotReady
	}
	seed := timeSearchCursor{filter: filter, gapThreshold: gapThreshold}
	if token != 0 {
		found := false
		for _, saved := range d.timeCursors {
			if saved.id == token && saved.filter == filter && saved.gapThreshold == gapThreshold {
				seed, found = saved, true
				break
			}
		}
		if !found {
			return TimeSearchResult{}, ErrUnknownCursor
		}
	}
	seed.state = seed.state.Clone()
	d.admitLocked(work{id: id, kind: timeSearchWork, offset: token, filter: filter, gapThreshold: gapThreshold, timeSeed: &seed})
	return cloneTimeSearch(d.timeSearch), nil
}

func (d *Document) timeSearchIDLocked(id uint64) error {
	if d.closed || d.ctx.Err() != nil {
		return session.ErrClosed
	}
	if d.changed {
		return ErrSourceChanged
	}
	if id == 0 {
		return ErrInvalidRequest
	}
	if id == d.timeSearch.RequestID {
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
func (d *Document) GetTimeSearch(id uint64) (TimeSearchResult, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.timeSearchIDLocked(id); err != nil {
		return TimeSearchResult{}, err
	}
	return cloneTimeSearch(d.timeSearch), nil
}
func (d *Document) CancelTimeSearch(id uint64) (TimeSearchResult, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.timeSearchIDLocked(id); err != nil {
		return TimeSearchResult{}, err
	}
	d.cancelTimeSearchLocked()
	return cloneTimeSearch(d.timeSearch), nil
}
func (d *Document) cancelTimeSearchLocked() {
	if d.timeSearch.State != "pending" {
		return
	}
	d.timeSearch.Page = nil
	if d.active != nil && d.active.kind == timeSearchWork && d.active.id == d.timeSearch.RequestID {
		d.timeSearch.State = "canceling"
		if d.activeCancel != nil {
			d.activeCancel()
		}
		return
	}
	if d.pending != nil && d.pending.kind == timeSearchWork && d.pending.id == d.timeSearch.RequestID {
		d.pending = nil
	}
	d.timeSearch.State, d.timeSearch.Err = "failed", context.Canceled
}
func (d *Document) invalidateTimeSearchLocked(err error) {
	d.timeCursors, d.timeSearch.Page = nil, nil
	if d.timeSearch.RequestID != 0 {
		d.timeSearch.State, d.timeSearch.Err = "failed", err
	}
}

// This operation inspects index metadata only. In particular it intentionally
// does not call sourceUnchanged (which performs Stat); the known-change latch is
// checked at admission and again before publication by the serial worker.
func (d *Document) readTimeSearch(ctx context.Context, task work) (*TimeSearchPage, *timeSearchCursor, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	page, state, err := d.scan.TimeSearch(ctx, task.timeSeed.state, task.filter, task.gapThreshold)
	if err != nil {
		return nil, nil, err
	}
	result := &TimeSearchPage{Page: page, Cursor: task.offset, Filter: task.filter, GapThreshold: task.gapThreshold}
	var next *timeSearchCursor
	if !page.Exhausted {
		result.NextCursor = task.id
		next = &timeSearchCursor{id: task.id, filter: task.filter, gapThreshold: task.gapThreshold, state: state}
	}
	return result, next, nil
}
func (d *Document) commitTimeCursorLocked(task work, next *timeSearchCursor) {
	if task.offset == 0 {
		d.timeCursors = nil
	}
	if next == nil {
		return
	}
	if len(d.timeCursors) == MaxTimeSearchCursors {
		copy(d.timeCursors, d.timeCursors[1:])
		d.timeCursors = d.timeCursors[:MaxTimeSearchCursors-1]
	}
	d.timeCursors = append(d.timeCursors, *next)
}
func cloneTimeSearch(result TimeSearchResult) TimeSearchResult {
	if result.Page == nil {
		return result
	}
	page := *result.Page
	page.Rows = append([]timesearch.Row{}, page.Rows...)
	for i := range page.Rows {
		point := &page.Rows[i].Point
		if point.PreviousTagID != nil {
			value := *point.PreviousTagID
			point.PreviousTagID = &value
		}
		if point.PreviousDTS != nil {
			value := *point.PreviousDTS
			point.PreviousDTS = &value
		}
		if point.DeltaMS != nil {
			value := *point.DeltaMS
			point.DeltaMS = &value
		}
	}
	result.Page = &page
	return result
}
