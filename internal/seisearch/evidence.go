package seisearch

import (
	"github.com/sevico/muxray/internal/avc"
	"github.com/sevico/muxray/internal/cursor"
	"github.com/sevico/muxray/internal/flv"
)

func coverageRow(tag flv.TagRecord, reason string) Row {
	span := tag.PayloadSpan()
	return Row{TagID: tag.ID, DTS: tag.DTS, Offset: tag.Offset, Kind: "coverage", Status: "unknown", ContextState: "not_applicable", Reason: reason, SourceSpan: &span}
}

func messageRows(tag flv.TagRecord, parsed flv.AVCVideo) ([]Row, bool) {
	var rows []Row
	incomplete := false
	visit := func(index uint32, header avc.NALHeader, present bool, sei *avc.SEIMessageSequence) {
		if !present || header.NALUnitType != avc.NALUnitTypeSEI {
			return
		}
		if sei == nil {
			incomplete = true
			return
		}
		incomplete = incomplete || !sei.FramingComplete
		for _, message := range sei.Messages {
			validHeader := !header.ForbiddenZeroBit && header.NALRefIDC == 0
			row := Row{TagID: tag.ID, DTS: tag.DTS, Offset: tag.Offset, NALIndex: copyPtr(&index), MessageIndex: copyPtr(&message.Index), Kind: message.Kind, Complete: message.Complete && validHeader, Status: "complete", ContextState: "not_applicable"}
			if message.PayloadType != nil {
				value := uint64(message.PayloadType.Value)
				row.PayloadType = &value
				addMappedSpan(&row.SourceSpan, message.PayloadType.Span)
			}
			if message.PayloadSize != nil {
				addMappedSpan(&row.SourceSpan, message.PayloadSize.Span)
			}
			if message.PayloadSpan != nil {
				addMappedSpan(&row.SourceSpan, *message.PayloadSpan)
			}
			var timing *avc.SEITimingContext
			if message.BufferingPeriod != nil {
				timing = &message.BufferingPeriod.Context
			} else if message.PictureTiming != nil {
				timing = &message.PictureTiming.Context
			}
			if timing != nil {
				row.ContextState, row.ContextSource = timing.State, timing.Source
				row.SPSID, row.SPSSpan, row.EvidenceSpan = copyPtr(timing.SPSID), copyPtr(timing.SPSSpan), copyPtr(timing.EvidenceSpan)
			}
			switch {
			case row.ContextState == "missing" || row.ContextState == "ambiguous":
				row.Status, row.Reason = "unknown", "timing_context_"+row.ContextState
			case !validHeader:
				row.Status, row.Reason = "incomplete", "sei_header_invalid"
			case !row.Complete:
				row.Status, row.Reason = "incomplete", "message_incomplete"
			case !message.SyntaxSupported:
				row.Status, row.Reason = "opaque", "unsupported_payload_type"
			}
			rows = append(rows, row)
		}
	}
	for _, unit := range parsed.NALUnits.NALUnits {
		visit(unit.Index, unit.Header, unit.HeaderPresent, unit.SEI)
	}
	// Config-array indices use SPS, PPS, then extension source order.
	index := uint32(0)
	for _, group := range [][]avc.ParameterSet{parsed.Config.SequenceParameterSets, parsed.Config.PictureParameterSets, parsed.Config.SequenceParameterSetExts} {
		for _, unit := range group {
			visit(index, unit.Header, unit.HeaderPresent, unit.SEI)
			index++
		}
	}
	return rows, incomplete
}

// A single physical envelope preserves original-source navigation without
// retaining an attacker-controlled list of RBSP mapping segments.
func addMappedSpan(envelope **cursor.Span, mapped avc.MappedSpan) {
	for _, span := range mapped.SourceSpans {
		if span.BitLength == 0 {
			continue
		}
		if *envelope == nil {
			*envelope = copyPtr(&span)
			continue
		}
		start := (*envelope).ByteOffset
		startBit := uint64((*envelope).BitOffset)
		if span.ByteOffset < start || span.ByteOffset == start && uint64(span.BitOffset) < startBit {
			start, startBit = span.ByteOffset, uint64(span.BitOffset)
		}
		end := ((*envelope).ByteOffset-start)*8 + uint64((*envelope).BitOffset) + uint64((*envelope).BitLength)
		end = max(end, (span.ByteOffset-start)*8+uint64(span.BitOffset)+uint64(span.BitLength))
		*envelope = &cursor.Span{ByteOffset: start, BitOffset: uint8(startBit), BitLength: uint32(end - startBit)}
	}
}

func limitReason(diagnostics []avc.Diagnostic) string {
	for _, d := range diagnostics {
		switch d.Code {
		case avc.CodeSEIByteLimit:
			return "sei_byte_limit"
		case avc.CodeSEIMessageLimit:
			return "sei_message_limit"
		case avc.CodeNALUnitLimitReached:
			return "nal_count_limit"
		case avc.CodeParameterSetLimitReached, avc.CodeParameterSetTooLarge:
			return "parameter_set_limit"
		case avc.CodeRBSPMappingLimitReached:
			return "mapping_limit"
		case avc.CodeDiagnosticLimitReached:
			return "diagnostic_limit"
		case avc.CodeSliceHeaderBudgetReached:
			return "slice_limit"
		}
	}
	return ""
}
