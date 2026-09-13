package desktop

import (
	"encoding/json"
	"errors"

	"github.com/sevico/muxray/internal/document"
	"github.com/sevico/muxray/internal/summaryview"
)

const (
	CodeSummaryNotFound    = "summary_not_found"
	CodeSummaryFailed      = "summary_failed"
	CodeSummaryUnsupported = "summary_unsupported"
)

type SummaryQuery struct {
	SessionID string `json:"sessionId"`
	RequestID string `json:"requestId"`
}
type SummaryResponse struct {
	APIVersion uint8                 `json:"apiVersion"`
	SessionID  string                `json:"sessionId"`
	RequestID  string                `json:"requestId"`
	State      string                `json:"state"`
	Processed  string                `json:"processed"`
	Total      string                `json:"total"`
	Overview   *summaryview.Overview `json:"overview"`
	Error      *Fault                `json:"error"`
}
type SummaryPageRequest struct {
	SessionID string `json:"sessionId"`
	RequestID string `json:"requestId"`
	Kind      string `json:"kind"`
	Start     string `json:"start"`
	Count     uint32 `json:"count"`
}
type SummaryPage struct {
	Bounds PageBounds          `json:"bounds"`
	Events []summaryview.Event `json:"events"`
}
type SummaryPageResponse struct {
	APIVersion uint8        `json:"apiVersion"`
	SessionID  string       `json:"sessionId"`
	RequestID  string       `json:"requestId"`
	Kind       string       `json:"kind"`
	Page       *SummaryPage `json:"page"`
	Error      *Fault       `json:"error"`
}
type summarySession interface {
	StartSummary(uint64) (document.SummaryResult, error)
	GetSummary(uint64) (document.SummaryResult, error)
	CancelSummary(uint64) (document.SummaryResult, error)
	SummaryEvents(uint64, string, uint64, uint32) (document.SummaryPage, error)
}

func (a *App) summarySource(id string) (summarySession, *Fault) {
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
	owner, ok := e.scan.(summarySession)
	if !ok {
		return nil, summaryFault(CodeSummaryUnsupported)
	}
	return owner, nil
}
func summaryResponse(q SummaryQuery) SummaryResponse {
	if len(q.SessionID) > 64 {
		q.SessionID = ""
	}
	if len(q.RequestID) > 20 {
		q.RequestID = ""
	}
	return SummaryResponse{APIVersion: APIVersion, SessionID: q.SessionID, RequestID: q.RequestID, State: "failed", Processed: "0", Total: "0"}
}

func (b *Bindings) StartSummary(q SummaryQuery) SummaryResponse {
	return b.summaryOperation(q, "start")
}
func (b *Bindings) GetSummary(q SummaryQuery) SummaryResponse { return b.summaryOperation(q, "get") }
func (b *Bindings) CancelSummary(q SummaryQuery) SummaryResponse {
	return b.summaryOperation(q, "cancel")
}
func (b *Bindings) summaryOperation(q SummaryQuery, op string) SummaryResponse {
	response := summaryResponse(q)
	id, valid := parseStart(q.RequestID)
	if !valid || id == 0 {
		response.Error = fault(CodeInvalidArgument)
		return response
	}
	owner, failure := b.app.summarySource(q.SessionID)
	if failure != nil {
		response.Error = failure
		return response
	}
	var result document.SummaryResult
	var err error
	switch op {
	case "start":
		result, err = owner.StartSummary(id)
	case "cancel":
		result, err = owner.CancelSummary(id)
	default:
		result, err = owner.GetSummary(id)
	}
	if err != nil {
		response.Error = summaryFailure(err)
		return response
	}
	response.RequestID = decimal(result.RequestID)
	response.State = result.State
	response.Processed = decimal(result.Processed)
	response.Total = decimal(result.Total)
	response.Overview = result.Overview
	if result.Err != nil {
		response.Error = summaryFailure(result.Err)
	}
	if encoded, err := json.Marshal(response); err != nil || len(encoded) > summaryview.MaxResponseBytes {
		response.Overview = nil
		response.State = "failed"
		response.Error = summaryFault(CodeSummaryFailed)
	}
	return response
}
func (b *Bindings) GetSummaryEvents(q SummaryPageRequest) SummaryPageResponse {
	base := summaryResponse(SummaryQuery{q.SessionID, q.RequestID})
	response := SummaryPageResponse{APIVersion: APIVersion, SessionID: base.SessionID, RequestID: base.RequestID}
	if summaryview.ValidKind(q.Kind) {
		response.Kind = q.Kind
	}
	id, validID := parseStart(q.RequestID)
	start, validStart := parseStart(q.Start)
	if !validID || id == 0 || !validStart || !summaryview.ValidKind(q.Kind) || q.Count > 100 {
		response.Error = fault(CodeInvalidArgument)
		return response
	}
	owner, failure := b.app.summarySource(q.SessionID)
	if failure != nil {
		response.Error = failure
		return response
	}
	page, err := owner.SummaryEvents(id, q.Kind, start, q.Count)
	if err != nil {
		response.Error = summaryFailure(err)
		return response
	}
	response.Page = &SummaryPage{Bounds: PageBounds{Start: decimal(page.Start), Next: decimal(page.Next), Total: decimal(page.Total), Limit: page.Limit}, Events: page.Events}
	if encoded, err := json.Marshal(response); err != nil || len(encoded) > summaryview.MaxResponseBytes {
		response.Page = nil
		response.Error = summaryFault(CodeSummaryFailed)
	}
	return response
}
func summaryFailure(err error) *Fault {
	if errors.Is(err, document.ErrUnknownRequest) {
		return summaryFault(CodeSummaryNotFound)
	}
	failure := detailFailure(err)
	if failure.Code == CodeDetailFailed {
		return summaryFault(CodeSummaryFailed)
	}
	return failure
}
func summaryFault(code string) *Fault {
	message := "The media summary could not be produced."
	switch code {
	case CodeSummaryNotFound:
		message = "The summary request has not been admitted."
	case CodeSummaryUnsupported:
		message = "This session does not support media summaries."
	}
	return &Fault{Code: code, Message: message}
}
