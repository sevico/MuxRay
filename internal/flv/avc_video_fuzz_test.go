package flv

import (
	"bytes"
	"errors"
	"testing"

	"github.com/sevico/muxray/internal/avc"
)

func FuzzAVCVideo(f *testing.F) {
	for _, seed := range [][]byte{
		nil,
		{0x17},
		{0x17, 0x00},
		{0x17, 0x00, 0, 0, 0},
		{0x17, 0x00, 0, 0, 0, 1, 0x42, 0, 0x1e, 0xff, 0xe0, 0},
		{0x27, 0x01, 0, 0, 0, 0, 0, 0, 1, 9},
		{0x17, 0x02, 0, 0, 0},
		{0x17, 0x03, 0, 0, 0},
		{0x57, 0x00},
		{0x12, 0x00, 0, 0, 0},
	} {
		f.Add(seed, uint8(0))
	}

	f.Fuzz(func(t *testing.T, payload []byte, lengthSize uint8) {
		if len(payload) > 4_096 {
			t.Skip()
		}
		lengthSizes := [...]uint8{0, 1, 2, 4}
		lengthSize = lengthSizes[int(lengthSize)%len(lengthSizes)]
		data, tag := scriptDataFixture(t, byte(TagTypeVideo), payload)
		detail, err := ParseAVCVideo(bytes.NewReader(data), tag, AVCVideoOptions{NALLengthSize: lengthSize})
		if err != nil {
			if errors.Is(err, ErrAVCVideoWrongCodecID) || errors.Is(err, ErrAVCVideoCommandFrame) {
				return
			}
			t.Fatalf("ParseAVCVideo returned unexpected in-memory source error: %v", err)
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
			if diagnostic.RecoveryOffset != nil {
				if *diagnostic.RecoveryOffset < payloadStart || *diagnostic.RecoveryOffset > payloadEnd {
					t.Fatalf("recovery offset %d outside payload [%d,%d]", *diagnostic.RecoveryOffset, payloadStart, payloadEnd)
				}
				if diagnostic.Severity == avc.SeverityError && *diagnostic.RecoveryOffset != payloadEnd {
					t.Fatalf("fatal recovery offset = %d, want payload end %d", *diagnostic.RecoveryOffset, payloadEnd)
				}
			}
		}
	})
}
