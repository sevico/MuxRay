package desktop

import (
	"github.com/sevico/muxray/internal/cursor"
	"github.com/sevico/muxray/internal/document"
	"github.com/sevico/muxray/internal/flv"
	"github.com/sevico/muxray/internal/mp3search"
	"github.com/sevico/muxray/internal/seisearch"
)

// SnapshotCodecSearchRequest freezes one retained response. Cursor is an
// opaque server token, never a source offset or a client-authored parser state.
type SnapshotCodecSearchRequest struct {
	RequestID string `json:"requestId"`
	Cursor    string `json:"cursor"`
	Kind      string `json:"kind"`
	Filter    string `json:"filter"`
}

type snapshotCodecSearchSession interface {
	SnapshotCodecSearch(uint64) (document.CodecSearchResult, error)
}

func validSnapshotCodecSearch(q SnapshotCodecSearchRequest) bool {
	id, valid := parseStart(q.RequestID)
	_, cursorValid := parseStart(q.Cursor)
	return valid && id != 0 && cursorValid && document.ValidCodecSearch(q.Kind, q.Filter)
}

func (a *App) captureCodecSearch(e *slot, r *DiagnosticSnapshot, omit func(string, string)) {
	q := r.Selection.CodecSearch
	if q == nil {
		omit("codecSearchPage", "not_requested")
		return
	}
	owner, ok := e.scan.(snapshotCodecSearchSession)
	if !ok {
		omit("codecSearchPage", CodeCodecSearchUnsupported)
		return
	}
	id, _ := parseStart(q.RequestID)
	result, err := owner.SnapshotCodecSearch(id)
	response := mapCodecSearchResult(CodecSearchResponse{APIVersion: APIVersion, SessionID: e.id, RequestID: q.RequestID, State: "failed"}, result, err)
	if response.Error != nil {
		omit("codecSearchPage", response.Error.Code)
		return
	}
	if result.RequestID != id {
		omit("codecSearchPage", "identity_mismatch")
		return
	}
	if result.State != "ready" || result.Page == nil {
		omit("codecSearchPage", CodeNotReady)
		return
	}
	token, _ := parseStart(q.Cursor)
	total, _ := parseStart(r.Session.Progress.TagCount)
	size, _ := parseStart(r.Session.Progress.SourceSize)
	p := result.Page
	if p.Cursor != token || p.Kind != q.Kind || p.Filter != q.Filter || p.Total != total || p.ScanComplete != r.Session.Scan.Complete || !validSnapshotCodecPage(p, id, size) {
		omit("codecSearchPage", "identity_mismatch")
		return
	}
	// Index lookups are bounded in-memory reads, not source Stat/ReadAt calls.
	for _, row := range p.Rows {
		page, err := e.scan.Page(row.TagID, 1)
		if err != nil || len(page.Records) != 1 || !validSnapshotCodecTag(p.Kind, row, page.Records[0]) {
			omit("codecSearchPage", "identity_mismatch")
			return
		}
	}
	r.CodecSearchPage = &response
}

func codecPositionLess(a, b document.CodecSearchPosition) bool {
	return a.TagID < b.TagID || a.TagID == b.TagID && a.FrameIndex < b.FrameIndex
}

func validSnapshotCodecPage(p *document.CodecSearchPage, requestID, size uint64) bool {
	if !document.ValidCodecSearch(p.Kind, p.Filter) || p.Total > flv.HardMaxTags || p.Start.TagID > p.Next.TagID || p.Next.TagID > p.Total || p.ScannedTags > 256 || len(p.Rows) > 64 || p.Start.TagID == p.Total && p.Start.FrameIndex != 0 || p.Next.TagID == p.Total && p.Next.FrameIndex != 0 || p.Exhausted != (p.Next.TagID == p.Total && p.Pending == 0) || p.Exhausted && p.NextCursor != 0 || !p.Exhausted && p.NextCursor != requestID {
		return false
	}
	if p.Kind == "mp3" {
		if p.Pending != 0 || p.BytesRead > mp3search.MaxBytes || p.ScannedFrames > mp3search.MaxScanFrames || codecPositionLess(p.Next, p.Start) || !p.Exhausted && p.Start == p.Next || uint64(p.ScannedTags) < p.Next.TagID-p.Start.TagID || uint64(p.ScannedTags) > p.Next.TagID-p.Start.TagID+1 || len(p.Rows) > int(p.ScannedFrames+p.ScannedTags) {
			return false
		}
	} else if p.Start.FrameIndex != 0 || p.Next.FrameIndex != 0 || p.ScannedFrames != 0 || p.BytesRead > seisearch.MaxBytes || p.Pending > uint32(seisearch.MaxPending) || p.Next.TagID-p.Start.TagID > uint64(p.ScannedTags)+1 || uint64(p.ScannedTags) > p.Next.TagID-p.Start.TagID+1 || !p.Exhausted && p.Start == p.Next && len(p.Rows) == 0 && p.ScannedTags == 0 {
		return false
	}
	var previous document.CodecSearchRow
	for i, row := range p.Rows {
		if row.TagID < p.Start.TagID || row.TagID >= p.Total || row.TagID > p.Next.TagID || row.Offset > size || len(row.Values) > 12 || row.DTS < 0 || row.DTS > 1<<32-1 {
			return false
		}
		for _, span := range []*cursor.Span{row.Span, row.SPSSpan, row.EvidenceSpan} {
			if span != nil && (span.BitLength == 0 || span.Validate() != nil || span.ByteOffset > size || (uint64(span.BitOffset)+uint64(span.BitLength)+7)/8 > size-span.ByteOffset) {
				return false
			}
		}
		seen := make(map[string]bool, len(row.Values))
		for _, v := range row.Values {
			if v.Name == "" || len(v.Name) > 64 || len(v.Value) > 256 || seen[v.Name] {
				return false
			}
			seen[v.Name] = true
		}
		if p.Kind == "mp3" {
			if row.FrameIndex == nil || row.NALIndex != nil || row.MessageIndex != nil || row.SPSSpan != nil || row.EvidenceSpan != nil || row.ContextState != "" {
				return false
			}
			position := document.CodecSearchPosition{TagID: row.TagID, FrameIndex: *row.FrameIndex}
			if codecPositionLess(position, p.Start) || !codecPositionLess(position, p.Next) || i > 0 && !codecPositionLess(document.CodecSearchPosition{TagID: previous.TagID, FrameIndex: *previous.FrameIndex}, position) || !validSnapshotMP3Row(row, p.Filter) {
				return false
			}
		} else {
			if row.FrameIndex != nil || row.TagID == p.Next.TagID && p.Pending == 0 || i > 0 && row.TagID < previous.TagID || !validSnapshotSEIRow(row, p.Filter) {
				return false
			}
			if i > 0 && row.TagID == previous.TagID && previous.Kind == "coverage" || i > 0 && row.TagID == previous.TagID && row.Kind != "coverage" && previous.NALIndex != nil && (row.NALIndex == nil || *row.NALIndex < *previous.NALIndex || *row.NALIndex == *previous.NALIndex && *row.MessageIndex <= *previous.MessageIndex) {
				return false
			}
		}
		previous = row
	}
	return true
}

func validSnapshotMP3Row(row document.CodecSearchRow, filter string) bool {
	if row.Complete != (row.Status == "complete") {
		return false
	}
	if row.Complete {
		if row.Span == nil || row.Kind != "parameters" && row.Kind != "bitrate" && row.Kind != "parametersAndBitrate" {
			return false
		}
		values := codecSnapshotValues(row)
		if len(values) != 12 {
			return false
		}
		parameters := false
		for _, name := range []string{"version", "sampleRateHz", "channelMode", "channels", "samplesPerFrame", "bitrateKbps"} {
			before, after := values["previous."+name], values["current."+name]
			if before == "" || after == "" {
				return false
			}
			if name != "bitrateKbps" && before != after {
				parameters = true
			}
		}
		bitrate := values["previous.bitrateKbps"] != values["current.bitrateKbps"]
		kind := ""
		switch {
		case parameters && bitrate:
			kind = "parametersAndBitrate"
		case parameters:
			kind = "parameters"
		case bitrate:
			kind = "bitrate"
		}
		return row.Kind == kind && (filter == "all" || filter == "parameters" && parameters || filter == "bitrate" && bitrate)
	}
	if row.Kind != "stop" || filter != "all" && filter != "stops" {
		return false
	}
	switch row.Status {
	case "truncatedHeader", "malformedHeader", "unsupportedLayer", "freeFormat", "truncatedFrame", "filteredAudio", "truncatedAudio", "missingAudioHeader", "unsupportedAudio":
		return true
	}
	return false
}

func validSnapshotSEIRow(row document.CodecSearchRow, filter string) bool {
	if row.Status != "complete" && row.Status != "incomplete" && row.Status != "unknown" && row.Status != "opaque" {
		return false
	}
	if row.ContextState != "resolved" && row.ContextState != "missing" && row.ContextState != "ambiguous" && row.ContextState != "not_applicable" {
		return false
	}
	if row.Kind == "coverage" {
		return !row.Complete && row.Status == "unknown" && row.NALIndex == nil && row.MessageIndex == nil && codecSnapshotValues(row)["reason"] != ""
	}
	if (row.ContextState == "missing" || row.ContextState == "ambiguous") && row.Status != "unknown" || row.Status == "opaque" && row.Kind != "unknown" {
		return false
	}
	if row.NALIndex == nil || row.MessageIndex == nil || *row.MessageIndex >= 64 || row.Complete != (row.Status == "complete" || row.Status == "opaque") || row.ContextState == "resolved" && (row.SPSSpan == nil || row.EvidenceSpan == nil) || (row.ContextState == "missing" || row.ContextState == "ambiguous") && row.Complete {
		return false
	}
	if row.Kind != "bufferingPeriod" && row.Kind != "pictureTiming" && row.Kind != "recoveryPoint" && row.Kind != "userDataUnregistered" && row.Kind != "unknown" {
		return false
	}
	values := codecSnapshotValues(row)
	if row.Complete && (row.Span == nil || values["payloadType"] == "") {
		return false
	}
	if value, ok := values["payloadType"]; ok {
		payload, valid := parseStart(value)
		kind := map[uint64]string{0: "bufferingPeriod", 1: "pictureTiming", 5: "userDataUnregistered", 6: "recoveryPoint"}[payload]
		if kind == "" {
			kind = "unknown"
		}
		if !valid || payload > 1<<32-1 || kind != row.Kind {
			return false
		}
	}
	if row.ContextState == "resolved" {
		sps, valid := parseStart(values["spsId"])
		if !valid || sps > 31 || values["contextSource"] != "bufferingPeriod" && values["contextSource"] != "slice" {
			return false
		}
	}
	return filter == "all" || filter != "unknown" && filter == row.Kind || filter == "unknown" && (!row.Complete || row.ContextState == "missing" || row.ContextState == "ambiguous")
}

func codecSnapshotValues(row document.CodecSearchRow) map[string]string {
	values := make(map[string]string, len(row.Values))
	for _, item := range row.Values {
		values[item.Name] = item.Value
	}
	return values
}

func validSnapshotCodecTag(kind string, row document.CodecSearchRow, tag flv.TagRecord) bool {
	if tag.ID != row.TagID || tag.DTS != row.DTS || kind == "mp3" && tag.Type() != flv.TagTypeAudio || kind == "sei" && (tag.Type() != flv.TagTypeVideo || row.Offset != tag.Offset) {
		return false
	}
	start, end := tag.PayloadSpan().ByteOffset, tag.PayloadSpan().ByteOffset+uint64(tag.AvailableDataSize)
	if kind == "mp3" && (row.Offset < start || row.Offset > end || row.Span != nil && row.Offset != row.Span.ByteOffset) {
		return false
	}
	return row.Span == nil || row.Span.ByteOffset >= start && row.Span.ByteOffset <= end && (uint64(row.Span.BitOffset)+uint64(row.Span.BitLength)+7)/8 <= end-row.Span.ByteOffset
}
