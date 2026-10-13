package avccontext

import (
	"bytes"
	"context"
	"errors"
	"io"
	"reflect"
	"testing"

	"github.com/sevico/muxray/internal/avc"
	"github.com/sevico/muxray/internal/cursor"
	"github.com/sevico/muxray/internal/flv"
)

func TestParameterSegmentsReachSameContextWithBoundedWork(t *testing.T) {
	packets := []packet{baseParameterConfig()}
	for range 12 {
		packets = append(packets, parameterNAL(4, []byte{9}))
	}
	packets = append(packets, parameterNAL(4, []byte{0x65, 0xb8, 0x40}))
	data, index := fixture(t, packets...)
	full, err := ResolveParameters(t.Context(), bytes.NewReader(data), selected(index), lookup(index), parameterFraming(), flv.AVCVideoOptions{}, ParameterLimits{})
	if err != nil || full.State != StateFound {
		t.Fatalf("full=%+v err=%v", full, err)
	}
	var checkpoint *ParameterCheckpoint
	var got ParameterResolution
	for start := uint64(0); start < 13; start += 4 {
		got, checkpoint, err = ResolveParameterSegment(t.Context(), bytes.NewReader(data), selected(index), lookup(index), parameterFraming(), flv.AVCVideoOptions{}, ParameterLimits{MaxTags: 4}, checkpoint)
		if err != nil || checkpoint == nil || got.ResumeStartTagID != start || got.ScannedTags > 4 || got.BytesRead > DefaultParameterMaxBytes {
			t.Fatalf("segment=%+v err=%v", got, err)
		}
		if got.NextTagID < 13 && (got.State != StateBudgetExhausted || !got.CanResume || got.Context != nil) {
			t.Fatalf("partial=%+v", got)
		}
	}
	if got.State != StateFound || got.CanResume || got.NextTagID != 13 {
		t.Fatalf("final=%+v", got)
	}
	parse := func(sets *avc.ParameterSetContext) flv.AVCVideo {
		p, e := flv.ParseAVCVideo(bytes.NewReader(data), selected(index), flv.AVCVideoOptions{NALLengthSize: 4, AVC: avc.Options{ParsePPS: true, ParseSliceHeaders: true, ParameterSets: sets}})
		if e != nil || !p.Complete {
			t.Fatalf("slice=%+v err=%v", p, e)
		}
		return p
	}
	if !reflect.DeepEqual(parse(full.Context), parse(got.Context)) {
		t.Fatal("compact resumed context changed slice syntax or source spans")
	}
	reused, _, err := ResolveParameterSegment(t.Context(), bytes.NewReader(data), selected(index), lookup(index), parameterFraming(), flv.AVCVideoOptions{}, ParameterLimits{}, checkpoint)
	if err != nil || reused.State != StateFound || reused.ScannedTags != 0 || reused.BytesRead != 2 || reused.ResumeStartTagID != 13 {
		t.Fatalf("exact cache hit=%+v err=%v", reused, err)
	}
}

func TestParameterCheckpointExcludesPartlyReadReplacement(t *testing.T) {
	data, index := fixture(t, baseParameterConfig(), parameterConfig(4, [][]byte{parameterSPS(1, false)}, nil), nalu())
	first, seed, err := ResolveParameterSegment(t.Context(), bytes.NewReader(data), selected(index), lookup(index), parameterFraming(), flv.AVCVideoOptions{}, ParameterLimits{MaxTags: 1}, nil)
	if err != nil || seed == nil || first.NextTagID != 1 {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	got, checkpoint, err := ResolveParameterSegment(t.Context(), bytes.NewReader(data), selected(index), lookup(index), parameterFraming(), flv.AVCVideoOptions{}, ParameterLimits{MaxBytes: first.BytesRead + 8}, nil)
	if err != nil || got.State != StateBudgetExhausted || !got.CanResume || got.NextTagID != 1 || checkpoint == nil {
		t.Fatalf("partial replacement=%+v err=%v", got, err)
	}
	s, p := checkpoint.sets.Counts()
	if s != 1 || p != 1 || checkpoint.width != 4 {
		t.Fatal("partial configuration mutated preceding snapshot")
	}
	got, _, err = ResolveParameterSegment(t.Context(), bytes.NewReader(data), selected(index), lookup(index), parameterFraming(), flv.AVCVideoOptions{}, ParameterLimits{}, checkpoint)
	if err != nil || got.State != StateFound || got.SPSCount != 1 || got.PPSCount != 0 || got.ResumeStartTagID != 1 {
		t.Fatalf("replacement retry=%+v err=%v", got, err)
	}
	s, p = seed.sets.Counts()
	if s != 1 || p != 1 {
		t.Fatal("replay mutated prior immutable checkpoint")
	}
}

func TestParameterSegmentCannotContinueOversizedTag(t *testing.T) {
	large := append(parameterSPS(1, false), bytes.Repeat([]byte{0xff}, 60000)...)
	nals := make([][]byte, 18)
	for i := range nals {
		nals[i] = large
	}
	data, index := fixture(t, baseParameterConfig(), parameterNAL(4, nals...), nalu())
	got, checkpoint, err := ResolveParameterSegment(t.Context(), bytes.NewReader(data), selected(index), lookup(index), parameterFraming(), flv.AVCVideoOptions{}, ParameterLimits{}, nil)
	if err != nil || !got.CanResume || got.NextTagID != 1 || checkpoint == nil {
		t.Fatalf("first=%+v err=%v", got, err)
	}
	got, next, err := ResolveParameterSegment(t.Context(), bytes.NewReader(data), selected(index), lookup(index), parameterFraming(), flv.AVCVideoOptions{}, ParameterLimits{}, checkpoint)
	if err != nil || got.State != StateBudgetExhausted || got.CanResume || got.NextTagID != 1 || got.ResumeStartTagID != 1 || next != nil || got.Context != nil {
		t.Fatalf("unresumable=%+v err=%v", got, err)
	}
}

func TestParameterSegmentErrorsDoNotPublishCheckpoints(t *testing.T) {
	data, index := fixture(t, baseParameterConfig(), nalu(), nalu())
	ctx, cancel := context.WithCancel(t.Context())
	get := lookup(index)
	got, checkpoint, err := ResolveParameterSegment(ctx, bytes.NewReader(data), selected(index), func(id uint64) (flv.TagRecord, error) {
		if id == 1 {
			cancel()
		}
		return get(id)
	}, parameterFraming(), flv.AVCVideoOptions{}, ParameterLimits{}, nil)
	if !errors.Is(err, context.Canceled) || checkpoint != nil || got.Context != nil || got.Configurations != 1 {
		t.Fatalf("cancel=%+v checkpoint=%v err=%v", got, checkpoint, err)
	}
	middle, _ := index.Get(1)
	reader := readerFunc(func(p []byte, off int64) (int, error) {
		if off == int64(middle.PayloadSpan().ByteOffset) {
			return 0, io.ErrClosedPipe
		}
		return bytes.NewReader(data).ReadAt(p, off)
	})
	got, checkpoint, err = ResolveParameterSegment(t.Context(), reader, selected(index), get, parameterFraming(), flv.AVCVideoOptions{}, ParameterLimits{}, nil)
	if !errors.Is(err, io.ErrClosedPipe) || checkpoint != nil || got.Context != nil {
		t.Fatalf("source error=%+v checkpoint=%v err=%v", got, checkpoint, err)
	}
}

func TestParameterSegmentBarriersAndCheckpointIdentity(t *testing.T) {
	data, index := fixture(t, baseParameterConfig(), packet{9, []byte{0x17, 0, 0, 0, 0, 1, 66}}, nalu(), nalu())
	got, checkpoint, err := ResolveParameterSegment(t.Context(), bytes.NewReader(data), selected(index), lookup(index), parameterFraming(), flv.AVCVideoOptions{}, ParameterLimits{MaxTags: 2}, nil)
	if err != nil || got.State != StateBudgetExhausted || checkpoint == nil || checkpoint.width != 0 {
		t.Fatalf("barrier segment=%+v err=%v", got, err)
	}
	got, _, err = ResolveParameterSegment(t.Context(), bytes.NewReader(data), selected(index), lookup(index), parameterFraming(), flv.AVCVideoOptions{}, ParameterLimits{}, checkpoint)
	if err != nil || got.State != StateInvalidated || got.Context != nil {
		t.Fatalf("stale parameters=%+v err=%v", got, err)
	}
	framing := parameterFraming()
	framing.SourceTagID = 1
	if _, _, err = ResolveParameterSegment(t.Context(), bytes.NewReader(data), selected(index), lookup(index), framing, flv.AVCVideoOptions{}, ParameterLimits{}, checkpoint); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("wrong checkpoint accepted: %v", err)
	}
}

func TestParameterCheckpointCacheBoundAndBackwardSelection(t *testing.T) {
	var cache ParameterCheckpoints
	o, _ := parameterOptions(avc.Options{})
	for next := uint64(1); next <= 24; next++ {
		cache.Commit(&ParameterCheckpoint{nextTagID: next, declaredWidth: 4, options: o, sets: avc.NewParameterSetContext()})
	}
	if cache.Count() != MaxParameterCheckpoints {
		t.Fatalf("cache count %d", cache.Count())
	}
	if cache.Lookup(parameterFraming(), 16, flv.AVCVideoOptions{}) != nil {
		t.Fatal("evicted/future checkpoint reused")
	}
	if p := cache.Lookup(parameterFraming(), 20, flv.AVCVideoOptions{}); p == nil || p.nextTagID != 20 {
		t.Fatalf("nearest checkpoint %+v", p)
	}
	if cache.Lookup(parameterFraming(), 30, flv.AVCVideoOptions{AVC: avc.Options{MaxNALUnits: 1}}) != nil {
		t.Fatal("different parser limits reused")
	}
	cache.Commit(cache.entries[0])
	if cache.Count() != MaxParameterCheckpoints {
		t.Fatal("duplicate changed count")
	}
	cache.Clear()
	if cache.Count() != 0 || cache.entries[0] != nil {
		t.Fatal("clear retains checkpoint")
	}
}

func TestCompactCheckpointStorageHasFixedBound(t *testing.T) {
	sets := avc.NewParameterSetContext()
	span := avc.MappedSpan{BitLength: 1, SourceSpans: make([]cursor.Span, 128)}
	for id := uint32(0); id < 32; id++ {
		sets.AddNALUnit(avc.NALUnit{HeaderPresent: true, SummaryComplete: true, Header: avc.NALHeader{NALUnitType: 7}, SequenceParameterSet: &avc.SequenceParameterSet{Complete: true, SequenceParameterSetID: id, SequenceParameterSetIDSpan: span, Timing: &avc.SPSTimingParameters{SPSID: id, SPSSpan: cursor.Span{ByteOffset: uint64(id + 1), BitLength: 8}, NALHRD: avc.HRDTimingParameters{Present: true, CPBCount: 32, InitialCPBRemovalDelayLength: 32, CPBRemovalDelayLength: 32, DPBOutputDelayLength: 32, TimeOffsetLength: 31}, VCLHRD: avc.HRDTimingParameters{Present: true, CPBCount: 32}}}})
	}
	for id := uint32(0); id < 256; id++ {
		sets.AddNALUnit(avc.NALUnit{HeaderPresent: true, SummaryComplete: true, Header: avc.NALHeader{NALUnitType: 8}, PictureParameterSet: &avc.PictureParameterSet{Complete: true, PictureParameterSetID: id, PictureParameterSetIDSpan: span, Deep: &avc.PPSDetails{PrefixComplete: true, PicInitQPMinus26: &avc.SignedField{Span: span}, RedundantPicCntPresentFlag: &avc.VUIBooleanField{Span: span},
			NumSliceGroupsMinus1: &avc.VUIUnsignedField{Span: span}, NumRefIdxL0DefaultActiveMinus1: &avc.VUIUnsignedField{Span: span}, NumRefIdxL1DefaultActiveMinus1: &avc.VUIUnsignedField{Span: span}, WeightedPredFlag: &avc.VUIBooleanField{Span: span}, WeightedBipredIDC: &avc.VUIUnsignedField{Span: span}, PicInitQSMinus26: &avc.SignedField{Span: span}, DeblockingFilterControlPresentFlag: &avc.VUIBooleanField{Span: span},
			Full: &avc.PPSFullDetails{Complete: true, SliceGroupMapType: &avc.VUIUnsignedField{Span: span}, SliceGroupChangeRateMinus1: &avc.VUIUnsignedField{Span: span}, SliceGroupID: make([]*avc.VUIUnsignedField, avc.MaxFMOMapUnits), ScalingLists: make([]avc.ScalingList, 12)},
		}}})
	}
	checkpoint := &ParameterCheckpoint{sets: sets.CompactClone()}
	seen := map[uintptr]bool{}
	var heap func(reflect.Value) uint64
	heap = func(v reflect.Value) uint64 {
		var n uint64
		switch v.Kind() {
		case reflect.Pointer:
			if !v.IsNil() && !seen[v.Pointer()] {
				seen[v.Pointer()] = true
				n = uint64(v.Type().Elem().Size()) + heap(v.Elem())
			}
		case reflect.Struct:
			for i := 0; i < v.NumField(); i++ {
				n += heap(v.Field(i))
			}
		case reflect.Array:
			for i := 0; i < v.Len(); i++ {
				n += heap(v.Index(i))
			}
		case reflect.Slice, reflect.String:
			if v.Len() != 0 {
				t.Fatal("compact context retains dynamic data")
			}
		}
		return n
	}
	retained := heap(reflect.ValueOf(checkpoint))
	if retained > MaxParameterCheckpointBytes {
		t.Fatalf("checkpoint=%d exceeds %d", retained, MaxParameterCheckpointBytes)
	}
	t.Logf("full 32-SPS/256-PPS checkpoint retains %d Go storage bytes; cap %d", retained, MaxParameterCheckpointBytes)
}
