package desktop

import (
	"errors"

	"github.com/sevico/muxray/internal/document"
	"github.com/sevico/muxray/internal/timesearch"
)

const (
	CodeTimeSearchNotFound      = "time_search_not_found"
	CodeTimeSearchFailed        = "time_search_failed"
	CodeTimeSearchCursorExpired = "time_search_cursor_expired"
	CodeTimeSearchUnsupported   = "time_search_unsupported"
)

type TimeSearchRequest struct {
	SessionID    string `json:"sessionId"`
	RequestID    string `json:"requestId"`
	Cursor       string `json:"cursor"`
	Filter       string `json:"filter"`
	GapThreshold string `json:"gapThreshold"`
}
type TimeSearchRow struct {
	Kind  string        `json:"kind"`
	Point TagTrendPoint `json:"point"`
}
type TimeSearchPage struct {
	Cursor              string          `json:"cursor"`
	NextCursor          string          `json:"nextCursor"`
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
}
type TimeSearchResponse struct {
	APIVersion uint8           `json:"apiVersion"`
	SessionID  string          `json:"sessionId"`
	RequestID  string          `json:"requestId"`
	State      string          `json:"state"`
	Page       *TimeSearchPage `json:"page"`
	Error      *Fault          `json:"error"`
}
type timeSearchSession interface {
	StartTimeSearch(uint64, uint64, string, uint32) (document.TimeSearchResult, error)
	GetTimeSearch(uint64) (document.TimeSearchResult, error)
	CancelTimeSearch(uint64) (document.TimeSearchResult, error)
}

func (a *App) timeSearchSource(id string) (timeSearchSession, *Fault) {
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
	owner, ok := e.scan.(timeSearchSession)
	if !ok {
		return nil, timeSearchFault(CodeTimeSearchUnsupported)
	}
	return owner, nil
}
func (b *Bindings) StartTimeSearch(q TimeSearchRequest) TimeSearchResponse {
	return b.timeSearchOperation(q, "start")
}
func (b *Bindings) GetTimeSearch(q DetailQuery) TimeSearchResponse {
	return b.timeSearchOperation(TimeSearchRequest{SessionID: q.SessionID, RequestID: q.RequestID}, "get")
}
func (b *Bindings) CancelTimeSearch(q DetailQuery) TimeSearchResponse {
	return b.timeSearchOperation(TimeSearchRequest{SessionID: q.SessionID, RequestID: q.RequestID}, "cancel")
}
func (b *Bindings) timeSearchOperation(q TimeSearchRequest, op string) TimeSearchResponse {
	base := detailResponse(q.SessionID, q.RequestID, "")
	response := TimeSearchResponse{APIVersion: APIVersion, SessionID: base.SessionID, RequestID: base.RequestID, State: "failed"}
	id, valid := parseStart(q.RequestID)
	token, tokenValid := parseStart(q.Cursor)
	threshold, thresholdValid := parseStart(q.GapThreshold)
	if !valid || id == 0 || op == "start" && (!tokenValid || !thresholdValid || threshold == 0 || threshold > uint64(^uint32(0)) || !timesearch.ValidFilter(q.Filter)) {
		response.Error = fault(CodeInvalidArgument)
		return response
	}
	owner, failure := b.app.timeSearchSource(q.SessionID)
	if failure != nil {
		response.Error = failure
		return response
	}
	var result document.TimeSearchResult
	var err error
	switch op {
	case "start":
		result, err = owner.StartTimeSearch(id, token, q.Filter, uint32(threshold))
	case "cancel":
		result, err = owner.CancelTimeSearch(id)
	default:
		result, err = owner.GetTimeSearch(id)
	}
	if err != nil {
		response.Error = timeSearchFailure(err)
		return response
	}
	response.RequestID, response.State = decimal(result.RequestID), result.State
	if result.Err != nil {
		response.Error = timeSearchFailure(result.Err)
	}
	if result.Page != nil {
		p := result.Page
		page := &TimeSearchPage{Cursor: decimal(p.Cursor), NextCursor: decimal(p.NextCursor), Start: decimal(p.Start), Next: decimal(p.Next), Total: decimal(p.Total), Scanned: p.Scanned, Filter: p.Filter, GapThreshold: decimal(uint64(p.GapThreshold)), Exhausted: p.Exhausted, ScanComplete: p.ScanComplete, UnknownPredecessors: p.UnknownPredecessors, Rows: []TimeSearchRow{}}
		for _, row := range p.Rows {
			page.Rows = append(page.Rows, TimeSearchRow{Kind: row.Kind, Point: mapTrendPoint(row.Point)})
		}
		response.Page = page
	}
	return response
}
func timeSearchFailure(err error) *Fault {
	if errors.Is(err, document.ErrUnknownRequest) {
		return timeSearchFault(CodeTimeSearchNotFound)
	}
	if errors.Is(err, document.ErrUnknownCursor) {
		return timeSearchFault(CodeTimeSearchCursorExpired)
	}
	if errors.Is(err, timesearch.ErrInvalidInput) {
		return fault(CodeInvalidArgument)
	}
	failure := detailFailure(err)
	if failure.Code == CodeDetailFailed {
		return timeSearchFault(CodeTimeSearchFailed)
	}
	return failure
}
func timeSearchFault(code string) *Fault {
	message := "The bounded timestamp observation search could not be produced."
	switch code {
	case CodeTimeSearchNotFound:
		message = "The timestamp search request has not been admitted."
	case CodeTimeSearchUnsupported:
		message = "This session does not support timestamp observation searches."
	case CodeTimeSearchCursorExpired:
		message = "The timestamp search cursor has expired; restart from the beginning."
	}
	return &Fault{Code: code, Message: message}
}
