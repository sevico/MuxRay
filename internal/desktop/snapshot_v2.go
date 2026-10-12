package desktop

import (
	"github.com/sevico/muxray/internal/analysis"
)

const SnapshotMediaCurveLimit uint32 = 64

// SnapshotMediaCurveRequest identifies a view of an existing summary result.
// Its source identity is SnapshotRequest.SummaryRequestID, never client data.
type SnapshotMediaCurveRequest struct {
	Start  string `json:"start"`
	Count  uint32 `json:"count"`
	Metric string `json:"metric"`
}

func validSnapshotMediaCurve(q SnapshotMediaCurveRequest) bool {
	if _, ok := parseStart(q.Start); !ok || q.Count == 0 || q.Count > SnapshotMediaCurveLimit {
		return false
	}
	switch q.Metric {
	case "audio", "video", "mp3", "gopAverage", "gopMax":
		return true
	default:
		return false
	}
}

func (a *App) captureMediaCurve(e *slot, r *DiagnosticSnapshot, omit func(string, string)) {
	q := r.Selection.MediaCurve
	if q == nil {
		omit("mediaCurvePage", "not_requested")
		return
	}
	if r.Summary == nil || r.Selection.SummaryRequestID == nil {
		omit("mediaCurvePage", "summary_unavailable")
		return
	}
	owner, ok := e.scan.(mediaCurveSession)
	if !ok {
		omit("mediaCurvePage", CodeSummaryUnsupported)
		return
	}
	id, _ := parseStart(*r.Selection.SummaryRequestID)
	start, _ := parseStart(q.Start)
	page, err := owner.MediaCurves(id, start, q.Count)
	if err != nil {
		omit("mediaCurvePage", summaryFailure(err).Code)
		return
	}
	if page.Start != start || page.Limit != q.Count || page.Start > page.Next || page.Next > page.Total ||
		page.Next-page.Start != uint64(len(page.Points)) || len(page.Points) > int(q.Count) {
		omit("mediaCurvePage", "identity_mismatch")
		return
	}
	view := &MediaCurvePage{Bounds: mapBounds(page.Start, page.Limit, page.Total, page.Next),
		WindowTags: uint32(analysis.CurveWindowTags), IndexedTags: decimal(page.IndexedTags),
		ScanComplete: page.ScanComplete, Complete: page.Complete, Points: make([]MediaCurvePoint, 0, len(page.Points))}
	for i, p := range page.Points {
		if p.Index != page.Start+uint64(i) {
			omit("mediaCurvePage", "identity_mismatch")
			return
		}
		g := p.GOP
		view.Points = append(view.Points, MediaCurvePoint{Index: decimal(p.Index), StartTag: decimal(p.StartTag), NextTag: decimal(p.NextTag),
			Audio: mapCurveRate(p.Audio), Video: mapCurveRate(p.Video), MP3: mapCurveRate(p.MP3), UnclassifiedTags: decimal(p.UnclassifiedTags),
			GOP: MediaCurveGOP{DeclaredCount: decimal(g.DeclaredCount), ClosedCount: decimal(g.ClosedCount), TotalDurationMS: decimal(g.TotalDurationMS),
				MinDurationMS: curveOptional(g.Present, g.MinDurationMS), MaxDurationMS: curveOptional(g.Present, g.MaxDurationMS),
				AverageDurationMS: curveOptional(g.Present, g.AverageDurationMS), TagID: curveOptional(g.Present, g.TagID)}})
	}
	r.MediaCurvePage = &MediaCurveResponse{APIVersion: APIVersion, SessionID: e.id, RequestID: decimal(id), Page: view}
}
