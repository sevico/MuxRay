package desktop

import (
	"errors"
	"strconv"

	"github.com/sevico/muxray/internal/avcsearch"
	"github.com/sevico/muxray/internal/detailview"
	"github.com/sevico/muxray/internal/document"
)

const (
	CodeAVCSearchNotFound    = "avc_search_not_found"
	CodeAVCSearchFailed      = "avc_search_failed"
	CodeAVCSearchUnsupported = "avc_search_unsupported"
)

type AVCSearchRequest struct {
	SessionID string `json:"sessionId"`
	RequestID string `json:"requestId"`
	Start     string `json:"start"`
	Filter    string `json:"filter"`
}
type AVCSearchRow struct {
	TagID               string           `json:"tagId"`
	DTS                 string           `json:"dts"`
	Offset              string           `json:"offset"`
	DeclaredFrameType   *uint8           `json:"declaredFrameType"`
	ObservedIDR         string           `json:"observedIDR"`
	DeclarationRelation string           `json:"declarationRelation"`
	Reason              string           `json:"reason"`
	Span                *detailview.Span `json:"span"`
}
type AVCSearchPage struct {
	Start        string         `json:"start"`
	Next         string         `json:"next"`
	Total        string         `json:"total"`
	Scanned      uint32         `json:"scanned"`
	BytesRead    string         `json:"bytesRead"`
	Exhausted    bool           `json:"exhausted"`
	ScanComplete bool           `json:"scanComplete"`
	Filter       string         `json:"filter"`
	Rows         []AVCSearchRow `json:"rows"`
}
type AVCSearchResponse struct {
	APIVersion uint8          `json:"apiVersion"`
	SessionID  string         `json:"sessionId"`
	RequestID  string         `json:"requestId"`
	State      string         `json:"state"`
	Page       *AVCSearchPage `json:"page"`
	Error      *Fault         `json:"error"`
}
type avcSearchSession interface {
	StartAVCSearch(uint64, uint64, string) (document.AVCSearchResult, error)
	GetAVCSearch(uint64) (document.AVCSearchResult, error)
	CancelAVCSearch(uint64) (document.AVCSearchResult, error)
}

func (a *App) avcSearchSource(id string) (avcSearchSession, *Fault) {
	a.mu.Lock()
	defer a.mu.Unlock()
	e := a.findLocked(id)
	if e == nil {
		return nil, fault(CodeSessionNotFound)
	}
	if e.closed {
		return nil, fault(CodeSessionClosed)
	}
	if !a.terminalLocked(e) {
		return nil, fault(CodeNotReady)
	}
	if e.scan == nil {
		return nil, fault(CodeOpenFailed)
	}
	owner, ok := e.scan.(avcSearchSession)
	if !ok {
		return nil, avcSearchFault(CodeAVCSearchUnsupported)
	}
	return owner, nil
}
func (b *Bindings) StartAVCSearch(q AVCSearchRequest) AVCSearchResponse {
	return b.avcSearchOperation(q, "start")
}
func (b *Bindings) GetAVCSearch(q DetailQuery) AVCSearchResponse {
	return b.avcSearchOperation(AVCSearchRequest{SessionID: q.SessionID, RequestID: q.RequestID}, "get")
}
func (b *Bindings) CancelAVCSearch(q DetailQuery) AVCSearchResponse {
	return b.avcSearchOperation(AVCSearchRequest{SessionID: q.SessionID, RequestID: q.RequestID}, "cancel")
}
func (b *Bindings) avcSearchOperation(q AVCSearchRequest, op string) AVCSearchResponse {
	base := detailResponse(q.SessionID, q.RequestID, "")
	response := AVCSearchResponse{APIVersion: APIVersion, SessionID: base.SessionID, RequestID: base.RequestID, State: "failed"}
	id, valid := parseStart(q.RequestID)
	start, startValid := parseStart(q.Start)
	if !valid || id == 0 || op == "start" && (!startValid || !avcsearch.ValidFilter(q.Filter)) {
		response.Error = fault(CodeInvalidArgument)
		return response
	}
	owner, failure := b.app.avcSearchSource(q.SessionID)
	if failure != nil {
		response.Error = failure
		return response
	}
	var result document.AVCSearchResult
	var err error
	switch op {
	case "start":
		result, err = owner.StartAVCSearch(id, start, q.Filter)
	case "cancel":
		result, err = owner.CancelAVCSearch(id)
	default:
		result, err = owner.GetAVCSearch(id)
	}
	if err != nil {
		response.Error = avcSearchFailure(err)
		return response
	}
	response.RequestID = decimal(result.RequestID)
	response.State = result.State
	if result.Err != nil {
		response.Error = avcSearchFailure(result.Err)
	}
	if result.Page != nil {
		p := result.Page
		page := &AVCSearchPage{Start: decimal(p.Start), Next: decimal(p.Next), Total: decimal(p.Total), Scanned: p.Scanned, BytesRead: decimal(p.BytesRead), Exhausted: p.Exhausted, ScanComplete: p.ScanComplete, Filter: p.Filter, Rows: make([]AVCSearchRow, 0, len(p.Rows))}
		for _, r := range p.Rows {
			row := AVCSearchRow{TagID: decimal(r.TagID), DTS: strconv.FormatInt(r.DTS, 10), Offset: decimal(r.Offset), DeclaredFrameType: r.DeclaredFrameType, ObservedIDR: r.ObservedIDR, DeclarationRelation: r.DeclarationRelation, Reason: r.Reason}
			if r.Span != nil {
				row.Span = &detailview.Span{ByteOffset: decimal(r.Span.ByteOffset), BitOffset: r.Span.BitOffset, BitLength: r.Span.BitLength}
			}
			page.Rows = append(page.Rows, row)
		}
		response.Page = page
	}
	return response
}
func avcSearchFailure(err error) *Fault {
	if errors.Is(err, document.ErrUnknownRequest) {
		return avcSearchFault(CodeAVCSearchNotFound)
	}
	if errors.Is(err, avcsearch.ErrInvalidInput) {
		return fault(CodeInvalidArgument)
	}
	failure := detailFailure(err)
	if failure.Code == CodeDetailFailed {
		return avcSearchFault(CodeAVCSearchFailed)
	}
	return failure
}
func avcSearchFault(code string) *Fault {
	message := "The bounded AVC evidence search could not be produced."
	if code == CodeAVCSearchNotFound {
		message = "The AVC search request has not been admitted."
	}
	if code == CodeAVCSearchUnsupported {
		message = "This session does not support AVC evidence searches."
	}
	return &Fault{Code: code, Message: message}
}
