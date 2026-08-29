package flv

import (
	"errors"
	"fmt"
	"io"

	"github.com/sevico/muxray/internal/aac"
	"github.com/sevico/muxray/internal/amf0"
)

// DetailKind identifies the syntax contract returned for one selected FLV tag.
type DetailKind string

const (
	DetailKindAMF0ScriptData DetailKind = "amf0ScriptData"
	DetailKindAACAudio       DetailKind = "aacAudio"
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
		detail, err := ParseAACAudio(source, tag, options.AAC)
		return detail, err
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

func (AVCVideo) DetailKind() DetailKind { return DetailKindAVCVideo }

func (detail AVCVideo) IsComplete() bool { return detail.Complete }

var (
	_ ParsedTagDetail = ScriptData{}
	_ ParsedTagDetail = AACAudio{}
	_ ParsedTagDetail = AVCVideo{}
)
