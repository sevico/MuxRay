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

func (s *fakeScan) TrendPage(start uint64, count uint32) (flv.TagTrendPage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.snapshot.Closed {
		return flv.TagTrendPage{}, session.ErrClosed
	}
	if !s.snapshot.State.Terminal() {
		return flv.TagTrendPage{}, session.ErrNotReady
	}
	return flv.TrendTags(s.result.Index(), start, count)
}

func TestTagTrendAPIPrecisionBoundsAndOwnership(t *testing.T) {
	scan := newFakeScan(t, false)
	scan.result = flv.ScanResult{Tags: []flv.TagRecord{
		{ID: 0, Offset: 1<<53 + 1, DTS: math.MaxUint32, RawTypeFlags: 8},
		{ID: 1, Offset: 1<<53 + 20, DTS: 0, RawTypeFlags: 8},
		{ID: 2, Offset: 1<<53 + 40, DTS: 0, RawTypeFlags: 8},
	}}
	app := newApp(nil, func(context.Context, string, flv.Options) (scanSession, error) { return scan, nil })
	cleanupApp(t, app)
	b := app.Bindings()
	id := b.OpenFile(OpenRequest{Path: "x"}).Session.SessionID
	waitOpened(t, app)
	q := PageRequest{id, "1", 512}
	response := b.GetTagTrends(q)
	if response.APIVersion != APIVersion || response.Error != nil || response.Page == nil || response.Page.SessionID != id || response.Page.Bounds != (PageBounds{"1", 512, "3", "3"}) || response.Page.ContextStart != "0" || response.Page.ContextScanned != 1 || len(response.Page.Points) != 2 {
		t.Fatalf("response: %+v", response)
	}
	first, second := response.Page.Points[0], response.Page.Points[1]
	if first.Offset != "9007199254741012" || first.DTS != "0" || *first.PreviousTagID != "0" || *first.PreviousDTS != "4294967295" || *first.DeltaMS != "-4294967295" || first.Context != "found" || *second.PreviousTagID != "1" || *second.DeltaMS != "0" {
		t.Fatalf("exact values: %+v %+v", first, second)
	}
	encoded, err := json.Marshal(response)
	if err != nil || !strings.Contains(string(encoded), `"deltaMs":"-4294967295"`) || !strings.Contains(string(encoded), `"offset":"9007199254741012"`) {
		t.Fatalf("JSON: %s %v", encoded, err)
	}
	var roundTrip TagTrendPageResponse
	if err := json.Unmarshal(encoded, &roundTrip); err != nil || !reflect.DeepEqual(response, roundTrip) {
		t.Fatalf("round trip: %v", err)
	}
	*first.PreviousTagID, *first.PreviousDTS, *first.DeltaMS = "99", "99", "99"
	response.Page.Points[0].TagID = "99"
	if got, _ := json.Marshal(b.GetTagTrends(q)); string(got) != string(encoded) {
		t.Fatal("response aliases owned data")
	}
	begin := b.GetTagTrends(PageRequest{id, "0", 1})
	if begin.Error != nil || begin.Page.ContextScanned != 0 || begin.Page.Points[0].Context != "start" {
		t.Fatalf("first window: %+v", begin)
	}
	encoded, _ = json.Marshal(begin)
	for _, field := range []string{"previousTagId", "previousDts", "deltaMs"} {
		if !strings.Contains(string(encoded), `"`+field+`":null`) {
			t.Fatalf("missing nullable field %s: %s", field, encoded)
		}
	}
	end := b.GetTagTrends(PageRequest{id, "3", 512})
	if end.Error != nil || end.Page == nil || end.Page.Points == nil || len(end.Page.Points) != 0 || end.Page.ContextStart != "3" || end.Page.ContextScanned != 0 || end.Page.Bounds.Next != "3" {
		t.Fatalf("empty EOF: %+v", end)
	}
}

func TestTagTrendMaximumJSONBudget(t *testing.T) {
	previousID := uint64(math.MaxUint64 - 1)
	previousDTS, delta := int64(math.MaxUint32), -int64(math.MaxUint32)
	point := mapTrendPoint(flv.TagTrendPoint{TagID: math.MaxUint64, Offset: math.MaxUint64, DTS: math.MaxUint32, TypeCode: math.MaxUint8, StreamID: math.MaxUint32,
		DataSize: math.MaxUint32, AvailableDataSize: math.MaxUint32, PreviousTagSizeState: "invalid", PreviousTagID: &previousID, PreviousDTS: &previousDTS, DeltaMS: &delta, Context: "found"})
	response := TagTrendPageResponse{APIVersion: APIVersion, Page: &TagTrendPage{SessionID: "scan-18446744073709551615",
		Bounds: PageBounds{"18446744073709551103", 512, "18446744073709551615", "18446744073709551615"}, ContextStart: "18446744073709547007", ContextScanned: 4096, Points: make([]TagTrendPoint, 512)}}
	for i := range response.Page.Points {
		response.Page.Points[i] = point
	}
	encoded, err := json.Marshal(response)
	if err != nil || len(encoded) > 256<<10 {
		t.Fatalf("512 maximum-width points exceed response budget: %d %v", len(encoded), err)
	}
	for i := range response.Page.Points {
		p := &response.Page.Points[i]
		p.Context, p.PreviousTagID, p.PreviousDTS, p.DeltaMS = "budget_exhausted", nil, nil, nil
	}
	encoded, err = json.Marshal(response)
	if err != nil || len(encoded) > 256<<10 {
		t.Fatalf("unknown-context page exceeds budget: %d %v", len(encoded), err)
	}
}

func TestTagTrendAPIArgumentsPartialAndLifecycle(t *testing.T) {
	scan := newFakeScan(t, true)
	app := newApp(nil, func(context.Context, string, flv.Options) (scanSession, error) { return scan, nil })
	cleanupApp(t, app)
	b := app.Bindings()
	id := b.OpenFile(OpenRequest{Path: "x"}).Session.SessionID
	waitOpened(t, app)
	q := PageRequest{id, "0", 512}
	checkFault(t, b.GetTagTrends(q).Error, CodeNotReady)
	scan.finish(context.Canceled)
	if page := b.GetTagTrends(q); page.Error != nil || page.Page == nil || len(page.Page.Points) != 2 {
		t.Fatalf("partial index: %+v", page)
	}
	for _, start := range []string{"", "01", "-1", "+1", "1.0", "1e3", " 1", "3", "18446744073709551615", "18446744073709551616", strings.Repeat("9", 10000)} {
		got := b.GetTagTrends(PageRequest{id, start, 512})
		checkFault(t, got.Error, CodeInvalidArgument)
		encoded, _ := json.Marshal(got)
		if got.Page != nil || len(encoded) > 256 {
			t.Fatal("bad request echoed input")
		}
	}
	for _, count := range []uint32{0, 513, math.MaxUint32} {
		checkFault(t, b.GetTagTrends(PageRequest{id, "0", count}).Error, CodeInvalidArgument)
	}
	checkFault(t, b.GetTagTrends(PageRequest{"unknown", "0", 512}).Error, CodeSessionNotFound)
	b.CloseFile(id)
	checkFault(t, b.GetTagTrends(q).Error, CodeSessionClosed)
}

func TestTagTrendAPIReadySummaryAndSourceChangeLatch(t *testing.T) {
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
	q := PageRequest{id, "0", 512}
	if page := b.GetTagTrends(q); page.Error != nil || page.Page == nil || len(page.Page.Points) != 2 {
		t.Fatalf("trend query: %+v", page)
	}
	if got, _ := json.Marshal(b.GetSummary(sq)); string(got) != string(want) {
		t.Fatal("trend query invalidated summary")
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
	checkFault(t, b.GetTagTrends(q).Error, CodeSourceChanged)
	if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	checkFault(t, b.GetTagTrends(q).Error, CodeSourceChanged)
}

func TestTagTrendAPIOpenFailure(t *testing.T) {
	app := New(nil)
	cleanupApp(t, app)
	id := app.Bindings().OpenFile(OpenRequest{Path: filepath.Join(t.TempDir(), "missing.flv")}).Session.SessionID
	finishReal(t, app, id)
	checkFault(t, app.Bindings().GetTagTrends(PageRequest{id, "0", 512}).Error, CodeOpenFailed)
}
