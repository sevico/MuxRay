package avc

import (
	"errors"
	"fmt"
	"io"

	"github.com/sevico/muxray/internal/cursor"
)

// ParseDecoderConfigurationRecord parses one AVCDecoderConfigurationRecord.
// All spans and diagnostics use absolute source positions. Malformed syntax
// returns a partial result; invalid arguments and ReaderAt failures are Go
// errors.
func ParseDecoderConfigurationRecord(source io.ReaderAt, payload cursor.Span, options Options) (ConfigurationRecord, error) {
	result := ConfigurationRecord{Span: payload}
	start, end, normalized, err := validateInput(source, payload, options)
	if err != nil {
		return result, err
	}
	p := configurationParser{
		source: source, start: start, end: end, position: start, options: normalized,
		diagnostics: newDiagnosticCollector(normalized.MaxDiagnostics, end), result: result,
		budget: newParseBudget(normalized),
	}
	p.parse()
	p.finish()
	if p.err != nil {
		return p.result, p.err
	}
	return p.result, nil
}

type configurationParser struct {
	source        io.ReaderAt
	start         uint64
	end           uint64
	position      uint64
	options       Options
	diagnostics   *diagnosticCollector
	result        ConfigurationRecord
	framingFatal  bool
	err           error
	parameterSets uint32
	budget        *parseBudget
}

func (p *configurationParser) parse() {
	value, span, ok := p.readU8(CodeRecordHeaderTruncated, "configurationVersion is truncated")
	p.result.ConfigurationVersion, p.result.ConfigurationVersionSpan = value, span
	if !ok {
		return
	}
	if value != 1 {
		p.fatal(Diagnostic{
			Code: CodeConfigurationVersionUnexpected, Severity: SeverityError,
			Offset: span.ByteOffset, Message: "configurationVersion should be 1",
			Expected: number(1), Actual: number(uint64(value)),
		})
		return
	}
	value, span, ok = p.readU8(CodeRecordHeaderTruncated, "AVCProfileIndication is truncated")
	p.result.AVCProfileIndication, p.result.AVCProfileIndicationSpan = value, span
	if !ok {
		return
	}
	value, span, ok = p.readU8(CodeRecordHeaderTruncated, "profile_compatibility is truncated")
	p.result.ProfileCompatibility, p.result.ProfileCompatibilitySpan = value, span
	if !ok {
		return
	}
	value, span, ok = p.readU8(CodeRecordHeaderTruncated, "AVCLevelIndication is truncated")
	p.result.AVCLevelIndication, p.result.AVCLevelIndicationSpan = value, span
	if !ok {
		return
	}

	value, span, ok = p.readU8(CodeRecordHeaderTruncated, "lengthSizeMinusOne byte is truncated")
	if !ok {
		return
	}
	p.result.LengthSizeReserved = value >> 2
	p.result.LengthSizeReservedSpan = cursor.Span{ByteOffset: span.ByteOffset, BitLength: 6}
	p.result.LengthSizeMinusOne = value & 0x03
	p.result.LengthSizeMinusOneSpan = cursor.Span{ByteOffset: span.ByteOffset, BitOffset: 6, BitLength: 2}
	if p.result.LengthSizeReserved != 0x3f {
		p.reservedWarning(p.result.LengthSizeReservedSpan, 0x3f, uint64(p.result.LengthSizeReserved))
	}
	if p.result.LengthSizeMinusOne == 2 {
		p.diagnostics.add(Diagnostic{
			Code: CodeLengthSizeReserved, Severity: SeverityError,
			Offset: p.result.LengthSizeMinusOneSpan.ByteOffset, BitOffset: 6,
			Message: "lengthSizeMinusOne value 2 is reserved", Actual: number(2),
			RecoveryOffset: number(p.end),
		})
	} else {
		p.result.LengthSize = p.result.LengthSizeMinusOne + 1
	}

	value, span, ok = p.readU8(CodeParameterSetCountTruncated, "SPS count byte is truncated")
	if !ok {
		return
	}
	p.result.SPSCountReserved = value >> 5
	p.result.SPSCountReservedSpan = cursor.Span{ByteOffset: span.ByteOffset, BitLength: 3}
	p.result.NumSequenceParameterSets = value & 0x1f
	p.result.NumSequenceParameterSetsSpan = cursor.Span{ByteOffset: span.ByteOffset, BitOffset: 3, BitLength: 5}
	if p.result.SPSCountReserved != 0x07 {
		p.reservedWarning(p.result.SPSCountReservedSpan, 0x07, uint64(p.result.SPSCountReserved))
	}
	p.result.SequenceParameterSets = p.readParameterSets(uint32(p.result.NumSequenceParameterSets), NALUnitTypeSequenceParameterSet, "SPS")
	if p.framingFatal || p.err != nil {
		return
	}

	value, span, ok = p.readU8(CodeParameterSetCountTruncated, "PPS count is truncated")
	p.result.NumPictureParameterSets, p.result.NumPictureParameterSetsSpan = value, span
	if !ok {
		return
	}
	p.result.PictureParameterSets = p.readParameterSets(uint32(value), NALUnitTypePictureParameterSet, "PPS")
	if p.framingFatal || p.err != nil {
		return
	}

	if configurationHasExtension(p.result.AVCProfileIndication) {
		p.result.HighProfileFieldsPresent = true
		value, span, ok = p.readU8(CodeHighProfileExtensionTruncated, "chroma_format extension byte is truncated")
		if !ok {
			return
		}
		p.result.ChromaFormatReserved = value >> 2
		p.result.ChromaFormatReservedSpan = cursor.Span{ByteOffset: span.ByteOffset, BitLength: 6}
		p.result.ChromaFormat = value & 0x03
		p.result.ChromaFormatSpan = cursor.Span{ByteOffset: span.ByteOffset, BitOffset: 6, BitLength: 2}
		if p.result.ChromaFormatReserved != 0x3f {
			p.reservedWarning(p.result.ChromaFormatReservedSpan, 0x3f, uint64(p.result.ChromaFormatReserved))
		}
		value, span, ok = p.readU8(CodeHighProfileExtensionTruncated, "bit_depth_luma_minus8 extension byte is truncated")
		if !ok {
			return
		}
		p.result.BitDepthLumaReserved = value >> 3
		p.result.BitDepthLumaReservedSpan = cursor.Span{ByteOffset: span.ByteOffset, BitLength: 5}
		p.result.BitDepthLumaMinus8 = value & 0x07
		p.result.BitDepthLumaMinus8Span = cursor.Span{ByteOffset: span.ByteOffset, BitOffset: 5, BitLength: 3}
		if p.result.BitDepthLumaReserved != 0x1f {
			p.reservedWarning(p.result.BitDepthLumaReservedSpan, 0x1f, uint64(p.result.BitDepthLumaReserved))
		}
		if p.result.BitDepthLumaMinus8 > 4 {
			p.diagnostics.add(Diagnostic{
				Code: CodeConfigurationBitDepthInvalid, Severity: SeverityError,
				Offset: p.result.BitDepthLumaMinus8Span.ByteOffset, BitOffset: p.result.BitDepthLumaMinus8Span.BitOffset,
				Message: "avcC bit_depth_luma_minus8 exceeds 4", Expected: number(4),
				Actual: number(uint64(p.result.BitDepthLumaMinus8)), RecoveryOffset: number(p.end),
			})
		}
		value, span, ok = p.readU8(CodeHighProfileExtensionTruncated, "bit_depth_chroma_minus8 extension byte is truncated")
		if !ok {
			return
		}
		p.result.BitDepthChromaReserved = value >> 3
		p.result.BitDepthChromaReservedSpan = cursor.Span{ByteOffset: span.ByteOffset, BitLength: 5}
		p.result.BitDepthChromaMinus8 = value & 0x07
		p.result.BitDepthChromaMinus8Span = cursor.Span{ByteOffset: span.ByteOffset, BitOffset: 5, BitLength: 3}
		if p.result.BitDepthChromaReserved != 0x1f {
			p.reservedWarning(p.result.BitDepthChromaReservedSpan, 0x1f, uint64(p.result.BitDepthChromaReserved))
		}
		if p.result.BitDepthChromaMinus8 > 4 {
			p.diagnostics.add(Diagnostic{
				Code: CodeConfigurationBitDepthInvalid, Severity: SeverityError,
				Offset: p.result.BitDepthChromaMinus8Span.ByteOffset, BitOffset: p.result.BitDepthChromaMinus8Span.BitOffset,
				Message: "avcC bit_depth_chroma_minus8 exceeds 4", Expected: number(4),
				Actual: number(uint64(p.result.BitDepthChromaMinus8)), RecoveryOffset: number(p.end),
			})
		}
		value, span, ok = p.readU8(CodeHighProfileExtensionTruncated, "SPS extension count is truncated")
		p.result.NumSequenceParameterSetExt, p.result.NumSequenceParameterSetExtSpan = value, span
		if !ok {
			return
		}
		p.result.SequenceParameterSetExts = p.readParameterSets(uint32(value), NALUnitTypeSequenceParameterSetExt, "SPS extension")
	}
}

func (p *configurationParser) readParameterSets(count uint32, expected NALUnitType, label string) []ParameterSet {
	capacity := count
	remainingBudget := p.options.MaxParameterSets - minUint32(p.parameterSets, p.options.MaxParameterSets)
	if capacity > remainingBudget {
		capacity = remainingBudget
	}
	items := make([]ParameterSet, 0, capacity)
	limitReported := false
	for index := uint32(0); index < count; index++ {
		length, lengthSpan, ok := p.readU16(CodeParameterSetLengthTruncated, label+" length is truncated")
		if !ok {
			return items
		}
		available := p.end - p.position
		if uint64(length) > available {
			span := cursor.Span{ByteOffset: p.position, BitLength: uint32(available * 8)}
			if p.parameterSets < p.options.MaxParameterSets {
				items = append(items, ParameterSet{Index: index, Length: uint32(length), LengthSpan: lengthSpan, Span: span})
				p.parameterSets++
			}
			p.position = p.end
			p.fatal(Diagnostic{
				Code: CodeParameterSetDataTruncated, Severity: SeverityError,
				Offset: p.end, Message: label + " data is truncated",
				Expected: number(uint64(length)), Actual: number(available),
			})
			return items
		}
		span := cursor.Span{ByteOffset: p.position, BitLength: uint32(length) * 8}
		p.position += uint64(length)
		if p.parameterSets >= p.options.MaxParameterSets {
			if !limitReported {
				p.diagnostics.add(Diagnostic{
					Code: CodeParameterSetLimitReached, Severity: SeverityError,
					Offset: lengthSpan.ByteOffset, Message: "parameter-set count exceeds the configured limit",
					Expected: number(uint64(p.options.MaxParameterSets)), Actual: number(uint64(p.parameterSets + 1)),
					RecoveryOffset: number(p.end),
				})
				limitReported = true
			}
			continue
		}
		item := ParameterSet{Index: index, Length: uint32(length), LengthSpan: lengthSpan, Span: span}
		header, sps, pps, summaryComplete, err := parseParameterSetSummary(p.source, span, p.options, p.budget, p.diagnostics)
		item.Header, item.HeaderPresent = header, header.Complete
		item.SequenceParameterSet, item.PictureParameterSet = sps, pps
		item.SummaryComplete = summaryComplete
		items = append(items, item)
		p.parameterSets++
		if err != nil {
			p.err = fmt.Errorf("avc: parse %s %d: %w", label, index, err)
			return items
		}
		if header.Complete && header.NALUnitType != expected && header.NALUnitType != NALUnitTypeSEI {
			p.diagnostics.add(Diagnostic{
				Code: CodeParameterSetNALTypeUnexpected, Severity: SeverityWarning,
				Offset: header.NALUnitTypeSpan.ByteOffset, BitOffset: header.NALUnitTypeSpan.BitOffset,
				Message:  label + " array entry has an unexpected NAL unit type",
				Expected: number(uint64(expected)), Actual: number(uint64(header.NALUnitType)),
			})
		}
	}
	return items
}

func (p *configurationParser) readU8(code, message string) (uint8, cursor.Span, bool) {
	if p.framingFatal || p.err != nil {
		return 0, cursor.Span{}, false
	}
	if p.position >= p.end {
		p.fatal(Diagnostic{
			Code: code, Severity: SeverityError, Offset: p.end, Message: message,
			Expected: number(1), Actual: number(0),
		})
		return 0, cursor.Span{ByteOffset: p.end}, false
	}
	var buffer [1]byte
	if err := readExactAt(p.source, p.position, buffer[:]); err != nil {
		p.err = fmt.Errorf("avc: read byte at %d: %w", p.position, err)
		return 0, cursor.Span{}, false
	}
	span := cursor.Span{ByteOffset: p.position, BitLength: 8}
	p.position++
	return buffer[0], span, true
}

func (p *configurationParser) readU16(code, message string) (uint16, cursor.Span, bool) {
	if p.framingFatal || p.err != nil {
		return 0, cursor.Span{}, false
	}
	available := p.end - p.position
	if available < 2 {
		start := p.position
		p.position = p.end
		p.fatal(Diagnostic{
			Code: code, Severity: SeverityError, Offset: p.end, Message: message,
			Expected: number(2), Actual: number(available),
		})
		return 0, cursor.Span{ByteOffset: start, BitLength: uint32(available * 8)}, false
	}
	var buffer [2]byte
	if err := readExactAt(p.source, p.position, buffer[:]); err != nil {
		p.err = fmt.Errorf("avc: read uint16 at %d: %w", p.position, err)
		return 0, cursor.Span{}, false
	}
	span := cursor.Span{ByteOffset: p.position, BitLength: 16}
	p.position += 2
	return uint16(buffer[0])<<8 | uint16(buffer[1]), span, true
}

func (p *configurationParser) reservedWarning(span cursor.Span, expected, actual uint64) {
	p.warn(Diagnostic{
		Code: CodeReservedBitsUnexpected, Severity: SeverityWarning,
		Offset: span.ByteOffset, BitOffset: span.BitOffset,
		Message:  "reserved bits differ from their required all-ones value",
		Expected: number(expected), Actual: number(actual),
	})
}

func (p *configurationParser) warn(item Diagnostic) { p.diagnostics.add(item) }

func (p *configurationParser) fatal(item Diagnostic) {
	item.RecoveryOffset = number(p.end)
	p.diagnostics.add(item)
	p.framingFatal = true
}

func (p *configurationParser) finish() {
	p.result.BytesConsumed = p.position - p.start
	p.result.SyntaxSpan = cursor.Span{ByteOffset: p.start, BitLength: uint32(p.result.BytesConsumed * 8)}
	p.result.TrailingSpan = cursor.Span{ByteOffset: p.position, BitLength: uint32((p.end - p.position) * 8)}
	p.result.Diagnostics = p.diagnostics.items
	p.result.FramingComplete = p.err == nil && !p.framingFatal
	p.result.Complete = p.result.FramingComplete && !hasError(p.result.Diagnostics)
}

// ParseLengthPrefixedNALUnits parses one bounded sequence of AVC NAL units.
// lengthSize must be 1, 2, or 4 bytes and normally comes from an avcC record.
func ParseLengthPrefixedNALUnits(source io.ReaderAt, payload cursor.Span, lengthSize uint8, options Options) (NALUnitSequence, error) {
	result := NALUnitSequence{Span: payload, LengthSize: lengthSize}
	if lengthSize != 1 && lengthSize != 2 && lengthSize != 4 {
		return result, fmt.Errorf("avc: NAL length size %d is invalid; expected 1, 2, or 4", lengthSize)
	}
	start, end, normalized, err := validateInput(source, payload, options)
	if err != nil {
		return result, err
	}
	diagnostics := newDiagnosticCollector(normalized.MaxDiagnostics, end)
	budget := newParseBudget(normalized)
	parameterContext := normalized.ParameterSets.Clone()
	position := start
	framingComplete := true
	maximumPossible := (end - start) / uint64(lengthSize+1)
	capacity := maximumPossible
	if capacity > uint64(normalized.MaxNALUnits) {
		capacity = uint64(normalized.MaxNALUnits)
	}
	result.NALUnits = make([]NALUnit, 0, int(capacity))
	for position < end {
		if uint32(len(result.NALUnits)) >= normalized.MaxNALUnits {
			diagnostics.add(Diagnostic{
				Code: CodeNALUnitLimitReached, Severity: SeverityError,
				Offset: position, Message: "NAL-unit count exceeds the configured limit",
				Expected: number(uint64(normalized.MaxNALUnits)), Actual: number(uint64(len(result.NALUnits) + 1)),
				RecoveryOffset: number(end),
			})
			framingComplete = false
			break
		}
		available := end - position
		if available < uint64(lengthSize) {
			prefixStart := position
			position = end
			diagnostics.add(Diagnostic{
				Code: CodeNALLengthTruncated, Severity: SeverityError,
				Offset: end, Message: "NAL length prefix is truncated",
				Expected: number(uint64(lengthSize)), Actual: number(available), RecoveryOffset: number(end),
			})
			_ = prefixStart
			framingComplete = false
			break
		}
		var prefix [4]byte
		if err := readExactAt(source, position, prefix[:lengthSize]); err != nil {
			result.BytesConsumed = position - start
			result.SyntaxSpan = cursor.Span{ByteOffset: start, BitLength: uint32(result.BytesConsumed * 8)}
			result.TrailingSpan = cursor.Span{ByteOffset: position, BitLength: uint32((end - position) * 8)}
			result.Diagnostics = diagnostics.items
			return result, fmt.Errorf("avc: read NAL length at byte %d: %w", position, err)
		}
		lengthSpan := cursor.Span{ByteOffset: position, BitLength: uint32(lengthSize) * 8}
		var length uint32
		for index := uint8(0); index < lengthSize; index++ {
			length = length<<8 | uint32(prefix[index])
		}
		position += uint64(lengthSize)
		available = end - position
		if uint64(length) > available {
			span := cursor.Span{ByteOffset: position, BitLength: uint32(available * 8)}
			result.NALUnits = append(result.NALUnits, NALUnit{
				Index: uint32(len(result.NALUnits)), Length: length, LengthSpan: lengthSpan, Span: span,
			})
			position = end
			diagnostics.add(Diagnostic{
				Code: CodeNALPayloadTruncated, Severity: SeverityError,
				Offset: end, Message: "NAL payload is truncated",
				Expected: number(uint64(length)), Actual: number(available), RecoveryOffset: number(end),
			})
			framingComplete = false
			break
		}
		span := cursor.Span{ByteOffset: position, BitLength: length * 8}
		unit := NALUnit{Index: uint32(len(result.NALUnits)), Length: length, LengthSpan: lengthSpan, Span: span}
		if length == 0 {
			diagnostics.add(Diagnostic{
				Code: CodeNALUnitZeroLength, Severity: SeverityError,
				Offset: position, Message: "zero-length NAL unit has no header",
				Expected: number(1), Actual: number(0), RecoveryOffset: number(end),
			})
		} else {
			header, sps, pps, summaryComplete, parseErr := parseParameterSetSummary(source, span, normalized, budget, diagnostics)
			unit.Header, unit.HeaderPresent = header, header.Complete
			unit.SequenceParameterSet, unit.PictureParameterSet = sps, pps
			unit.SummaryComplete = summaryComplete
			if normalized.ParseSliceHeaders && parseErr == nil && header.Complete &&
				(header.NALUnitType == NALUnitTypeNonIDRSlice || header.NALUnitType == NALUnitTypeIDRSlice) {
				unit.SliceHeader, parseErr = parseSliceHeader(source, span, header, parameterContext, budget, diagnostics)
				unit.SummaryComplete = unit.SliceHeader != nil && unit.SliceHeader.PrefixComplete
			}
			if parseErr != nil {
				result.NALUnits = append(result.NALUnits, unit)
				position += uint64(length)
				result.BytesConsumed = position - start
				result.SyntaxSpan = cursor.Span{ByteOffset: start, BitLength: uint32(result.BytesConsumed * 8)}
				result.TrailingSpan = cursor.Span{ByteOffset: position, BitLength: uint32((end - position) * 8)}
				result.Diagnostics = diagnostics.items
				return result, fmt.Errorf("avc: parse NAL unit %d: %w", unit.Index, parseErr)
			}
		}
		if normalized.ParseSliceHeaders {
			parameterContext.AddNALUnit(unit)
		}
		result.NALUnits = append(result.NALUnits, unit)
		position += uint64(length)
	}
	result.BytesConsumed = position - start
	result.SyntaxSpan = cursor.Span{ByteOffset: start, BitLength: uint32(result.BytesConsumed * 8)}
	result.TrailingSpan = cursor.Span{ByteOffset: position, BitLength: uint32((end - position) * 8)}
	result.Diagnostics = diagnostics.items
	result.FramingComplete = framingComplete && position == end
	result.Complete = result.FramingComplete && !hasError(result.Diagnostics)
	return result, nil
}

func validateInput(source io.ReaderAt, payload cursor.Span, options Options) (uint64, uint64, Options, error) {
	if source == nil {
		return 0, 0, Options{}, errors.New("avc: nil source")
	}
	if err := payload.Validate(); err != nil {
		return 0, 0, Options{}, fmt.Errorf("avc: invalid payload span: %w", err)
	}
	if !payload.IsByteAligned() {
		return 0, 0, Options{}, errors.New("avc: payload span must be byte-aligned")
	}
	start, end, err := payload.ByteRange()
	if err != nil {
		return 0, 0, Options{}, fmt.Errorf("avc: payload byte range: %w", err)
	}
	if end-start > HardMaxPayloadBytes {
		return 0, 0, Options{}, fmt.Errorf("avc: payload length %d exceeds hard limit %d", end-start, HardMaxPayloadBytes)
	}
	normalized, err := options.Normalized()
	if err != nil {
		return 0, 0, Options{}, err
	}
	return start, end, normalized, nil
}

func minUint32(a, b uint32) uint32 {
	if a < b {
		return a
	}
	return b
}

func configurationHasExtension(profile uint8) bool {
	return profile != 66 && profile != 77 && profile != 88
}
