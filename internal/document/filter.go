package document

import (
	"github.com/sevico/muxray/internal/flv"
	"github.com/sevico/muxray/internal/session"
)

// FilteredPage is an in-memory query, independent of the source-read worker.
// It fences closure and a previously detected source change without Stat or
// ReadAt. Actual source-change detection remains with detail/Hex/Summary reads.
func (d *Document) FilteredPage(start uint64, count uint32, filter flv.TagFilter) (flv.FilteredTagPage, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed || d.ctx.Err() != nil {
		return flv.FilteredTagPage{}, session.ErrClosed
	}
	if d.changed {
		return flv.FilteredTagPage{}, ErrSourceChanged
	}
	return d.scan.FilteredPage(start, count, filter)
}
