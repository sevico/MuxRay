package mp3

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/sevico/muxray/internal/cursor"
)

func FuzzFrameHeader(f *testing.F) {
	for _, seed := range [][]byte{
		nil,
		{0xff},
		{0xff, 0xfb, 0x90},
		{0xff, 0xfb, 0x90, 0x64},
		{0xff, 0xf3, 0x80, 0xc4},
		{0xff, 0xe3, 0x48, 0xc4},
		{0xff, 0xfb, 0x00, 0x64},
		{0xff, 0xeb, 0xfc, 0x02},
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 4_096 {
			t.Skip()
		}
		span := cursor.Span{BitLength: uint32(len(data) * 8)}
		first, err := ParseFrameHeader(bytes.NewReader(data), span, Options{})
		if err != nil {
			t.Fatalf("ParseFrameHeader returned unexpected in-memory error: %v", err)
		}
		second, err := ParseFrameHeader(bytes.NewReader(data), span, Options{})
		if err != nil || !reflect.DeepEqual(first, second) {
			t.Fatalf("nondeterministic parse: first=%+v second=%+v err=%v", first, second, err)
		}
		if first.BitsConsumed > 32 || first.BitsConsumed > uint64(span.BitLength) || len(first.Diagnostics) > int(DefaultMaxDiagnostics) {
			t.Fatalf("resource bound exceeded: %+v", first)
		}
		end := uint64(len(data))
		for _, diagnostic := range first.Diagnostics {
			if diagnostic.Offset > end || diagnostic.BitOffset > 7 || (diagnostic.Offset == end && diagnostic.BitOffset != 0) {
				t.Fatalf("diagnostic outside input: %+v", diagnostic)
			}
			if diagnostic.RecoveryOffset != nil && *diagnostic.RecoveryOffset != end {
				t.Fatalf("recovery offset outside bounded payload: %+v", diagnostic)
			}
		}
		if first.Complete && (!first.HeaderPresent || first.HasErrors()) {
			t.Fatalf("complete result violates invariants: %+v", first)
		}
	})
}
