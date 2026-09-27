package document

import (
	"github.com/sevico/muxray/internal/flv"
	"github.com/sevico/muxray/internal/session"
)

// TrendPage fences logical closure and an already detected source change. It
// neither calls Stat/ReadAt nor joins the read worker or invalidates Summary.
func (d *Document) TrendPage(start uint64, count uint32) (flv.TagTrendPage, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed || d.ctx.Err() != nil {
		return flv.TagTrendPage{}, session.ErrClosed
	}
	if d.changed {
		return flv.TagTrendPage{}, ErrSourceChanged
	}
	return d.scan.TrendPage(start, count)
}
