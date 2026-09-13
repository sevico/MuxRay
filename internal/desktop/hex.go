package desktop

import (
	"encoding/hex"
	"errors"

	"github.com/sevico/muxray/internal/document"
)

const (
	CodeHexNotFound    = "hex_not_found"
	CodeHexFailed      = "hex_failed"
	CodeHexUnsupported = "hex_unsupported"
)

// HexRequest selects a small window in the currently retained source. All
// identities and file coordinates cross the JavaScript boundary as strings.
type HexRequest struct {
	SessionID string `json:"sessionId"`
	RequestID string `json:"requestId"`
	Offset    string `json:"offset"`
	Count     uint32 `json:"count"`
}

type HexQuery struct {
	SessionID string `json:"sessionId"`
	RequestID string `json:"requestId"`
}

type HexWindow struct {
	Offset     string `json:"offset"`
	SourceSize string `json:"sourceSize"`
	Hex        string `json:"hex"`
	EOF        bool   `json:"eof"`
}

type HexResponse struct {
	APIVersion uint8      `json:"apiVersion"`
	SessionID  string     `json:"sessionId"`
	RequestID  string     `json:"requestId"`
	Offset     string     `json:"offset"`
	Count      uint32     `json:"count"`
	State      string     `json:"state"`
	Window     *HexWindow `json:"window"`
	Error      *Fault     `json:"error"`
}

type hexSession interface {
	StartHex(uint64, uint64, uint32) (document.HexResult, error)
	GetHex(uint64) (document.HexResult, error)
}

func (a *App) hexSource(id string) (hexSession, *Fault) {
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
	owner, ok := e.scan.(hexSession)
	if !ok {
		return nil, hexFault(CodeHexUnsupported)
	}
	return owner, nil
}

func hexResponse(sessionID, requestID, offset string) HexResponse {
	if len(sessionID) > 64 {
		sessionID = ""
	}
	if len(requestID) > 20 {
		requestID = ""
	}
	if len(offset) > 20 {
		offset = ""
	}
	return HexResponse{APIVersion: APIVersion, SessionID: sessionID, RequestID: requestID, Offset: offset, State: "failed"}
}

// StartHex shares a monotonically increasing request namespace with StartDetail.
// Repeating the same admitted request returns its outcome without another read.
func (b *Bindings) StartHex(request HexRequest) HexResponse {
	response := hexResponse(request.SessionID, request.RequestID, request.Offset)
	id, validID := parseStart(request.RequestID)
	offset, validOffset := parseStart(request.Offset)
	if !validID || id == 0 || !validOffset || request.Count > document.MaxHexBytes {
		response.Error = fault(CodeInvalidArgument)
		return response
	}
	owner, failure := b.app.hexSource(request.SessionID)
	if failure != nil {
		response.Error = failure
		return response
	}
	result, err := owner.StartHex(id, offset, request.Count)
	return mapHexResult(response, result, err)
}

// GetHex only queries bounded retained state. It never admits source I/O.
func (b *Bindings) GetHex(query HexQuery) HexResponse {
	response := hexResponse(query.SessionID, query.RequestID, "")
	id, valid := parseStart(query.RequestID)
	if !valid || id == 0 {
		response.Error = fault(CodeInvalidArgument)
		return response
	}
	owner, failure := b.app.hexSource(query.SessionID)
	if failure != nil {
		response.Error = failure
		return response
	}
	result, err := owner.GetHex(id)
	return mapHexResult(response, result, err)
}

func mapHexResult(response HexResponse, result document.HexResult, err error) HexResponse {
	if err != nil {
		response.Error = hexFailure(err)
		return response
	}
	response.RequestID, response.Offset = decimal(result.RequestID), decimal(result.Offset)
	response.Count, response.State = result.Count, result.State
	if result.Window != nil {
		response.Window = &HexWindow{Offset: decimal(result.Window.Offset), SourceSize: decimal(result.Window.SourceSize), Hex: hex.EncodeToString(result.Window.Bytes), EOF: result.Window.EOF}
	}
	if result.Err != nil {
		response.Error = hexFailure(result.Err)
	}
	return response
}

func hexFailure(err error) *Fault {
	if errors.Is(err, document.ErrUnknownRequest) {
		return hexFault(CodeHexNotFound)
	}
	failure := detailFailure(err)
	if failure.Code == CodeDetailFailed {
		return hexFault(CodeHexFailed)
	}
	return failure
}

func hexFault(code string) *Fault {
	message := "The source byte window could not be read."
	switch code {
	case CodeHexNotFound:
		message = "The byte window request has not been admitted."
	case CodeHexUnsupported:
		message = "This session does not support source byte windows."
	}
	return &Fault{Code: code, Message: message}
}
