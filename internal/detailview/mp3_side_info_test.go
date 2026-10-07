package detailview

import (
	"bytes"
	"context"
	"encoding/hex"
	"strconv"
	"strings"
	"testing"
)

// These side-information fixtures contain structured declarations. Their
// zero-filled audio bodies intentionally do not promise decoder conformance.
func sideInfoViewFrame(t testing.TB) []byte {
	t.Helper()
	side, err := hex.DecodeString("645a5064055a56844e5c0c80ab5ad089cb8190156d5a1139703202adeb42272e")
	if err != nil {
		t.Fatal(err)
	}
	frame := make([]byte, 417)
	copy(frame, []byte{0xff, 0xfb, 0x90, 0})
	copy(frame[4:], side)
	return frame
}

func TestMP3SideInfoViewBeforeTagAndSourceBits(t *testing.T) {
	frame := sideInfoViewFrame(t)
	data, tag := fixture(t, 8, append([]byte{0x2f}, frame...))
	d, err := Parse(context.Background(), bytes.NewReader(data), tag)
	if err != nil || !d.Complete || d.Truncated || len(d.Diagnostics) != 0 {
		t.Fatalf("before-tag detail: %v %+v", err, d)
	}
	if f := find(t, d, "mainDataDependency"); !strings.Contains(f.Value, "before_tag") || !strings.Contains(f.Value, "bytes before tag=200") || f.Span != nil {
		t.Fatalf("dependency %+v", f)
	}
	if f := find(t, d, "mainDataBegin"); f.Value != "200" || f.Span == nil || f.Span.ByteOffset != "29" || f.Span.BitLength != 9 {
		t.Fatalf("backpointer %+v", f)
	}
	if optionalField(d, "region0Count") != nil || optionalField(d, "region1Count") != nil {
		t.Fatal("invented long-block fields for switched block")
	}
	count := 0
	for _, f := range d.Fields {
		if f.Type != "encoded unsigned" && f.Type != "encoded boolean" {
			continue
		}
		if f.Span == nil {
			t.Fatalf("missing source span %+v", f)
		}
		got := sideInfoViewBits(t, data, *f.Span)
		if f.Type == "encoded boolean" {
			want, err := strconv.ParseBool(f.Value)
			if err != nil || want != (got != 0) {
				t.Fatalf("flag/source mismatch %+v", f)
			}
		} else {
			want, err := strconv.ParseUint(f.Value, 10, 32)
			if err != nil || want != got {
				t.Fatalf("value/source mismatch %+v", f)
			}
		}
		count++
	}
	if count < 60 {
		t.Fatalf("side fields not expanded: %d", count)
	}
	assertBounded(t, d)
}

func sideInfoViewBits(t testing.TB, data []byte, s Span) uint64 {
	t.Helper()
	offset, err := strconv.ParseUint(s.ByteOffset, 10, 64)
	if err != nil || s.BitLength == 0 || s.BitLength > 32 || s.BitOffset > 7 || offset*8+uint64(s.BitOffset)+uint64(s.BitLength) > uint64(len(data))*8 {
		t.Fatalf("invalid source span %+v", s)
	}
	var result uint64
	for i := uint32(0); i < s.BitLength; i++ {
		bit := offset*8 + uint64(s.BitOffset) + uint64(i)
		result = result<<1 | uint64(data[bit/8]>>(7-bit%8)&1)
	}
	return result
}

func TestMP3SideInfoViewExpandsOnlyTwoAndKeepsSummaries(t *testing.T) {
	for _, frames := range []int{3, 64} {
		t.Run(strconv.Itoa(frames), func(t *testing.T) {
			frame := sideInfoViewFrame(t)
			// First-frame metadata belongs ahead of expanded syntax so a large
			// same-tag sequence does not displace stream-level declarations.
			payload := append([]byte{0x2f}, bytes.Repeat(frame, frames)...)
			copy(payload[37:], "Info") // canonical first-frame VBR marker; flags zero.
			d := parseFixture(t, 8, payload)
			if !d.Complete || !d.Truncated || len(d.Fields) > MaxFields {
				t.Fatalf("bounded expansion: complete=%t truncated=%t fields=%d", d.Complete, d.Truncated, len(d.Fields))
			}
			expanded, dependencies := 0, 0
			firstSide := -1
			for i, f := range d.Fields {
				if strings.HasPrefix(f.Name, "Side info Frame ") {
					expanded++
					if firstSide < 0 {
						firstSide = i
					}
				}
				if f.Name == "mainDataDependency" {
					dependencies++
				}
			}
			if expanded != 2 || dependencies != frames || optionalField(d, "Side info Frame 2") != nil {
				t.Fatalf("expanded=%d dependencies=%d", expanded, dependencies)
			}
			omitted := find(t, d, "sideInfoOmittedFrames")
			if !strings.HasPrefix(omitted.Value, strconv.Itoa(frames-2)+";") {
				t.Fatalf("omitted %+v", omitted)
			}
			for i := 0; i < frames; i++ {
				find(t, d, "Frame "+strconv.Itoa(i))
			}
			if f := find(t, d, "Info"); f.ID == "" {
				t.Fatal("VBR metadata omitted")
			}
			for i, f := range d.Fields {
				if f.Name == "Info" || strings.HasPrefix(f.Name, "Frame ") {
					if i >= firstSide {
						t.Fatal("summary or VBR placed after expanded side info")
					}
				}
			}
			assertBounded(t, d)
		})
	}
}

func TestMP3SideInfoViewTruncationOmitsUnreadFields(t *testing.T) {
	frame := sideInfoViewFrame(t)
	d := parseFixture(t, 8, append([]byte{0x2f}, frame[:5]...))
	if d.Complete || !d.Truncated || optionalField(d, "mainDataBegin") != nil || !hasDiagnostic(d, "MP3_SIDE_INFO_TRUNCATED") {
		t.Fatalf("truncated side info %+v", d)
	}
	if f := find(t, d, "mainDataDependency"); !strings.Contains(f.Value, "unknown") {
		t.Fatalf("guessed partial dependency %+v", f)
	}
	assertBounded(t, d)
}
