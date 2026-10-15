package seisearch

import (
	"bytes"
	"testing"

	"github.com/sevico/muxray/internal/cursor"
)

func FuzzSearchSEI(f *testing.F) {
	for _, seed := range [][]byte{messages(1), messages(65), bp(), pic(), {6, 255}, {6, 200, 3, 0, 0, 3, 1, 0x80}, {0x86, 200, 0, 0x80}} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, nal []byte) {
		if len(nal) > 128<<10 {
			t.Skip()
		}
		data, lookup, total := fixture(t, config(4, timingSPS()), nalu(4, bp()), nalu(4, nal), nalu(4, pic()))
		var state State
		for pages := 0; pages < 7; pages++ {
			p, err := Search(t.Context(), bytes.NewReader(data), lookup, state, total, true, "all")
			if err != nil {
				t.Fatal(err)
			}
			if len(p.Rows) > MaxResults || int(p.Pending) > MaxPending || p.BytesRead > MaxBytes || p.Scanned > MaxScanTags || p.Next > total {
				t.Fatal("search bounds exceeded")
			}
			for _, row := range p.Rows {
				for _, span := range []*cursor.Span{row.SourceSpan, row.SPSSpan, row.EvidenceSpan} {
					if span != nil && (span.Validate() != nil || span.ByteOffset > uint64(len(data)) || uint64(span.BitOffset)+uint64(span.BitLength) > (uint64(len(data))-span.ByteOffset)*8) {
						t.Fatalf("invalid source span %+v", span)
					}
				}
			}
			state = p.State
			if p.Exhausted {
				return
			}
		}
		t.Fatal("bounded source did not make progress")
	})
}
