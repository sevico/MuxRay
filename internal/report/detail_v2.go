package report

import (
	"errors"
	"fmt"

	"github.com/sevico/muxray/internal/flv"
)

var ErrDetailV2Unsupported = errors.New("report: detail schema version 2 supports only AVC and MP3")

// NewTagDetailV2 projects explicitly requested deeper codec syntax. The
// original NewTagDetail route and its version-one documents stay unchanged.
func NewTagDetailV2(sourceSize uint64, tag flv.TagRecord, detail flv.ParsedTagDetail) (ParsedTagDetailDocument, error) {
	switch parsed := detail.(type) {
	case flv.AVCVideo:
		return NewAVCTagDetailV2(sourceSize, tag, parsed), nil
	case *flv.AVCVideo:
		if parsed != nil {
			return NewAVCTagDetailV2(sourceSize, tag, *parsed), nil
		}
	case flv.MP3Audio:
		return NewMP3TagDetailV2(sourceSize, tag, parsed), nil
	case *flv.MP3Audio:
		if parsed != nil {
			return NewMP3TagDetailV2(sourceSize, tag, *parsed), nil
		}
	}
	return nil, fmt.Errorf("%w: %T", ErrDetailV2Unsupported, detail)
}
