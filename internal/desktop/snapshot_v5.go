package desktop

import (
	"github.com/sevico/muxray/internal/document"
	"github.com/sevico/muxray/internal/flv"
	"github.com/sevico/muxray/internal/timesearch"
)

// SnapshotTimeSearchRequest identifies a completed server-owned page. Neither
// caller-authored evidence nor a resumable cursor is part of the export.
type SnapshotTimeSearchRequest struct {
	RequestID    string `json:"requestId"`
	Start        string `json:"start"`
	Filter       string `json:"filter"`
	GapThreshold string `json:"gapThreshold"`
}

type SnapshotTimeSearchPage struct {
	Start               string          `json:"start"`
	Next                string          `json:"next"`
	Total               string          `json:"total"`
	Scanned             uint32          `json:"scanned"`
	Filter              string          `json:"filter"`
	GapThreshold        string          `json:"gapThreshold"`
	Exhausted           bool            `json:"exhausted"`
	ScanComplete        bool            `json:"scanComplete"`
	UnknownPredecessors uint32          `json:"unknownPredecessors"`
	Rows                []TimeSearchRow `json:"rows"`
	InspectedStart      string          `json:"inspectedStart"`
	InspectedEnd        string          `json:"inspectedEnd"`
}

type SnapshotTimeSearchResponse struct {
	APIVersion uint8                   `json:"apiVersion"`
	SessionID  string                  `json:"sessionId"`
	RequestID  string                  `json:"requestId"`
	State      string                  `json:"state"`
	Page       *SnapshotTimeSearchPage `json:"page"`
	Error      *Fault                  `json:"error"`
}

type snapshotTimeSearchSession interface {
	SnapshotTimeSearch(uint64) (document.TimeSearchResult, error)
}

func validSnapshotTimeSearch(q SnapshotTimeSearchRequest) bool {
	id, idValid := parseStart(q.RequestID)
	_, startValid := parseStart(q.Start)
	gap, gapValid := parseStart(q.GapThreshold)
	return idValid && id != 0 && startValid && gapValid && gap > 0 && gap <= 1<<32-1 && timesearch.ValidFilter(q.Filter)
}

func (a *App) captureTimeSearch(e *slot, r *DiagnosticSnapshot, omit func(string, string)) {
	q := r.Selection.TimeSearch
	if q == nil {
		omit("timeSearchPage", "not_requested")
		return
	}
	owner, ok := e.scan.(snapshotTimeSearchSession)
	if !ok {
		omit("timeSearchPage", CodeTimeSearchUnsupported)
		return
	}
	id, _ := parseStart(q.RequestID)
	result, err := owner.SnapshotTimeSearch(id)
	if err == nil {
		err = result.Err
	}
	if err != nil {
		omit("timeSearchPage", timeSearchFailure(err).Code)
		return
	}
	if result.RequestID != id {
		omit("timeSearchPage", "identity_mismatch")
		return
	}
	if result.State != "ready" || result.Page == nil {
		omit("timeSearchPage", CodeNotReady)
		return
	}
	p := result.Page
	start, _ := parseStart(q.Start)
	gap, _ := parseStart(q.GapThreshold)
	total, _ := parseStart(r.Session.Progress.TagCount)
	size, _ := parseStart(r.Session.Progress.SourceSize)
	if p.Start != start || p.Filter != q.Filter || uint64(p.GapThreshold) != gap || p.Total != total || p.ScanComplete != r.Session.Scan.Complete || !validSnapshotTimePage(p, id, size) {
		omit("timeSearchPage", "identity_mismatch")
		return
	}
	// At most 64 current/predecessor pairs: no source I/O, prefix traversal or
	// reconstruction of the private 512-track continuation state.
	for _, row := range p.Rows {
		point := row.Point
		current, err := e.scan.Page(point.TagID, 1)
		if err != nil || len(current.Records) != 1 || !validSnapshotTimeTag(point, current.Records[0]) {
			omit("timeSearchPage", "identity_mismatch")
			return
		}
		if point.PreviousTagID != nil {
			prior, err := e.scan.Page(*point.PreviousTagID, 1)
			if err != nil || len(prior.Records) != 1 || prior.Records[0].ID != *point.PreviousTagID || prior.Records[0].DTS != *point.PreviousDTS || uint8(prior.Records[0].Type()) != point.TypeCode || prior.Records[0].StreamID != point.StreamID {
				omit("timeSearchPage", "identity_mismatch")
				return
			}
		}
	}
	page := &SnapshotTimeSearchPage{Start: decimal(p.Start), Next: decimal(p.Next), Total: decimal(p.Total), Scanned: p.Scanned, Filter: p.Filter, GapThreshold: decimal(uint64(p.GapThreshold)), Exhausted: p.Exhausted, ScanComplete: p.ScanComplete, UnknownPredecessors: p.UnknownPredecessors, Rows: []TimeSearchRow{}, InspectedStart: "0", InspectedEnd: decimal(p.Next)}
	for _, row := range p.Rows {
		page.Rows = append(page.Rows, TimeSearchRow{Kind: row.Kind, Point: mapTrendPoint(row.Point)})
	}
	r.TimeSearchPage = &SnapshotTimeSearchResponse{APIVersion: APIVersion, SessionID: e.id, RequestID: q.RequestID, State: "ready", Page: page}
}

func validSnapshotTimePage(p *document.TimeSearchPage, requestID, size uint64) bool {
	if !timesearch.ValidFilter(p.Filter) || p.GapThreshold == 0 || p.Total > flv.HardMaxTags || p.Start > p.Next || p.Next > p.Total || p.Next-p.Start != uint64(p.Scanned) || p.Scanned > timesearch.MaxScanned || len(p.Rows) > timesearch.MaxRows || len(p.Rows) > int(p.Scanned) || p.UnknownPredecessors > uint32(len(p.Rows)) || p.Exhausted != (p.Next == p.Total) || p.Exhausted && p.NextCursor != 0 || !p.Exhausted && p.NextCursor != requestID || (p.Start == 0) != (p.Cursor == 0) {
		return false
	}
	if !p.Exhausted && p.Scanned != timesearch.MaxScanned && len(p.Rows) != timesearch.MaxRows {
		return false
	}
	if len(p.Rows) == timesearch.MaxRows && p.Rows[len(p.Rows)-1].Point.TagID != p.Next-1 {
		return false
	}
	var unknown uint32
	for i, row := range p.Rows {
		v := row.Point
		if v.TagID < p.Start || v.TagID >= p.Next || i > 0 && v.TagID <= p.Rows[i-1].Point.TagID || v.DTS < 0 || v.DTS > 1<<32-1 || v.TypeCode > 31 || v.StreamID > 1<<24-1 || v.DataSize > 1<<24-1 || v.AvailableDataSize > v.DataSize || v.PayloadComplete != (v.DataSize == v.AvailableDataSize) || v.Offset > size || size-v.Offset < flv.TagHeaderSize || uint64(v.AvailableDataSize) > size-v.Offset-flv.TagHeaderSize {
			return false
		}
		if v.PreviousTagSizeState != "valid" && v.PreviousTagSizeState != "missing" && v.PreviousTagSizeState != "invalid" {
			return false
		}
		if row.Kind == "unknown" {
			if v.Context != "budget_exhausted" || v.PreviousTagID != nil || v.PreviousDTS != nil || v.DeltaMS != nil || v.TagID < timesearch.MaxTracks {
				return false
			}
			unknown++
			continue
		}
		if v.Context != "found" || v.PreviousTagID == nil || v.PreviousDTS == nil || v.DeltaMS == nil || *v.PreviousTagID >= v.TagID || *v.PreviousDTS < 0 || *v.PreviousDTS > 1<<32-1 || *v.DeltaMS != v.DTS-*v.PreviousDTS {
			return false
		}
		kind := ""
		switch {
		case *v.DeltaMS < 0:
			kind = "regression"
		case *v.DeltaMS == 0:
			kind = "duplicate"
		case *v.DeltaMS > int64(p.GapThreshold):
			kind = "gap"
		}
		if kind == "" || row.Kind != kind || p.Filter != "all" && p.Filter != kind+"s" {
			return false
		}
	}
	return unknown == p.UnknownPredecessors
}

func validSnapshotTimeTag(point flv.TagTrendPoint, tag flv.TagRecord) bool {
	previous := "valid"
	if !tag.PreviousTagSizePresent() {
		previous = "missing"
	} else if !tag.PreviousTagSizeValid() {
		previous = "invalid"
	}
	return point.TagID == tag.ID && point.Offset == tag.Offset && point.DTS == tag.DTS && point.TypeCode == uint8(tag.Type()) && point.StreamID == tag.StreamID && point.DataSize == tag.DataSize && point.AvailableDataSize == tag.AvailableDataSize && point.PayloadComplete == tag.PayloadComplete() && point.Filtered == tag.Filter() && point.PreviousTagSizeState == previous
}
