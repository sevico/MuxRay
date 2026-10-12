package detailview

import (
	"bytes"
	"context"
	"testing"

	"github.com/sevico/muxray/internal/avccontext"
	"github.com/sevico/muxray/internal/flv"
)

func TestAVCFullHeaderProjectionPreservesTailSourceAndPartialState(t *testing.T) {
	for _, complete := range []bool{false, true} {
		config := avcHex(t, "0142001effe100086742001ef40a0fc801000468ce3c80")
		data, _ := fixture(t, 9, append([]byte{0x17, 0, 0, 0, 0}, config...))
		nal := []byte{0x65, 0xb8, 0x40}
		if complete {
			nal = append(nal, 0xa8)
		}
		second, _ := fixture(t, 9, avcNALPayload(4, nal))
		data = append(data, second[13:]...)
		source := bytes.NewReader(data)
		scan, err := flv.Scan(source, uint64(len(data)), flv.Options{})
		if err != nil {
			t.Fatal(err)
		}
		lookup := func(id uint64) (flv.TagRecord, error) { return scan.Tags[id], nil }
		tag := scan.Tags[1]
		r, err := avccontext.Resolve(context.Background(), source, tag, lookup, DesktopAVCOptions(), avccontext.Limits{})
		if err != nil {
			t.Fatal(err)
		}
		p, err := avccontext.ResolveParameters(context.Background(), source, tag, lookup, r, DesktopFullAVCOptions(), avccontext.ParameterLimits{})
		if err != nil {
			t.Fatal(err)
		}
		d, err := ParseWithAVCFullParameters(context.Background(), source, tag, r, p)
		if err != nil {
			t.Fatal(err)
		}
		assertBounded(t, d)
		if d.Complete != complete || find(t, d, "prefixComplete").Value != "true" {
			t.Fatalf("full=%t: %+v", complete, d)
		}
		if complete {
			if find(t, d, "headerComplete").Value != "true" || find(t, d, "headerBytesRead").Value != "3" {
				t.Fatal("full header accounting missing")
			}
			qp, deblock := find(t, d, "sliceQpDelta"), find(t, d, "disableDeblockingFilterIdc")
			if qp.Value != "0" || qp.Span == nil || qp.Span.ByteOffset != "79" || qp.Span.BitOffset != 0 || qp.Span.BitLength != 1 {
				t.Fatalf("QP source: %+v", qp)
			}
			if deblock.Value != "1" || deblock.Span == nil || deblock.Span.ByteOffset != "79" || deblock.Span.BitOffset != 1 || deblock.Span.BitLength != 3 {
				t.Fatalf("deblock source: %+v", deblock)
			}
			if find(t, d, "headerBits").Span != nil {
				t.Fatal("derived count invented source")
			}
		} else if find(t, d, "headerComplete").Value != "false" || optionalField(d, "sliceQpDelta") != nil {
			t.Fatal("partial tail invented decoded field")
		}
	}
}
