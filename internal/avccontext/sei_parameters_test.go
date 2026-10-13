package avccontext

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/sevico/muxray/internal/avc"
	"github.com/sevico/muxray/internal/flv"
)

type seiReplayGuard struct {
	io.ReaderAt
	start, end int64
}

func (r seiReplayGuard) ReadAt(p []byte, offset int64) (int, error) {
	if offset < r.end && offset+int64(len(p)) > r.start {
		return 0, errors.New("parameter replay inspected SEI payload")
	}
	return r.ReaderAt.ReadAt(p, offset)
}
func TestParameterReplayNeverInspectsSEI(t *testing.T) {
	data, index := fixture(t, baseParameterConfig(), parameterNAL(4, []byte{6, 255, 255, 255}), parameterNAL(4, []byte{6, 1, 0, 128}))
	tag, _ := index.Get(1)
	// FLV payload has five video bytes, a four-byte NAL length and one NAL header.
	start := int64(tag.PayloadSpan().ByteOffset) + 10
	reader := seiReplayGuard{ReaderAt: bytes.NewReader(data), start: start, end: start + 3}
	o := flv.AVCVideoOptions{AVC: avc.Options{ParseSEI: true, ParseFullHeaders: true, ParseSliceHeaders: true}}
	r, cp, e := ResolveParameterSegment(t.Context(), reader, selected(index), lookup(index), parameterFraming(), o, ParameterLimits{}, nil)
	if e != nil || r.State != StateFound || r.BarrierCount != 0 || cp.options.ParseSEI || r.Context.UsablePPSCount() != 1 {
		t.Fatalf("replay %+v %v", r, e)
	}
	var cache ParameterCheckpoints
	cache.Commit(cp)
	o.AVC.ParseSEI = false
	if cache.Lookup(parameterFraming(), 2, o) == nil {
		t.Fatal("SEI selected-tag option prevented compatible parameter checkpoint reuse")
	}
}
