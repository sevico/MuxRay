package document

import (
	"context"
	"encoding/json"
	"slices"
	"time"

	"github.com/sevico/muxray/internal/analysis"
	"github.com/sevico/muxray/internal/cursor"
	"github.com/sevico/muxray/internal/session"
	"github.com/sevico/muxray/internal/summaryview"
)

const (
	SummaryTimeout                  = 120 * time.Second
	MaxSummaryPageEvents     uint32 = 100
	summaryPageJSONBytes            = 256 << 10
	summaryPageEnvelopeBytes        = 1024
)

// SummaryResult copies only the bounded overview. Event pages are queried
// separately against this retained request identity, even after Detail/Hex.
type SummaryResult struct {
	RequestID        uint64
	State            string
	Processed, Total uint64
	Overview         *summaryview.Overview
	Err              error
}

type SummaryPage struct {
	Kind               string
	Start, Next, Total uint64
	Limit              uint32
	Events             []summaryview.Event
}

// StartSummary admits one whole-index analysis in the same worker as Detail
// and Hex. A captured terminal index is shared, never reconstructed from pages.
func (d *Document) StartSummary(id uint64) (SummaryResult, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed || d.ctx.Err() != nil {
		return SummaryResult{}, session.ErrClosed
	}
	if id == 0 {
		return SummaryResult{}, ErrInvalidRequest
	}
	if id < d.request.id {
		return SummaryResult{}, ErrStaleRequest
	}
	if id == d.request.id {
		if d.request.kind != summaryWork {
			return SummaryResult{}, ErrInvalidRequest
		}
		return cloneSummaryResult(d.summary), nil
	}
	if !d.scan.Snapshot().State.Terminal() {
		return SummaryResult{}, session.ErrNotReady
	}
	d.admitLocked(work{id: id, kind: summaryWork})
	return cloneSummaryResult(d.summary), nil
}

func (d *Document) summaryIDLocked(id uint64) error {
	if d.closed || d.ctx.Err() != nil {
		return session.ErrClosed
	}
	if id == 0 {
		return ErrInvalidRequest
	}
	if id == d.summary.RequestID {
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

// GetSummary is an in-memory query. Its retained identity need not be the
// latest Detail/Hex identity, and polling cannot admit or restart analysis.
func (d *Document) GetSummary(id uint64) (SummaryResult, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.summaryIDLocked(id); err != nil {
		return SummaryResult{}, err
	}
	return cloneSummaryResult(d.summary), nil
}

// CancelSummary is idempotent for a retained terminal result. An active read
// remains canceling until the worker returns; only a queued intent stops now.
func (d *Document) CancelSummary(id uint64) (SummaryResult, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.summaryIDLocked(id); err != nil {
		return SummaryResult{}, err
	}
	d.cancelSummaryLocked()
	return cloneSummaryResult(d.summary), nil
}

func (d *Document) cancelSummaryLocked() {
	if d.summary.State != "pending" {
		return
	}
	d.summary.Overview, d.summaryReport = nil, nil
	if d.active != nil && d.active.kind == summaryWork && d.active.id == d.summary.RequestID {
		d.summary.State = "canceling"
		if d.activeCancel != nil {
			d.activeCancel()
		}
		return
	}
	if d.pending != nil && d.pending.kind == summaryWork && d.pending.id == d.summary.RequestID {
		d.pending = nil
	}
	d.summary.State, d.summary.Err = "failed", context.Canceled
}

func (d *Document) invalidateSummaryLocked(err error) {
	d.summary.Overview, d.summaryReport = nil, nil
	if d.summary.RequestID != 0 {
		d.summary.State, d.summary.Err = "failed", err
	}
}

func validSummaryKind(kind string) bool {
	switch kind {
	case "keyframes", "aac", "avc", "mp3", "diagnostics":
		return true
	}
	return false
}

// SummaryEvents pages only the retained prefix. Total is its length, not the
// observed count. The byte budget can shorten a page below its requested limit.
func (d *Document) SummaryEvents(id uint64, kind string, start uint64, count uint32) (SummaryPage, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.summaryIDLocked(id); err != nil {
		return SummaryPage{}, err
	}
	if !validSummaryKind(kind) || count > MaxSummaryPageEvents {
		return SummaryPage{}, ErrInvalidRequest
	}
	if count == 0 {
		count = MaxSummaryPageEvents
	}
	if d.summary.Err != nil {
		return SummaryPage{}, d.summary.Err
	}
	if d.summary.State != "ready" || d.summaryReport == nil {
		return SummaryPage{}, session.ErrNotReady
	}
	events := d.summaryReport.Events[kind]
	total := uint64(len(events))
	if start > total {
		return SummaryPage{}, ErrInvalidRequest
	}
	page := SummaryPage{Kind: kind, Start: start, Next: start, Total: total, Limit: count, Events: make([]summaryview.Event, 0, min(uint64(count), total-start))}
	bytes := summaryPageEnvelopeBytes
	for end := min(start+uint64(count), total); page.Next < end; page.Next++ {
		event := events[page.Next]
		encoded, err := json.Marshal(event)
		if err != nil {
			return SummaryPage{}, err
		}
		if bytes+len(encoded)+1 > summaryPageJSONBytes {
			if page.Next == start {
				return SummaryPage{}, ErrInvalidRequest
			}
			break
		}
		bytes += len(encoded) + 1
		page.Events = append(page.Events, cloneSummaryEvent(event))
	}
	return page, nil
}

func (d *Document) readSummary(ctx context.Context, id uint64) (*summaryview.Report, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := d.sourceUnchanged(); err != nil {
		return nil, err
	}
	scan, err := d.scan.Result()
	if err != nil {
		return nil, err
	}
	result, err := analysis.AnalyzeV2WithProgress(ctx, cursor.WithContext(ctx, d.source), scan, summaryview.DesktopOptions(), func(progress analysis.Progress) {
		d.mu.Lock()
		defer d.mu.Unlock()
		if !d.closed && !d.changed && d.summary.RequestID == id && (d.summary.State == "pending" || d.summary.State == "canceling") {
			d.summary.Processed, d.summary.Total = progress.Processed, progress.Total
		}
	})
	var report *summaryview.Report
	if err == nil {
		report, err = summaryview.Project(scan, result)
	}
	// An I/O error/cancellation may leave half a Tag in the raw aggregate. It
	// never becomes a usable summary. Observable source changes win over errors.
	if changed := d.sourceUnchanged(); changed != nil {
		return nil, changed
	}
	if canceled := ctx.Err(); canceled != nil {
		return nil, canceled
	}
	if err != nil {
		return nil, err
	}
	return report, nil
}

func cloneSummaryResult(result SummaryResult) SummaryResult {
	if result.Overview != nil {
		overview := *result.Overview
		overview.Metrics = cloneSummaryMetrics(overview.Metrics)
		overview.Lists = slices.Clone(overview.Lists)
		for i := range overview.Lists {
			overview.Lists[i].Observed = cloneSummaryString(overview.Lists[i].Observed)
		}
		result.Overview = &overview
	}
	return result
}

func cloneSummaryMetrics(metrics []summaryview.Metric) []summaryview.Metric {
	metrics = slices.Clone(metrics)
	for i := range metrics {
		metrics[i].Value = cloneSummaryString(metrics[i].Value)
	}
	return metrics
}

func cloneSummaryEvent(event summaryview.Event) summaryview.Event {
	event.TagID, event.DTS = cloneSummaryString(event.TagID), cloneSummaryString(event.DTS)
	event.Values = cloneSummaryMetrics(event.Values)
	return event
}

func cloneSummaryString(value *string) *string {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
