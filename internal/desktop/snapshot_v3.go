package desktop

import (
	"encoding/json"

	"github.com/sevico/muxray/internal/avcsearch"
	"github.com/sevico/muxray/internal/document"
)

// SnapshotAVCSearchRequest identifies only the current retained page, never
// caller-supplied rows or a request to resume the search.
type SnapshotAVCSearchRequest struct {
	RequestID string `json:"requestId"`
	Start     string `json:"start"`
	Filter    string `json:"filter"`
}

type snapshotAVCSearchSession interface {
	SnapshotAVCSearch(uint64) (document.AVCSearchResult, error)
}

// Versioned members remain absent from older formats. New formats always
// record their unavailable sections as explicit nulls with omission reasons.
func (s DiagnosticSnapshot) MarshalJSON() ([]byte, error) {
	type plain DiagnosticSnapshot
	switch s.SchemaVersion {
	case 4:
		return json.Marshal(struct {
			plain
			MediaCurvePage  *MediaCurveResponse  `json:"mediaCurvePage"`
			AVCSearchPage   *AVCSearchResponse   `json:"avcSearchPage"`
			CodecSearchPage *CodecSearchResponse `json:"codecSearchPage"`
		}{plain(s), s.MediaCurvePage, s.AVCSearchPage, s.CodecSearchPage})
	case 3:
		return json.Marshal(struct {
			plain
			MediaCurvePage *MediaCurveResponse `json:"mediaCurvePage"`
			AVCSearchPage  *AVCSearchResponse  `json:"avcSearchPage"`
		}{plain(s), s.MediaCurvePage, s.AVCSearchPage})
	case 2:
		return json.Marshal(struct {
			plain
			MediaCurvePage *MediaCurveResponse `json:"mediaCurvePage"`
		}{plain(s), s.MediaCurvePage})
	default:
		return json.Marshal(plain(s))
	}
}

func validSnapshotAVCSearch(q SnapshotAVCSearchRequest) bool {
	id, valid := parseStart(q.RequestID)
	_, startValid := parseStart(q.Start)
	return valid && id != 0 && startValid && avcsearch.ValidFilter(q.Filter)
}

func (a *App) captureAVCSearch(e *slot, r *DiagnosticSnapshot, omit func(string, string)) {
	q := r.Selection.AVCSearch
	if q == nil {
		omit("avcSearchPage", "not_requested")
		return
	}
	owner, ok := e.scan.(snapshotAVCSearchSession)
	if !ok {
		omit("avcSearchPage", CodeAVCSearchUnsupported)
		return
	}
	id, _ := parseStart(q.RequestID)
	result, err := owner.SnapshotAVCSearch(id)
	response := mapAVCSearchResult(AVCSearchResponse{APIVersion: APIVersion, SessionID: e.id, RequestID: q.RequestID, State: "failed"}, result, err)
	if response.Error != nil {
		omit("avcSearchPage", response.Error.Code)
		return
	}
	if response.RequestID != q.RequestID {
		omit("avcSearchPage", "identity_mismatch")
		return
	}
	if response.State != "ready" || result.Page == nil {
		omit("avcSearchPage", CodeNotReady)
		return
	}
	start, _ := parseStart(q.Start)
	p := result.Page
	if p.Start != start || p.Filter != q.Filter || p.Start > p.Next || p.Next > p.Total ||
		p.Next-p.Start != uint64(p.Scanned) || p.Scanned > avcsearch.MaxScanTags ||
		p.BytesRead > avcsearch.MaxBytes || len(p.Rows) > avcsearch.MaxResults ||
		len(p.Rows) > int(p.Scanned) || p.Exhausted != (p.Next == p.Total) {
		omit("avcSearchPage", "identity_mismatch")
		return
	}
	for i, row := range p.Rows {
		if row.TagID < p.Start || row.TagID >= p.Next || i > 0 && row.TagID <= p.Rows[i-1].TagID {
			omit("avcSearchPage", "identity_mismatch")
			return
		}
	}
	r.AVCSearchPage = &response
}
