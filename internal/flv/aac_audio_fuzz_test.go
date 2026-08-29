package flv

import (
	"bytes"
	"errors"
	"testing"

	"github.com/sevico/muxray/internal/aac"
)

func FuzzAACAudio(f *testing.F) {
	for _, seed := range [][]byte{
		nil,
		{0xaf},
		{0xaf, 0x00},
		{0xaf, 0x00, 0x12, 0x10},
		{0xaf, 0x01, 0xde, 0xad},
		{0xa8, 0x01},
		{0xaf, 0x02},
		{0x2f, 0x01},
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, payload []byte) {
		if len(payload) > 4_096 {
			t.Skip()
		}
		data, tag := scriptDataFixture(t, byte(TagTypeAudio), payload)
		detail, err := ParseAACAudio(bytes.NewReader(data), tag, aac.Options{})
		if err != nil {
			if errors.Is(err, ErrAACAudioWrongSoundFormat) {
				return
			}
			t.Fatalf("ParseAACAudio returned unexpected in-memory source error: %v", err)
		}
		if detail.TagID != tag.ID || detail.Span != tag.PayloadSpan() {
			t.Fatalf("detail lost tag identity or payload boundary: detail=%+v tag=%+v", detail, tag)
		}
		payloadStart := detail.Span.ByteOffset
		payloadEnd := payloadStart + uint64(tag.DataSize)
		if detail.BitsConsumed > uint64(detail.Span.BitLength) {
			t.Fatalf("bitsConsumed %d crosses %d-bit payload", detail.BitsConsumed, detail.Span.BitLength)
		}
		for _, diagnostic := range detail.Diagnostics {
			if diagnostic.Offset < payloadStart || diagnostic.Offset > payloadEnd {
				t.Fatalf("diagnostic outside payload [%d,%d]: %+v", payloadStart, payloadEnd, diagnostic)
			}
			if diagnostic.RecoveryOffset != nil && *diagnostic.RecoveryOffset != payloadEnd {
				t.Fatalf("recovery offset = %d, want payload end %d", *diagnostic.RecoveryOffset, payloadEnd)
			}
		}
	})
}
