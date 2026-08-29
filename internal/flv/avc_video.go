package flv

import (
	"errors"
	"fmt"
	"io"

	"github.com/sevico/muxray/internal/avc"
	"github.com/sevico/muxray/internal/cursor"
)

const (
	CodecIDAVC uint8 = 7

	CodeAVCVideoHeaderTruncated           = "AVC_FLV_VIDEO_HEADER_TRUNCATED"
	CodeAVCFrameTypeReserved              = "AVC_FLV_FRAME_TYPE_RESERVED"
	CodeAVCFrameTypeUnexpected            = "AVC_FLV_FRAME_TYPE_UNEXPECTED"
	CodeAVCPacketTypeTruncated            = "AVC_FLV_PACKET_TYPE_TRUNCATED"
	CodeAVCCompositionTimeTruncated       = "AVC_FLV_COMPOSITION_TIME_TRUNCATED"
	CodeAVCCompositionTimeNonZero         = "AVC_FLV_COMPOSITION_TIME_NON_ZERO"
	CodeAVCPacketTypeUnknown              = "AVC_FLV_PACKET_TYPE_UNKNOWN"
	CodeAVCNALLengthSizeUnavailable       = "AVC_NAL_LENGTH_SIZE_UNAVAILABLE"
	CodeAVCEndOfSequencePayloadUnexpected = "AVC_FLV_END_OF_SEQUENCE_PAYLOAD_UNEXPECTED"
)

var (
	ErrAVCVideoNilSource         = errors.New("flv: AVC video source is nil")
	ErrAVCVideoWrongTagType      = errors.New("flv: tag is not video")
	ErrAVCVideoFiltered          = errors.New("flv: filtered AVC video is unsupported")
	ErrAVCVideoPayloadIncomplete = errors.New("flv: AVC video payload is incomplete")
	ErrAVCVideoCommandFrame      = errors.New("flv: video command frame is not an AVC video packet")
	ErrAVCVideoWrongCodecID      = errors.New("flv: video tag is not AVC")
	ErrAVCNALLengthSizeInvalid   = errors.New("flv: AVC NAL length size must be 1, 2, or 4 bytes")
)

// AVCPacketType identifies the payload following an FLV AVC video header.
type AVCPacketType uint8

const (
	AVCPacketTypeSequenceHeader AVCPacketType = 0
	AVCPacketTypeNALU           AVCPacketType = 1
	AVCPacketTypeEndOfSequence  AVCPacketType = 2
)

func (packetType AVCPacketType) String() string {
	switch packetType {
	case AVCPacketTypeSequenceHeader:
		return "sequenceHeader"
	case AVCPacketTypeNALU:
		return "nalu"
	case AVCPacketTypeEndOfSequence:
		return "endOfSequence"
	default:
		return "unknown"
	}
}

// VideoTagHeader is the first byte of an FLV video payload. Field spans use
// absolute offsets in the original FLV source.
type VideoTagHeader struct {
	Raw           uint8
	Span          cursor.Span
	FrameType     uint8
	FrameTypeSpan cursor.Span
	CodecID       uint8
	CodecIDSpan   cursor.Span
}

// AVCVideoOptions controls parsing that cannot be inferred from one isolated
// FLV video tag. NALLengthSize must be supplied for an AVC NALU packet because
// that value lives in an earlier sequence header; zero means unavailable and
// is never guessed. Valid explicit values are 1, 2, and 4 bytes.
type AVCVideoOptions struct {
	AVC           avc.Options
	NALLengthSize uint8
}

func (options AVCVideoOptions) normalized() (AVCVideoOptions, error) {
	normalized, err := options.AVC.Normalized()
	if err != nil {
		return AVCVideoOptions{}, err
	}
	options.AVC = normalized
	if options.NALLengthSize != 0 && options.NALLengthSize != 1 && options.NALLengthSize != 2 && options.NALLengthSize != 4 {
		return AVCVideoOptions{}, fmt.Errorf("%w: got %d", ErrAVCNALLengthSizeInvalid, options.NALLengthSize)
	}
	return options, nil
}

// AVCVideo is the bounded, on-demand detail for one complete, unfiltered FLV
// AVC video tag. An isolated NALU packet is parsed only when its length-field
// width is explicitly provided by the caller.
type AVCVideo struct {
	TagID uint64
	Span  cursor.Span

	Header        VideoTagHeader
	HeaderPresent bool

	PacketType        AVCPacketType
	PacketTypeSpan    cursor.Span
	PacketTypePresent bool

	CompositionTime        int32
	CompositionTimeSpan    cursor.Span
	CompositionTimePresent bool

	DataSpan cursor.Span
	Config   avc.ConfigurationRecord
	NALUnits avc.NALUnitSequence

	ConfigPresent        bool
	NALUnitsPresent      bool
	EndOfSequencePresent bool
	BitsConsumed         uint64
	Diagnostics          []avc.Diagnostic
	Complete             bool
}

// ParseAVCVideo parses FLV VideoTagHeader/AVCPacketType/CompositionTime
// framing and then dispatches sequence headers and length-prefixed NAL units
// to the bounded AVC syntax parser. Malformed syntax is returned as partial
// detail; invalid selection and source I/O failures are returned as Go errors.
func ParseAVCVideo(source io.ReaderAt, tag TagRecord, options AVCVideoOptions) (AVCVideo, error) {
	detail := AVCVideo{TagID: tag.ID, Span: tag.PayloadSpan()}

	if source == nil {
		return detail, ErrAVCVideoNilSource
	}
	if tag.Type() != TagTypeVideo {
		return detail, fmt.Errorf("%w: got %s", ErrAVCVideoWrongTagType, tag.Type())
	}
	if tag.Filter() {
		return detail, ErrAVCVideoFiltered
	}
	if !tag.PayloadComplete() || tag.AvailableDataSize != tag.DataSize {
		return detail, ErrAVCVideoPayloadIncomplete
	}
	normalized, err := options.normalized()
	if err != nil {
		return detail, err
	}

	payloadEnd := detail.Span.ByteOffset + uint64(tag.DataSize)
	if tag.DataSize == 0 {
		detail.Diagnostics = appendAVCVideoDiagnostic(detail.Diagnostics, avc.Diagnostic{
			Code: CodeAVCVideoHeaderTruncated, Severity: avc.SeverityError,
			Offset: detail.Span.ByteOffset, Message: "FLV video tag header is missing",
			Expected: avcNumber(1), Actual: avcNumber(0), RecoveryOffset: avcNumber(payloadEnd),
		}, normalized.AVC.MaxDiagnostics, payloadEnd)
		return detail, nil
	}

	headerReader, err := cursor.NewBitReaderAt(source, detail.Span.ByteOffset, 1)
	if err != nil {
		return detail, err
	}
	frameType, frameTypeSpan, err := headerReader.ReadBitsWithSpan(4)
	if err != nil {
		return detail, err
	}
	codecID, codecIDSpan, err := headerReader.ReadBitsWithSpan(4)
	if err != nil {
		return detail, err
	}
	detail.Header = VideoTagHeader{
		Raw: uint8(frameType<<4 | codecID), Span: cursor.Span{ByteOffset: detail.Span.ByteOffset, BitLength: 8},
		FrameType: uint8(frameType), FrameTypeSpan: frameTypeSpan,
		CodecID: uint8(codecID), CodecIDSpan: codecIDSpan,
	}
	detail.HeaderPresent = true
	detail.BitsConsumed = 8

	if detail.Header.FrameType == 5 {
		return detail, ErrAVCVideoCommandFrame
	}
	if detail.Header.CodecID != CodecIDAVC {
		return detail, fmt.Errorf("%w: got CodecID %d", ErrAVCVideoWrongCodecID, detail.Header.CodecID)
	}
	if detail.Header.FrameType == 0 || detail.Header.FrameType > 5 {
		detail.Diagnostics = appendAVCVideoDiagnostic(detail.Diagnostics, avc.Diagnostic{
			Code: CodeAVCFrameTypeReserved, Severity: avc.SeverityWarning,
			Offset: detail.Header.FrameTypeSpan.ByteOffset, BitOffset: detail.Header.FrameTypeSpan.BitOffset,
			Message: "FLV AVC frame type is reserved",
			Actual:  avcNumber(uint64(detail.Header.FrameType)),
		}, normalized.AVC.MaxDiagnostics, payloadEnd)
	}
	if detail.Header.FrameType == 3 {
		detail.Diagnostics = appendAVCVideoDiagnostic(detail.Diagnostics, avc.Diagnostic{
			Code: CodeAVCFrameTypeUnexpected, Severity: avc.SeverityWarning,
			Offset: detail.Header.FrameTypeSpan.ByteOffset, BitOffset: detail.Header.FrameTypeSpan.BitOffset,
			Message: "FLV disposable inter frame type is defined only for H.263 video",
			Actual:  avcNumber(uint64(detail.Header.FrameType)),
		}, normalized.AVC.MaxDiagnostics, payloadEnd)
	}
	if tag.DataSize == 1 {
		detail.Diagnostics = appendAVCVideoDiagnostic(detail.Diagnostics, avc.Diagnostic{
			Code: CodeAVCPacketTypeTruncated, Severity: avc.SeverityError,
			Offset: payloadEnd, Message: "FLV AVC packet type is missing",
			Expected: avcNumber(2), Actual: avcNumber(1), RecoveryOffset: avcNumber(payloadEnd),
		}, normalized.AVC.MaxDiagnostics, payloadEnd)
		return detail, nil
	}

	framingReader, err := cursor.NewBitReaderAt(source, detail.Span.ByteOffset+1, uint64(tag.DataSize-1))
	if err != nil {
		return detail, err
	}
	packetType, packetSpan, err := framingReader.ReadBitsWithSpan(8)
	if err != nil {
		return detail, err
	}
	detail.PacketType = AVCPacketType(packetType)
	detail.PacketTypeSpan = packetSpan
	detail.PacketTypePresent = true
	detail.BitsConsumed = 16

	if tag.DataSize < 5 {
		detail.Diagnostics = appendAVCVideoDiagnostic(detail.Diagnostics, avc.Diagnostic{
			Code: CodeAVCCompositionTimeTruncated, Severity: avc.SeverityError,
			Offset: payloadEnd, Message: "FLV AVC composition time is truncated",
			Expected: avcNumber(5), Actual: avcNumber(uint64(tag.DataSize)), RecoveryOffset: avcNumber(payloadEnd),
		}, normalized.AVC.MaxDiagnostics, payloadEnd)
		return detail, nil
	}

	compositionTimeRaw, compositionTimeSpan, err := framingReader.ReadBitsWithSpan(24)
	if err != nil {
		return detail, err
	}
	detail.CompositionTime = signedInt24(uint32(compositionTimeRaw))
	detail.CompositionTimeSpan = compositionTimeSpan
	detail.CompositionTimePresent = true
	detail.DataSpan = cursor.Span{ByteOffset: detail.Span.ByteOffset + 5, BitLength: (tag.DataSize - 5) * 8}
	detail.BitsConsumed = 40
	if detail.CompositionTime != 0 && (detail.PacketType == AVCPacketTypeSequenceHeader || detail.PacketType == AVCPacketTypeEndOfSequence) {
		detail.Diagnostics = appendAVCVideoDiagnostic(detail.Diagnostics, avc.Diagnostic{
			Code: CodeAVCCompositionTimeNonZero, Severity: avc.SeverityWarning,
			Offset: detail.CompositionTimeSpan.ByteOffset, BitOffset: detail.CompositionTimeSpan.BitOffset,
			Message:  "FLV AVC sequence-header and end-of-sequence composition time should be zero",
			Expected: avcNumber(0), Actual: avcNumber(uint64(uint32(detail.CompositionTime) & 0x00ff_ffff)),
		}, normalized.AVC.MaxDiagnostics, payloadEnd)
	}

	switch detail.PacketType {
	case AVCPacketTypeSequenceHeader:
		detail.ConfigPresent = true
		config, parseErr := avc.ParseDecoderConfigurationRecord(source, detail.DataSpan, normalized.AVC)
		detail.Config = config
		if parseErr != nil {
			return detail, parseErr
		}
		for _, diagnostic := range config.Diagnostics {
			if diagnostic.Severity == avc.SeverityError {
				diagnostic.RecoveryOffset = avcNumber(payloadEnd)
			}
			detail.Diagnostics = appendAVCVideoDiagnostic(detail.Diagnostics, diagnostic, normalized.AVC.MaxDiagnostics, payloadEnd)
		}
		detail.BitsConsumed += config.BytesConsumed * 8
		detail.Complete = config.Complete
	case AVCPacketTypeNALU:
		if normalized.NALLengthSize == 0 {
			detail.Diagnostics = appendAVCVideoDiagnostic(detail.Diagnostics, avc.Diagnostic{
				Code: CodeAVCNALLengthSizeUnavailable, Severity: avc.SeverityError,
				Offset:         detail.DataSpan.ByteOffset,
				Message:        "AVC NAL length size is unavailable; provide it from the applicable sequence header",
				RecoveryOffset: avcNumber(payloadEnd),
			}, normalized.AVC.MaxDiagnostics, payloadEnd)
			break
		}
		detail.NALUnitsPresent = true
		sequence, parseErr := avc.ParseLengthPrefixedNALUnits(source, detail.DataSpan, normalized.NALLengthSize, normalized.AVC)
		detail.NALUnits = sequence
		if parseErr != nil {
			return detail, parseErr
		}
		for _, diagnostic := range sequence.Diagnostics {
			if diagnostic.Severity == avc.SeverityError {
				diagnostic.RecoveryOffset = avcNumber(payloadEnd)
			}
			detail.Diagnostics = appendAVCVideoDiagnostic(detail.Diagnostics, diagnostic, normalized.AVC.MaxDiagnostics, payloadEnd)
		}
		detail.BitsConsumed += sequence.BytesConsumed * 8
		detail.Complete = sequence.Complete
	case AVCPacketTypeEndOfSequence:
		detail.EndOfSequencePresent = true
		detail.Complete = true
		if !detail.DataSpan.Empty() {
			detail.Diagnostics = appendAVCVideoDiagnostic(detail.Diagnostics, avc.Diagnostic{
				Code: CodeAVCEndOfSequencePayloadUnexpected, Severity: avc.SeverityError,
				Offset:   detail.DataSpan.ByteOffset,
				Message:  "FLV AVC end-of-sequence packet contains unexpected payload",
				Expected: avcNumber(0), Actual: avcNumber(uint64(detail.DataSpan.BitLength / 8)), RecoveryOffset: avcNumber(payloadEnd),
			}, normalized.AVC.MaxDiagnostics, payloadEnd)
		}
	default:
		detail.Diagnostics = appendAVCVideoDiagnostic(detail.Diagnostics, avc.Diagnostic{
			Code: CodeAVCPacketTypeUnknown, Severity: avc.SeverityError,
			Offset: packetSpan.ByteOffset, BitOffset: packetSpan.BitOffset,
			Message: "FLV AVC packet type is neither sequence header, NALU, nor end of sequence",
			Actual:  avcNumber(packetType), RecoveryOffset: avcNumber(payloadEnd),
		}, normalized.AVC.MaxDiagnostics, payloadEnd)
	}
	if hasAVCVideoErrors(detail.Diagnostics) {
		detail.Complete = false
	}

	return detail, nil
}

func signedInt24(value uint32) int32 {
	value &= 0x00ff_ffff
	if value&0x0080_0000 != 0 {
		value |= 0xff00_0000
	}
	return int32(value)
}

func avcNumber(value uint64) *uint64 { return &value }

func hasAVCVideoErrors(items []avc.Diagnostic) bool {
	for _, diagnostic := range items {
		if diagnostic.Severity == avc.SeverityError {
			return true
		}
	}
	return false
}

// appendAVCVideoDiagnostic applies the AVC diagnostic budget while preserving
// a fatal cause and recovery offset when earlier diagnostics consume the limit.
func appendAVCVideoDiagnostic(items []avc.Diagnostic, diagnostic avc.Diagnostic, limit uint32, fallbackRecovery uint64) []avc.Diagnostic {
	if uint32(len(items)) < limit {
		return append(items, diagnostic)
	}
	if limit == 0 || len(items) == 0 {
		return items
	}

	markerIndex := len(items) - 1
	markerPresent := items[markerIndex].Code == avc.CodeDiagnosticLimitReached
	marker := items[markerIndex]
	if !markerPresent {
		marker = avc.Diagnostic{
			Code: avc.CodeDiagnosticLimitReached, Severity: avc.SeverityError,
			Offset: diagnostic.Offset, BitOffset: diagnostic.BitOffset,
			Message:        "diagnostic limit reached; additional diagnostics were omitted",
			RecoveryOffset: avcNumber(fallbackRecovery),
		}
	}

	searchEnd := len(items)
	if markerPresent {
		searchEnd = markerIndex
	}
	fatal := avc.Diagnostic{}
	hasFatal := false
	for _, existing := range items[:searchEnd] {
		if existing.Severity == avc.SeverityError {
			fatal, hasFatal = existing, true
			break
		}
	}
	if !hasFatal && diagnostic.Severity == avc.SeverityError {
		fatal, hasFatal = diagnostic, true
	}
	if hasFatal {
		retainedFatal := false
		for _, existing := range items[:markerIndex] {
			if existing.Severity == avc.SeverityError {
				retainedFatal = true
				break
			}
		}
		if !retainedFatal && markerIndex > 0 {
			items[markerIndex-1] = fatal
		}
		marker.Offset = fatal.Offset
		marker.BitOffset = fatal.BitOffset
		marker.RecoveryOffset = fatal.RecoveryOffset
	}
	items[markerIndex] = marker
	return items
}
