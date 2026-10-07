package avc

import (
	"bytes"
	"encoding/binary"
	"reflect"
	"testing"

	"github.com/sevico/muxray/internal/cursor"
)

func vuiSPSPrefix(present bool) avcBitBuilder {
	var b avcBitBuilder
	b.write(66, 8)
	b.write(0, 8)
	b.write(30, 8)
	b.writeUE(0)
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
	if present {
		b.write(1, 1)
	} else {
		b.write(0, 1)
	}
	return b
}

func vuiSPSNAL(b avcBitBuilder) []byte {
	b.write(1, 1)
	return append([]byte{0x67}, escapeRBSP(b.bytes())...)
}

func vuiSequence(nal []byte) []byte {
	data := make([]byte, 4, len(nal)+4)
	binary.BigEndian.PutUint32(data, uint32(len(nal)))
	return append(data, nal...)
}

func vuiFullSyntax(b *avcBitBuilder, cpbMinus1 uint32) {
	b.write(1, 1)
	b.write(255, 8)
	b.write(4, 16)
	b.write(3, 16)
	b.write(1, 1)
	b.write(1, 1)
	b.write(1, 1)
	b.write(5, 3)
	b.write(1, 1)
	b.write(1, 1)
	b.write(1, 8)
	b.write(1, 8)
	b.write(1, 8)
	b.write(1, 1)
	b.writeUE(5)
	b.writeUE(0)
	b.write(1, 1)
	b.write(1001, 32)
	b.write(60000, 32)
	b.write(1, 1)
	b.write(1, 1)
	vuiHRDBits(b, cpbMinus1)
	b.write(1, 1)
	vuiHRDBits(b, cpbMinus1)
	b.write(0, 1)
	b.write(1, 1)
	b.write(1, 1)
	b.write(1, 1)
	b.writeUE(16)
	b.writeUE(16)
	b.writeUE(15)
	b.writeUE(15)
	b.writeUE(2)
	b.writeUE(4)
}

func vuiHRDBits(b *avcBitBuilder, count uint32) {
	b.writeUE(count)
	b.write(4, 4)
	b.write(7, 4)
	for i := uint32(0); i <= count; i++ {
		b.writeUE(i)
		b.writeUE(count - i)
		b.write(uint64(i&1), 1)
	}
	b.write(19, 5)
	b.write(9, 5)
	b.write(6, 5)
	b.write(0, 5)
}

type vuiRead struct {
	Offset int64
	Bytes  int
}
type vuiReader struct {
	*bytes.Reader
	reads []vuiRead
}

func (r *vuiReader) ReadAt(p []byte, off int64) (int, error) {
	r.reads = append(r.reads, vuiRead{off, len(p)})
	return r.Reader.ReadAt(p, off)
}

func parseVUINAL(t *testing.T, nal []byte) (NALUnitSequence, *VUIParameters) {
	t.Helper()
	r := parseNALsAt(t, 19, vuiSequence(nal), 4, Options{ParseVUI: true})
	if len(r.NALUnits) != 1 || r.NALUnits[0].SequenceParameterSet == nil {
		t.Fatalf("missing SPS: %+v", r)
	}
	return r, r.NALUnits[0].SequenceParameterSet.VUI
}

func TestVUIAllBranchesAndHardHRDBound(t *testing.T) {
	for _, count := range []uint32{0, 31} {
		b := vuiSPSPrefix(true)
		vuiFullSyntax(&b, count)
		nal := vuiSPSNAL(b)
		data := vuiSequence(nal)
		r, v := parseVUINAL(t, nal)
		if !r.Complete || v == nil || !v.Complete || !v.Present || !v.TrailingBitsValid || v.RemainingRBSPBits != 0 ||
			v.SARWidth.Value != 4 || v.SARHeight.Value != 3 || v.ChromaSampleLocTypeTopField.Value != 5 ||
			v.NumUnitsInTick.Value != 1001 || v.TimeScale.Value != 60000 || !v.FixedFrameRateFlag.Value ||
			v.MaxBytesPerPicDenom.Value != 16 || v.Log2MaxMVLengthHorizontal.Value != 15 || v.MaxNumReorderFrames.Value != 2 ||
			len(v.NALHRD.CPB) != int(count+1) || len(v.VCLHRD.CPB) != int(count+1) || !v.NALHRD.Complete || !v.VCLHRD.Complete {
			t.Fatalf("VUI count %d: %+v diagnostics=%+v", count, v, r.Diagnostics)
		}
		options, _ := (Options{ParseVUI: true}).Normalized()
		assertNALSequenceInvariants(t, r, options, 19, len(data))
		oldReader := &vuiReader{Reader: bytes.NewReader(data)}
		newReader := &vuiReader{Reader: bytes.NewReader(data)}
		span := cursor.Span{BitLength: uint32(len(data) * 8)}
		old, _ := ParseLengthPrefixedNALUnits(oldReader, span, 4, Options{})
		expanded, _ := ParseLengthPrefixedNALUnits(newReader, span, 4, Options{ParseVUI: true})
		expanded.NALUnits[0].SequenceParameterSet.VUI = nil
		if !reflect.DeepEqual(old, expanded) || !reflect.DeepEqual(oldReader.reads, newReader.reads) {
			t.Fatal("VUI changed base result or source reads")
		}
	}
}

func TestVUIAbsentUnspecifiedSARAndReservedValues(t *testing.T) {
	absent := vuiSPSPrefix(false)
	r, v := parseVUINAL(t, vuiSPSNAL(absent))
	if !r.Complete || v == nil || v.Present || !v.Complete || v.SyntaxBits != 0 || v.AspectRatioInfoPresentFlag != nil {
		t.Fatalf("absent: %+v %+v", v, r.Diagnostics)
	}
	for _, id := range []uint64{0, 1, 14, 17, 254, 255} {
		b := vuiSPSPrefix(true)
		b.write(1, 1)
		b.write(id, 8)
		if id == 255 {
			b.write(0, 16)
			b.write(3, 16)
		}
		b.write(0, 8) // remaining presence flags, including pic_struct_present_flag
		r, v := parseVUINAL(t, vuiSPSNAL(b))
		if !r.Complete || !v.Complete || v.AspectRatioIDC.Value != uint32(id) {
			t.Fatalf("id %d: %+v %+v", id, v, r.Diagnostics)
		}
		if id == 255 && (v.SARWidth.Value != 0 || v.SARHeight.Value != 3) {
			t.Fatal("zero SAR was normalized or rejected")
		}
		if v.NumUnitsInTick != nil || v.NALHRD != nil || v.LowDelayHRDFlag != nil || v.VideoFormat != nil {
			t.Fatal("absent syntax was inferred")
		}
	}
}

func TestVUIInvalidSyntaxKeepsBaseSPSAndPropagatesFailure(t *testing.T) {
	for _, tc := range []struct {
		name, code string
		write      func(*avcBitBuilder)
	}{
		{"SAR not coprime", CodeVUISARInvalid, func(b *avcBitBuilder) { b.write(1, 1); b.write(255, 8); b.write(4, 16); b.write(2, 16); b.write(0, 8) }},
		{"chroma location", CodeVUIChromaLocationInvalid, func(b *avcBitBuilder) { b.write(0, 3); b.write(1, 1); b.writeUE(6); b.writeUE(0); b.write(0, 5) }},
		{"timing zero", CodeVUITimingInvalid, func(b *avcBitBuilder) {
			b.write(0, 4)
			b.write(1, 1)
			b.write(0, 32)
			b.write(50, 32)
			b.write(1, 1)
			b.write(0, 4)
		}},
		{"HRD count", CodeVUIHRDCountInvalid, func(b *avcBitBuilder) { b.write(0, 5); b.write(1, 1); b.writeUE(32) }},
		{"HRD overflow", CodeVUIHRDValueInvalid, func(b *avcBitBuilder) {
			b.write(0, 5)
			b.write(1, 1)
			b.writeUE(0)
			b.write(0, 8)
			b.writeUE(^uint32(0))
			b.writeUE(0)
			b.write(0, 1)
			b.write(0, 20)
			b.write(0, 4)
		}},
		{"restriction denom", CodeVUIRestrictionInvalid, func(b *avcBitBuilder) {
			b.write(0, 8)
			b.write(1, 1)
			b.write(1, 1)
			for _, u := range []uint32{17, 0, 15, 15, 0, 1} {
				b.writeUE(u)
			}
		}},
		{"restriction mv", CodeVUIRestrictionInvalid, func(b *avcBitBuilder) {
			b.write(0, 8)
			b.write(1, 1)
			b.write(1, 1)
			for _, u := range []uint32{0, 0, 16, 15, 0, 1} {
				b.writeUE(u)
			}
		}},
		{"restriction reorder", CodeVUIRestrictionInvalid, func(b *avcBitBuilder) {
			b.write(0, 8)
			b.write(1, 1)
			b.write(1, 1)
			for _, u := range []uint32{0, 0, 15, 15, 2, 1} {
				b.writeUE(u)
			}
		}},
		{"restriction refs", CodeVUIRestrictionInvalid, func(b *avcBitBuilder) {
			b.write(0, 8)
			b.write(1, 1)
			b.write(1, 1)
			for _, u := range []uint32{0, 0, 15, 15, 0, 0} {
				b.writeUE(u)
			}
		}},
		{"ExpGolomb", CodeExpGolombOverflow, func(b *avcBitBuilder) { b.write(0, 5); b.write(1, 1); b.write(0, 33) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := vuiSPSPrefix(true)
			tc.write(&b)
			r, v := parseVUINAL(t, vuiSPSNAL(b))
			if r.Complete || !r.FramingComplete || r.NALUnits[0].SummaryComplete || !r.NALUnits[0].SequenceParameterSet.Complete || v == nil || v.Complete {
				t.Fatalf("deep failure changed base/framing: %+v %+v", r, v)
			}
			assertDiagnostic(t, r.Diagnostics, tc.code)
		})
	}
}

func TestVUITrailingBitsAndTruncation(t *testing.T) {
	full := vuiSPSPrefix(true)
	vuiFullSyntax(&full, 0)
	raw := append([]byte{}, full.bytes()...)
	for size := 1; size < len(raw); size++ {
		r, _ := parseVUINAL(t, append([]byte{0x67}, escapeRBSP(raw[:size])...))
		if r.Complete {
			t.Fatalf("truncated VUI complete at %d/%d", size, len(raw))
		}
	}
	absent := vuiSPSPrefix(false)
	for _, tc := range []struct {
		name   string
		mutate func(*avcBitBuilder)
	}{
		{"missing stop", func(b *avcBitBuilder) { b.write(0, 1) }},
		{"alignment one", func(b *avcBitBuilder) { b.write(1, 1); b.write(1, 1) }},
		{"extra byte", func(b *avcBitBuilder) {
			b.write(1, 1)
			for b.length%8 != 0 {
				b.write(0, 1)
			}
			b.write(0, 8)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := avcBitBuilder{data: append([]byte{}, absent.data...), length: absent.length}
			tc.mutate(&b)
			r, v := parseVUINAL(t, append([]byte{0x67}, escapeRBSP(b.bytes())...))
			if r.Complete || v == nil || v.TrailingBitsValid || v.Complete {
				t.Fatalf("invalid tail accepted: %+v", v)
			}
			assertDiagnostic(t, r.Diagnostics, CodeRBSPTrailingBitsInvalid)
		})
	}
}

func TestVUIMappingAcrossEPBAndBudgets(t *testing.T) {
	b := vuiSPSPrefix(true)
	b.write(0, 4)
	b.write(1, 1)
	b.write(1, 32)
	b.write(50, 32)
	b.write(1, 1)
	b.write(0, 4)
	nal := vuiSPSNAL(b)
	r, v := parseVUINAL(t, nal)
	if !r.Complete || len(v.NumUnitsInTick.Span.SourceSpans) < 2 {
		t.Fatalf("no EPB crossing: %+v", v)
	}
	data := append(make([]byte, 19), vuiSequence(nal)...)
	for _, field := range []*VUIUnsignedField{v.NumUnitsInTick, v.TimeScale} {
		var decoded uint64
		var count uint32
		for _, span := range field.Span.SourceSpans {
			for i := uint32(0); i < span.BitLength; i++ {
				bit := uint32(span.BitOffset) + i
				decoded = decoded<<1 | uint64((data[span.ByteOffset+uint64(bit/8)]>>(7-bit%8))&1)
				count++
			}
		}
		if decoded != uint64(field.Value) || count != 32 {
			t.Fatalf("incorrect source mapping: %+v = %d/%d", field, decoded, count)
		}
	}
	for _, o := range []Options{{ParseVUI: true, MaxParameterSetBytes: 1}, {ParseVUI: true, MaxRBSPMappingEntries: 1}} {
		r := parseNALsAt(t, 0, vuiSequence(nal), 4, o)
		if r.Complete || r.NALUnits[0].SequenceParameterSet.VUI != nil {
			t.Fatal("budget-limited SPS was expanded")
		}
	}
}

func FuzzSPSVUI(f *testing.F) {
	full := vuiSPSPrefix(true)
	vuiFullSyntax(&full, 0)
	f.Add(vuiSPSNAL(full))
	f.Add([]byte{0x67})
	f.Add(mustDecodeHex("6742001e80"))
	options, _ := (Options{ParseVUI: true, MaxDiagnostics: 16, MaxNALUnits: 1, MaxParameterSetBytes: 4096, MaxRBSPMappingEntries: 256}).Normalized()
	f.Fuzz(func(t *testing.T, nal []byte) {
		if len(nal) > 8192 {
			return
		}
		nal = append([]byte{}, nal...)
		if len(nal) > 0 {
			nal[0] = 0x67
		}
		payload := vuiSequence(nal)
		r, err := ParseLengthPrefixedNALUnits(bytes.NewReader(payload), cursor.Span{BitLength: uint32(len(payload) * 8)}, 4, options)
		if err != nil {
			t.Fatal(err)
		}
		assertNALSequenceInvariants(t, r, options, 0, len(payload))
		for _, n := range r.NALUnits {
			if n.SequenceParameterSet == nil || n.SequenceParameterSet.VUI == nil {
				continue
			}
			v := n.SequenceParameterSet.VUI
			if v.Complete && (!v.TrailingBitsValid || v.RemainingRBSPBits != 0) {
				t.Fatal("invalid VUI completeness")
			}
			for _, h := range []*HRDParameters{v.NALHRD, v.VCLHRD} {
				if h != nil && len(h.CPB) > 32 {
					t.Fatal("HRD allocation escaped bound")
				}
			}
		}
	})
}

func TestVUIHRDConsistency(t *testing.T) {
	for _, tc := range []struct {
		name             string
		rates, sizes     []uint32
		lowDelay         bool
		secondTimeOffset uint64
	}{
		{"rate order", []uint32{2, 1}, []uint32{2, 1}, false, 0},
		{"size order", []uint32{1, 2}, []uint32{1, 2}, false, 0},
		{"low delay", []uint32{1, 2}, []uint32{2, 1}, true, 0},
		{"dual delays", []uint32{1}, []uint32{1}, false, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := vuiSPSPrefix(true)
			b.write(0, 5)
			for group := 0; group < 2; group++ {
				b.write(1, 1)
				b.writeUE(uint32(len(tc.rates) - 1))
				b.write(0, 8)
				for i, rate := range tc.rates {
					b.writeUE(rate)
					b.writeUE(tc.sizes[i])
					b.write(0, 1)
				}
				b.write(0, 15)
				if group == 1 {
					b.write(tc.secondTimeOffset, 5)
				} else {
					b.write(0, 5)
				}
			}
			if tc.lowDelay {
				b.write(1, 1)
			} else {
				b.write(0, 1)
			}
			b.write(0, 2)
			r, v := parseVUINAL(t, vuiSPSNAL(b))
			if r.Complete || v.Complete || !v.TrailingBitsValid {
				t.Fatalf("HRD constraint ignored: %+v", v)
			}
			assertDiagnostic(t, r.Diagnostics, CodeVUIHRDValueInvalid)
		})
	}
}
