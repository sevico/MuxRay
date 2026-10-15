package document

import (
	"context"
	"strconv"

	"github.com/sevico/muxray/internal/cursor"
	"github.com/sevico/muxray/internal/mp3search"
	"github.com/sevico/muxray/internal/seisearch"
)

func (d *Document) searchCodec(ctx context.Context, task work, total uint64, complete bool) (*CodecSearchPage, *codecSearchCursor, error) {
	if task.codecSeed == nil {
		return nil, nil, ErrInvalidRequest
	}
	page := &CodecSearchPage{Kind: task.codec, Filter: task.filter, Cursor: task.offset, Total: total, ScanComplete: complete, Rows: []CodecSearchRow{}}
	next := &codecSearchCursor{id: task.id, kind: task.codec, filter: task.filter}
	switch task.codec {
	case "mp3":
		p, err := mp3search.Search(ctx, d.source, d.scan.Tag, task.codecSeed.mp3, total, complete, task.filter)
		if err != nil {
			return nil, nil, err
		}
		page.Start = CodecSearchPosition{TagID: p.Start.TagID, FrameIndex: p.Start.FrameIndex}
		page.Next = CodecSearchPosition{TagID: p.Next.TagID, FrameIndex: p.Next.FrameIndex}
		page.ScannedTags, page.ScannedFrames, page.BytesRead, page.Exhausted = p.ScannedTags, p.ScannedFrames, p.BytesRead, p.Exhausted
		next.mp3 = p.NextState
		for _, r := range p.Rows {
			frame := r.FrameIndex
			row := CodecSearchRow{TagID: r.TagID, Offset: r.Span.ByteOffset, DTS: r.DTS, FrameIndex: &frame, Kind: "stop", Status: r.Status, Complete: r.Status == "complete", Values: []CodecSearchValue{}}
			if r.Span.BitLength != 0 {
				span := r.Span
				row.Span = &span
			}
			if row.Complete {
				switch {
				case r.ParametersChangedFromPrevious && r.BitrateChangedFromPrevious:
					row.Kind = "parametersAndBitrate"
				case r.ParametersChangedFromPrevious:
					row.Kind = "parameters"
				default:
					row.Kind = "bitrate"
				}
			}
			if r.ParametersPresent {
				row.Values = append(row.Values, searchParameters("current", r.Parameters)...)
			}
			if r.BitratePresent {
				row.Values = append(row.Values, searchNumber("current.bitrateKbps", uint64(r.BitrateKbps)))
			}
			if r.ComparedToPrevious {
				row.Values = append(row.Values, searchParameters("previous", r.PreviousParameters)...)
				row.Values = append(row.Values, searchNumber("previous.bitrateKbps", uint64(r.PreviousBitrateKbps)))
			}
			page.Rows = append(page.Rows, row)
		}
	case "sei":
		p, err := seisearch.Search(ctx, d.source, d.scan.Tag, task.codecSeed.sei, total, complete, task.filter)
		if err != nil {
			return nil, nil, err
		}
		page.Start, page.Next = CodecSearchPosition{TagID: p.Start}, CodecSearchPosition{TagID: p.Next}
		page.ScannedTags, page.BytesRead, page.Exhausted, page.Pending = p.Scanned, p.BytesRead, p.Exhausted, p.Pending
		next.sei = p.State
		for _, r := range p.Rows {
			row := CodecSearchRow{TagID: r.TagID, Offset: r.Offset, DTS: r.DTS, NALIndex: r.NALIndex, MessageIndex: r.MessageIndex, Kind: r.Kind, Status: r.Status, ContextState: r.ContextState, Complete: r.Complete, Span: nonemptySearchSpan(r.SourceSpan), SPSSpan: nonemptySearchSpan(r.SPSSpan), EvidenceSpan: nonemptySearchSpan(r.EvidenceSpan), Values: []CodecSearchValue{}}
			if r.PayloadType != nil {
				row.Values = append(row.Values, searchNumber("payloadType", *r.PayloadType))
			}
			if r.SPSID != nil {
				row.Values = append(row.Values, searchNumber("spsId", uint64(*r.SPSID)))
			}
			if r.ContextSource != "" {
				row.Values = append(row.Values, CodecSearchValue{"contextSource", r.ContextSource})
			}
			if r.Reason != "" {
				row.Values = append(row.Values, CodecSearchValue{"reason", r.Reason})
			}
			page.Rows = append(page.Rows, row)
		}
	default:
		return nil, nil, ErrInvalidRequest
	}
	if page.Exhausted {
		next = nil
	} else {
		page.NextCursor = task.id
	}
	return page, next, nil
}

func nonemptySearchSpan(span *cursor.Span) *cursor.Span {
	if span == nil || span.BitLength == 0 {
		return nil
	}
	copy := *span
	return &copy
}
func searchNumber(name string, value uint64) CodecSearchValue {
	return CodecSearchValue{name, strconv.FormatUint(value, 10)}
}
func searchParameters(prefix string, p mp3search.Parameters) []CodecSearchValue {
	return []CodecSearchValue{
		{prefix + ".version", p.Version.String()}, searchNumber(prefix+".sampleRateHz", uint64(p.SamplingFrequencyHz)),
		{prefix + ".channelMode", p.ChannelMode.String()}, searchNumber(prefix+".channels", uint64(p.Channels)), searchNumber(prefix+".samplesPerFrame", uint64(p.SamplesPerFrame)),
	}
}
