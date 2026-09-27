package desktop

import (
	"errors"
	"strconv"

	"github.com/sevico/muxray/internal/document"
	"github.com/sevico/muxray/internal/flv"
)

type TagTrendPoint struct {
	TagID                string  `json:"tagId"`
	Offset               string  `json:"offset"`
	DTS                  string  `json:"dts"`
	TypeCode             uint8   `json:"typeCode"`
	StreamID             uint32  `json:"streamId"`
	DataSize             uint32  `json:"dataSize"`
	AvailableDataSize    uint32  `json:"availableDataSize"`
	PayloadComplete      bool    `json:"payloadComplete"`
	PreviousTagSizeState string  `json:"previousTagSizeState"`
	Filtered             bool    `json:"filtered"`
	PreviousTagID        *string `json:"previousTagId"`
	PreviousDTS          *string `json:"previousDts"`
	DeltaMS              *string `json:"deltaMs"`
	Context              string  `json:"context"`
}

type TagTrendPage struct {
	SessionID      string          `json:"sessionId"`
	Bounds         PageBounds      `json:"bounds"`
	ContextStart   string          `json:"contextStart"`
	ContextScanned uint32          `json:"contextScanned"`
	Points         []TagTrendPoint `json:"points"`
}

type TagTrendPageResponse struct {
	APIVersion uint8         `json:"apiVersion"`
	Page       *TagTrendPage `json:"page"`
	Error      *Fault        `json:"error"`
}

// GetTagTrends projects a continuous window of at most 512 records, with up to
// 4096 preceding records inspected only for same-type/StreamID DTS adjacency.
func (b *Bindings) GetTagTrends(request PageRequest) TagTrendPageResponse {
	response := TagTrendPageResponse{APIVersion: APIVersion}
	if request.Count == 0 || request.Count > flv.MaxTrendPoints {
		response.Error = fault(CodeInvalidArgument)
		return response
	}
	scan, start, failure := b.app.pageSource(request)
	if failure != nil {
		response.Error = failure
		return response
	}
	page, err := scan.TrendPage(start, request.Count)
	if err != nil {
		if errors.Is(err, document.ErrSourceChanged) {
			response.Error = detailFault(CodeSourceChanged)
		} else {
			response.Error = pageFailure(err)
		}
		return response
	}
	view := &TagTrendPage{SessionID: request.SessionID, Bounds: mapBounds(page.Start, page.Limit, page.Total, page.Next),
		ContextStart: decimal(page.ContextStart), ContextScanned: page.ContextScanned, Points: make([]TagTrendPoint, 0, len(page.Points))}
	for _, point := range page.Points {
		view.Points = append(view.Points, mapTrendPoint(point))
	}
	response.Page = view
	return response
}

func mapTrendPoint(point flv.TagTrendPoint) TagTrendPoint {
	return TagTrendPoint{TagID: decimal(point.TagID), Offset: decimal(point.Offset), DTS: strconv.FormatInt(point.DTS, 10),
		TypeCode: point.TypeCode, StreamID: point.StreamID, DataSize: point.DataSize, AvailableDataSize: point.AvailableDataSize,
		PayloadComplete: point.PayloadComplete, PreviousTagSizeState: point.PreviousTagSizeState, Filtered: point.Filtered,
		PreviousTagID: optionalDecimal(point.PreviousTagID), PreviousDTS: trendSigned(point.PreviousDTS), DeltaMS: trendSigned(point.DeltaMS), Context: point.Context}
}

func trendSigned(value *int64) *string {
	if value == nil {
		return nil
	}
	text := strconv.FormatInt(*value, 10)
	return &text
}
