package avc

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sync"
	"testing"

	"github.com/sevico/muxray/internal/cursor"
)

type sliceFixture struct {
	poc                                            uint32
	fields, bottom, deltaAlways, redundant, colour bool
	first, sliceType, ppsID, frame, idrID          uint32
	idr                                            bool
}

func prefixSPS(f sliceFixture) []byte {
	var b avcBitBuilder
	if f.colour {
		b.write(100, 8)
	} else {
		b.write(66, 8)
	}
	b.write(0, 8)
	b.write(40, 8)
	b.writeUE(0)
	if f.colour {
		b.writeUE(3)
		b.write(1, 1)
		b.writeUE(0)
		b.writeUE(0)
		b.write(0, 2)
	}
	b.writeUE(0)
	b.writeUE(f.poc)
	if f.poc == 0 {
		b.writeUE(0)
	}
	if f.poc == 1 {
		b.write(boolUint(f.deltaAlways), 1)
		b.writeSE(0)
		b.writeSE(0)
		b.writeUE(0)
	}
	b.writeUE(1)
	b.write(0, 1)
	b.writeUE(65535)
	b.writeUE(65535)
	b.write(boolUint(!f.fields), 1)
	if f.fields {
		b.write(0, 1)
	}
	b.write(1, 1)
	b.write(0, 2)
	b.write(1, 1)
	return append([]byte{0x67}, escapeRBSP(b.bytes())...)
}

func prefixPPS(f sliceFixture) []byte {
	var b avcBitBuilder
	b.writeUE(f.ppsID)
	b.writeUE(0)
	b.write(0, 1)
	b.write(boolUint(f.bottom), 1)
	b.writeUE(0)
	b.writeUE(0)
	b.writeUE(0)
	b.write(0, 3)
	b.writeSE(0)
	b.writeSE(0)
	b.writeSE(0)
	b.write(1, 1)
	b.write(0, 1)
	b.write(boolUint(f.redundant), 1)
	b.write(1, 1)
	return append([]byte{0x68}, escapeRBSP(b.bytes())...)
}

func prefixSlice(f sliceFixture) []byte {
	var b avcBitBuilder
	b.writeUE(f.first)
	b.writeUE(f.sliceType)
	b.writeUE(f.ppsID)
	if f.colour {
		b.write(2, 2)
	}
	b.write(uint64(f.frame), 4)
	if f.fields {
		b.write(1, 1)
		b.write(1, 1)
	}
	if f.idr {
		b.writeUE(f.idrID)
	}
	if f.poc == 0 {
		b.write(7, 4)
		if f.bottom && !f.fields {
			b.writeSE(-2)
		}
	}
	if f.poc == 1 && !f.deltaAlways {
		b.writeSE(3)
		if f.bottom && !f.fields {
			b.writeSE(-2)
		}
	}
	if f.redundant {
		b.writeUE(2)
	}
	header := byte(0x41)
	if f.idr {
		header = 0x65
	}
	return append([]byte{header}, escapeRBSP(b.bytes())...)
}
func boolUint(v bool) uint64 {
	if v {
		return 1
	}
	return 0
}
func prefixPacket(units ...[]byte) []byte {
	var packet []byte
	for _, unit := range units {
		packet = binary.BigEndian.AppendUint32(packet, uint32(len(unit)))
		packet = append(packet, unit...)
	}
	return packet
}
func parsePrefixPacket(t testing.TB, packet []byte, context *ParameterSetContext) NALUnitSequence {
	t.Helper()
	r, err := ParseLengthPrefixedNALUnits(bytes.NewReader(packet), cursor.Span{BitLength: uint32(len(packet) * 8)}, 4, Options{ParsePPS: true, ParseSliceHeaders: true, ParameterSets: context})
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func prefixContext(t testing.TB, f sliceFixture) *ParameterSetContext {
	t.Helper()
	r := parsePrefixPacket(t, prefixPacket(prefixSPS(f), prefixPPS(f)), nil)
	if !r.Complete {
		t.Fatalf("context parse: %+v", r.Diagnostics)
	}
	c := NewParameterSetContext()
	for _, unit := range r.NALUnits {
		c.AddNALUnit(unit)
	}
	if c.UsablePPSCount() != 1 {
		t.Fatal("missing usable pair")
	}
	return c
}

func TestSlicePrefixBranches(t *testing.T) {
	for _, f := range []sliceFixture{
		{poc: 0, sliceType: 2, idr: true},
		{poc: 0, sliceType: 0, frame: 3, bottom: true},
		{poc: 0, sliceType: 6, frame: 3, first: 2},
		{poc: 1, sliceType: 1, frame: 4, bottom: true, redundant: true},
		{poc: 1, sliceType: 0, frame: 4, deltaAlways: true},
		{poc: 2, sliceType: 0, frame: 5},
		{poc: 0, sliceType: 2, fields: true, idr: true, colour: true},
		{poc: 1, sliceType: 1, fields: true, bottom: true},
	} {
		t.Run(fmt.Sprintf("%+v", f), func(t *testing.T) {
			c := prefixContext(t, f)
			r := parsePrefixPacket(t, prefixPacket(prefixSlice(f)), c)
			if !r.Complete {
				t.Fatalf("slice: %+v", r.Diagnostics)
			}
			s := r.NALUnits[0].SliceHeader
			if s == nil || !s.PrefixComplete || s.FrameNum.Value != f.frame || s.SliceType.Value != f.sliceType || s.FirstMBInSlice.Value != f.first {
				t.Fatalf("slice %+v", s)
			}
			if s.ReferencedSPSID == nil || *s.ReferencedSPSID != 0 || s.ReferencedPPSSpan == nil {
				t.Fatal("missing parameter provenance")
			}
			if f.poc == 1 && !f.deltaAlways && s.DeltaPicOrderCnt0.Value != 3 {
				t.Fatal("POC1")
			}
			if f.poc == 0 && s.PicOrderCntLSB.Value != 7 {
				t.Fatal("POC0")
			}
			if f.fields && (s.BottomFieldFlag == nil || !s.BottomFieldFlag.Value || s.DeltaPicOrderCnt1 != nil || s.DeltaPicOrderCntBottom != nil) {
				t.Fatal("field picture")
			}
			if f.redundant && s.RedundantPicCnt.Value != 2 {
				t.Fatal("redundant")
			}
			if f.colour && s.ColourPlaneID.Value != 2 {
				t.Fatal("colour plane")
			}
			if s.FrameNum.Span.BitLength != 4 || len(s.FrameNum.Span.SourceSpans) == 0 {
				t.Fatal("frame mapping")
			}
		})
	}
}

func TestSlicePrefixOrderingAndContextIsolation(t *testing.T) {
	f := sliceFixture{poc: 0, sliceType: 0}
	c := NewParameterSetContext()
	r := parsePrefixPacket(t, prefixPacket(prefixSlice(f), prefixSPS(f), prefixPPS(f), prefixSlice(f), prefixSlice(f)), c)
	if r.NALUnits[0].SliceHeader.PrefixComplete || r.NALUnits[0].SliceHeader.FrameNum != nil {
		t.Fatal("used future parameter sets")
	}
	if !r.NALUnits[3].SliceHeader.PrefixComplete || !r.NALUnits[4].SliceHeader.PrefixComplete {
		t.Fatal("ordered parameters were not applied")
	}
	if a, b := c.Counts(); a != 0 || b != 0 {
		t.Fatal("caller context mutated")
	}
	c = prefixContext(t, f)
	for _, barrier := range [][]byte{{0x0a}, {0x0b}, {}, {0x68}, {0x67}, {0xe7, 0}} {
		r := parsePrefixPacket(t, prefixPacket(barrier, prefixSlice(f)), c)
		if r.NALUnits[1].SliceHeader.PrefixComplete {
			t.Fatalf("stale context after %x", barrier)
		}
		if c.UsablePPSCount() != 1 {
			t.Fatal("barrier mutated caller")
		}
	}
	// A same-ID truncated update invalidates that slot while another PPS is
	// still usable, instead of silently falling back to the old definition.
	var bad avcBitBuilder
	bad.writeUE(0)
	bad.writeUE(0)
	bad.write(0, 2)
	r = parsePrefixPacket(t, prefixPacket(append([]byte{0x68}, bad.bytes()...), prefixSlice(f)), c)
	if r.NALUnits[1].SliceHeader.PrefixComplete {
		t.Fatal("stale PPS")
	}
}

func TestSlicePrefixReadsOnlyNeededBytesAndPreservesEPBMapping(t *testing.T) {
	f := sliceFixture{poc: 2, sliceType: 1, first: (1 << 24) - 1}
	c := prefixContext(t, f)
	prefix := prefixSlice(f)
	packet := prefixPacket(append(append([]byte{}, prefix...), bytes.Repeat([]byte{0}, 1<<20)...))
	reader := &prefixReadRecorder{data: packet}
	r, err := ParseLengthPrefixedNALUnits(reader, cursor.Span{BitLength: uint32(len(packet) * 8)}, 4, Options{ParsePPS: true, ParseSliceHeaders: true, ParameterSets: c})
	if err != nil || !r.Complete {
		t.Fatalf("%v %+v", err, r.Diagnostics)
	}
	s := r.NALUnits[0].SliceHeader
	if s.BytesRead != uint32(len(prefix)-1) || reader.end != uint64(4+len(prefix)) {
		t.Fatalf("body read: %+v end=%d prefix=%d", s, reader.end, len(prefix))
	}
	if len(s.RBSP.EmulationPreventionBytes) == 0 || len(s.FirstMBInSlice.Span.SourceSpans) < 2 {
		t.Fatal("expected field crossing EPB")
	}
	if s.FirstMBInSlice.Value != f.first {
		t.Fatal("decoded incorrect EPB-crossing value")
	}
	for cut := 1; cut < len(prefix); cut++ {
		partial := parsePrefixPacket(t, prefixPacket(prefix[:cut]), c)
		if partial.Complete || partial.NALUnits[0].SliceHeader.PrefixComplete {
			t.Fatalf("truncated prefix accepted at %d", cut)
		}
	}
}

type prefixReadRecorder struct {
	data []byte
	end  uint64
}

func (r *prefixReadRecorder) ReadAt(b []byte, off int64) (int, error) {
	if uint64(off)+uint64(len(b)) > r.end {
		r.end = uint64(off) + uint64(len(b))
	}
	return bytes.NewReader(r.data).ReadAt(b, off)
}

func TestSlicePrefixIOLimitsAndErrors(t *testing.T) {
	f := sliceFixture{poc: 0, sliceType: 0}
	c := prefixContext(t, f)
	packet := prefixPacket(prefixSlice(f))
	sentinel := errors.New("cancelled")
	reader := prefixFailureReader{data: packet, after: 5, err: sentinel}
	_, err := ParseLengthPrefixedNALUnits(reader, cursor.Span{BitLength: uint32(len(packet) * 8)}, 4, Options{ParseSliceHeaders: true, ParameterSets: c})
	if !errors.Is(err, sentinel) {
		t.Fatalf("lost source error %v", err)
	}
	options, _ := (Options{}).Normalized()
	for _, exhausted := range []bool{false, true} {
		budget := newParseBudget(options)
		if exhausted {
			budget.sliceBytesRead = MaxSliceHeaderTotalBytes
		}
		d := newDiagnosticCollector(options.MaxDiagnostics)
		nal := prefixSlice(f)
		header := NALHeader{Complete: true, NALUnitType: NALUnitTypeNonIDRSlice}
		if !exhausted {
			budget.mappingEntriesRemaining = 0
		}
		s, err := parseSliceHeader(bytes.NewReader(nal), cursor.Span{BitLength: uint32(len(nal) * 8)}, header, c, budget, d)
		if err != nil || s.PrefixComplete || len(d.items) == 0 {
			t.Fatalf("budget result %+v %v", s, err)
		}
		if exhausted && s.BytesRead != 0 {
			t.Fatal("read after cumulative exhaustion")
		}
	}
}

type prefixFailureReader struct {
	data  []byte
	after int64
	err   error
}

func (r prefixFailureReader) ReadAt(b []byte, off int64) (int, error) {
	n, e := bytes.NewReader(r.data).ReadAt(b, off)
	if off >= r.after {
		return n, r.err
	}
	return n, e
}

func TestPPSDeepKeepsOriginalContractAndRejectsUnsupportedSyntax(t *testing.T) {
	f := sliceFixture{}
	packet := prefixPacket(prefixPPS(f))
	legacy, err := ParseLengthPrefixedNALUnits(bytes.NewReader(packet), cursor.Span{BitLength: uint32(len(packet) * 8)}, 4, Options{})
	if err != nil {
		t.Fatal(err)
	}
	deep := parsePrefixPacket(t, packet, nil)
	a, b := *legacy.NALUnits[0].PictureParameterSet, *deep.NALUnits[0].PictureParameterSet
	if b.Deep == nil || !b.Deep.PrefixComplete {
		t.Fatal("PPS prefix absent")
	}
	b.Deep = nil
	if !reflect.DeepEqual(a, b) {
		t.Fatal("legacy PPS fields changed")
	}
	for _, groups := range []uint32{1, 7, 8} {
		var bits avcBitBuilder
		bits.writeUE(0)
		bits.writeUE(0)
		bits.write(0, 2)
		bits.writeUE(groups)
		r := parsePrefixPacket(t, prefixPacket(append([]byte{0x68}, bits.bytes()...)), nil)
		p := r.NALUnits[0].PictureParameterSet
		if !p.Complete || p.Deep.PrefixComplete || r.Complete {
			t.Fatal("slice groups silently accepted")
		}
	}
}

func TestSlicePrefixInvalidValues(t *testing.T) {
	for _, f := range []sliceFixture{{poc: 0, sliceType: 10}, {poc: 0, ppsID: 256}, {poc: 0, sliceType: 2, idr: true, idrID: 65536}, {poc: 0, sliceType: 0, idr: true}, {poc: 0, sliceType: 2, idr: true, frame: 1}} {
		c := prefixContext(t, sliceFixture{poc: 0})
		r := parsePrefixPacket(t, prefixPacket(prefixSlice(f)), c)
		if r.Complete || r.NALUnits[0].SliceHeader.PrefixComplete {
			t.Fatalf("invalid accepted %+v", f)
		}
	}
}

func TestSlicePrefixSharedContextRace(t *testing.T) {
	f := sliceFixture{poc: 0, sliceType: 0}
	c := prefixContext(t, f)
	packet := prefixPacket(prefixSlice(f), prefixSPS(f), prefixPPS(f), prefixSlice(f))
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := parsePrefixPacket(t, packet, c)
			if !r.Complete {
				t.Errorf("%+v", r.Diagnostics)
			}
		}()
	}
	wg.Wait()
}

func FuzzSlicePrefixes(f *testing.F) {
	for _, fixture := range []sliceFixture{{poc: 0, sliceType: 2, idr: true}, {poc: 1, sliceType: 1, redundant: true}, {poc: 2, sliceType: 0}} {
		f.Add(prefixPacket(prefixSPS(fixture), prefixPPS(fixture), prefixSlice(fixture)))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 64<<10 {
			t.Skip()
		}
		options := Options{ParsePPS: true, ParseSliceHeaders: true, MaxNALUnits: 64, MaxParameterSetBytes: 1024, MaxRBSPMappingEntries: 128, MaxDiagnostics: 8}
		span := cursor.Span{BitLength: uint32(len(data) * 8)}
		r, err := ParseLengthPrefixedNALUnits(bytes.NewReader(data), span, 4, options)
		if err != nil && !errors.Is(err, io.EOF) {
			t.Fatal(err)
		}
		again, err2 := ParseLengthPrefixedNALUnits(bytes.NewReader(data), span, 4, options)
		if (err == nil) != (err2 == nil) || !reflect.DeepEqual(r, again) {
			t.Fatal("non-deterministic")
		}
		var total uint32
		for _, u := range r.NALUnits {
			if u.SliceHeader != nil {
				s := u.SliceHeader
				total += s.BytesRead
				if s.BytesRead > MaxSliceHeaderBytes || s.SyntaxBits > s.RBSP.ByteLength*8 {
					t.Fatal("prefix budget")
				}
			}
		}
		if total > MaxSliceHeaderTotalBytes {
			t.Fatal("cumulative prefix budget")
		}
	})
}

func TestSlicePrefixCumulativeBudget(t *testing.T) {
	f := sliceFixture{poc: 1, sliceType: 1, bottom: true, redundant: true}
	context := prefixContext(t, f)
	var bits avcBitBuilder
	bits.writeUE(^uint32(0))
	bits.writeUE(1)
	bits.writeUE(0)
	bits.write(0, 4)
	bits.writeSE(2147483647)
	bits.writeSE(-2147483647)
	bits.writeUE(127)
	nal := append([]byte{0x41}, escapeRBSP(bits.bytes())...)
	packet := bytes.Repeat(prefixPacket(nal), int(DefaultMaxNALUnits))
	r, err := ParseLengthPrefixedNALUnits(bytes.NewReader(packet), cursor.Span{BitLength: uint32(len(packet) * 8)}, 4, Options{ParsePPS: true, ParseSliceHeaders: true, ParameterSets: context, MaxRBSPMappingEntries: HardMaxRBSPMappingEntries})
	if err != nil {
		t.Fatal(err)
	}
	if r.Complete || !r.FramingComplete || len(r.NALUnits) != int(DefaultMaxNALUnits) {
		t.Fatalf("budget framing: %d %+v", len(r.NALUnits), r.Diagnostics)
	}
	var total uint32
	var complete int
	for _, unit := range r.NALUnits {
		s := unit.SliceHeader
		total += s.BytesRead
		if s.PrefixComplete {
			complete++
		}
	}
	if total != MaxSliceHeaderTotalBytes || complete == 0 || complete == len(r.NALUnits) {
		t.Fatalf("budget total %d complete %d", total, complete)
	}
}

func TestSlicePrefixPerNALBudget(t *testing.T) {
	options, _ := (Options{}).Normalized()
	data := bytes.Repeat([]byte{0xff}, int(MaxSliceHeaderBytes)+16)
	diagnostics := newDiagnosticCollector(options.MaxDiagnostics)
	p := newRBSPParser(rbspData{valid: true, end: uint64(len(data))}, diagnostics)
	loader := slicePrefixLoader{source: bytes.NewReader(data), end: uint64(len(data)), budget: newParseBudget(options), p: p}
	if loader.ensure(MaxSliceHeaderBytes*8+1) || loader.position != uint64(MaxSliceHeaderBytes) || len(diagnostics.items) != 1 || diagnostics.items[0].Code != CodeSliceHeaderBudgetReached {
		t.Fatalf("per-NAL bound: %d %+v", loader.position, diagnostics.items)
	}
}

func TestSliceFullReadDoesNotSwallowSourceError(t *testing.T) {
	f := sliceFixture{poc: 0, sliceType: 0}
	c := prefixContext(t, f)
	packet := prefixPacket(prefixSlice(f))
	sentinel := errors.New("cancelled")
	for _, offset := range []int64{0, 4, 5} {
		_, err := ParseLengthPrefixedNALUnits(prefixFailureReader{data: packet, after: offset, err: sentinel}, cursor.Span{BitLength: uint32(len(packet) * 8)}, 4, Options{ParseSliceHeaders: true, ParameterSets: c})
		if !errors.Is(err, sentinel) {
			t.Fatalf("error at %d was swallowed: %v", offset, err)
		}
	}
}

func TestSliceOptionEnablesOrderedInBandPPS(t *testing.T) {
	f := sliceFixture{poc: 0, sliceType: 0}
	packet := prefixPacket(prefixSPS(f), prefixPPS(f), prefixSlice(f))
	r, err := ParseLengthPrefixedNALUnits(bytes.NewReader(packet), cursor.Span{BitLength: uint32(len(packet) * 8)}, 4, Options{ParseSliceHeaders: true})
	if err != nil || !r.Complete || r.NALUnits[1].PictureParameterSet.Deep == nil || !r.NALUnits[2].SliceHeader.PrefixComplete {
		t.Fatalf("in-band activation %v %+v", err, r.Diagnostics)
	}
}

func TestPPSPrefixInvalidRanges(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		refs0, refs1, weighted uint32
		qp, qs, chroma         int32
	}{
		{name: "ref0", refs0: 32}, {name: "ref1", refs1: 32}, {name: "weighted", weighted: 3},
		{name: "qp", qp: -63}, {name: "qs", qs: 26}, {name: "chroma", chroma: 13},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var b avcBitBuilder
			b.writeUE(0)
			b.writeUE(0)
			b.write(0, 2)
			b.writeUE(0)
			b.writeUE(tc.refs0)
			b.writeUE(tc.refs1)
			b.write(0, 1)
			b.write(uint64(tc.weighted), 2)
			b.writeSE(tc.qp)
			b.writeSE(tc.qs)
			b.writeSE(tc.chroma)
			b.write(0, 3)
			b.write(1, 1)
			r := parsePrefixPacket(t, prefixPacket(append([]byte{0x68}, escapeRBSP(b.bytes())...)), nil)
			p := r.NALUnits[0].PictureParameterSet
			if !p.Complete || p.Deep == nil || p.Deep.PrefixComplete || r.Complete {
				t.Fatalf("invalid PPS accepted %+v", p)
			}
			if len(r.Diagnostics) == 0 || r.Diagnostics[0].Code != CodePPSValueInvalid {
				t.Fatalf("diagnostic %+v", r.Diagnostics)
			}
		})
	}
}
