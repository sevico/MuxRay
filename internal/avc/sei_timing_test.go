package avc

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/sevico/muxray/internal/cursor"
)

func timingSPS(id uint32, nalHRD, vclHRD, picStruct bool, count uint32, offsetWidth uint8) []byte {
	var b avcBitBuilder
	b.write(66, 8)
	b.write(0, 8)
	b.write(30, 8)
	b.writeUE(id)
	b.writeUE(0)
	b.writeUE(0)
	b.writeUE(0)
	b.writeUE(1)
	b.write(0, 1)
	b.writeUE(19)
	b.writeUE(14)
	b.write(1, 1)
	b.write(1, 1)
	b.write(0, 1)
	b.write(1, 1)
	b.write(0, 4)
	b.write(1, 1)
	b.write(1001, 32)
	b.write(60000, 32)
	b.write(1, 1)
	hrd := func() {
		b.writeUE(count - 1)
		b.write(0, 8)
		for i := uint32(0); i < count; i++ {
			b.writeUE(i)
			b.writeUE(count - i)
			b.write(1, 1)
		}
		b.write(7, 5)
		b.write(7, 5)
		b.write(7, 5)
		b.write(uint64(offsetWidth), 5)
	}
	b.write(boolUint(nalHRD), 1)
	if nalHRD {
		hrd()
	}
	b.write(boolUint(vclHRD), 1)
	if vclHRD {
		hrd()
	}
	if nalHRD || vclHRD {
		b.write(0, 1)
	}
	b.write(boolUint(picStruct), 1)
	b.write(0, 1)
	return vuiSPSNAL(b)
}
func timingBuffer(id, count uint32, nalHRD, vclHRD bool) []byte {
	var b avcBitBuilder
	b.writeUE(id)
	for _, present := range []bool{nalHRD, vclHRD} {
		if present {
			for i := uint32(0); i < count; i++ {
				b.write(uint64(20+i), 8)
				b.write(uint64(7+i), 8)
			}
		}
	}
	b.write(1, 1)
	return seiMessageBytes(0, b.bytes())
}
func timingPicture(hrd bool, picStruct uint32, clocks bool, offsetWidth uint8, offset int64) []byte {
	var b avcBitBuilder
	if hrd {
		b.write(12, 8)
		b.write(3, 8)
	}
	b.write(uint64(picStruct), 4)
	if picStruct <= 8 {
		for i := uint32(0); i < [...]uint32{1, 1, 1, 2, 2, 3, 3, 2, 3}[picStruct]; i++ {
			b.write(boolUint(clocks), 1)
			if clocks {
				b.write(2, 2)
				b.write(0, 1)
				b.write(6, 5)
				b.write(1, 1)
				b.write(0, 1)
				b.write(1, 1)
				b.write(17, 8)
				b.write(59, 6)
				b.write(58, 6)
				b.write(23, 5)
				if offsetWidth > 0 {
					b.write(uint64(offset)&((uint64(1)<<offsetWidth)-1), offsetWidth)
				}
			}
		}
	}
	b.write(1, 1)
	return seiMessageBytes(1, b.bytes())
}
func parseTimingPacket(t *testing.T, context *ParameterSetContext, nals ...[]byte) NALUnitSequence {
	t.Helper()
	packet := prefixPacket(nals...)
	r, e := ParseLengthPrefixedNALUnits(bytes.NewReader(packet), cursor.Span{BitLength: uint32(len(packet) * 8)}, 4, Options{ParseSEI: true, ParseSEITiming: true, ParameterSets: context})
	if e != nil {
		t.Fatal(e)
	}
	if context == nil {
		assertSpansByReflection(t, reflect.ValueOf(r), 0, len(packet))
	}
	return r
}
func timingLast(r NALUnitSequence) *SEIMessageSequence { return r.NALUnits[len(r.NALUnits)-1].SEI }
func TestSEITimingBufferingBothHRDsAndPictureClocks(t *testing.T) {
	for _, count := range []uint32{1, 32} {
		r := parseTimingPacket(t, nil, timingSPS(0, true, true, true, count, 5), seiNAL(timingBuffer(0, count, true, true), timingPicture(true, 8, true, 5, -7)))
		if !r.Complete {
			t.Fatalf("count %d: %+v", count, r.Diagnostics)
		}
		messages := timingLast(r).Messages
		bp, pt := messages[0].BufferingPeriod, messages[1].PictureTiming
		if bp.Context.State != "resolved" || len(bp.NALInitialDelays) != int(count) || len(bp.VCLInitialDelays) != int(count) || bp.NALInitialDelays[count-1].InitialCPBRemovalDelay.Value != 19+count {
			t.Fatalf("BP %+v", bp)
		}
		if pt.Context.Source != "bufferingPeriod" || pt.CPBRemovalDelay.Value != 12 || pt.DPBOutputDelay.Value != 3 || len(pt.ClockTimestamps) != 3 {
			t.Fatalf("PT %+v", pt)
		}
		for _, c := range pt.ClockTimestamps {
			if c.TimeOffset.Value != -7 || c.SecondsValue.Value != 59 || c.HoursValue.Value != 23 {
				t.Fatalf("clock %+v", c)
			}
		}
	}
}
func TestSEITimingRequiresActivationAndKeepsV5Opaque(t *testing.T) {
	sps, sei := timingSPS(0, true, false, true, 1, 5), seiNAL(timingPicture(true, 0, false, 5, 0))
	r := parseTimingPacket(t, nil, sps, sei)
	if r.Complete || !seiHasCode(r, CodeSEITimingContextMissing) || timingLast(r).Messages[0].PictureTiming.CPBRemovalDelay != nil {
		t.Fatalf("guessed active SPS %+v", r.Diagnostics)
	}
	packet := prefixPacket(sps, sei)
	legacy, e := ParseLengthPrefixedNALUnits(bytes.NewReader(packet), cursor.Span{BitLength: uint32(len(packet) * 8)}, 4, Options{ParseSEI: true})
	if e != nil || !legacy.Complete || legacy.NALUnits[0].SequenceParameterSet.Timing != nil || timingLast(legacy).Messages[0].Kind != "unknown" || timingLast(legacy).Messages[0].PictureTiming != nil {
		t.Fatal("v5 changed")
	}
}
func TestSEITimingUsesOnlyFollowingSamePacketSlice(t *testing.T) {
	sei := seiNAL(timingPicture(true, 0, false, 5, 0))
	for _, before := range []bool{false, true} {
		nals := [][]byte{timingSPS(0, true, false, true, 1, 5), prefixPPS(sliceFixture{})}
		if before {
			nals = append(nals, prefixSlice(sliceFixture{sliceType: 2}), sei)
		} else {
			nals = append(nals, sei, prefixSlice(sliceFixture{sliceType: 2}))
		}
		r := parseTimingPacket(t, nil, nals...)
		if before {
			if !seiHasCode(r, CodeSEITimingContextAmbiguous) {
				t.Fatalf("preceding picture used: %+v", r.Diagnostics)
			}
			continue
		}
		if !r.Complete {
			t.Fatalf("before %v: %+v", before, r.Diagnostics)
		}
		for _, u := range r.NALUnits {
			if u.SEI != nil && u.SEI.Messages[0].PictureTiming.Context.Source != "slice" {
				t.Fatal("no slice evidence")
			}
		}
	}
}
func TestSEITimingRejectsMultiplePicturesAndSPSAfterMessage(t *testing.T) {
	sps, pps := timingSPS(0, true, false, true, 1, 5), prefixPPS(sliceFixture{})
	sei := seiNAL(timingPicture(true, 0, false, 5, 0))
	slice := prefixSlice(sliceFixture{sliceType: 2})
	r := parseTimingPacket(t, nil, sps, pps, sei, slice, slice)
	if !seiHasCode(r, CodeSEITimingContextAmbiguous) {
		t.Fatalf("multiple pictures %+v", r.Diagnostics)
	}
	r = parseTimingPacket(t, nil, sei, sps, pps, slice)
	if !seiHasCode(r, CodeSEITimingContextMissing) {
		t.Fatalf("future SPS used %+v", r.Diagnostics)
	}
}
func TestSEITimingCompactActivationAndReplacement(t *testing.T) {
	initial := parseTimingPacket(t, nil, timingSPS(0, true, false, true, 1, 5), seiNAL(timingBuffer(0, 1, true, false)))
	if !initial.Complete {
		t.Fatal(initial.Diagnostics)
	}
	context := NewParameterSetContext()
	for _, u := range initial.NALUnits {
		context.AddNALUnit(u)
	}
	compact := context.CompactClone()
	if compact.sps[0].VUI != nil || compact.sps[0].Timing == nil || compact.timingActive == nil {
		t.Fatal("timing summary lost")
	}
	sei := seiNAL(timingPicture(true, 0, true, 5, -1))
	a, b := parseTimingPacket(t, context, sei), parseTimingPacket(t, compact, sei)
	if !a.Complete || !reflect.DeepEqual(a, b) {
		t.Fatalf("cold/compact mismatch %+v", a.Diagnostics)
	}
	if !reflect.DeepEqual(context, compact) && context.timingActive != compact.timingActive {
		t.Fatal("wrong activation")
	}
	changed := parseTimingPacket(t, compact, timingSPS(0, true, false, true, 1, 6), sei)
	if !seiHasCode(changed, CodeSEITimingContextMissing) {
		t.Fatalf("stale replacement context %+v", changed.Diagnostics)
	}
	broken := parseTimingPacket(t, compact, []byte{0x67}, sei)
	if !seiHasCode(broken, CodeSEITimingContextMissing) {
		t.Fatalf("stale broken SPS %+v", broken.Diagnostics)
	}
	// Parsing clones the caller's context; neither successful nor bad updates leak.
	if !parseTimingPacket(t, compact, sei).Complete {
		t.Fatal("input context mutated")
	}
}
func TestSEITimingInvalidAndTruncatedPayloads(t *testing.T) {
	sps := timingSPS(0, true, false, true, 1, 5)
	for _, payload := range [][]byte{{0x80}, {0x80, 0, 0}} {
		r := parseTimingPacket(t, nil, sps, seiNAL(seiMessageBytes(0, payload)))
		if r.Complete {
			t.Fatal("bad BP accepted")
		}
		if seiHasCode(r, CodeSEITimingContextMissing) {
			t.Fatal("truncation mistaken for context absence")
		}
	}
	r := parseTimingPacket(t, nil, sps, seiNAL(timingBuffer(0, 1, true, false), timingPicture(true, 9, false, 5, 0)))
	if r.Complete || !seiHasCode(r, CodeSEISyntaxInvalid) {
		t.Fatal("pic_struct 9 accepted")
	}
	r = parseTimingPacket(t, nil, sps, seiNAL(timingBuffer(31, 1, true, false)))
	if !seiHasCode(r, CodeSEITimingContextMissing) || timingLast(r).Messages[0].BufferingPeriod.SequenceParameterSetID.Value != 31 {
		t.Fatal("missing referenced SPS")
	}
}
func TestSEITimingNoHRDDefaultSignedOffset(t *testing.T) {
	r := parseTimingPacket(t, nil, timingSPS(0, false, false, true, 1, 0), seiNAL(timingBuffer(0, 1, false, false), timingPicture(false, 0, true, 24, -8388608)))
	if !r.Complete {
		t.Fatal(r.Diagnostics)
	}
	p := timingLast(r).Messages[1].PictureTiming
	if p.CPBRemovalDelay != nil || p.ClockTimestamps[0].TimeOffset.Value != -8388608 || p.ClockTimestamps[0].TimeOffset.Span.BitLength != 24 {
		t.Fatalf("no-HRD fallback %+v", p)
	}
}
func TestSEITimingPartialClockAndNoOffset(t *testing.T) {
	for _, flags := range []uint64{0, 1, 3, 7} {
		var b avcBitBuilder
		b.write(1, 8)
		b.write(2, 8)
		b.write(0, 4)
		b.write(1, 1)
		b.write(0, 2)
		b.write(1, 1)
		b.write(0, 5)
		b.write(0, 3)
		b.write(4, 8)
		b.write(flags&1, 1)
		if flags&1 != 0 {
			b.write(5, 6)
			b.write((flags>>1)&1, 1)
			if flags&2 != 0 {
				b.write(6, 6)
				b.write((flags>>2)&1, 1)
				if flags&4 != 0 {
					b.write(7, 5)
				}
			}
		}
		b.write(1, 1)
		r := parseTimingPacket(t, nil, timingSPS(0, false, true, true, 1, 0), seiNAL(timingBuffer(0, 1, false, true), seiMessageBytes(1, b.bytes())))
		if !r.Complete {
			t.Fatalf("flags %d: %+v", flags, r.Diagnostics)
		}
		c := timingLast(r).Messages[1].PictureTiming.ClockTimestamps[0]
		if c.TimeOffset != nil || c.HoursValue != nil && c.HoursValue.Value != 7 {
			t.Fatalf("clock %+v", c)
		}
	}
}
func FuzzSEITimingBounded(f *testing.F) {
	f.Add(seiNAL(timingBuffer(0, 1, true, false), timingPicture(true, 0, true, 5, -1))[1:])
	f.Add(seiNAL(seiMessageBytes(1, []byte{0, 0, 0}))[1:])
	f.Fuzz(func(t *testing.T, nal []byte) {
		if len(nal) > 4096 {
			t.Skip()
		}
		nal = append([]byte{6}, nal...)
		r := parseTimingPacket(t, nil, timingSPS(0, true, false, true, 1, 5), nal)
		if len(r.NALUnits) > 2 || r.NALUnits[1].SEI.BytesRead > MaxSEINALBytes || len(r.NALUnits[1].SEI.Messages) > int(MaxSEIMessagesPerNAL) {
			t.Fatal("unbounded")
		}
	})
}

func TestSEITimingInvalidIDsAndUnsupportedSlicesClearActivation(t *testing.T) {
	initial := parseTimingPacket(t, nil, timingSPS(0, true, false, true, 1, 5), seiNAL(timingBuffer(0, 1, true, false)))
	context := NewParameterSetContext()
	for _, u := range initial.NALUnits {
		context.AddNALUnit(u)
	}
	pic := seiNAL(timingPicture(true, 0, false, 5, 0))
	for _, nal := range [][]byte{timingSPS(32, true, false, true, 1, 5), {0x62}, {0x63}, {0x64}, {0x73}, {0x74}, {0x75}, {0x6a}, {0x6b}} {
		r := parseTimingPacket(t, context, nal)
		changed := context.Clone()
		for _, u := range r.NALUnits {
			changed.AddNALUnit(u)
		}
		if changed.timingActive != nil {
			t.Fatalf("type %d retained activation", nal[0]&31)
		}
		r = parseTimingPacket(t, changed, pic)
		if !seiHasCode(r, CodeSEITimingContextMissing) {
			t.Fatalf("type %d used stale timing: %+v", nal[0]&31, r.Diagnostics)
		}
	}
}

func TestSEITimingBadSEIHeaderAndFramingCannotActivate(t *testing.T) {
	sps := timingSPS(0, true, false, true, 1, 5)
	good := seiNAL(timingBuffer(0, 1, true, false))
	for _, nal := range [][]byte{append([]byte{0x26}, good[1:]...), append([]byte{0x86}, good[1:]...), good[:len(good)-1]} {
		r := parseTimingPacket(t, nil, sps, nal)
		context := NewParameterSetContext()
		for _, u := range r.NALUnits {
			context.AddNALUnit(u)
		}
		if context.timingActive != nil {
			t.Fatal("invalid SEI published activation")
		}
	}
}

func TestSEITimingDifferentPicturesWithNonzeroFirstMB(t *testing.T) {
	r := parseTimingPacket(t, nil, timingSPS(0, true, false, true, 1, 5), prefixPPS(sliceFixture{}), seiNAL(timingPicture(true, 0, false, 5, 0)), prefixSlice(sliceFixture{sliceType: 2}), prefixSlice(sliceFixture{sliceType: 2, frame: 1, first: 1}))
	if !seiHasCode(r, CodeSEITimingContextAmbiguous) {
		t.Fatalf("distinct frame_num ignored: %+v", r.Diagnostics)
	}
}
func TestSEITimingUnknownNALAndAccessUnitBoundaryDoNotReuseSlice(t *testing.T) {
	for _, barrier := range [][]byte{{0}, {9}, {0x6a}, {0x6b}, {0x6e}, {0x6f}} {
		r := parseTimingPacket(t, nil, timingSPS(0, true, false, true, 1, 5), prefixPPS(sliceFixture{}), seiNAL(timingPicture(true, 0, false, 5, 0)), prefixSlice(sliceFixture{sliceType: 2}), barrier)
		if !seiHasCode(r, CodeSEITimingContextAmbiguous) {
			t.Fatalf("NAL type %d did not fence picture evidence", barrier[0]&31)
		}
	}
}
