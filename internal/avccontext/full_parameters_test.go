package avccontext

import (
	"bytes"
	"github.com/sevico/muxray/internal/avc"
	"github.com/sevico/muxray/internal/flv"
	"reflect"
	"testing"
)

func TestFullParameterCheckpointModeAndNoSliceReads(t *testing.T) {
	data, index := fixture(t, baseParameterConfig(), parameterNAL(4, []byte{0x65}), parameterNAL(4, []byte{0x65, 0xb8, 0x40, 0xf0}))
	options := flv.AVCVideoOptions{AVC: avc.Options{ParseFullHeaders: true, ParseSliceHeaders: true}}
	r, cp, e := ResolveParameterSegment(t.Context(), bytes.NewReader(data), selected(index), lookup(index), parameterFraming(), options, ParameterLimits{MaxTags: 1}, nil)
	if e != nil || cp == nil || r.State != StateBudgetExhausted {
		t.Fatalf("first %+v %v", r, e)
	}
	var cache ParameterCheckpoints
	cache.Commit(cp)
	if cache.Lookup(parameterFraming(), 2, flv.AVCVideoOptions{}) != nil || cache.Lookup(parameterFraming(), 2, options) == nil {
		t.Fatal("full/prefix checkpoint mode leaked")
	}
	r, cp, e = ResolveParameterSegment(t.Context(), bytes.NewReader(data), selected(index), lookup(index), parameterFraming(), options, ParameterLimits{}, cp)
	if e != nil || r.State != StateFound || r.BarrierCount != 0 || cp.options.ParameterSets != nil {
		t.Fatalf("replay parsed opaque truncated slice or retained context pointer %+v %v", r, e)
	}
	whole, e := ResolveParameters(t.Context(), bytes.NewReader(data), selected(index), lookup(index), parameterFraming(), options, ParameterLimits{})
	if e != nil {
		t.Fatal(e)
	}
	parse := func(c *avc.ParameterSetContext) flv.AVCVideo {
		o := options
		o.NALLengthSize = 4
		o.AVC.ParameterSets = c
		p, e := flv.ParseAVCVideo(bytes.NewReader(data), selected(index), o)
		if e != nil || !p.Complete || !p.NALUnits.NALUnits[0].SliceHeader.Full.Complete {
			t.Fatalf("parse %v %+v", e, p.Diagnostics)
		}
		return p
	}
	if !reflect.DeepEqual(parse(r.Context), parse(whole.Context)) {
		t.Fatal("compaction changed full header")
	}
}
