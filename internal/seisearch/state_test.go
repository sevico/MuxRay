package seisearch

import (
	"reflect"
	"testing"

	"github.com/sevico/muxray/internal/avc"
	"github.com/sevico/muxray/internal/cursor"
)

func TestMaximumStateRetainsLessThan512KiB(t *testing.T) {
	sets := avc.NewParameterSetContext()
	mapped := avc.MappedSpan{BitLength: 1, SourceSpans: make([]cursor.Span, 128)}
	for id := uint32(0); id < 32; id++ {
		sets.AddNALUnit(avc.NALUnit{HeaderPresent: true, SummaryComplete: true, Header: avc.NALHeader{NALUnitType: 7}, SequenceParameterSet: &avc.SequenceParameterSet{Complete: true, SequenceParameterSetID: id, SequenceParameterSetIDSpan: mapped, Timing: &avc.SPSTimingParameters{SPSID: id, SPSSpan: cursor.Span{ByteOffset: uint64(id + 1), BitLength: 8}, NALHRD: avc.HRDTimingParameters{Present: true, CPBCount: 32, InitialCPBRemovalDelayLength: 32, CPBRemovalDelayLength: 32, DPBOutputDelayLength: 32, TimeOffsetLength: 31}, VCLHRD: avc.HRDTimingParameters{Present: true, CPBCount: 32}}}})
	}
	for id := uint32(0); id < 256; id++ {
		sets.AddNALUnit(avc.NALUnit{HeaderPresent: true, SummaryComplete: true, Header: avc.NALHeader{NALUnitType: 8}, PictureParameterSet: &avc.PictureParameterSet{Complete: true, PictureParameterSetID: id, PictureParameterSetIDSpan: mapped, Deep: &avc.PPSDetails{PrefixComplete: true, PicInitQPMinus26: &avc.SignedField{Span: mapped}, RedundantPicCntPresentFlag: &avc.VUIBooleanField{Span: mapped},
			NumSliceGroupsMinus1: &avc.VUIUnsignedField{Span: mapped}, NumRefIdxL0DefaultActiveMinus1: &avc.VUIUnsignedField{Span: mapped}, NumRefIdxL1DefaultActiveMinus1: &avc.VUIUnsignedField{Span: mapped}, WeightedPredFlag: &avc.VUIBooleanField{Span: mapped}, WeightedBipredIDC: &avc.VUIUnsignedField{Span: mapped}, PicInitQSMinus26: &avc.SignedField{Span: mapped}, DeblockingFilterControlPresentFlag: &avc.VUIBooleanField{Span: mapped},
			Full: &avc.PPSFullDetails{Complete: true, SliceGroupMapType: &avc.VUIUnsignedField{Span: mapped}, SliceGroupChangeRateMinus1: &avc.VUIUnsignedField{Span: mapped}, SliceGroupID: make([]*avc.VUIUnsignedField, avc.MaxFMOMapUnits), ScalingLists: make([]avc.ScalingList, 12)},
		}}})
	}
	state := State{sets: sets.CompactClone(), filter: "bufferingPeriod"}
	for range MaxPending {
		index, payload := uint32(0), uint64(0)
		span := cursor.Span{BitLength: 8}
		state.pending = append(state.pending, cloneRow(Row{NALIndex: &index, MessageIndex: &index, PayloadType: &payload, SPSID: &index, SourceSpan: &span, SPSSpan: &span, EvidenceSpan: &span, Kind: "userDataUnregistered", Status: "incomplete", ContextState: "not_applicable", ContextSource: "bufferingPeriod", Reason: "timing_context_ambiguous"}))
	}
	// Count capacity, pointees and string backing storage. Counting repeated
	// constant strings separately intentionally overestimates their heap cost.
	seen := map[uintptr]bool{}
	var heap func(reflect.Value, bool) uint64
	heap = func(v reflect.Value, inContext bool) uint64 {
		var n uint64
		switch v.Kind() {
		case reflect.Pointer:
			if !v.IsNil() && !seen[v.Pointer()] {
				seen[v.Pointer()] = true
				n = uint64(v.Type().Elem().Size()) + heap(v.Elem(), inContext || v.Type() == reflect.TypeFor[*avc.ParameterSetContext]())
			}
		case reflect.Struct:
			for i := 0; i < v.NumField(); i++ {
				n += heap(v.Field(i), inContext)
			}
		case reflect.Array:
			for i := 0; i < v.Len(); i++ {
				n += heap(v.Index(i), inContext)
			}
		case reflect.Slice:
			if inContext && v.Len() != 0 {
				t.Fatal("context retains dynamic mappings or payload")
			}
			n += uint64(v.Cap()) * uint64(v.Type().Elem().Size())
			for i := 0; i < v.Len(); i++ {
				n += heap(v.Index(i), inContext)
			}
		case reflect.String:
			n += uint64(v.Len())
		}
		return n
	}
	for name, state := range map[string]State{"pending capacity": state, "clone": state.Clone()} {
		seen = map[uintptr]bool{}
		retained := uint64(reflect.TypeFor[State]().Size()) + heap(reflect.ValueOf(state), false)
		if retained > 512<<10 {
			t.Fatalf("%s retains %d bytes", name, retained)
		}
		t.Logf("%s: maximum 32 SPS + 256 PPS + %d rows retains at most %d Go storage bytes", name, MaxPending, retained)
	}
}
