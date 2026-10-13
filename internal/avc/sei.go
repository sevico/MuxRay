package avc

// Syntax references: ITU-T H.264 (08/2024), 7.3.2.3, D.1.6-D.1.7
// and D.2.6-D.2.7; FFmpeg's primary coded-bitstream implementations:
// https://ffmpeg.org/doxygen/9.0/cbs__h264__syntax__template_8c_source.html
// https://ffmpeg.org/doxygen/trunk/cbs__sei__syntax__template_8c_source.html

import (
	"encoding/hex"
	"fmt"
	"io"
	"math"
	"math/bits"

	"github.com/sevico/muxray/internal/cursor"
)

const (
	MaxSEINALBytes         uint32 = 64 << 10
	MaxSEIBytesPerParse    uint32 = 256 << 10
	MaxSEIMessagesPerNAL   uint32 = 64
	MaxSEIMessagesPerParse uint32 = 256
	MaxSEIDataPreviewBytes uint32 = 64

	CodeSEIByteLimit           = "AVC_SEI_BYTE_LIMIT_REACHED"
	CodeSEIMessageLimit        = "AVC_SEI_MESSAGE_LIMIT_REACHED"
	CodeSEIHeaderTruncated     = "AVC_SEI_HEADER_TRUNCATED"
	CodeSEIPayloadTruncated    = "AVC_SEI_PAYLOAD_TRUNCATED"
	CodeSEISyntaxInvalid       = "AVC_SEI_SYNTAX_INVALID"
	CodeSEITrailingBitsInvalid = "AVC_SEI_TRAILING_BITS_INVALID"
)

// SEIMessageSequence describes bounded SEI syntax evidence, not decoder state.
// FramingComplete requires every message boundary and the exact trailing bits.
// Complete additionally validates the supported payload syntax. Unknown kinds
// are retained as opaque payload spans, explicitly marked SyntaxSupported=false.
type SEIMessageSequence struct {
	// EBSPSpan covers the inspected source prefix, bounded by BytesRead.
	RBSP             RBSPView
	Messages         []SEIMessage
	RBSPTrailingBits *MappedSpan
	BytesRead        uint32
	SyntaxBits       uint32
	FramingComplete  bool
	Complete         bool
	timingValid      bool
}

type SEIMessage struct {
	Index                uint32
	PayloadType          *VUIUnsignedField
	PayloadSize          *VUIUnsignedField
	PayloadSpan          *MappedSpan
	Kind                 string
	SyntaxSupported      bool
	RecoveryPoint        *SEIRecoveryPoint
	BufferingPeriod      *SEIBufferingPeriod
	PictureTiming        *SEIPictureTiming
	UserDataUnregistered *SEIUserDataUnregistered
	PayloadComplete      bool
	Complete             bool
}

type SEIRecoveryPoint struct {
	RecoveryFrameCnt      *VUIUnsignedField
	ExactMatchFlag        *VUIBooleanField
	BrokenLinkFlag        *VUIBooleanField
	ChangingSliceGroupIDC *VUIUnsignedField
	PayloadExtensionBits  *MappedSpan
	PayloadAlignmentBits  *MappedSpan
}

// SEIHexBytes keeps arbitrary application data binary; Hex never interprets it
// as text or executable content. Span maps only the retained bytes.
type SEIHexBytes struct {
	Hex  string
	Span MappedSpan
}

type SEIUserDataUnregistered struct {
	UUID        *SEIHexBytes
	DataPreview *SEIHexBytes
	// DataBytes is the declared application payload length, excluding UUID.
	DataBytes        uint32
	PreviewTruncated bool
}

func parseSEI(source io.ReaderAt, span cursor.Span, header NALHeader, options Options, budget *parseBudget, diagnostics *diagnosticCollector) (*SEIMessageSequence, error) {
	r := &SEIMessageSequence{}
	data, read, err := readSEIRBSP(source, span, options, budget, diagnostics)
	r.RBSP, r.BytesRead = data.view, read
	if err != nil {
		return r, err
	}
	p := newRBSPParser(data, diagnostics)
	valid := data.valid && !header.ForbiddenZeroBit
	if header.NALRefIDC != 0 {
		diagnostics.add(Diagnostic{Code: CodeSEISyntaxInvalid, Severity: SeverityError, Offset: header.NALRefIDCSpan.ByteOffset, BitOffset: header.NALRefIDCSpan.BitOffset, Message: "SEI nal_ref_idc must be zero", Expected: number(0), Actual: number(uint64(header.NALRefIDC))})
		valid = false
	}
	for {
		// At byte-aligned SEI message boundaries, rbsp_trailing_bits is exactly
		// one final 0x80 byte. Never treat an unread prefix as the end of a NAL.
		if data.valid && p.remainingBits() == 8 && data.bytes[p.bitPosition/8] == 0x80 {
			if len(r.Messages) == 0 {
				p.fail(CodeSEISyntaxInvalid, "SEI RBSP requires at least one message", 1, 0)
				break
			}
			s := p.mappedSpan(p.bitPosition, 8)
			r.RBSPTrailingBits = &s
			p.bitPosition += 8
			r.FramingComplete = true
			break
		}
		if p.remainingBits() == 0 {
			if data.valid {
				p.fail(CodeSEITrailingBitsInvalid, "SEI RBSP trailing bits are missing", 8, 0)
			}
			break
		}
		if uint32(len(r.Messages)) >= MaxSEIMessagesPerNAL || budget.seiMessages >= MaxSEIMessagesPerParse {
			p.fail(CodeSEIMessageLimit, "SEI message count exceeds the per-NAL or cumulative limit", uint64(MaxSEIMessagesPerNAL), uint64(len(r.Messages)+1))
			break
		}
		m := SEIMessage{Index: uint32(len(r.Messages)), Kind: "unknown"}
		budget.seiMessages++
		m.PayloadType = p.seiHeaderValue()
		if m.PayloadType != nil {
			switch m.PayloadType.Value {
			case 0:
				if options.ParseSEITiming {
					m.Kind, m.SyntaxSupported = "bufferingPeriod", true
				}
			case 1:
				if options.ParseSEITiming {
					m.Kind, m.SyntaxSupported = "pictureTiming", true
				}
			case 5:
				m.Kind, m.SyntaxSupported = "userDataUnregistered", true
			case 6:
				m.Kind, m.SyntaxSupported = "recoveryPoint", true
			}
			m.PayloadSize = p.seiHeaderValue()
		}
		if m.PayloadSize == nil {
			r.Messages = append(r.Messages, m)
			break
		}
		start := p.bitPosition
		available := p.remainingBits() / 8
		length := m.PayloadSize.Value
		if length > available {
			length = available
		}
		if length != 0 {
			s := p.mappedSpan(start, length*8)
			m.PayloadSpan = &s
		}
		m.PayloadComplete = length == m.PayloadSize.Value
		payloadData := data
		payloadData.bytes = data.bytes[:start/8+length]
		payloadData.end = data.end
		if end := int(start/8 + length); end < len(data.sourceOffsets) {
			payloadData.end = data.sourceOffsets[end]
		}
		q := newRBSPParser(payloadData, diagnostics)
		q.bitPosition = start
		switch m.Kind {
		case "bufferingPeriod":
			m.BufferingPeriod = q.seiBufferingPeriod(m.PayloadComplete, options.ParameterSets, timingEvidence(&m))
			if options.ParameterSets != nil {
				options.ParameterSets.timingActive = nil
				if m.PayloadComplete && !q.fatal && !q.invalid && data.valid && !header.ForbiddenZeroBit && header.NALRefIDC == 0 {
					options.ParameterSets.timingActive = m.BufferingPeriod.Context.Parameters
					options.ParameterSets.timingEvidence = timingEvidence(&m)
				}
			}
		case "pictureTiming":
			m.PictureTiming = &SEIPictureTiming{Context: SEITimingContext{State: "missing"}}
			pending := pendingSEITiming{sequence: r, index: m.Index, parser: q, afterSlice: budget.timingSlice.seen}
			if c := options.ParameterSets; c != nil {
				for id, set := range c.sps {
					if set != nil {
						pending.sets[id] = set.Timing
					}
				}
				pending.active, pending.evidence = c.timingActive, c.timingEvidence
			}
			budget.pendingTiming = append(budget.pendingTiming, pending)
		case "recoveryPoint":
			m.RecoveryPoint = q.seiRecoveryPoint(m.PayloadComplete)
		case "userDataUnregistered":
			m.UserDataUnregistered = q.seiUserData(m.PayloadSize.Value)
		}
		m.Complete = m.PayloadComplete && !q.fatal && !q.invalid
		valid = valid && m.Complete
		p.bitPosition += length * 8
		r.Messages = append(r.Messages, m)
		if !m.PayloadComplete {
			p.fail(CodeSEIPayloadTruncated, "SEI declared payload exceeds the available inspected RBSP bytes", uint64(m.PayloadSize.Value), uint64(available))
			break
		}
	}
	r.SyntaxBits = p.bitPosition
	r.Complete = r.FramingComplete && valid && !p.fatal && !p.invalid
	r.timingValid = r.FramingComplete && data.valid && !header.ForbiddenZeroBit && header.NALRefIDC == 0
	if options.ParseSEITiming && options.ParameterSets != nil && (!r.FramingComplete || !data.valid || header.ForbiddenZeroBit || header.NALRefIDC != 0) {
		options.ParameterSets.timingActive = nil
	}
	return r, nil
}

func (p *rbspParser) seiHeaderValue() *VUIUnsignedField {
	start := p.bitPosition
	var value uint32
	for p.remainingBits() >= 8 {
		v, _, _ := p.readBits(8)
		value += uint32(v) // bounded by at most MaxSEINALBytes header bytes
		if v != 255 {
			return &VUIUnsignedField{Value: value, Span: p.mappedSpan(start, p.bitPosition-start)}
		}
	}
	p.fail(CodeSEIHeaderTruncated, "SEI payload type or size continuation has no terminating byte", 8, uint64(p.remainingBits()))
	return nil
}

// H.264 D.1.7/D.2.7; the absolute recovery_frame_cnt limit is 65535.
// Its active-SPS/decoded-picture semantics are deliberately not inferred.
func (p *rbspParser) seiRecoveryPoint(payloadComplete bool) *SEIRecoveryPoint {
	r := &SEIRecoveryPoint{}
	r.RecoveryFrameCnt = p.fullUE(65535, CodeSEISyntaxInvalid, "recovery_frame_cnt")
	r.ExactMatchFlag = p.vuiFlag()
	r.BrokenLinkFlag = p.vuiFlag()
	r.ChangingSliceGroupIDC = p.vuiBits(2)
	if r.ChangingSliceGroupIDC != nil && r.ChangingSliceGroupIDC.Value > 2 {
		p.semanticFatal(r.ChangingSliceGroupIDC.Span, CodeSEISyntaxInvalid, "changing_slice_group_idc exceeds syntax range", 2, uint64(r.ChangingSliceGroupIDC.Value))
	}
	r.PayloadExtensionBits, r.PayloadAlignmentBits = p.seiPayloadTail(payloadComplete)
	return r
}

func (p *rbspParser) seiPayloadTail(payloadComplete bool) (extension, alignment *MappedSpan) {
	if p.fatal || !payloadComplete || p.remainingBits() == 0 {
		return
	}
	// sei_payload() permits opaque reserved extension bits before the final
	// bit_equal_to_one and byte-alignment zero bits.
	left := p.remainingBits()
	last := p.data.bytes[len(p.data.bytes)-1]
	zeros := uint32(bits.TrailingZeros8(last))
	if zeros == 8 || zeros+1 > left {
		p.fail(CodeSEITrailingBitsInvalid, "SEI payload alignment lacks bit_equal_to_one", 1, 0)
		return
	}
	if count := left - zeros - 1; count > 0 {
		s := p.mappedSpan(p.bitPosition, count)
		extension = &s
		p.bitPosition += count
	}
	s := p.mappedSpan(p.bitPosition, zeros+1)
	alignment = &s
	p.bitPosition += zeros + 1
	return
}

func (p *rbspParser) seiUserData(size uint32) *SEIUserDataUnregistered {
	r := &SEIUserDataUnregistered{}
	if size < 16 {
		p.fail(CodeSEISyntaxInvalid, "user_data_unregistered payload is smaller than its 16-byte UUID", 16, uint64(size))
		return r
	}
	r.DataBytes = size - 16
	if p.remainingBits() < 128 {
		p.fail(CodeSEIPayloadTruncated, "user_data_unregistered UUID is truncated", 128, uint64(p.remainingBits()))
		r.PreviewTruncated = r.DataBytes != 0
		return r
	}
	readHex := func(length uint32) *SEIHexBytes {
		start := p.bitPosition
		p.bitPosition += length * 8
		return &SEIHexBytes{Hex: hex.EncodeToString(p.data.bytes[start/8 : p.bitPosition/8]), Span: p.mappedSpan(start, length*8)}
	}
	r.UUID = readHex(16)
	length := min(r.DataBytes, MaxSEIDataPreviewBytes, p.remainingBits()/8)
	if length != 0 {
		r.DataPreview = readHex(length)
	}
	r.PreviewTruncated = length < r.DataBytes
	return r
}

func readSEIRBSP(source io.ReaderAt, nal cursor.Span, options Options, budget *parseBudget, diagnostics *diagnosticCollector) (rbspData, uint32, error) {
	start, end, err := nal.ByteRange()
	if err != nil {
		return rbspData{}, 0, err
	}
	start++ // NAL header has already been read by the common framing parser.
	data := rbspData{end: end, valid: true}
	data.view.EBSPSpan = cursor.Span{ByteOffset: start}
	length := min(end-start, uint64(MaxSEINALBytes), uint64(MaxSEIBytesPerParse-budget.seiBytesRead))
	if budget.seiMessages >= MaxSEIMessagesPerParse {
		length = 0
	}
	input := make([]byte, int(length))
	var read uint32
	for offset := 0; offset < len(input); {
		amount := min(4096, len(input)-offset)
		position := start + uint64(offset)
		if position > math.MaxInt64 || uint64(amount-1) > math.MaxInt64-position {
			return data, read, fmt.Errorf("SEI source range at %d with length %d exceeds int64 offsets", position, amount)
		}
		n, e := source.ReadAt(input[offset:offset+amount], int64(start)+int64(offset))
		if n > 0 {
			read += uint32(n)
			budget.seiBytesRead += uint32(n)
			data.view.EBSPSpan.BitLength = read * 8
		}
		if n != amount || (e != nil && e != io.EOF) {
			if e == nil {
				e = io.ErrUnexpectedEOF
			}
			return data, read, fmt.Errorf("read SEI EBSP at byte %d: read %d of %d bytes: %w", start+uint64(offset), n, amount, e)
		}
		offset += amount
	}
	if length != end-start {
		code, message := CodeSEIByteLimit, "SEI source bytes exceed the per-NAL or cumulative read limit"
		if budget.seiMessages >= MaxSEIMessagesPerParse {
			code, message = CodeSEIMessageLimit, "cumulative SEI message limit reached"
		}
		diagnostics.add(Diagnostic{Code: code, Severity: SeverityError, Offset: start + length, Message: message, RecoveryOffset: number(end)})
		data.valid = false
	}
	zeros := 0
	for i, value := range input {
		offset := start + uint64(i)
		if zeros >= 2 && value <= 3 {
			if value != 3 || i+1 == len(input) || input[i+1] > 3 {
				// A budget-limited terminal 03 is undecidable, not malformed.
				if !(value == 3 && i+1 == len(input) && length != end-start) {
					diagnostics.add(Diagnostic{Code: CodeEmulationPreventionInvalid, Severity: SeverityError, Offset: offset, Message: "SEI contains an invalid or missing emulation-prevention byte", RecoveryOffset: number(end)})
				}
				data.valid = false
				break
			}
			if !budget.takeMappingEntry() {
				seiMappingLimit(&data, offset, options, diagnostics)
				break
			}
			data.view.EmulationPreventionBytes = append(data.view.EmulationPreventionBytes, cursor.Span{ByteOffset: offset, BitLength: 8})
			zeros = 0
			continue
		}
		if len(data.sourceOffsets) == 0 || offset != data.sourceOffsets[len(data.sourceOffsets)-1]+1 {
			if !budget.takeMappingEntry() {
				seiMappingLimit(&data, offset, options, diagnostics)
				break
			}
		}
		data.bytes = append(data.bytes, value)
		data.sourceOffsets = append(data.sourceOffsets, offset)
		if value == 0 {
			zeros++
		} else {
			zeros = 0
		}
	}
	data.view.ByteLength = uint32(len(data.bytes))
	data.view.SourceSegments = sourceSegments(data.sourceOffsets)
	return data, read, nil
}

func seiMappingLimit(data *rbspData, offset uint64, options Options, diagnostics *diagnosticCollector) {
	data.valid = false
	diagnostics.add(Diagnostic{Code: CodeRBSPMappingLimitReached, Severity: SeverityError, Offset: offset, Message: "cumulative SEI RBSP source-mapping entries exceed the configured limit", Expected: number(uint64(options.MaxRBSPMappingEntries)), RecoveryOffset: number(data.end)})
}
