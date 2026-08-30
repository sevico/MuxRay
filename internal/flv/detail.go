package flv

import (
	"errors"
	"fmt"
	"io"
	"math"

	"github.com/sevico/muxray/internal/aac"
	"github.com/sevico/muxray/internal/amf0"
	"github.com/sevico/muxray/internal/mp3"
)

// DetailKind identifies the syntax contract returned for one selected FLV tag.
type DetailKind string

const (
	DetailKindAMF0ScriptData DetailKind = "amf0ScriptData"
	DetailKindAACAudio       DetailKind = "aacAudio"
	DetailKindMP3Audio       DetailKind = "mp3Audio"
	DetailKindAVCVideo       DetailKind = "avcVideo"
)

// ErrTagDetailUnsupported identifies a tag for which no on-demand detail
// parser is currently available.
var ErrTagDetailUnsupported = errors.New("flv: tag detail is unsupported")

// ParsedTagDetail is one strongly typed, on-demand tag detail. Concrete
// results retain their codec-specific fields while callers can route and
// determine completion without inspecting those fields.
type ParsedTagDetail interface {
	DetailKind() DetailKind
	IsComplete() bool
}

// DetailOptions groups parser-specific limits. A parser only consumes the
// options for the selected detail kind.
type DetailOptions struct {
	AMF0 amf0.Options
	AAC  aac.Options
	MP3  mp3.Options
	AVC  AVCVideoOptions
}

// ParseTagDetail dispatches one indexed tag to its syntax parser without
// reading or retaining payload bytes itself. Unsupported tag types return
// ErrTagDetailUnsupported; parser input and source failures are preserved.
func ParseTagDetail(source io.ReaderAt, tag TagRecord, options DetailOptions) (ParsedTagDetail, error) {
	switch tag.Type() {
	case TagTypeScript:
		detail, err := ParseScriptData(source, tag, options.AMF0)
		return detail, err
	case TagTypeAudio:
		return parseAudioTagDetail(source, tag, options)
	case TagTypeVideo:
		detail, err := ParseAVCVideo(source, tag, options.AVC)
		return detail, err
	default:
		return nil, fmt.Errorf("%w: tag type %s (%d)", ErrTagDetailUnsupported, tag.Type(), tag.Type())
	}
}

func (ScriptData) DetailKind() DetailKind { return DetailKindAMF0ScriptData }

func (detail ScriptData) IsComplete() bool { return detail.Complete }

func (AACAudio) DetailKind() DetailKind { return DetailKindAACAudio }

func (detail AACAudio) IsComplete() bool { return detail.Complete }

func (MP3Audio) DetailKind() DetailKind { return DetailKindMP3Audio }

func (detail MP3Audio) IsComplete() bool { return detail.Complete }

func (AVCVideo) DetailKind() DetailKind { return DetailKindAVCVideo }

func (detail AVCVideo) IsComplete() bool { return detail.Complete }

var (
	_ ParsedTagDetail = ScriptData{}
	_ ParsedTagDetail = AACAudio{}
	_ ParsedTagDetail = MP3Audio{}
	_ ParsedTagDetail = AVCVideo{}
)

// parseAudioTagDetail preserves all pre-MP3 AAC behavior unless a safe,
// complete one-byte probe positively identifies SoundFormat 2. ReaderAt is
// positional, so the selected parser still owns and validates its full input.
func parseAudioTagDetail(source io.ReaderAt, tag TagRecord, options DetailOptions) (ParsedTagDetail, error) {
	if source != nil && !tag.Filter() && tag.PayloadComplete() && tag.AvailableDataSize == tag.DataSize && tag.DataSize >= 1 {
		payloadOffset := tag.PayloadSpan().ByteOffset
		if payloadOffset <= math.MaxInt64 {
			var header [1]byte
			if read, _ := source.ReadAt(header[:], int64(payloadOffset)); read == len(header) && header[0]>>4 == SoundFormatMP3 {
				detail, err := ParseMP3Audio(source, tag, options.MP3)
				return detail, err
			}
		}
	}
	detail, err := ParseAACAudio(source, tag, options.AAC)
	return detail, err
}
