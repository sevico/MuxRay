package detailview

import (
	"bytes"
	"context"
	"strconv"
	"testing"

	"github.com/sevico/muxray/internal/avccontext"
)

func TestSEITimingProjectionValuesAndOriginalSources(t *testing.T) {
	// Independent authored SPS: HRD 8/8/7/6-bit widths, timing 1001/60000.
	sps := avcHex(t, "6742001ef6146d0800001f480007530700080201ce6314")
	sei := avcHex(t, "0600038881c0010d2526b44a1752a7bd442170e20580")
	data, tag := fixture(t, 9, avcNALPayload(4, sps, sei))
	detail, err := ParseWithAVCFullParameters(context.Background(), bytes.NewReader(data), tag, avcResolution(4), avccontext.ParameterResolution{})
	if err != nil || !detail.Complete {
		t.Fatalf("%v %+v", err, detail)
	}
	assertBounded(t, detail)
	for name, value := range map[string]string{"cpbRemovalDelay": "37", "dpbOutputDelay": "19", "picStruct": "5", "initialCpbRemovalDelay": "17", "timeOffset": "-17", "timingContextState": "resolved", "timingContextSource": "bufferingPeriod", "timingNumUnitsInTick": "1001", "timingTimeScale": "60000"} {
		f := find(t, detail, name)
		if f.Value != value {
			t.Fatalf("%s = %s want %s", name, f.Value, value)
		}
	}
	for _, name := range []string{"cpbRemovalDelay", "dpbOutputDelay", "picStruct", "initialCpbRemovalDelay", "timeOffset"} {
		f := find(t, detail, name)
		if f.Span == nil {
			t.Fatalf("%s lost its source", name)
		}
		raw := sideInfoViewBits(t, data, *f.Span)
		want, _ := strconv.ParseInt(f.Value, 10, 64)
		if want < 0 {
			want += int64(uint64(1) << f.Span.BitLength)
		}
		if raw != uint64(want) {
			t.Fatalf("%s source=%d value=%s", name, raw, f.Value)
		}
	}
	sp := find(t, detail, "timingSpsSource").Span
	if sp == nil {
		t.Fatal("missing SPS source")
	}
	off, _ := strconv.ParseUint(sp.ByteOffset, 10, 64)
	if !bytes.Equal(data[off:off+uint64(sp.BitLength)/8], sps) {
		t.Fatal("wrong SPS original source")
	}
	if f := find(t, detail, "timingNumUnitsInTick"); f.Span != nil {
		t.Fatal("derived context fabricated an encoded span")
	}
}

func TestSEITimingProjectionMissingContextStaysOpaque(t *testing.T) {
	detail, _ := seiProjection(t, []byte{6, 1, 1, 0, 0x80}, true)
	if detail.Complete || find(t, detail, "timingContextState").Value != "missing" || optionalField(detail, "cpbRemovalDelay") != nil || optionalField(detail, "timingSpsSource") != nil {
		t.Fatalf("invented timing %+v", detail)
	}
	if find(t, detail, "seiPayload").Span == nil {
		t.Fatal("lost opaque payload source")
	}
}
