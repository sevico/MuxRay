package report

import (
	"errors"
	"testing"

	"github.com/sevico/muxray/internal/flv"
)

func TestDetailV2RejectsUnsupportedAndNilDetails(t *testing.T) {
	for _, detail := range []flv.ParsedTagDetail{nil, (*flv.AVCVideo)(nil), (*flv.MP3Audio)(nil), flv.AACAudio{}, flv.ScriptData{}} {
		if document, err := NewTagDetailV2(0, flv.TagRecord{}, detail); document != nil || !errors.Is(err, ErrDetailV2Unsupported) {
			t.Fatalf("unsupported %T: %v %v", detail, document, err)
		}
	}
}
