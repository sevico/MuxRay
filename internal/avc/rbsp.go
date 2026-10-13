package avc

import (
	"fmt"
	"io"
	"math"

	"github.com/sevico/muxray/internal/cursor"
)

type rbspData struct {
	bytes         []byte
	sourceOffsets []uint64
	view          RBSPView
	valid         bool
	end           uint64
}

type parseBudget struct {
	mappingEntriesRemaining uint32
	sliceBytesRead          uint32
	seiBytesRead            uint32
	seiMessages             uint32
}

func newParseBudget(options Options) *parseBudget {
	return &parseBudget{mappingEntriesRemaining: options.MaxRBSPMappingEntries}
}

func (b *parseBudget) takeMappingEntry() bool {
	if b == nil || b.mappingEntriesRemaining == 0 {
		return false
	}
	b.mappingEntriesRemaining--
	return true
}

func decodeRBSP(source io.ReaderAt, nalSpan cursor.Span, options Options, budget *parseBudget, diagnostics *diagnosticCollector) (rbspData, error) {
	start, end, err := nalSpan.ByteRange()
	if err != nil {
		return rbspData{}, fmt.Errorf("avc: NAL byte range: %w", err)
	}
	data := rbspData{valid: true, end: end}
	if end-start <= 1 {
		data.view.EBSPSpan = cursor.Span{ByteOffset: end}
		return data, nil
	}
	ebspStart := start + 1
	ebspLength := end - ebspStart
	data.view.EBSPSpan = cursor.Span{ByteOffset: ebspStart, BitLength: uint32(ebspLength * 8)}
	if ebspLength > uint64(options.MaxParameterSetBytes) {
		diagnostics.add(Diagnostic{
			Code: CodeParameterSetTooLarge, Severity: SeverityError,
			Offset: ebspStart, Message: "parameter-set EBSP exceeds the configured summary budget",
			Expected: number(uint64(options.MaxParameterSetBytes)), Actual: number(ebspLength),
			RecoveryOffset: number(end),
		})
		data.valid = false
		return data, nil
	}

	ebsp := make([]byte, int(ebspLength))
	if err := readExactAt(source, ebspStart, ebsp); err != nil {
		return data, fmt.Errorf("avc: read parameter-set EBSP at byte %d: %w", ebspStart, err)
	}
	data.bytes = make([]byte, 0, len(ebsp))
	data.sourceOffsets = make([]uint64, 0, len(ebsp))
	zeroCount := 0
	for index, value := range ebsp {
		sourceOffset := ebspStart + uint64(index)
		if value == 0x03 && zeroCount >= 2 {
			if index+1 < len(ebsp) && ebsp[index+1] <= 0x03 {
				if !budget.takeMappingEntry() {
					return rbspMappingLimit(data, options, diagnostics, sourceOffset), nil
				}
				data.view.EmulationPreventionBytes = append(data.view.EmulationPreventionBytes, cursor.Span{
					ByteOffset: sourceOffset, BitLength: 8,
				})
				zeroCount = 0
				continue
			}
			diagnostics.add(Diagnostic{
				Code: CodeEmulationPreventionInvalid, Severity: SeverityError,
				Offset: sourceOffset, Message: "0x000003 is not followed by an EBSP byte in the range 0x00..0x03",
				RecoveryOffset: number(end),
			})
			data.valid = false
		}
		if value <= 0x02 && zeroCount >= 2 {
			diagnostics.add(Diagnostic{
				Code: CodeEmulationPreventionInvalid, Severity: SeverityError,
				Offset: sourceOffset, Message: "EBSP contains an unescaped 0x000000, 0x000001, or 0x000002 sequence",
				Actual: number(uint64(value)), RecoveryOffset: number(end),
			})
			data.valid = false
		}
		if len(data.sourceOffsets) == 0 || sourceOffset != data.sourceOffsets[len(data.sourceOffsets)-1]+1 {
			if !budget.takeMappingEntry() {
				return rbspMappingLimit(data, options, diagnostics, sourceOffset), nil
			}
		}

		data.bytes = append(data.bytes, value)
		data.sourceOffsets = append(data.sourceOffsets, sourceOffset)
		if value == 0 {
			zeroCount++
		} else {
			zeroCount = 0
		}
	}
	data.view.ByteLength = uint32(len(data.bytes))
	data.view.SourceSegments = sourceSegments(data.sourceOffsets)
	return data, nil
}

func rbspMappingLimit(data rbspData, options Options, diagnostics *diagnosticCollector, offset uint64) rbspData {
	diagnostics.add(Diagnostic{
		Code: CodeRBSPMappingLimitReached, Severity: SeverityError,
		Offset: offset, Message: "cumulative RBSP source-mapping entries exceed the configured limit",
		Expected: number(uint64(options.MaxRBSPMappingEntries)),
		Actual:   number(uint64(options.MaxRBSPMappingEntries) + 1), RecoveryOffset: number(data.end),
	})
	data.valid = false
	data.bytes = nil
	data.sourceOffsets = nil
	data.view.ByteLength = 0
	data.view.SourceSegments = nil
	data.view.EmulationPreventionBytes = nil
	return data
}

func sourceSegments(offsets []uint64) []RBSPSourceSegment {
	if len(offsets) == 0 {
		return nil
	}
	segments := make([]RBSPSourceSegment, 0, 1)
	runStart := 0
	for index := 1; index <= len(offsets); index++ {
		if index < len(offsets) && offsets[index] == offsets[index-1]+1 {
			continue
		}
		length := index - runStart
		segments = append(segments, RBSPSourceSegment{
			RBSPByteOffset: uint32(runStart), RBSPByteLength: uint32(length),
			SourceSpan: cursor.Span{ByteOffset: offsets[runStart], BitLength: uint32(length * 8)},
		})
		runStart = index
	}
	return segments
}

type rbspParser struct {
	data        rbspData
	bitPosition uint32
	diagnostics *diagnosticCollector
	fatal       bool
	invalid     bool
	// ensure is set only for on-demand slice prefixes. It makes at least
	// width bits available without inspecting bytes after the requested field.
	ensure func(width uint32) bool
}

func newRBSPParser(data rbspData, diagnostics *diagnosticCollector) *rbspParser {
	return &rbspParser{data: data, diagnostics: diagnostics}
}

func (p *rbspParser) remainingBits() uint32 {
	total := uint32(len(p.data.bytes) * 8)
	if p.bitPosition >= total {
		return 0
	}
	return total - p.bitPosition
}

func (p *rbspParser) readBits(width uint8) (uint64, MappedSpan, bool) {
	start := p.bitPosition
	if p.fatal {
		return 0, MappedSpan{RBSPBitOffset: start}, false
	}
	if p.ensure != nil && !p.ensure(uint32(width)) {
		return 0, MappedSpan{RBSPBitOffset: start}, false
	}
	available := p.remainingBits()
	if available < uint32(width) {
		p.bitPosition += available
		p.fail(CodeRBSPSyntaxTruncated, "RBSP field is truncated", uint64(width), uint64(available))
		return 0, MappedSpan{RBSPBitOffset: start}, false
	}
	var value uint64
	for count := uint8(0); count < width; count++ {
		byteIndex := p.bitPosition / 8
		bitOffset := p.bitPosition % 8
		bit := (p.data.bytes[byteIndex] >> (7 - bitOffset)) & 1
		value = value<<1 | uint64(bit)
		p.bitPosition++
	}
	return value, p.mappedSpan(start, uint32(width)), true
}

func (p *rbspParser) readUE() (uint32, MappedSpan, bool) {
	start := p.bitPosition
	if p.fatal {
		return 0, MappedSpan{RBSPBitOffset: start}, false
	}
	var leading uint32
	for {
		if p.ensure != nil && !p.ensure(1) {
			return 0, MappedSpan{RBSPBitOffset: start}, false
		}
		if p.remainingBits() == 0 {
			p.fail(CodeRBSPSyntaxTruncated, "unsigned Exp-Golomb code is truncated", 1, 0)
			return 0, MappedSpan{RBSPBitOffset: start}, false
		}
		value, _, ok := p.readBits(1)
		if !ok {
			return 0, MappedSpan{RBSPBitOffset: start}, false
		}
		if value != 0 {
			break
		}
		leading++
		if leading > 32 {
			p.fail(CodeExpGolombOverflow, "Exp-Golomb prefix exceeds uint32 representation", 32, uint64(leading))
			return 0, MappedSpan{RBSPBitOffset: start}, false
		}
	}
	var suffix uint64
	if leading != 0 {
		value, _, ok := p.readBits(uint8(leading))
		if !ok {
			return 0, MappedSpan{RBSPBitOffset: start}, false
		}
		suffix = value
	}
	codeNum := (uint64(1)<<leading - 1) + suffix
	if codeNum > math.MaxUint32 {
		p.fail(CodeExpGolombOverflow, "Exp-Golomb value exceeds uint32", math.MaxUint32, codeNum)
		return 0, MappedSpan{RBSPBitOffset: start}, false
	}
	return uint32(codeNum), p.mappedSpan(start, p.bitPosition-start), true
}

func (p *rbspParser) readSE() (int64, MappedSpan, bool) {
	value, span, ok := p.readUE()
	if !ok {
		return 0, span, false
	}
	if value&1 != 0 {
		return int64(value/2 + 1), span, true
	}
	return -int64(value / 2), span, true
}

func (p *rbspParser) mappedSpan(start, length uint32) MappedSpan {
	result := MappedSpan{RBSPBitOffset: start, BitLength: length}
	if length == 0 || len(p.data.sourceOffsets) == 0 {
		return result
	}
	position := start
	remaining := length
	for remaining > 0 {
		byteIndex := position / 8
		bitOffset := uint8(position % 8)
		take := uint32(8 - bitOffset)
		if take > remaining {
			take = remaining
		}
		piece := cursor.Span{
			ByteOffset: p.data.sourceOffsets[byteIndex], BitOffset: bitOffset, BitLength: take,
		}
		result.SourceSpans = appendMappedPiece(result.SourceSpans, piece)
		position += take
		remaining -= take
	}
	return result
}

func appendMappedPiece(spans []cursor.Span, piece cursor.Span) []cursor.Span {
	if len(spans) == 0 {
		return append(spans, piece)
	}
	last := &spans[len(spans)-1]
	endByte, endBit, err := last.End()
	if err == nil && endByte == piece.ByteOffset && endBit == piece.BitOffset &&
		uint64(last.BitLength)+uint64(piece.BitLength) <= math.MaxUint32 {
		last.BitLength += piece.BitLength
		return spans
	}
	return append(spans, piece)
}

func (p *rbspParser) fail(code, message string, expected, actual uint64) {
	offset, bitOffset := p.sourcePosition()
	p.diagnostics.add(Diagnostic{
		Code: code, Severity: SeverityError, Offset: offset, BitOffset: bitOffset,
		Message: message, Expected: number(expected), Actual: number(actual),
		RecoveryOffset: number(p.data.end),
	})
	p.fatal = true
}

func (p *rbspParser) addAt(span MappedSpan, item Diagnostic) {
	if len(span.SourceSpans) > 0 {
		item.Offset = span.SourceSpans[0].ByteOffset
		item.BitOffset = span.SourceSpans[0].BitOffset
	} else {
		item.Offset, item.BitOffset = p.sourcePosition()
	}
	if item.RecoveryOffset == nil && item.Severity == SeverityError {
		item.RecoveryOffset = number(p.data.end)
	}
	p.diagnostics.add(item)
	if item.Severity == SeverityError {
		p.invalid = true
	}
}

func (p *rbspParser) sourcePosition() (uint64, uint8) {
	if p.bitPosition < uint32(len(p.data.bytes)*8) {
		byteIndex := p.bitPosition / 8
		return p.data.sourceOffsets[byteIndex], uint8(p.bitPosition % 8)
	}
	return p.data.end, 0
}

func readExactAt(source io.ReaderAt, offset uint64, destination []byte) error {
	if len(destination) == 0 {
		return nil
	}
	if offset > math.MaxInt64 || uint64(len(destination)-1) > uint64(math.MaxInt64)-offset {
		return fmt.Errorf("ReaderAt range at %d with length %d exceeds int64 offsets", offset, len(destination))
	}
	read, err := source.ReadAt(destination, int64(offset))
	if read == len(destination) && (err == nil || err == io.EOF) {
		return nil
	}
	if err == nil {
		err = io.ErrUnexpectedEOF
	}
	return fmt.Errorf("read %d of %d bytes: %w", read, len(destination), err)
}
