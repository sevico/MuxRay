package desktop

import (
	"errors"
	"math"

	"github.com/sevico/muxray/internal/document"
	"github.com/sevico/muxray/internal/flv"
)

type TagFilter struct {
	Type      string  `json:"type"`
	DTSMin    *string `json:"dtsMin"`
	DTSMax    *string `json:"dtsMax"`
	Structure string  `json:"structure"`
}

type FilteredTagRequest struct {
	SessionID string    `json:"sessionId"`
	Start     string    `json:"start"`
	Count     uint32    `json:"count"`
	Filter    TagFilter `json:"filter"`
}

type FilteredTagPage struct {
	SessionID    string    `json:"sessionId"`
	Filter       TagFilter `json:"filter"`
	Start        string    `json:"start"`
	Next         string    `json:"next"`
	IndexedTotal string    `json:"indexedTotal"`
	Limit        uint32    `json:"limit"`
	ScanLimit    uint32    `json:"scanLimit"`
	Scanned      uint32    `json:"scanned"`
	Exhausted    bool      `json:"exhausted"`
	Tags         []Tag     `json:"tags"`
}

type FilteredTagPageResponse struct {
	APIVersion uint8            `json:"apiVersion"`
	Page       *FilteredTagPage `json:"page"`
	Error      *Fault           `json:"error"`
}

func parseTagFilter(filter TagFilter) (flv.TagFilter, bool) {
	result := flv.TagFilter{Type: filter.Type, Structure: filter.Structure}
	for _, bound := range []struct {
		text *string
		out  **uint32
	}{{filter.DTSMin, &result.DTSMin}, {filter.DTSMax, &result.DTSMax}} {
		if bound.text == nil {
			continue
		}
		value, valid := parseStart(*bound.text)
		if !valid || value > math.MaxUint32 {
			return flv.TagFilter{}, false
		}
		converted := uint32(value)
		*bound.out = &converted
	}
	return result, result.Validate() == nil
}

func mapTagFilter(filter flv.TagFilter) TagFilter {
	result := TagFilter{Type: filter.Type, Structure: filter.Structure}
	if filter.DTSMin != nil {
		value := decimal(uint64(*filter.DTSMin))
		result.DTSMin = &value
	}
	if filter.DTSMax != nil {
		value := decimal(uint64(*filter.DTSMax))
		result.DTSMax = &value
	}
	return result
}

// GetFilteredTags returns at most 100 source-ordered matches after examining at
// most 4,096 index records. Next is the next source cursor, even for zero hits.
func (b *Bindings) GetFilteredTags(request FilteredTagRequest) FilteredTagPageResponse {
	response := FilteredTagPageResponse{APIVersion: APIVersion}
	filter, valid := parseTagFilter(request.Filter)
	if !valid || request.Count == 0 || request.Count > flv.MaxFilteredTags {
		response.Error = fault(CodeInvalidArgument)
		return response
	}
	scan, start, failure := b.app.pageSource(PageRequest{SessionID: request.SessionID, Start: request.Start, Count: request.Count})
	if failure != nil {
		response.Error = failure
		return response
	}
	page, err := scan.FilteredPage(start, request.Count, filter)
	if err != nil {
		if errors.Is(err, document.ErrSourceChanged) {
			response.Error = detailFault(CodeSourceChanged)
		} else {
			response.Error = pageFailure(err)
		}
		return response
	}
	view := &FilteredTagPage{SessionID: request.SessionID, Filter: mapTagFilter(filter), Start: decimal(page.Start), Next: decimal(page.Next), IndexedTotal: decimal(page.IndexedTotal), Limit: page.Limit, ScanLimit: page.ScanLimit, Scanned: page.Scanned, Exhausted: page.Exhausted, Tags: make([]Tag, 0, len(page.Records))}
	for _, record := range page.Records {
		view.Tags = append(view.Tags, mapTag(record))
	}
	response.Page = view
	return response
}
