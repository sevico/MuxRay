package desktop

import (
	"encoding/json"

	"github.com/sevico/muxray/internal/analysis"
	"github.com/sevico/muxray/internal/document"
	"github.com/sevico/muxray/internal/summaryview"
)

type MediaCurveRequest struct {
	SessionID string `json:"sessionId"`
	RequestID string `json:"requestId"`
	Start     string `json:"start"`
	Count     uint32 `json:"count"`
}

type MediaCurveRate struct {
	CoveredBytes      string  `json:"coveredBytes"`
	CoveredIntervalMS string  `json:"coveredIntervalMs"`
	BitsPerSecond     *string `json:"bitsPerSecond"`
	TagID             *string `json:"tagId"`
}

type MediaCurveGOP struct {
	DeclaredCount     string  `json:"declaredCount"`
	ClosedCount       string  `json:"closedCount"`
	TotalDurationMS   string  `json:"totalDurationMs"`
	MinDurationMS     *string `json:"minDurationMs"`
	MaxDurationMS     *string `json:"maxDurationMs"`
	AverageDurationMS *string `json:"averageDurationMs"`
	TagID             *string `json:"tagId"`
}

type MediaCurvePoint struct {
	Index            string         `json:"index"`
	StartTag         string         `json:"startTag"`
	NextTag          string         `json:"nextTag"`
	Audio            MediaCurveRate `json:"audio"`
	Video            MediaCurveRate `json:"video"`
	MP3              MediaCurveRate `json:"mp3"`
	GOP              MediaCurveGOP  `json:"gop"`
	UnclassifiedTags string         `json:"unclassifiedTags"`
}

type MediaCurvePage struct {
	Bounds       PageBounds        `json:"bounds"`
	WindowTags   uint32            `json:"windowTags"`
	IndexedTags  string            `json:"indexedTags"`
	ScanComplete bool              `json:"scanComplete"`
	Complete     bool              `json:"complete"`
	Points       []MediaCurvePoint `json:"points"`
}

type MediaCurveResponse struct {
	APIVersion uint8           `json:"apiVersion"`
	SessionID  string          `json:"sessionId"`
	RequestID  string          `json:"requestId"`
	Page       *MediaCurvePage `json:"page"`
	Error      *Fault          `json:"error"`
}

type mediaCurveSession interface {
	MediaCurves(uint64, uint64, uint32) (document.MediaCurvePage, error)
}

func (b *Bindings) GetMediaCurves(q MediaCurveRequest) MediaCurveResponse {
	base := summaryResponse(SummaryQuery{q.SessionID, q.RequestID})
	response := MediaCurveResponse{APIVersion: APIVersion, SessionID: base.SessionID, RequestID: base.RequestID}
	id, validID := parseStart(q.RequestID)
	start, validStart := parseStart(q.Start)
	if !validID || id == 0 || !validStart || q.Count > document.MaxMediaCurvePagePoints {
		response.Error = fault(CodeInvalidArgument)
		return response
	}
	summary, failure := b.app.summarySource(q.SessionID)
	if failure != nil {
		response.Error = failure
		return response
	}
	owner, ok := summary.(mediaCurveSession)
	if !ok {
		response.Error = summaryFault(CodeSummaryUnsupported)
		return response
	}
	page, err := owner.MediaCurves(id, start, q.Count)
	if err != nil {
		response.Error = summaryFailure(err)
		return response
	}
	view := &MediaCurvePage{Bounds: mapBounds(page.Start, page.Limit, page.Total, page.Next),
		WindowTags: uint32(analysis.CurveWindowTags), IndexedTags: decimal(page.IndexedTags),
		ScanComplete: page.ScanComplete, Complete: page.Complete, Points: make([]MediaCurvePoint, 0, len(page.Points))}
	for _, p := range page.Points {
		g := p.GOP
		view.Points = append(view.Points, MediaCurvePoint{Index: decimal(p.Index), StartTag: decimal(p.StartTag), NextTag: decimal(p.NextTag),
			Audio: mapCurveRate(p.Audio), Video: mapCurveRate(p.Video), MP3: mapCurveRate(p.MP3), UnclassifiedTags: decimal(p.UnclassifiedTags),
			GOP: MediaCurveGOP{DeclaredCount: decimal(g.DeclaredCount), ClosedCount: decimal(g.ClosedCount), TotalDurationMS: decimal(g.TotalDurationMS),
				MinDurationMS: curveOptional(g.Present, g.MinDurationMS), MaxDurationMS: curveOptional(g.Present, g.MaxDurationMS),
				AverageDurationMS: curveOptional(g.Present, g.AverageDurationMS), TagID: curveOptional(g.Present, g.TagID)}})
	}
	response.Page = view
	if encoded, err := json.Marshal(response); err != nil || len(encoded) > summaryview.MaxResponseBytes {
		response.Page = nil
		response.Error = summaryFault(CodeSummaryFailed)
	}
	return response
}

func mapCurveRate(rate analysis.MediaCurveRate) MediaCurveRate {
	return MediaCurveRate{CoveredBytes: decimal(rate.CoveredBytes), CoveredIntervalMS: decimal(rate.CoveredIntervalMS),
		BitsPerSecond: curveOptional(rate.Present, rate.BitsPerSecond), TagID: curveOptional(rate.Present, rate.TagID)}
}

func curveOptional(present bool, value uint64) *string {
	if !present {
		return nil
	}
	text := decimal(value)
	return &text
}
