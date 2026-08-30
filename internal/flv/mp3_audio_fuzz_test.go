package flv

import (
	"bytes"
	"errors"
	"testing"

	"github.com/sevico/muxray/internal/mp3"
)

func FuzzMP3Audio(f *testing.F) {
	for _, seed := range [][]byte{
		nil,
		{0x2f},
		{0x2f, 0xff, 0xfb, 0x90, 0x64},
		{0x2f, 0xff, 0xf3, 0x80, 0xc4},
		{0x2f, 0xff, 0xfb, 0x00, 0x64},
		{0x2f, 0xfe, 0xeb, 0xfc, 0x02},
		{0xaf, 0x01},
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, payload []byte) {
		if len(payload) > 4_096 {
			t.Skip()
		}
		data, tag := scriptDataFixture(t, byte(TagTypeAudio), payload)
		detail, err := ParseMP3Audio(bytes.NewReader(data), tag, mp3.Options{})
		if err != nil {
			if errors.Is(err, ErrMP3AudioWrongSoundFormat) {
				return
			}
			t.Fatalf("ParseMP3Audio returned unexpected in-memory source error: %v", err)
		}
		if detail.TagID != tag.ID || detail.Span != tag.PayloadSpan() || detail.BitsConsumed > uint64(detail.Span.BitLength) {
			t.Fatalf("detail lost bounds or identity: detail=%+v tag=%+v", detail, tag)
		}
		payloadStart := detail.Span.ByteOffset
		payloadEnd := payloadStart + uint64(tag.DataSize)
		for _, diagnostic := range detail.Diagnostics {
			if diagnostic.Offset < payloadStart || diagnostic.Offset > payloadEnd || diagnostic.BitOffset > 7 ||
				(diagnostic.Offset == payloadEnd && diagnostic.BitOffset != 0) {
				t.Fatalf("diagnostic outside payload [%d,%d]: %+v", payloadStart, payloadEnd, diagnostic)
			}
			if diagnostic.RecoveryOffset != nil && *diagnostic.RecoveryOffset != payloadEnd {
				t.Fatalf("recovery offset = %d, want payload end %d", *diagnostic.RecoveryOffset, payloadEnd)
			}
		}
	})
}
