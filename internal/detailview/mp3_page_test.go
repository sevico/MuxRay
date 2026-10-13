package detailview

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/sevico/muxray/internal/cursor"
	"github.com/sevico/muxray/internal/flv"
	"github.com/sevico/muxray/internal/mp3"
	"github.com/sevico/muxray/internal/mp3context"
)

func TestMP3PageLargeIndicesSelectedSideInfoAndCRCSourceBits(t *testing.T) {
	data, tag := fixture(t, 8, append([]byte{0x2f}, bytes.Repeat(sideInfoViewFrame(t), 1100)...))
	var checkpoint *mp3.FrameCursor
	var p flv.MP3Audio
	var page mp3.FramePage
	for {
		var err error
		p, page, err = flv.ParseMP3AudioPage(t.Context(), bytes.NewReader(data), tag, checkpoint)
		if err != nil {
			t.Fatal(err)
		}
		if page.StartIndex == 1024 {
			break
		}
		checkpoint = page.Next
	}
	index := uint32(1070)
	info := MP3PageInfo{State: "ready", RequestedStart: 1024, StartIndex: 1024, RequestedFrame: &index, Frames: 64, ReplayPageBudget: 16}
	d, err := ProjectMP3Page(tag.ID, &p, &index, info, nil)
	if err != nil {
		t.Fatal(err)
	}
	if find(t, d, "selectableFrameIndices").Value != "" || len(strings.Split(find(t, d, "selectableFrameOffsets").Value, ",")) != 64 || find(t, d, "selectedFrameIndex").Value != "1070" {
		t.Fatal("long absolute CSV was truncated or indices lost")
	}
	for _, f := range d.Fields {
		if len(f.Value) > MaxPreviewBytes {
			t.Fatalf("field exceeded string contract %+v", f)
		}
		if f.Type == "encoded unsigned" || f.Type == "encoded boolean" {
			got := sideInfoViewBits(t, data, *f.Span)
			want := strconv.FormatUint(got, 10)
			if f.Type == "encoded boolean" {
				want = strconv.FormatBool(got != 0)
			}
			if f.Value != want {
				t.Fatalf("selected original source bits %+v want%s", f, want)
			}
		}
	}
	if find(t, d, "Side info Frame 1070").Span == nil || find(t, d, "Frame 1070").Span == nil {
		t.Fatal("selected source missing")
	}
	assertBounded(t, d)
}

func TestMP3PageProjectionRetainsNavigationAnd64DependencySources(t *testing.T) {
	data, tag := fixture(t, 8, append([]byte{0x2f}, bytes.Repeat(sideInfoViewFrame(t), 130)...))
	_, first, err := flv.ParseMP3AudioPage(t.Context(), bytes.NewReader(data), tag, nil)
	if err != nil {
		t.Fatal(err)
	}
	p, _, err := flv.ParseMP3AudioPage(t.Context(), bytes.NewReader(data), tag, first.Next)
	if err != nil {
		t.Fatal(err)
	}
	index := uint32(65)
	r := mp3context.Resolution{State: "unknown", Reason: "source_limit", TargetTagID: tag.ID, TargetFrameIndex: index, Limits: mp3context.Limits{MaxSources: 64}, Sources: []mp3context.Source{}}
	for i := uint32(0); i < 64; i++ {
		r.Sources = append(r.Sources, mp3context.Source{TagID: tag.ID, FrameIndex: i, Span: cursor.Span{ByteOffset: 25 + uint64(i)*417 + 36, BitLength: 8}})
	}
	info := MP3PageInfo{State: "ready", RequestedStart: 64, StartIndex: 64, RequestedFrame: &index, Frames: 64, ReplayPageBudget: 16}
	d, err := ProjectMP3Page(tag.ID, &p, &index, info, &r)
	if err != nil {
		t.Fatal(err)
	}
	if !d.Truncated || find(t, d, "selectedFrameIndex").Value != "65" || len(strings.Split(find(t, d, "selectableFrameOffsets").Value, ",")) != 64 || find(t, d, "sourceCount").Value != "64" {
		t.Fatal("projection lost request/source identity")
	}
	count := 0
	for _, f := range d.Fields {
		if f.Name == "sourceSpan" {
			count++
		}
	}
	if count != 64 {
		t.Fatalf("source table incomplete %d", count)
	}
	assertBounded(t, d)
}

func TestAnnotateMP3PreviewAccountsWireBudgetAndScopedFields(t *testing.T) {
	d := &Detail{Kind: "mp3Audio", Fields: []Field{
		{ID: "old0", Name: "Frame inspection", Type: "bounded sequence", Value: "64 complete frame boundaries; 64 headers; stop=frame_limit; audio main data is not decoded"},
		{ID: "old1", ParentID: "old0", Name: "checkedFrameCount", Value: "64"},
		{ID: "old2", ParentID: "old0", Name: "bytesRead", Value: "2312"},
		{ID: "dependency", Name: "MP3 dependency sources"},
		{ID: "depbytes", ParentID: "dependency", Name: "bytesRead", Value: "65535"},
	}, Diagnostics: []Diagnostic{}}
	for i := 0; i < MaxFields-5; i++ {
		d.Fields = append(d.Fields, Field{ID: "long" + strconv.Itoa(i), Name: strings.Repeat("&", 256), Type: strings.Repeat("&", 256), Value: strings.Repeat("&", 256)})
	}
	AnnotateMP3Preview(d, nil)
	if find(t, d, "pageBytesRead").Value != "2312" || find(t, d, "nextPageStart").Value != "64" || !d.Truncated {
		t.Fatal("wrong scope/budget")
	}
	if find(t, d, "Frame inspection").ID != "old0" || find(t, d, "checkedFrameCount").ParentID != "old0" {
		t.Fatal("legacy source IDs changed")
	}
	encoded, err := json.Marshal(d)
	if err != nil || len(encoded) > MaxJSONBytes || len(d.Fields) > MaxFields {
		t.Fatalf("budget %d %v", len(encoded), err)
	}
}
