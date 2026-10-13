package avccontext

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/sevico/muxray/internal/avc"
	"github.com/sevico/muxray/internal/flv"
)

func replayTimingSPS() []byte {
	var b parameterBits
	b.bits(66, 8)
	b.bits(0, 8)
	b.bits(30, 8)
	b.ue(0)
	b.ue(0)
	b.ue(0)
	b.ue(0)
	b.ue(1)
	b.bits(0, 1)
	b.ue(19)
	b.ue(14)
	b.bits(1, 1)
	b.bits(1, 1)
	b.bits(0, 1)
	b.bits(1, 1)
	b.bits(0, 5) // no aspect/overscan/video/chroma/timing
	b.bits(1, 1)
	b.ue(0)
	b.bits(0, 8)
	b.ue(0)
	b.ue(0)
	b.bits(1, 1)
	b.bits(7, 5)
	b.bits(7, 5)
	b.bits(7, 5)
	b.bits(0, 5)
	b.bits(0, 4) // no VCL HRD, low delay, pic_struct, restrictions
	b.bits(1, 1)
	return b.nal(0x67)
}
func replayTimingBP() []byte  { return []byte{6, 0, 3, 0x8a, 0x03, 0xc0, 0x80} } // ID=0, delay=20, offset=7, alignment
func replayTimingPic() []byte { return []byte{6, 1, 2, 12, 3, 0x80} }
func TestSEITimingReplayPreservesActivationColdWarmAndSeparatesCache(t *testing.T) {
	config := parameterConfig(4, [][]byte{replayTimingSPS()}, [][]byte{parameterPPS(0, 0, false)})
	packets := []packet{config, parameterNAL(4, replayTimingBP())}
	for range 5 {
		packets = append(packets, parameterNAL(4, []byte{9}))
	}
	packets = append(packets, parameterNAL(4, replayTimingPic()))
	data, index := fixture(t, packets...)
	o := flv.AVCVideoOptions{AVC: avc.Options{ParseSEITiming: true}}
	cold, e := ResolveParameters(t.Context(), bytes.NewReader(data), selected(index), lookup(index), parameterFraming(), o, ParameterLimits{})
	if e != nil || cold.State != StateFound {
		t.Fatalf("cold %+v %v", cold, e)
	}
	var cp *ParameterCheckpoint
	var warm ParameterResolution
	for warm.NextTagID < selected(index).ID {
		warm, cp, e = ResolveParameterSegment(t.Context(), bytes.NewReader(data), selected(index), lookup(index), parameterFraming(), o, ParameterLimits{MaxTags: 2}, cp)
		if e != nil || cp == nil {
			t.Fatalf("warm %+v %v", warm, e)
		}
	}
	parse := func(c *avc.ParameterSetContext) flv.AVCVideo {
		p, e := flv.ParseAVCVideo(bytes.NewReader(data), selected(index), flv.AVCVideoOptions{NALLengthSize: 4, AVC: avc.Options{ParseSEI: true, ParseSEITiming: true, ParameterSets: c}})
		if e != nil || !p.Complete {
			t.Fatalf("selected %+v %v", p.Diagnostics, e)
		}
		m := p.NALUnits.NALUnits[0].SEI.Messages[0]
		if m.PictureTiming.Context.Source != "bufferingPeriod" || m.PictureTiming.CPBRemovalDelay.Value != 12 {
			t.Fatalf("timing %+v", m)
		}
		return p
	}
	if !reflect.DeepEqual(parse(cold.Context), parse(warm.Context)) {
		t.Fatal("timing cold/warm differs")
	}
	var cache ParameterCheckpoints
	cache.Commit(cp)
	if cache.Lookup(parameterFraming(), selected(index).ID, o) == nil {
		t.Fatal("timing checkpoint not reusable")
	}
	if cache.Lookup(parameterFraming(), selected(index).ID, flv.AVCVideoOptions{}) != nil {
		t.Fatal("timing checkpoint reused by legacy depth")
	}
	legacy, old, e := ResolveParameterSegment(t.Context(), bytes.NewReader(data), selected(index), lookup(index), parameterFraming(), flv.AVCVideoOptions{}, ParameterLimits{}, nil)
	if e != nil || legacy.State != StateFound || old.options.ParseSEI || old.options.ParseSEITiming || old.options.ParseVUI {
		t.Fatal("legacy parameter path expanded")
	}
	cache.Clear()
	cache.Commit(old)
	if cache.Lookup(parameterFraming(), selected(index).ID, o) != nil {
		t.Fatal("shallow checkpoint reused for timing")
	}
	if warm.BytesRead > DefaultParameterMaxBytes || warm.ScannedTags > 2 {
		t.Fatal("replay budgets expanded")
	}
}
func TestSEITimingReplayDropsInvalidatingUpdates(t *testing.T) {
	config := parameterConfig(4, [][]byte{replayTimingSPS()}, [][]byte{parameterPPS(0, 0, false)})
	for _, update := range []packet{parameterNAL(4, parameterSPS(32, false)), parameterNAL(4, []byte{0x62}), parameterNAL(4, []byte{0x65}), parameterNAL(4, replayTimingSPS()), parameterConfig(4, [][]byte{replayTimingSPS()}, nil)} {
		data, index := fixture(t, config, parameterNAL(4, replayTimingBP()), update, parameterNAL(4, replayTimingPic()))
		o := flv.AVCVideoOptions{AVC: avc.Options{ParseSEITiming: true}}
		r, e := ResolveParameters(t.Context(), bytes.NewReader(data), selected(index), lookup(index), parameterFraming(), o, ParameterLimits{})
		if e != nil {
			t.Fatal(e)
		}
		p, e := flv.ParseAVCVideo(bytes.NewReader(data), selected(index), flv.AVCVideoOptions{NALLengthSize: 4, AVC: avc.Options{ParseSEI: true, ParseSEITiming: true, ParameterSets: r.Context}})
		if e != nil || p.Complete || p.NALUnits.NALUnits[0].SEI.Messages[0].PictureTiming.Context.State != "missing" {
			t.Fatalf("stale activation %+v %v", p.Diagnostics, e)
		}
	}
}
