package flv

import (
	"errors"
	"fmt"
	"io"

	"github.com/sevico/muxray/internal/aac"
	"github.com/sevico/muxray/internal/cursor"
)

const (
	SoundFormatAAC uint8 = 10

	CodeAACAudioHeaderTruncated = "AAC_FLV_AUDIO_HEADER_TRUNCATED"
	CodeAACPacketTypeTruncated  = "AAC_FLV_PACKET_TYPE_TRUNCATED"
	CodeAACPacketTypeUnknown    = "AAC_FLV_PACKET_TYPE_UNKNOWN"
	CodeAACSoundRateUnexpected  = "AAC_FLV_SOUND_RATE_UNEXPECTED"
	CodeAACSoundTypeUnexpected  = "AAC_FLV_SOUND_TYPE_UNEXPECTED"
)

var (
	ErrAACAudioNilSource         = errors.New("flv: AAC audio source is nil")
	ErrAACAudioWrongTagType      = errors.New("flv: tag is not audio")
	ErrAACAudioFiltered          = errors.New("flv: filtered AAC audio is unsupported")
	ErrAACAudioPayloadIncomplete = errors.New("flv: AAC audio payload is incomplete")
	ErrAACAudioWrongSoundFormat  = errors.New("flv: audio tag is not AAC")
)

// AACPacketType identifies the payload following an FLV AAC audio header.
type AACPacketType uint8

const (
	AACPacketTypeSequenceHeader AACPacketType = 0
	AACPacketTypeRaw            AACPacketType = 1
)

func (t AACPacketType) String() string {
	switch t {
	case AACPacketTypeSequenceHeader:
		return "sequenceHeader"
	case AACPacketTypeRaw:
		return "raw"
	default:
		return "unknown"
	}
}

// AudioTagHeader is the first byte of an FLV audio payload. Field spans use
// absolute offsets in the original FLV source.
type AudioTagHeader struct {
	Raw             uint8
	Span            cursor.Span
	SoundFormat     uint8
	SoundFormatSpan cursor.Span
	SoundRate       uint8
	SoundRateSpan   cursor.Span
	SoundSize       uint8
	SoundSizeSpan   cursor.Span
	SoundType       uint8
	SoundTypeSpan   cursor.Span
}

// AACAudio is the bounded, on-demand detail for one complete, unfiltered FLV
// AAC audio tag. Raw AAC access units are described by span only; only
// sequence headers are parsed as AudioSpecificConfig.
type AACAudio struct {
	TagID uint64
	Span  cursor.Span

	Header        AudioTagHeader
	HeaderPresent bool

	PacketType        AACPacketType
	PacketTypeSpan    cursor.Span
	PacketTypePresent bool

	DataSpan cursor.Span
	Config   aac.Result

	ConfigPresent  bool
	RawDataPresent bool
	BitsConsumed   uint64
	Diagnostics    []aac.Diagnostic
	Complete       bool
}

// ParseAACAudio parses FLV AudioTagHeader/AACPacketType framing and, for an
// AAC sequence header, its bounded AudioSpecificConfig. Malformed framing or
// configuration is returned as partial detail; invalid selection and source
// I/O failures are returned as Go errors.
func ParseAACAudio(source io.ReaderAt, tag TagRecord, options aac.Options) (AACAudio, error) {
	detail := AACAudio{TagID: tag.ID, Span: tag.PayloadSpan()}

	if source == nil {
		return detail, ErrAACAudioNilSource
	}
	if tag.Type() != TagTypeAudio {
		return detail, fmt.Errorf("%w: got %s", ErrAACAudioWrongTagType, tag.Type())
	}
	if tag.Filter() {
		return detail, ErrAACAudioFiltered
	}
	if !tag.PayloadComplete() || tag.AvailableDataSize != tag.DataSize {
		return detail, ErrAACAudioPayloadIncomplete
	}
	normalized, err := options.Normalized()
	if err != nil {
		return detail, err
	}

	payloadEnd := detail.Span.ByteOffset + uint64(tag.DataSize)
	if tag.DataSize == 0 {
		detail.Diagnostics = appendAACAudioDiagnostic(detail.Diagnostics, aac.Diagnostic{
			Code: CodeAACAudioHeaderTruncated, Severity: aac.SeverityError,
			Offset: detail.Span.ByteOffset, Message: "FLV audio tag header is missing",
			Expected: aacNumber(1), Actual: aacNumber(0), RecoveryOffset: aacNumber(payloadEnd),
		}, normalized.MaxDiagnostics)
		return detail, nil
	}

	headerReader, err := cursor.NewBitReaderAt(source, detail.Span.ByteOffset, 1)
	if err != nil {
		return detail, err
	}
	format, formatSpan, err := headerReader.ReadBitsWithSpan(4)
	if err != nil {
		return detail, err
	}
	rate, rateSpan, err := headerReader.ReadBitsWithSpan(2)
	if err != nil {
		return detail, err
	}
	size, sizeSpan, err := headerReader.ReadBitsWithSpan(1)
	if err != nil {
		return detail, err
	}
	channelType, typeSpan, err := headerReader.ReadBitsWithSpan(1)
	if err != nil {
		return detail, err
	}
	detail.Header = AudioTagHeader{
		Raw:         uint8(format<<4 | rate<<2 | size<<1 | channelType),
		Span:        cursor.Span{ByteOffset: detail.Span.ByteOffset, BitLength: 8},
		SoundFormat: uint8(format), SoundFormatSpan: formatSpan,
		SoundRate: uint8(rate), SoundRateSpan: rateSpan,
		SoundSize: uint8(size), SoundSizeSpan: sizeSpan,
		SoundType: uint8(channelType), SoundTypeSpan: typeSpan,
	}
	detail.HeaderPresent = true
	detail.BitsConsumed = 8

	if detail.Header.SoundFormat != SoundFormatAAC {
		return detail, fmt.Errorf("%w: got SoundFormat %d", ErrAACAudioWrongSoundFormat, detail.Header.SoundFormat)
	}
	if detail.Header.SoundRate != 3 {
		detail.Diagnostics = appendAACAudioDiagnostic(detail.Diagnostics, aac.Diagnostic{
			Code: CodeAACSoundRateUnexpected, Severity: aac.SeverityWarning,
			Offset: detail.Header.SoundRateSpan.ByteOffset, BitOffset: detail.Header.SoundRateSpan.BitOffset,
			Message:  "FLV AAC SoundRate should be 3; use AudioSpecificConfig for the actual sample rate",
			Expected: aacNumber(3), Actual: aacNumber(uint64(detail.Header.SoundRate)),
		}, normalized.MaxDiagnostics)
	}
	if detail.Header.SoundType != 1 {
		detail.Diagnostics = appendAACAudioDiagnostic(detail.Diagnostics, aac.Diagnostic{
			Code: CodeAACSoundTypeUnexpected, Severity: aac.SeverityWarning,
			Offset: detail.Header.SoundTypeSpan.ByteOffset, BitOffset: detail.Header.SoundTypeSpan.BitOffset,
			Message:  "FLV AAC SoundType should be 1; use AudioSpecificConfig for the actual channel configuration",
			Expected: aacNumber(1), Actual: aacNumber(uint64(detail.Header.SoundType)),
		}, normalized.MaxDiagnostics)
	}

	if tag.DataSize == 1 {
		detail.Diagnostics = appendAACAudioDiagnostic(detail.Diagnostics, aac.Diagnostic{
			Code: CodeAACPacketTypeTruncated, Severity: aac.SeverityError,
			Offset: payloadEnd, Message: "FLV AAC packet type is missing",
			Expected: aacNumber(2), Actual: aacNumber(1), RecoveryOffset: aacNumber(payloadEnd),
		}, normalized.MaxDiagnostics)
		return detail, nil
	}

	packetReader, err := cursor.NewBitReaderAt(source, detail.Span.ByteOffset+1, 1)
	if err != nil {
		return detail, err
	}
	packetType, packetSpan, err := packetReader.ReadBitsWithSpan(8)
	if err != nil {
		return detail, err
	}
	detail.PacketType = AACPacketType(packetType)
	detail.PacketTypeSpan = packetSpan
	detail.PacketTypePresent = true
	detail.DataSpan = cursor.Span{
		ByteOffset: detail.Span.ByteOffset + 2,
		BitLength:  (tag.DataSize - 2) * 8,
	}
	detail.BitsConsumed = 16

	switch detail.PacketType {
	case AACPacketTypeSequenceHeader:
		detail.ConfigPresent = true
		config, parseErr := aac.ParseAudioSpecificConfig(source, detail.DataSpan, normalized)
		detail.Config = config
		if parseErr != nil {
			return detail, parseErr
		}
		for _, diagnostic := range config.Diagnostics {
			if diagnostic.Severity == aac.SeverityError {
				diagnostic.RecoveryOffset = aacNumber(payloadEnd)
			}
			detail.Diagnostics = appendAACAudioDiagnostic(detail.Diagnostics, diagnostic, normalized.MaxDiagnostics)
		}
		detail.BitsConsumed += config.BitsConsumed
		detail.Complete = config.Complete
	case AACPacketTypeRaw:
		detail.RawDataPresent = true
		detail.Complete = true
	default:
		detail.Diagnostics = appendAACAudioDiagnostic(detail.Diagnostics, aac.Diagnostic{
			Code: CodeAACPacketTypeUnknown, Severity: aac.SeverityError,
			Offset: packetSpan.ByteOffset, BitOffset: packetSpan.BitOffset,
			Message: "FLV AAC packet type is neither sequence header nor raw AAC",
			Actual:  aacNumber(packetType), RecoveryOffset: aacNumber(payloadEnd),
		}, normalized.MaxDiagnostics)
	}
	if hasAACAudioErrors(detail.Diagnostics) {
		detail.Complete = false
	}

	return detail, nil
}

func aacNumber(value uint64) *uint64 { return &value }

func hasAACAudioErrors(items []aac.Diagnostic) bool {
	for _, diagnostic := range items {
		if diagnostic.Severity == aac.SeverityError {
			return true
		}
	}
	return false
}

// appendAACAudioDiagnostic applies the AAC diagnostic budget while preserving
// a fatal cause and a recovery offset when earlier warnings consume the limit.
func appendAACAudioDiagnostic(items []aac.Diagnostic, diagnostic aac.Diagnostic, limit uint32) []aac.Diagnostic {
	if uint32(len(items)) < limit {
		return append(items, diagnostic)
	}
	if limit == 0 || len(items) == 0 {
		return items
	}

	markerIndex := len(items) - 1
	markerPresent := items[markerIndex].Code == aac.CodeDiagnosticLimitReached
	marker := items[markerIndex]
	if !markerPresent {
		marker = aac.Diagnostic{
			Code: aac.CodeDiagnosticLimitReached, Severity: aac.SeverityError,
			Offset: diagnostic.Offset, BitOffset: diagnostic.BitOffset,
			Message: "diagnostic limit reached; additional diagnostics were omitted",
		}
	}

	searchEnd := len(items)
	if markerPresent {
		searchEnd = markerIndex
	}
	fatal := aac.Diagnostic{}
	hasFatal := false
	for _, existing := range items[:searchEnd] {
		if existing.Severity == aac.SeverityError {
			fatal, hasFatal = existing, true
			break
		}
	}
	if !hasFatal && diagnostic.Severity == aac.SeverityError {
		fatal, hasFatal = diagnostic, true
	}
	if hasFatal {
		retainedFatal := false
		for _, existing := range items[:markerIndex] {
			if existing.Severity == aac.SeverityError {
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
