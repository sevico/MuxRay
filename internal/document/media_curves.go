package document

import (
	"slices"

	"github.com/sevico/muxray/internal/analysis"
	"github.com/sevico/muxray/internal/session"
)

const (
	DefaultMediaCurvePagePoints uint32 = 64
	MaxMediaCurvePagePoints     uint32 = 128
)

type MediaCurvePage struct {
	Start, Next, Total uint64
	Limit              uint32
	IndexedTags        uint64
	ScanComplete       bool
	Complete           bool
	Points             []analysis.MediaCurvePoint
}

// MediaCurves pages a completed summary's compact source-order windows.
// It neither reads the source nor starts analysis, and a later Detail/Hex
// request does not evict this immutable result. Source-change detection is
// performed by those reads, not by this in-memory query.
func (d *Document) MediaCurves(id, start uint64, count uint32) (MediaCurvePage, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.summaryIDLocked(id); err != nil {
		return MediaCurvePage{}, err
	}
	if count > MaxMediaCurvePagePoints {
		return MediaCurvePage{}, ErrInvalidRequest
	}
	if count == 0 {
		count = DefaultMediaCurvePagePoints
	}
	if d.changed {
		return MediaCurvePage{}, ErrSourceChanged
	}
	if d.summary.Err != nil {
		return MediaCurvePage{}, d.summary.Err
	}
	if d.summary.State != "ready" || d.summaryReport == nil || d.summaryReport.Curves == nil {
		return MediaCurvePage{}, session.ErrNotReady
	}
	report := d.summaryReport
	curves := report.Curves
	total := uint64(len(curves.Points))
	if start > total {
		return MediaCurvePage{}, ErrInvalidRequest
	}
	next := start + min(uint64(count), total-start)
	points := slices.Clone(curves.Points[start:next])
	if points == nil {
		points = []analysis.MediaCurvePoint{}
	}
	return MediaCurvePage{Start: start, Next: next, Total: total, Limit: count,
		IndexedTags: curves.IndexedTags, ScanComplete: report.Overview.ScanComplete,
		Complete: report.Overview.Complete, Points: points}, nil
}
