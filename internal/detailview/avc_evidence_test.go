package detailview

import (
	"bytes"
	"context"
	"reflect"
	"testing"

	"github.com/sevico/muxray/internal/avccontext"
	"github.com/sevico/muxray/internal/flv"
)

func TestAVCEvidenceProjectionAndReads(t *testing.T) {
	payload := avcNALPayload(4, []byte{0x65, 0xb8, 0x40}, []byte{0x61, 0xe0})
	data, tag := fixture(t, 9, payload)
	source := &selectedPayloadReader{source: bytes.NewReader(data), start: 24, end: int64(len(data)) - 4}
	r := avcResolution(4)
	p := avccontext.ParameterResolution{State: avccontext.StateMissing}
	d, err := ParseWithAVCParameters(context.Background(), source, tag, r, p)
	if err != nil {
		t.Fatal(err)
	}
	reads := source.reads
	source.reads = 0
	options := DesktopAVCOptions()
	options.NALLengthSize = 4
	options.AVC.ParseVUI, options.AVC.ParsePPS, options.AVC.ParseSliceHeaders = true, true, true
	parsed, err := flv.ParseAVCVideo(source, tag, options)
	if err != nil {
		t.Fatal(err)
	}
	if source.reads != reads || d.Complete != parsed.Complete || len(d.Diagnostics) != len(parsed.Diagnostics) {
		t.Fatalf("comparison changed reads/completeness/diagnostics: reads %d/%d", reads, source.reads)
	}
	assertBounded(t, d)
	for name, want := range map[string]string{
		"Keyframe evidence": "inter_with_idr", "declaredFrameType": "2", "observedIDR": "yes",
		"declarationRelation": "inter_with_idr", "nalHeaderCoverageComplete": "true", "sliceTypeCoverageComplete": "true",
		"observedSliceTypes": "P,I", "retainedNALUnits": "2", "inspectedNALHeaders": "2", "unclassifiedNALHeaders": "0", "observedSliceHeaders": "2",
	} {
		if got := find(t, d, name).Value; got != want {
			t.Fatalf("%s: %q != %q", name, got, want)
		}
	}
	group := find(t, d, "Keyframe evidence")
	if group.Type != "bounded NAL evidence" {
		t.Fatalf("group: %+v", group)
	}
	for _, name := range []string{"declaredFrameType", "evidenceNALType", "evidenceIDRNALType", "evidenceSliceTypeI", "evidenceSliceTypeP"} {
		f := find(t, d, name)
		if f.ParentID != group.ID || f.Span == nil {
			t.Fatalf("missing source link: %+v", f)
		}
	}
	if !reflect.DeepEqual(find(t, d, "declaredFrameType").Span, span(parsed.Header.FrameTypeSpan)) ||
		!reflect.DeepEqual(find(t, d, "evidenceIDRNALType").Span, span(parsed.NALUnits.NALUnits[0].Header.NALUnitTypeSpan)) ||
		!reflect.DeepEqual(find(t, d, "evidenceSliceTypeI").Span, mappedPrefixSpan(parsed.NALUnits.NALUnits[0].SliceHeader.SliceType.Span)) {
		t.Fatal("evidence source mapping differs from parser")
	}
	for _, name := range []string{"observedIDR", "declarationRelation", "observedSliceTypes", "nalHeaderCoverageComplete"} {
		if find(t, d, name).Span != nil {
			t.Fatalf("derived %s invented source span", name)
		}
	}
}

func TestAVCEvidenceSurvivesBudgetsAndMissingFraming(t *testing.T) {
	var units [][]byte
	for i := 0; i < 257; i++ {
		units = append(units, []byte{0x61})
	}
	d := parseAVCFixture(t, avcNALPayload(4, units...), avcResolution(4))
	if !d.Truncated || find(t, d, "observedIDR").Value != "unknown" ||
		find(t, d, "retainedNALUnits").Value != "256" || find(t, d, "evidenceScope").Value == "" {
		t.Fatalf("bounded comparison missing or overclaimed: %+v", d)
	}
	for _, state := range []string{avccontext.StateMissing, avccontext.StateBudgetExhausted} {
		d = parseAVCFixture(t, avcNALPayload(4, []byte{0x65}), avccontext.Resolution{State: state})
		if find(t, d, "observedIDR").Value != "unknown" || find(t, d, "retainedNALUnits").Value != "0" || find(t, d, "observedSliceTypes").Value != "" {
			t.Fatalf("unavailable boundaries: %+v", d)
		}
	}
	config := avcHex(t, desktopHighAVCC)
	d = parseAVCFixture(t, append([]byte{0x17, 0, 0, 0, 0}, config...), avccontext.Resolution{})
	if optionalField(d, "Keyframe evidence") != nil {
		t.Fatal("avcC became picture evidence")
	}
}
