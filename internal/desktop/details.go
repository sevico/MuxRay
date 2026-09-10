package desktop

import (
	"context"
	"errors"

	"github.com/sevico/muxray/internal/detailview"
	"github.com/sevico/muxray/internal/document"
	"github.com/sevico/muxray/internal/session"
)

const (
	CodeDetailNotFound    = "detail_not_found"
	CodeStaleRequest      = "stale_request"
	CodeSourceChanged     = "source_changed"
	CodeDetailUnsupported = "detail_unsupported"
	CodeDetailFiltered    = "detail_filtered"
	CodeDetailTruncated   = "detail_truncated"
	CodeTagNotFound       = "tag_not_found"
	CodeDetailFailed      = "detail_failed"
)

type DetailRequest struct {
	SessionID string `json:"sessionId"`
	RequestID string `json:"requestId"`
	TagID     string `json:"tagId"`
}

type DetailQuery struct {
	SessionID string `json:"sessionId"`
	RequestID string `json:"requestId"`
}

type DetailResponse struct {
	APIVersion uint8              `json:"apiVersion"`
	SessionID  string             `json:"sessionId"`
	RequestID  string             `json:"requestId"`
	TagID      string             `json:"tagId"`
	State      string             `json:"state"`
	Detail     *detailview.Detail `json:"detail"`
	Error      *Fault             `json:"error"`
}

type detailSession interface {
	StartDetail(uint64, uint64) (document.DetailResult, error)
	GetDetail(uint64) (document.DetailResult, error)
}

// detailSource captures only the current, open document. The owner rechecks
// logical closure after capture, so a concurrent close cannot start new reads.
func (a *App) detailSource(id string) (detailSession, *Fault) {
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
	owner, ok := e.scan.(detailSession)
	if !ok {
		return nil, detailFault(CodeDetailUnsupported)
	}
	return owner, nil
}

func detailResponse(sessionID, requestID, tagID string) DetailResponse {
	// Invalid callers cannot make a failed response echo an unbounded string.
	if len(sessionID) > 64 {
		sessionID = ""
	}
	if len(requestID) > 20 {
		requestID = ""
	}
	if len(tagID) > 20 {
		tagID = ""
	}
	return DetailResponse{APIVersion: APIVersion, SessionID: sessionID, RequestID: requestID, TagID: tagID, State: "failed"}
}

// StartDetail admits a monotonically numbered selection. Repeating an admitted
// ID/tag returns its current outcome; it never creates another parsing task.
func (b *Bindings) StartDetail(request DetailRequest) DetailResponse {
	response := detailResponse(request.SessionID, request.RequestID, request.TagID)
	id, validID := parseStart(request.RequestID)
	tagID, validTag := parseStart(request.TagID)
	if !validID || id == 0 || !validTag {
		response.Error = fault(CodeInvalidArgument)
		return response
	}
	owner, failure := b.app.detailSource(request.SessionID)
	if failure != nil {
		response.Error = failure
		return response
	}
	result, err := owner.StartDetail(id, tagID)
	return mapDetailResult(response, result, err)
}

// GetDetail is a bounded in-memory query. Unknown IDs do not admit work, so a
// caller may explicitly repeat StartDetail with the same ID after a lost call.
func (b *Bindings) GetDetail(query DetailQuery) DetailResponse {
	response := detailResponse(query.SessionID, query.RequestID, "")
	id, valid := parseStart(query.RequestID)
	if !valid || id == 0 {
		response.Error = fault(CodeInvalidArgument)
		return response
	}
	owner, failure := b.app.detailSource(query.SessionID)
	if failure != nil {
		response.Error = failure
		return response
	}
	result, err := owner.GetDetail(id)
	return mapDetailResult(response, result, err)
}

func mapDetailResult(response DetailResponse, result document.DetailResult, err error) DetailResponse {
	if err != nil {
		response.Error = detailFailure(err)
		return response
	}
	response.RequestID, response.TagID = decimal(result.RequestID), decimal(result.TagID)
	response.State, response.Detail = result.State, result.Detail
	if result.Err != nil {
		response.Error = detailFailure(result.Err)
	}
	return response
}

func detailFailure(err error) *Fault {
	switch {
	case errors.Is(err, session.ErrClosed):
		return fault(CodeSessionClosed)
	case errors.Is(err, session.ErrNotReady):
		return fault(CodeNotReady)
	case errors.Is(err, document.ErrInvalidRequest), errors.Is(err, detailview.ErrInvalidInput):
		return fault(CodeInvalidArgument)
	case errors.Is(err, document.ErrSourceChanged):
		return detailFault(CodeSourceChanged)
	case errors.Is(err, document.ErrStaleRequest):
		return detailFault(CodeStaleRequest)
	case errors.Is(err, document.ErrUnknownRequest):
		return detailFault(CodeDetailNotFound)
	case errors.Is(err, document.ErrTagNotFound):
		return detailFault(CodeTagNotFound)
	case errors.Is(err, detailview.ErrUnsupported):
		return detailFault(CodeDetailUnsupported)
	case errors.Is(err, detailview.ErrFiltered):
		return detailFault(CodeDetailFiltered)
	case errors.Is(err, detailview.ErrTruncated):
		return detailFault(CodeDetailTruncated)
	case errors.Is(err, context.DeadlineExceeded):
		return fault(CodeDeadline)
	case errors.Is(err, context.Canceled):
		return fault(CodeCanceled)
	default:
		return detailFault(CodeDetailFailed)
	}
}

func detailFault(code string) *Fault {
	message := "The selected tag detail could not be read."
	switch code {
	case CodeDetailNotFound:
		message = "The detail request has not been admitted."
	case CodeStaleRequest:
		message = "The detail request has been superseded."
	case CodeSourceChanged:
		message = "The source file changed; close it and reopen to rebuild the index."
	case CodeDetailUnsupported:
		message = "Details are supported for AMF0, AAC and AVC tags only."
	case CodeDetailFiltered:
		message = "Filtered tag payloads cannot be inspected."
	case CodeDetailTruncated:
		message = "The selected tag payload is truncated."
	case CodeTagNotFound:
		message = "The selected tag is outside the committed index."
	}
	return &Fault{Code: code, Message: message}
}
