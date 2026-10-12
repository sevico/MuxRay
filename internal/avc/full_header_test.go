package avc

import (
	"bytes"
	"github.com/sevico/muxray/internal/cursor"
	"testing"
)

func fullSPS() []byte {
	var b avcBitBuilder
	b.write(100, 8)
	b.write(0, 8)
	b.write(30, 8)
	b.writeUE(0)
	b.writeUE(1)
	b.writeUE(0)
	b.writeUE(0)
	b.write(0, 2)
	b.writeUE(0)
	b.writeUE(0)
	b.writeUE(0)
	b.writeUE(2)
	b.write(0, 1)
	b.writeUE(1)
	b.writeUE(1)
	b.write(1, 1)
	b.write(1, 1)
	b.write(0, 2)
	b.write(1, 1)
	return append([]byte{0x67}, escapeRBSP(b.bytes())...)
}
func fullPPS(group int, extension bool) []byte {
	var b avcBitBuilder
	b.writeUE(0)
	b.writeUE(0)
	b.write(1, 1)
	b.write(0, 1)
	if group < 0 {
		b.writeUE(0)
	} else {
		b.writeUE(1)
		b.writeUE(uint32(group))
		switch group {
		case 0:
			b.writeUE(0)
			b.writeUE(1)
		case 2:
			b.writeUE(0)
			b.writeUE(3)
		case 3, 4, 5:
			b.write(1, 1)
			b.writeUE(1)
		case 6:
			b.writeUE(3)
			b.write(5, 4)
		}
	}
	b.writeUE(0)
	b.writeUE(0)
	b.write(1, 1)
	b.write(1, 2)
	b.writeSE(0)
	b.writeSE(0)
	b.writeSE(0)
	b.write(1, 1)
	b.write(0, 2)
	if extension {
		b.write(1, 1)
		b.write(1, 1)
		for i := 0; i < 8; i++ {
			if i == 0 {
				b.write(1, 1)
				b.writeSE(-8)
			} else {
				b.write(0, 1)
			}
		}
		b.writeSE(1)
	}
	b.write(1, 1)
	return append([]byte{0x68}, escapeRBSP(b.bytes())...)
}
func fullSlice(kind uint32, idr bool, group int) []byte {
	var b avcBitBuilder
	b.writeUE(0)
	b.writeUE(kind)
	b.writeUE(0)
	b.write(0, 4)
	if idr {
		b.writeUE(0)
	}
	b.write(0, 4)
	if kind == 1 {
		b.write(1, 1)
	}
	if kind != 2 && kind != 4 {
		b.write(1, 1)
		b.writeUE(0)
		if kind == 1 {
			b.writeUE(0)
		}
		b.write(1, 1)
		b.writeUE(0)
		b.writeUE(1)
		b.writeUE(2)
		b.writeUE(0)
		b.writeUE(3)
		if kind == 1 {
			b.write(1, 1)
			b.writeUE(1)
			b.writeUE(0)
			b.writeUE(3)
		}
		b.writeUE(0)
		b.writeUE(0)
		for i := 0; i < 1+int(boolUint(kind == 1)); i++ {
			b.write(1, 1)
			b.writeSE(1)
			b.writeSE(-1)
			b.write(1, 1)
			b.writeSE(2)
			b.writeSE(0)
			b.writeSE(3)
			b.writeSE(1)
		}
	}
	if idr {
		b.write(1, 1)
		b.write(0, 1)
	} else {
		b.write(1, 1)
		for _, op := range []uint32{1, 2, 3, 4, 5, 6, 0} {
			b.writeUE(op)
			if op == 1 || op == 3 {
				b.writeUE(0)
			}
			if op == 2 {
				b.writeUE(0)
			}
			if op == 3 || op == 6 {
				b.writeUE(0)
			}
			if op == 4 {
				b.writeUE(1)
			}
		}
	}
	if kind != 2 && kind != 4 {
		b.writeUE(2)
	}
	b.writeSE(-1)
	if kind == 3 || kind == 4 {
		if kind == 3 {
			b.write(1, 1)
		}
		b.writeSE(1)
	}
	b.writeUE(0)
	b.writeSE(-2)
	b.writeSE(3)
	if group >= 3 && group <= 5 {
		b.write(2, 2)
	}
	// Deliberately opaque invalid byte sequence after a byte boundary must never
	// be inspected once the header is complete.
	data := append(escapeRBSP(b.bytes()), 0, 0, 1, 0xff)
	h := byte(0x41)
	if idr {
		h = 0x65
	}
	return append([]byte{h}, data...)
}
func parseFullPacket(t testing.TB, data []byte, sets *ParameterSetContext) NALUnitSequence {
	t.Helper()
	r, e := ParseLengthPrefixedNALUnits(bytes.NewReader(data), cursor.Span{BitLength: uint32(len(data) * 8)}, 4, Options{ParseFullHeaders: true, ParseSliceHeaders: true, ParameterSets: sets})
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func fullContext(t testing.TB, group int, extension bool) *ParameterSetContext {
	t.Helper()
	r := parseFullPacket(t, prefixPacket(fullSPS(), fullPPS(group, extension)), nil)
	if !r.Complete {
		t.Fatalf("sets: %+v", r.Diagnostics)
	}
	c := NewParameterSetContext()
	for _, n := range r.NALUnits {
		c.AddNALUnit(n)
	}
	return c
}
func TestFullHeadersConditionalSyntax(t *testing.T) {
	for _, kind := range []uint32{0, 1, 2, 3, 4} {
		for _, idr := range []bool{false, true} {
			if idr && kind != 2 && kind != 4 {
				continue
			}
			r := parseFullPacket(t, prefixPacket(fullSlice(kind, idr, -1)), fullContext(t, -1, true))
			if !r.Complete {
				t.Fatalf("kind %d idr %v: %+v", kind, idr, r.Diagnostics)
			}
			s := r.NALUnits[0].SliceHeader
			f := s.Full
			if !s.PrefixComplete || f == nil || !f.Complete || f.SliceQPDelta.Value != -1 || f.SliceBetaOffsetDiv2.Value != 3 || f.HeaderBits != s.SyntaxBits+f.SyntaxBits || f.BytesRead < s.BytesRead {
				t.Fatalf("full %+v", f)
			}
			if kind == 0 || kind == 1 || kind == 3 {
				if f.CABACInitIDC.Value != 2 || len(f.RefPicListModificationsL0) != 3 || len(f.PredictionWeightsL0) != 1 || f.PredictionWeightsL0[0].ChromaWeight[1].Value != 3 {
					t.Fatalf("prediction %+v", f)
				}
			}
			if kind == 1 && len(f.PredictionWeightsL1) != 1 {
				t.Fatal("B list1 omitted")
			}
			if idr {
				if !f.NoOutputOfPriorPicsFlag.Value || f.LongTermReferenceFlag.Value {
					t.Fatal("IDR marking")
				}
			} else if len(f.MemoryManagementOperations) != 7 {
				t.Fatal("MMCO operations")
			}
			if kind == 3 || kind == 4 {
				if f.SliceQSDelta.Value != 1 {
					t.Fatal("QS")
				}
			}
		}
	}
}
func TestFullPPSFMOAndScaling(t *testing.T) {
	for group := 0; group <= 6; group++ {
		r := parseFullPacket(t, prefixPacket(fullSPS(), fullPPS(group, true), fullSlice(2, true, group)), nil)
		if !r.Complete {
			t.Fatalf("group %d: %+v", group, r.Diagnostics)
		}
		f := r.NALUnits[1].PictureParameterSet.Deep.Full
		if !f.Complete || f.SliceGroupMapType.Value != uint32(group) || !f.ExtensionPresent || len(f.ScalingLists) != 8 || !f.ScalingLists[0].UseDefault || len(f.ScalingLists[0].DeltaScales) != 1 || f.SecondChromaQPIndexOffset.Value != 1 || f.RBSPTrailingBits == nil {
			t.Fatalf("group %d: %+v", group, f)
		}
		if group >= 3 && group <= 5 && r.NALUnits[2].SliceHeader.Full.SliceGroupChangeCycle.Value != 2 {
			t.Fatal("cycle")
		}
		if group == 6 && len(f.SliceGroupID) != 4 {
			t.Fatal("explicit map")
		}
		compact := NewParameterSetContext()
		for _, n := range r.NALUnits[:2] {
			compact.AddNALUnit(n)
		}
		replay := parseFullPacket(t, prefixPacket(fullSlice(2, true, group)), compact.CompactClone())
		if !replay.Complete {
			t.Fatalf("compact %d: %+v", group, replay.Diagnostics)
		}
	}
}
func TestFullHeadersTruncationAndLegacyIsolation(t *testing.T) {
	sets := fullContext(t, -1, true)
	nal := fullSlice(1, false, -1)
	whole := parseFullPacket(t, prefixPacket(nal), sets).NALUnits[0].SliceHeader.Full.BytesRead
	for n := 1; n < int(whole)+1; n++ {
		r := parseFullPacket(t, prefixPacket(nal[:n]), sets)
		if r.Complete || r.NALUnits[0].SliceHeader.Full.Complete {
			t.Fatalf("truncation %d accepted", n)
		}
	}
	old := parsePrefixPacket(t, prefixPacket(nal), sets)
	if !old.Complete || old.NALUnits[0].SliceHeader.Full != nil {
		t.Fatal("legacy expanded")
	}
	missing := parseFullPacket(t, prefixPacket(nal), prefixContext(t, sliceFixture{}))
	if missing.Complete || missing.NALUnits[0].SliceHeader.Full.Complete {
		t.Fatal("prefix-only context accepted full header")
	}
	pps := fullPPS(-1, false)
	pps[len(pps)-1] = 0
	bad := parseFullPacket(t, prefixPacket(fullSPS(), pps), nil)
	if bad.Complete || bad.NALUnits[1].PictureParameterSet.Deep.Full.Complete || !bad.NALUnits[1].PictureParameterSet.Deep.PrefixComplete {
		t.Fatal("invalid trailing bits not separated from prefix")
	}
}
func TestFullHeaderOperationBudget(t *testing.T) {
	var b avcBitBuilder
	b.writeUE(0)
	b.writeUE(0)
	b.writeUE(0)
	b.write(0, 8)
	b.write(0, 1)
	b.write(1, 1)
	for i := 0; i < MaxHeaderOperations+1; i++ {
		b.writeUE(0)
		b.writeUE(0)
	}
	nal := append([]byte{0x41}, escapeRBSP(b.bytes())...)
	r := parseFullPacket(t, prefixPacket(nal), fullContext(t, -1, false))
	if r.Complete {
		t.Fatal("unbounded list accepted")
	}
	assertDiagnostic(t, r.Diagnostics, CodeHeaderSyntaxBudgetReached)
	if len(r.NALUnits[0].SliceHeader.Full.RefPicListModificationsL0) != MaxHeaderOperations {
		t.Fatal("operation bound")
	}
}
func TestFullFMOMapBudget(t *testing.T) {
	var b avcBitBuilder
	b.writeUE(0)
	b.writeUE(0)
	b.write(0, 2)
	b.writeUE(1)
	b.writeUE(6)
	b.writeUE(^uint32(0))
	r := parseFullPacket(t, prefixPacket(prefixSPS(sliceFixture{}), append([]byte{0x68}, escapeRBSP(b.bytes())...)), nil)
	if r.Complete {
		t.Fatal("huge map accepted")
	}
	assertDiagnostic(t, r.Diagnostics, CodeHeaderSyntaxBudgetReached)
	if r.NALUnits[1].PictureParameterSet.Deep.SyntaxBits == 0 {
		t.Fatal("partial FMO prefix lost decoded bit accounting")
	}
}

func TestFullHeaderEPBMappingAndReadBudgets(t *testing.T) {
	var b avcBitBuilder
	b.writeUE(0)
	b.writeUE(2)
	b.writeUE(0)
	b.write(0, 8)
	b.write(1, 1)
	b.writeUE(1)
	b.writeUE((1 << 24) - 1)
	b.writeUE(0)
	b.writeSE(0)
	b.writeUE(1)
	nal := append([]byte{0x41}, escapeRBSP(b.bytes())...)
	r := parseFullPacket(t, prefixPacket(nal), fullContext(t, -1, false))
	if !r.Complete {
		t.Fatal(r.Diagnostics)
	}
	f := r.NALUnits[0].SliceHeader.Full.MemoryManagementOperations[0].DifferenceOfPicNumsMinus1
	if f.Value != (1<<24)-1 || len(f.Span.SourceSpans) < 2 || len(r.NALUnits[0].SliceHeader.Full.RBSP.EmulationPreventionBytes) == 0 {
		t.Fatalf("mapping %+v", f)
	}
	for _, epb := range r.NALUnits[0].SliceHeader.Full.RBSP.EmulationPreventionBytes {
		for _, span := range f.Span.SourceSpans {
			start, end, _ := span.ByteRange()
			if epb.ByteOffset >= start && epb.ByteOffset < end {
				t.Fatal("mapped EPB as field data")
			}
		}
	}
	for _, total := range []bool{false, true} {
		data := bytes.Repeat([]byte{0xff}, int(MaxFullSliceHeaderBytes+1))
		o, _ := Options{}.Normalized()
		budget := newParseBudget(o)
		if total {
			budget.sliceBytesRead = MaxFullSliceHeaderTotalBytes - 2
		}
		p := newRBSPParser(rbspData{valid: true, end: uint64(len(data))}, newDiagnosticCollector(8, uint64(len(data))))
		l := slicePrefixLoader{source: bytes.NewReader(data), end: uint64(len(data)), budget: budget, p: p, maxBytes: MaxFullSliceHeaderBytes, maxTotalBytes: MaxFullSliceHeaderTotalBytes}
		p.ensure = l.ensure
		for !p.fatal {
			p.readBits(8)
		}
		want := uint64(MaxFullSliceHeaderBytes)
		if total {
			want = 2
		}
		if l.position != want {
			t.Fatalf("total %v read %d want %d", total, l.position, want)
		}
		assertDiagnostic(t, p.diagnostics.items, CodeSliceHeaderBudgetReached)
	}
}
func TestFullHeaderUnsupportedNALAndMappingBudget(t *testing.T) {
	for _, kind := range []byte{2, 3, 4, 19, 20, 21} {
		r := parseFullPacket(t, prefixPacket([]byte{kind, 0xff}), nil)
		if r.Complete || r.NALUnits[0].SummaryComplete {
			t.Fatal("unsupported full header complete")
		}
		assertDiagnostic(t, r.Diagnostics, CodeSliceHeaderUnsupported)
	}
	data := prefixPacket(fullSPS(), fullPPS(-1, true), fullSlice(0, false, -1))
	r, e := ParseLengthPrefixedNALUnits(bytes.NewReader(data), cursor.Span{BitLength: uint32(len(data) * 8)}, 4, Options{ParseFullHeaders: true, ParseSliceHeaders: true, MaxRBSPMappingEntries: 1})
	if e != nil || r.Complete {
		t.Fatal("mapping budget", e)
	}
	assertDiagnostic(t, r.Diagnostics, CodeRBSPMappingLimitReached)
}
func FuzzFullHeaders(f *testing.F) {
	for _, kind := range []uint32{0, 1, 2, 3, 4} {
		f.Add(prefixPacket(fullSPS(), fullPPS(-1, true), fullSlice(kind, kind == 2, -1)))
	}
	for g := 0; g < 7; g++ {
		f.Add(prefixPacket(fullSPS(), fullPPS(g, true), fullSlice(2, true, g)))
	}
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<16 {
			t.Skip()
		}
		options, _ := Options{ParseFullHeaders: true, ParseSliceHeaders: true, ParseVUI: true, MaxNALUnits: 64, MaxParameterSetBytes: 4096, MaxRBSPMappingEntries: 1024}.Normalized()
		r, e := ParseLengthPrefixedNALUnits(bytes.NewReader(data), cursor.Span{BitLength: uint32(len(data) * 8)}, 4, options)
		if e != nil {
			t.Fatal(e)
		}
		assertNALSequenceInvariants(t, r, options, 0, len(data))
	})
}

func TestFullPPSInvalidatedByChangedSPSDependency(t *testing.T) {
	for _, kind := range []string{"dimensions", "chroma", "bit-depth", "invalid", "unknown-id", "identical"} {
		t.Run(kind, func(t *testing.T) {
			c := fullContext(t, 6, true)
			old := c.sps[0]
			s := *old
			s.SequenceParameterSetIDSpan = MappedSpan{BitLength: 1}
			u := NALUnit{HeaderPresent: true, SummaryComplete: true, Header: NALHeader{NALUnitType: 7}, SequenceParameterSet: &s}
			switch kind {
			case "dimensions":
				s.PicWidthInMbsMinus1++
			case "chroma":
				s.ChromaFormatIDC = 3
			case "bit-depth":
				s.BitDepthLumaMinus8++
			case "invalid":
				u.SummaryComplete = false
			case "unknown-id":
				s.SequenceParameterSetIDSpan = MappedSpan{}
			}
			c.AddNALUnit(u)
			if kind == "identical" {
				if c.pps[0] == nil {
					t.Fatal("identical SPS invalidated PPS")
				}
				return
			}
			if c.pps[0] != nil {
				t.Fatal("changed dependency retained full PPS")
			}
			// A later valid SPS alone cannot revive a previously invalid PPS.
			s = *old
			s.SequenceParameterSetIDSpan = MappedSpan{BitLength: 1}
			u.SequenceParameterSet = &s
			u.SummaryComplete = true
			c.AddNALUnit(u)
			r := parseFullPacket(t, prefixPacket(fullSlice(2, true, 6)), c.CompactClone())
			if r.Complete {
				t.Fatal("stale PPS revived")
			}
			assertDiagnostic(t, r.Diagnostics, CodeSliceParameterSetMissing)
		})
	}
}

func TestFullPPSBitDepthQPAndMissingSPS(t *testing.T) {
	var b avcBitBuilder
	b.writeUE(0)
	b.writeUE(0)
	b.write(0, 2)
	b.writeUE(0)
	b.writeUE(0)
	b.writeUE(0)
	b.write(0, 3)
	b.writeSE(-40)
	b.writeSE(0)
	b.writeSE(0)
	b.write(0, 3)
	b.write(1, 1)
	pps := append([]byte{0x68}, escapeRBSP(b.bytes())...)
	old := parsePrefixPacket(t, prefixPacket(fullSPS(), pps), nil)
	if !old.Complete {
		t.Fatal("legacy wide bitdepth range changed")
	}
	r := parseFullPacket(t, prefixPacket(fullSPS(), pps), nil)
	if r.Complete {
		t.Fatal("8-bit PPS QP invalid accepted")
	}
	assertDiagnostic(t, r.Diagnostics, CodePPSValueInvalid)
	missing := parseFullPacket(t, prefixPacket(fullPPS(-1, false)), nil)
	if missing.Complete || !missing.NALUnits[0].PictureParameterSet.Deep.PrefixComplete || missing.NALUnits[0].PictureParameterSet.Deep.Full.Complete {
		t.Fatal("missing SPS invented full validation")
	}
	assertDiagnostic(t, missing.Diagnostics, CodePPSParameterSetMissing)
}
