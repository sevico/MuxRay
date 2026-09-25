package desktop

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/sevico/muxray/internal/flv"
	"github.com/sevico/muxray/internal/session"
)

func (s *fakeScan) FilteredPage(start uint64, count uint32, filter flv.TagFilter) (flv.FilteredTagPage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.snapshot.Closed {
		return flv.FilteredTagPage{}, session.ErrClosed
	}
	if !s.snapshot.State.Terminal() {
		return flv.FilteredTagPage{}, session.ErrNotReady
	}
	return flv.FilterTags(s.result.Index(), start, count, filter)
}

func allTagFilter() TagFilter     { return TagFilter{Type: "all", Structure: "all"} }
func filterText(s string) *string { return &s }

func TestFilteredTagAPIPagingAndJSON(t *testing.T) {
	scan := newFakeScan(t, false)
	scan.result = flv.ScanResult{Tags: make([]flv.TagRecord, 8200)}
	for i := range scan.result.Tags {
		scan.result.Tags[i] = flv.TagRecord{ID: uint64(i), Offset: (1 << 53) + 1 + uint64(i)*15, RawTypeFlags: 18}
	}
	for _, id := range []int{4096, 8192} {
		scan.result.Tags[id].RawTypeFlags = 8
		scan.result.Tags[id].DTS = math.MaxUint32
	}
	app := newApp(nil, func(context.Context, string, flv.Options) (scanSession, error) { return scan, nil })
	cleanupApp(t, app)
	b := app.Bindings()
	id := b.OpenFile(OpenRequest{Path: "x"}).Session.SessionID
	waitOpened(t, app)
	q := FilteredTagRequest{SessionID: id, Start: "0", Count: 100, Filter: TagFilter{Type: "audio", Structure: "all", DTSMin: filterText("4294967295"), DTSMax: filterText("4294967295")}}
	first := b.GetFilteredTags(q)
	if first.Error != nil || first.Page == nil || first.Page.Tags == nil || len(first.Page.Tags) != 0 || first.Page.Start != "0" || first.Page.Next != "4096" || first.Page.Scanned != 4096 || first.Page.Exhausted || first.Page.IndexedTotal != "8200" || !reflect.DeepEqual(first.Page.Filter, q.Filter) {
		t.Fatalf("zero-hit continuation: %+v", first)
	}
	q.Start = first.Page.Next
	second := b.GetFilteredTags(q)
	if second.Error != nil || second.Page == nil || len(second.Page.Tags) != 1 || second.Page.Tags[0].ID != "4096" || second.Page.Next != "8192" || second.Page.Exhausted {
		t.Fatalf("sparse middle: %+v", second)
	}
	q.Start = second.Page.Next
	last := b.GetFilteredTags(q)
	if last.Error != nil || last.Page == nil || len(last.Page.Tags) != 1 || last.Page.Tags[0].ID != "8192" || last.Page.Next != "8200" || last.Page.Scanned != 8 || !last.Page.Exhausted {
		t.Fatalf("sparse last: %+v", last)
	}
	encoded, err := json.Marshal(last)
	if err != nil || !strings.Contains(string(encoded), `"dts":"4294967295"`) || strings.Contains(string(encoded), `"total"`) {
		t.Fatalf("JSON: %s %v", encoded, err)
	}
	var roundTrip FilteredTagPageResponse
	if err := json.Unmarshal(encoded, &roundTrip); err != nil || !reflect.DeepEqual(roundTrip, last) {
		t.Fatalf("round trip: %v", err)
	}
	*last.Page.Filter.DTSMin = "0"
	last.Page.Tags[0].ID = "0"
	if again := b.GetFilteredTags(q); *again.Page.Filter.DTSMin != "4294967295" || again.Page.Tags[0].ID != "8192" {
		t.Fatal("response aliased request or index")
	}
	q.Start = "8200"
	if end := b.GetFilteredTags(q); end.Error != nil || !end.Page.Exhausted || end.Page.Scanned != 0 || end.Page.Tags == nil || len(end.Page.Tags) != 0 {
		t.Fatalf("EOF: %+v", end)
	}

	// Maximal decimal and scalar widths still fit well within a 256 KiB IPC
	// response, including the complete 100-tag DTO and nullable filter fields.
	maximum := mapTag(flv.TagRecord{ID: math.MaxUint64, Offset: math.MaxUint64 - 100, DTS: math.MaxInt64, RawTypeFlags: 255, DataSize: math.MaxUint32, AvailableDataSize: math.MaxUint32, StreamID: math.MaxUint32, PreviousTagSize: math.MaxUint32})
	maximum.PreviousTagSize.Span = &Span{ByteOffset: "18446744073709551615", BitOffset: 7, BitLength: math.MaxUint32}
	last.Page.Tags = make([]Tag, 100)
	for i := range last.Page.Tags {
		last.Page.Tags[i] = maximum
	}
	last.Page.SessionID = "scan-18446744073709551615"
	encoded, err = json.Marshal(last)
	if err != nil || len(encoded) > 256<<10 {
		t.Fatalf("transport exceeds bound: %d %v", len(encoded), err)
	}
}

func TestFilteredTagAPIArgumentAndLifecycleErrors(t *testing.T) {
	scan := newFakeScan(t, true)
	app := newApp(nil, func(context.Context, string, flv.Options) (scanSession, error) { return scan, nil })
	cleanupApp(t, app)
	b := app.Bindings()
	id := b.OpenFile(OpenRequest{Path: "x"}).Session.SessionID
	waitOpened(t, app)
	q := FilteredTagRequest{id, "0", 100, allTagFilter()}
	checkFault(t, b.GetFilteredTags(q).Error, CodeNotReady)
	scan.finish(context.Canceled)
	if page := b.GetFilteredTags(q); page.Error != nil || page.Page == nil {
		t.Fatalf("partial index: %+v", page)
	}
	for _, bad := range []string{"", "01", "-1", "+1", "1.0", "1e3", " 1", "4294967296", "18446744073709551615", strings.Repeat("9", 10000)} {
		for _, minimum := range []bool{true, false} {
			invalid := q
			if minimum {
				invalid.Filter.DTSMin = &bad
			} else {
				invalid.Filter.DTSMax = &bad
			}
			got := b.GetFilteredTags(invalid)
			checkFault(t, got.Error, CodeInvalidArgument)
			encoded, _ := json.Marshal(got)
			if got.Page != nil || len(encoded) > 256 {
				t.Fatal("invalid filter echoed unbounded input")
			}
		}
	}
	for _, invalid := range []FilteredTagRequest{
		{id, "01", 1, allTagFilter()}, {id, "18446744073709551615", 1, allTagFilter()}, {id, "0", 0, allTagFilter()}, {id, "0", 101, allTagFilter()},
		{id, "0", 1, TagFilter{Type: "ALL", Structure: "all"}}, {id, "0", 1, TagFilter{Type: "all", Structure: "invalid"}},
		{id, "0", 1, TagFilter{Type: "all", Structure: "all", DTSMin: filterText("2"), DTSMax: filterText("1")}},
	} {
		checkFault(t, b.GetFilteredTags(invalid).Error, CodeInvalidArgument)
	}
	q.SessionID = "missing"
	checkFault(t, b.GetFilteredTags(q).Error, CodeSessionNotFound)
	q.SessionID = id
	b.CloseFile(id)
	checkFault(t, b.GetFilteredTags(q).Error, CodeSessionClosed)
}

func TestFilteredTagAPIReadySummaryAndSourceChangeLatch(t *testing.T) {
	path := detailFixture(t)
	app := New(nil)
	cleanupApp(t, app)
	b := app.Bindings()
	id := b.OpenFile(OpenRequest{Path: path}).Session.SessionID
	finishReal(t, app, id)
	sq := SummaryQuery{id, "1"}
	b.StartSummary(sq)
	ready := finishSummary(t, b, id, "1")
	if ready.Error != nil {
		t.Fatal(ready.Error)
	}
	want, _ := json.Marshal(ready)
	q := FilteredTagRequest{id, "0", 100, allTagFilter()}
	if page := b.GetFilteredTags(q); page.Error != nil || len(page.Page.Tags) != 2 {
		t.Fatalf("page: %+v", page)
	}
	if got, _ := json.Marshal(b.GetSummary(sq)); string(got) != string(want) {
		t.Fatal("metadata query invalidated Summary")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, info.ModTime(), info.ModTime().Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	b.StartHex(HexRequest{SessionID: id, RequestID: "2", Offset: "0", Count: 32})
	checkFault(t, finishHex(t, b, id, "2").Error, CodeSourceChanged)
	checkFault(t, b.GetFilteredTags(q).Error, CodeSourceChanged)
	if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	checkFault(t, b.GetFilteredTags(q).Error, CodeSourceChanged)
}

func TestFilteredTagAPIOpenFailure(t *testing.T) {
	app := New(nil)
	cleanupApp(t, app)
	id := app.Bindings().OpenFile(OpenRequest{Path: filepath.Join(t.TempDir(), "missing.flv")}).Session.SessionID
	finishReal(t, app, id)
	checkFault(t, app.Bindings().GetFilteredTags(FilteredTagRequest{id, "0", 1, allTagFilter()}).Error, CodeOpenFailed)
}
