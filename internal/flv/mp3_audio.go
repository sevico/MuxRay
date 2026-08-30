package flv

import (
	"errors"
	"fmt"
	"io"

	"github.com/sevico/muxray/internal/cursor"
	"github.com/sevico/muxray/internal/mp3"
)

const (
	SoundFormatMP3 uint8 = 2

	CodeMP3AudioHeaderTruncated = "MP3_FLV_AUDIO_HEADER_TRUNCATED"
)

var (
	ErrMP3AudioNilSource         = errors.New("flv: MP3 audio source is nil")
	ErrMP3AudioWrongTagType      = errors.New("flv: tag is not audio")
	ErrMP3AudioFiltered          = errors.New("flv: filtered MP3 audio is unsupported")
	ErrMP3AudioPayloadIncomplete = errors.New("flv: MP3 audio payload is incomplete")
	ErrMP3AudioWrongSoundFormat  = errors.New("flv: audio tag is not MP3")
)

// MP3Audio is the bounded, on-demand detail for one complete, unfiltered FLV
// MP3 audio tag. Only the first MPEG Audio frame header is parsed; all
// remaining SoundData stays in the source and is represented by spans.
type MP3Audio struct {
	TagID uint64
	Span  cursor.Span

	Header        AudioTagHeader
	HeaderPresent bool

	DataSpan    cursor.Span
	FrameHeader mp3.Result

	BitsConsumed uint64
	Diagnostics  []mp3.Diagnostic
	Complete     bool
}

// ParseMP3Audio parses the FLV AudioTagHeader and the first fixed MPEG Audio
// Layer III frame header. Malformed syntax returns partial detail; selection
// and source failures return Go errors.
func ParseMP3Audio(source io.ReaderAt, tag TagRecord, options mp3.Options) (MP3Audio, error) {
	detail := MP3Audio{TagID: tag.ID, Span: tag.PayloadSpan()}
	if source == nil {
		return detail, ErrMP3AudioNilSource
	}
	if tag.Type() != TagTypeAudio {
		return detail, fmt.Errorf("%w: got %s", ErrMP3AudioWrongTagType, tag.Type())
	}
	if tag.Filter() {
		return detail, ErrMP3AudioFiltered
	}
	if !tag.PayloadComplete() || tag.AvailableDataSize != tag.DataSize {
		return detail, ErrMP3AudioPayloadIncomplete
	}
	normalized, err := options.Normalized()
	if err != nil {
		return detail, err
	}

	payloadEnd := detail.Span.ByteOffset + uint64(tag.DataSize)
	if tag.DataSize == 0 {
		detail.Diagnostics = []mp3.Diagnostic{{
			Code: CodeMP3AudioHeaderTruncated, Severity: mp3.SeverityError,
			Offset: detail.Span.ByteOffset, Message: "FLV audio tag header is missing",
			Expected: mp3Number(1), Actual: mp3Number(0), RecoveryOffset: mp3Number(payloadEnd),
		}}
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
	if detail.Header.SoundFormat != SoundFormatMP3 {
		return detail, fmt.Errorf("%w: got SoundFormat %d", ErrMP3AudioWrongSoundFormat, detail.Header.SoundFormat)
	}

	detail.DataSpan = cursor.Span{
		ByteOffset: detail.Span.ByteOffset + 1,
		BitLength:  (tag.DataSize - 1) * 8,
	}
	parsed, parseErr := mp3.ParseFrameHeader(source, detail.DataSpan, normalized)
	detail.FrameHeader = parsed
	if parseErr != nil {
		return detail, parseErr
	}
	detail.BitsConsumed += parsed.BitsConsumed
	detail.Diagnostics = append(detail.Diagnostics, parsed.Diagnostics...)
	detail.Complete = parsed.Complete
	return detail, nil
}

func mp3Number(value uint64) *uint64 { return &value }
