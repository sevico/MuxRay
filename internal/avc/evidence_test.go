package avc

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"github.com/sevico/muxray/internal/cursor"
)

func evidenceSequence(t *testing.T, options Options, units ...[]byte) NALUnitSequence {
	t.Helper()
	var data []byte
	for _, n := range units {
		data = append(data, byte(len(n)))
		data = append(data, n...)
	}
	s, err := ParseLengthPrefixedNALUnits(bytes.NewReader(data), cursor.Span{BitLength: uint32(len(data) * 8)}, 1, options)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestKeyframeEvidenceDeclarationAndSliceTypes(t *testing.T) {
	for _, tc := range []struct {
		name              string
		frame             uint8
		header            byte
		wantIDR, relation string
	}{
		{"key-IDR", 1, 0x65, "yes", "keyframe_with_idr"},
		{"inter-IDR", 2, 0x65, "yes", "inter_with_idr"},
		{"key-I-slice", 1, 0x61, "no", "keyframe_without_idr"},
		{"inter-I-slice", 2, 0x61, "no", "inter_without_idr"},
		{"generated-key", 4, 0x65, "yes", "other_declaration"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := evidenceSequence(t, Options{ParseSliceHeaders: true}, []byte{tc.header, 0xb8, 0x40})
			if s.Complete || s.NALUnits[0].SliceHeader.PrefixComplete {
				t.Fatal("fixture must lack SPS/PPS context")
			}
			r := CompareKeyframeEvidence(tc.frame, &s)
			if r.ObservedIDR != tc.wantIDR || r.DeclarationRelation != tc.relation ||
				!r.NALHeaderCoverageComplete || !r.SliceTypeCoverageComplete || r.ObservedSliceHeaders != 1 ||
				len(r.SliceTypes) != 1 || r.SliceTypes[0].Name != "I" || r.SliceTypes[0].Field.Value != 2 {
				t.Fatalf("evidence: %+v", r)
			}
			if r.FirstNAL == nil || r.FirstNAL.Span != s.NALUnits[0].Header.NALUnitTypeSpan ||
				!reflect.DeepEqual(r.SliceTypes[0].Field.Span, s.NALUnits[0].SliceHeader.SliceType.Span) {
				t.Fatal("original source fields were lost")
			}
		})
	}
	s := evidenceSequence(t, Options{ParseSliceHeaders: true},
		[]byte{0x61, 0xb8}, []byte{0x61, 0xa8}, []byte{0x61, 0xe0},
		[]byte{0x61, 0x92}, []byte{0x61, 0x96}, []byte{0x61, 0xb8})
	r := CompareKeyframeEvidence(1, &s)
	var names []string
	for _, v := range r.SliceTypes {
		names = append(names, v.Name)
	}
	if strings.Join(names, ",") != "P,B,I,SP,SI" || r.ObservedSliceHeaders != 6 || r.SliceTypes[2].NALIndex != 0 {
		t.Fatalf("mixed set/order/first representative: %+v", r)
	}
	before := s.NALUnits[0].SliceHeader.SliceType.Value
	_ = CompareKeyframeEvidence(2, &s)
	if s.NALUnits[0].SliceHeader.SliceType.Value != before {
		t.Fatal("comparison mutated parse result")
	}
	// The all-slices form (5..9) still describes observed slice types, not a
	// validated picture. Keep the original raw value in the representative.
	for value, name := range []string{"P", "B", "I", "SP", "SI"} {
		unit := s.NALUnits[0]
		unit.SliceHeader = &SliceHeader{SliceType: &VUIUnsignedField{Value: uint32(value + 5)}}
		sequence := NALUnitSequence{FramingComplete: true, NALUnits: []NALUnit{unit}}
		r := CompareKeyframeEvidence(1, &sequence)
		if len(r.SliceTypes) != 1 || r.SliceTypes[0].Name != name || r.SliceTypes[0].Field.Value != uint32(value+5) {
			t.Fatalf("normalized slice_type %d: %+v", value+5, r)
		}
		unit.SliceHeader.SliceType.Value = 10
		r = CompareKeyframeEvidence(1, &sequence)
		if r.SliceTypeCoverageComplete || len(r.SliceTypes) != 0 || r.ObservedSliceHeaders != 0 {
			t.Fatalf("invalid slice_type became evidence: %+v", r)
		}
	}
}

func TestKeyframeEvidenceUnknownAndPartialCoverage(t *testing.T) {
	if r := CompareKeyframeEvidence(1, nil); r.ObservedIDR != "unknown" || r.DeclarationRelation != "undetermined" || r.NALHeaderCoverageComplete {
		t.Fatalf("unavailable framing: %+v", r)
	}
	for _, tc := range []struct {
		name string
		unit []byte
	}{
		{"zero length", nil}, {"unspecified", []byte{0}}, {"reserved", []byte{17}},
		{"prefix extension", []byte{14}}, {"slice extension", []byte{20}}, {"depth extension", []byte{21}},
		{"forbidden bit", []byte{0xe5}}, {"IDR non-reference", []byte{5}}, {"SEI reference", []byte{0x26}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := evidenceSequence(t, Options{}, tc.unit)
			r := CompareKeyframeEvidence(1, &s)
			if r.ObservedIDR != "unknown" || r.NALHeaderCoverageComplete || r.SliceTypeCoverageComplete || r.UnclassifiedNALHeaders != 1 {
				t.Fatalf("incomplete evidence became a negative: %+v", r)
			}
		})
	}
	for _, first := range []byte{0x61, 0x65} {
		s := evidenceSequence(t, Options{MaxNALUnits: 1}, []byte{first}, []byte{0x65})
		r := CompareKeyframeEvidence(1, &s)
		want := "unknown"
		if first == 0x65 {
			want = "yes"
		}
		if r.ObservedIDR != want || r.NALHeaderCoverageComplete || r.SliceTypeCoverageComplete || r.InspectedNALHeaders != 1 {
			t.Fatalf("NAL budget: %+v", r)
		}
		data := []byte{1, first, 4, 0x65}
		s, err := ParseLengthPrefixedNALUnits(bytes.NewReader(data), cursor.Span{BitLength: 32}, 1, Options{})
		if err != nil {
			t.Fatal(err)
		}
		r = CompareKeyframeEvidence(1, &s)
		if r.ObservedIDR != want || r.NALHeaderCoverageComplete || r.RetainedNALUnits != 2 || r.UnclassifiedNALHeaders != 1 {
			t.Fatalf("truncated NAL: %+v", r)
		}
	}
	// Header coverage can be complete even when slice_type was not inspected.
	s := evidenceSequence(t, Options{}, []byte{0x61, 0xb8})
	r := CompareKeyframeEvidence(1, &s)
	if r.ObservedIDR != "no" || !r.NALHeaderCoverageComplete || r.SliceTypeCoverageComplete || len(r.SliceTypes) != 0 {
		t.Fatalf("independent observation coverage: %+v", r)
	}
	s = evidenceSequence(t, Options{}, []byte{0x62, 0xb8})
	r = CompareKeyframeEvidence(1, &s)
	if r.ObservedIDR != "no" || r.SliceTypeCoverageComplete {
		t.Fatalf("partitioned slice: %+v", r)
	}
}
