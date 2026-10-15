package desktop

import (
	"errors"
	"strconv"

	"github.com/sevico/muxray/internal/cursor"
	"github.com/sevico/muxray/internal/detailview"
	"github.com/sevico/muxray/internal/document"
	"github.com/sevico/muxray/internal/mp3search"
	"github.com/sevico/muxray/internal/seisearch"
)

const (
	CodeCodecSearchNotFound      = "codec_search_not_found"
	CodeCodecSearchFailed        = "codec_search_failed"
	CodeCodecSearchCursorExpired = "codec_search_cursor_expired"
	CodeCodecSearchUnsupported   = "codec_search_unsupported"
)

type CodecSearchRequest struct {
	SessionID string `json:"sessionId"`
	RequestID string `json:"requestId"`
	Cursor    string `json:"cursor"`
	Kind      string `json:"kind"`
	Filter    string `json:"filter"`
}
type CodecSearchPosition struct {
	TagID      string `json:"tagId"`
	FrameIndex uint32 `json:"frameIndex"`
}
type CodecSearchValue struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}
type CodecSearchRow struct {
	TagID        string             `json:"tagId"`
	DTS          string             `json:"dts"`
	Offset       string             `json:"offset"`
	FrameIndex   *uint32            `json:"frameIndex"`
	NALIndex     *uint32            `json:"nalIndex"`
	MessageIndex *uint32            `json:"messageIndex"`
	Kind         string             `json:"kind"`
	Status       string             `json:"status"`
	ContextState string             `json:"contextState"`
	Complete     bool               `json:"complete"`
	Span         *detailview.Span   `json:"span"`
	SPSSpan      *detailview.Span   `json:"spsSpan"`
	EvidenceSpan *detailview.Span   `json:"evidenceSpan"`
	Values       []CodecSearchValue `json:"values"`
}
type CodecSearchPage struct {
	Kind          string              `json:"kind"`
	Filter        string              `json:"filter"`
	Cursor        string              `json:"cursor"`
	NextCursor    string              `json:"nextCursor"`
	Start         CodecSearchPosition `json:"start"`
	Next          CodecSearchPosition `json:"next"`
	Total         string              `json:"total"`
	ScannedTags   uint32              `json:"scannedTags"`
	ScannedFrames uint32              `json:"scannedFrames"`
	Pending       uint32              `json:"pending"`
	BytesRead     string              `json:"bytesRead"`
	Exhausted     bool                `json:"exhausted"`
	ScanComplete  bool                `json:"scanComplete"`
	Rows          []CodecSearchRow    `json:"rows"`
}
type CodecSearchResponse struct {
	APIVersion uint8            `json:"apiVersion"`
	SessionID  string           `json:"sessionId"`
	RequestID  string           `json:"requestId"`
	State      string           `json:"state"`
	Page       *CodecSearchPage `json:"page"`
	Error      *Fault           `json:"error"`
}
type codecSearchSession interface {
	StartCodecSearch(uint64, uint64, string, string) (document.CodecSearchResult, error)
	GetCodecSearch(uint64) (document.CodecSearchResult, error)
	CancelCodecSearch(uint64) (document.CodecSearchResult, error)
}

func (a *App) codecSearchSource(id string) (codecSearchSession, *Fault) {
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
	owner, ok := e.scan.(codecSearchSession)
	if !ok {
		return nil, codecSearchFault(CodeCodecSearchUnsupported)
	}
	return owner, nil
}
func (b *Bindings) StartCodecSearch(q CodecSearchRequest) CodecSearchResponse {
	return b.codecSearchOperation(q, "start")
}
func (b *Bindings) GetCodecSearch(q DetailQuery) CodecSearchResponse {
	return b.codecSearchOperation(CodecSearchRequest{SessionID: q.SessionID, RequestID: q.RequestID}, "get")
}
func (b *Bindings) CancelCodecSearch(q DetailQuery) CodecSearchResponse {
	return b.codecSearchOperation(CodecSearchRequest{SessionID: q.SessionID, RequestID: q.RequestID}, "cancel")
}
func (b *Bindings) codecSearchOperation(q CodecSearchRequest, op string) CodecSearchResponse {
	base := detailResponse(q.SessionID, q.RequestID, "")
	response := CodecSearchResponse{APIVersion: APIVersion, SessionID: base.SessionID, RequestID: base.RequestID, State: "failed"}
	id, valid := parseStart(q.RequestID)
	start, startValid := parseStart(q.Cursor)
	if !valid || id == 0 || op == "start" && (!startValid || !document.ValidCodecSearch(q.Kind, q.Filter)) {
		response.Error = fault(CodeInvalidArgument)
		return response
	}
	owner, failure := b.app.codecSearchSource(q.SessionID)
	if failure != nil {
		response.Error = failure
		return response
	}
	var result document.CodecSearchResult
	var err error
	switch op {
	case "start":
		result, err = owner.StartCodecSearch(id, start, q.Kind, q.Filter)
	case "cancel":
		result, err = owner.CancelCodecSearch(id)
	default:
		result, err = owner.GetCodecSearch(id)
	}
	return mapCodecSearchResult(response, result, err)
}

func mapCodecSearchResult(response CodecSearchResponse, result document.CodecSearchResult, err error) CodecSearchResponse {
	if err != nil {
		response.Error = codecSearchFailure(err)
		return response
	}
	response.RequestID = decimal(result.RequestID)
	response.State = result.State
	if result.Err != nil {
		response.Error = codecSearchFailure(result.Err)
	}
	if result.Page != nil {
		p := result.Page
		page := &CodecSearchPage{Kind: p.Kind, Filter: p.Filter, Cursor: decimal(p.Cursor), NextCursor: decimal(p.NextCursor), Start: CodecSearchPosition{decimal(p.Start.TagID), p.Start.FrameIndex}, Next: CodecSearchPosition{decimal(p.Next.TagID), p.Next.FrameIndex}, Total: decimal(p.Total), ScannedTags: p.ScannedTags, ScannedFrames: p.ScannedFrames, Pending: p.Pending, BytesRead: decimal(p.BytesRead), Exhausted: p.Exhausted, ScanComplete: p.ScanComplete, Rows: []CodecSearchRow{}}
		for _, r := range p.Rows {
			row := CodecSearchRow{TagID: decimal(r.TagID), DTS: strconv.FormatInt(r.DTS, 10), Offset: decimal(r.Offset), FrameIndex: r.FrameIndex, NALIndex: r.NALIndex, MessageIndex: r.MessageIndex, Kind: r.Kind, Status: r.Status, ContextState: r.ContextState, Complete: r.Complete, Values: []CodecSearchValue{}}
			for _, pair := range []struct {
				source *cursor.Span
				target **detailview.Span
			}{{r.Span, &row.Span}, {r.SPSSpan, &row.SPSSpan}, {r.EvidenceSpan, &row.EvidenceSpan}} {
				if pair.source != nil {
					*pair.target = &detailview.Span{ByteOffset: decimal(pair.source.ByteOffset), BitOffset: pair.source.BitOffset, BitLength: pair.source.BitLength}
				}
			}
			for _, value := range r.Values {
				row.Values = append(row.Values, CodecSearchValue{value.Name, value.Value})
			}
			page.Rows = append(page.Rows, row)
		}
		response.Page = page
	}
	return response
}
func codecSearchFailure(err error) *Fault {
	if errors.Is(err, document.ErrUnknownRequest) {
		return codecSearchFault(CodeCodecSearchNotFound)
	}
	if errors.Is(err, document.ErrUnknownCursor) {
		return codecSearchFault(CodeCodecSearchCursorExpired)
	}
	if errors.Is(err, mp3search.ErrInvalidInput) || errors.Is(err, seisearch.ErrInvalidInput) {
		return fault(CodeInvalidArgument)
	}
	failure := detailFailure(err)
	if failure.Code == CodeDetailFailed {
		return codecSearchFault(CodeCodecSearchFailed)
	}
	return failure
}
func codecSearchFault(code string) *Fault {
	message := "The bounded codec evidence search could not be produced."
	if code == CodeCodecSearchNotFound {
		message = "The codec search request has not been admitted."
	}
	if code == CodeCodecSearchUnsupported {
		message = "This session does not support codec evidence searches."
	}
	if code == CodeCodecSearchCursorExpired {
		message = "The codec search cursor has expired; restart from the beginning."
	}
	return &Fault{Code: code, Message: message}
}
