package detailview

import (
	"bytes"
	"context"
	"testing"

	"github.com/sevico/muxray/internal/avc"
	"github.com/sevico/muxray/internal/avccontext"
	"github.com/sevico/muxray/internal/cursor"
	"github.com/sevico/muxray/internal/flv"
)

func TestAVCPrefixSourceLinksAndMissingContext(t *testing.T) {
	// Structural parameter sets and slice prefix; not a decoded-picture fixture.
	config := avcHex(t, "0142001effe100086742001ef40a0fc801000468ce3c80")
	data, _ := fixture(t, 9, append([]byte{0x17, 0, 0, 0, 0}, config...))
	nalu, _ := fixture(t, 9, avcNALPayload(4, []byte{0x65, 0xb8, 0x40}))
	data = append(data, nalu[13:]...)
	source := bytes.NewReader(data)
	scan, err := flv.Scan(source, uint64(len(data)), flv.Options{})
	if err != nil || !scan.Complete {
		t.Fatalf("scan: %v", err)
	}
	lookup := func(id uint64) (flv.TagRecord, error) { return scan.Tags[id], nil }
	tag := scan.Tags[1]
	r, err := avccontext.Resolve(context.Background(), source, tag, lookup, DesktopAVCOptions(), avccontext.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	p, err := avccontext.ResolveParameters(context.Background(), source, tag, lookup, r, DesktopAVCOptions(), avccontext.ParameterLimits{})
	if err != nil {
		t.Fatal(err)
	}
	d, err := ParseWithAVCParameters(context.Background(), source, tag, r, p)
	if err != nil {
		t.Fatal(err)
	}
	if !d.Complete || d.Truncated || find(t, d, "sliceTypeName").Value != "I" || find(t, d, "frameNum").Value != "0" {
		t.Fatalf("prefix: %+v", d)
	}
	assertBounded(t, d)
	if f := find(t, d, "firstMbInSlice"); f.Span == nil || f.Span.BitOffset != 0 || f.Span.BitLength != 1 {
		t.Fatalf("first_mb source bit: %+v", f)
	}
	if f := find(t, d, "referencedPPS"); f.Span == nil || f.Span.ByteOffset != "48" || f.Span.BitLength != 32 {
		t.Fatalf("prior-tag PPS source: %+v", f)
	}
	if find(t, d, "sliceTypeName").Span != nil || find(t, d, "referencedSpsId").Span != nil {
		t.Fatal("derived values invented source bits")
	}
	p.Context, p.State = nil, avccontext.StateBudgetExhausted
	d, err = ParseWithAVCParameters(context.Background(), source, tag, r, p)
	if err != nil || d.Complete || !d.Truncated || optionalField(d, "frameNum") != nil || find(t, d, "AVC context").Value != "found" {
		t.Fatalf("independent replay budget and framing: %v %+v", err, d)
	}
}

func TestAVCPrefixDoesNotInventSpanAcrossEmulationPrevention(t *testing.T) {
	mapped := avc.MappedSpan{BitLength: 16, SourceSpans: []cursor.Span{
		{ByteOffset: 10, BitLength: 8}, {ByteOffset: 12, BitLength: 8},
	}}
	if mappedPrefixSpan(mapped) != nil {
		t.Fatal("discontinuous RBSP field acquired a contiguous span")
	}
	mapped.SourceSpans = []cursor.Span{{ByteOffset: 10, BitOffset: 3, BitLength: 16}}
	if s := mappedPrefixSpan(mapped); s == nil || s.BitOffset != 3 || s.BitLength != 16 {
		t.Fatal("contiguous source bits lost")
	}
}
