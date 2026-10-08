package detailview

import (
	"bytes"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/sevico/muxray/internal/flv"
	"github.com/sevico/muxray/internal/mp3"
	"github.com/sevico/muxray/internal/report"
)

func TestMP3FrameSelectionPreservesHistoryAndSourceSpans(t *testing.T) {
	for _, tc := range []struct{ count, selected uint32 }{{3, 0}, {3, 2}, {8, 7}, {65, 63}} {
		t.Run(strconv.Itoa(int(tc.count))+"/"+strconv.Itoa(int(tc.selected)), func(t *testing.T) {
			data, tag := fixture(t, 8, append([]byte{0x2f}, bytes.Repeat(sideInfoViewFrame(t), int(tc.count))...))
			d, err := ParseWithMP3Frame(t.Context(), bytes.NewReader(data), tag, tc.selected)
			if err != nil || d == nil || !d.Truncated {
				t.Fatalf("selected detail: %v %+v", err, d)
			}
			checked := min(tc.count, 64)
			if find(t, d, "selectedFrameIndex").Value != strconv.Itoa(int(tc.selected)) || find(t, d, "checkedFrameCount").Value != strconv.Itoa(int(checked)) || find(t, d, "frameBudget").Value != "64" || find(t, d, "byteCoverageBudget").Value != "1048576" {
				t.Fatal("selection changed budget or lost identity")
			}
			indices := strings.Split(find(t, d, "selectableFrameIndices").Value, ",")
			if len(indices) != int(checked) || indices[len(indices)-1] != strconv.Itoa(int(checked-1)) {
				t.Fatalf("selectable range %v", indices)
			}
			expanded := 0
			for _, f := range d.Fields {
				if strings.HasPrefix(f.Name, "Side info Frame ") {
					expanded++
					if f.Name != "Side info Frame "+strconv.Itoa(int(tc.selected)) {
						t.Fatalf("wrong frame expanded %+v", f)
					}
				}
				if f.Type == "encoded unsigned" || f.Type == "encoded boolean" {
					if f.Span == nil {
						t.Fatalf("missing source span %+v", f)
					}
					got := sideInfoViewBits(t, data, *f.Span)
					want := strconv.FormatUint(got, 10)
					if f.Type == "encoded boolean" {
						want = strconv.FormatBool(got != 0)
					}
					if f.Value != want {
						t.Fatalf("source value mismatch %+v: %s", f, want)
					}
				}
			}
			if expanded != 1 || !strings.HasPrefix(find(t, d, "sideInfoOmittedFrames").Value, strconv.Itoa(int(checked-1))+";") {
				t.Fatalf("expansion count %d", expanded)
			}
			parsed, err := flv.ParseMP3Audio(bytes.NewReader(data), tag, mp3.Options{InspectSideInfo: true})
			if err != nil {
				t.Fatal(err)
			}
			cli := report.NewMP3TagDetailV3(uint64(len(data)), tag, parsed)
			frame := cli.Detail.Frames.Items[tc.selected]
			backpointer := find(t, d, "mainDataBegin")
			if s := frame.SideInfo.MainDataBegin; backpointer.Value != strconv.FormatUint(s.Value, 10) || *backpointer.Span != (Span{ByteOffset: strconv.FormatUint(s.Span.ByteOffset, 10), BitOffset: s.Span.BitOffset, BitLength: s.Span.BitLength}) {
				t.Fatalf("CLI v3/source mapping differs: %+v %+v", backpointer, s)
			}
			state := "within_tag"
			if tc.selected == 0 {
				state = "before_tag"
			}
			if frame.Dependency.State != state {
				t.Fatalf("CLI dependency %+v", frame.Dependency)
			}
			parent := find(t, d, "Frame "+strconv.Itoa(int(tc.selected))).ID
			found := false
			for _, f := range d.Fields {
				if f.Name == "mainDataDependency" && f.ParentID == parent {
					found = strings.HasPrefix(f.Value, state+";") && strings.Contains(f.Value, "declared bits=400")
				}
			}
			if !found {
				t.Fatal("selection lost prior same-Tag physical slots")
			}
			assertBounded(t, d)
		})
	}
}

func TestMP3FrameSelectionRejectsUnconfirmedBoundaries(t *testing.T) {
	frame := sideInfoViewFrame(t)
	free := append([]byte(nil), frame...)
	free[2] = 0
	for _, tc := range []struct {
		name    string
		kind    byte
		payload []byte
		index   uint32
	}{
		{"out_of_range", 8, append([]byte{0x2f}, frame...), 1},
		{"budget", 8, append([]byte{0x2f}, bytes.Repeat(frame, 65)...), 64},
		{"free_format", 8, append([]byte{0x2f}, free...), 0},
		{"partial_side_info", 8, append([]byte{0x2f}, frame[:5]...), 0},
		{"partial_body", 8, append([]byte{0x2f}, frame[:400]...), 0},
		{"bad_header", 8, []byte{0x2f, 0, 0, 0, 0}, 0},
		{"aac", 8, []byte{0xaf, 0, 0x12, 0x10}, 0},
		{"script", 18, []byte{5}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data, tag := fixture(t, tc.kind, tc.payload)
			d, err := ParseWithMP3Frame(t.Context(), bytes.NewReader(data), tag, tc.index)
			if !errors.Is(err, ErrInvalidInput) || d != nil {
				t.Fatalf("unselectable frame published: %+v %v", d, err)
			}
			if tc.kind == 8 && tc.name != "aac" {
				preview, err := Parse(t.Context(), bytes.NewReader(data), tag)
				if err != nil || find(t, preview, "selectedFrameIndex").Value != "preview" {
					t.Fatalf("default preview changed: %+v %v", preview, err)
				}
				if tc.name != "out_of_range" && tc.name != "budget" && find(t, preview, "selectableFrameIndices").Value != "" {
					t.Fatal("unconfirmed frame offered as selectable")
				}
			}
		})
	}
}
