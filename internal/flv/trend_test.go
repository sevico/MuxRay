package flv

import (
	"math"
	"reflect"
	"testing"
)

func trendRecord(id uint64, kind uint8, stream uint32, dts int64) TagRecord {
	return TagRecord{ID: id, Offset: 13 + id*15, RawTypeFlags: kind, StreamID: stream, DTS: dts,
		flags: tagFlagPayloadComplete | tagFlagPreviousTagSizePresent | tagFlagPreviousTagSizeValid}
}

func TestTrendTagsTrackAdjacencyAndRawTimestamps(t *testing.T) {
	index := sliceTagIndex{
		trendRecord(0, 8, 0, 0),
		trendRecord(1, 9, 0, 100),
		trendRecord(2, 8, 1, 40),
		trendRecord(3, 0x28, 0, math.MaxUint32), // Filter flag still belongs to audio StreamID 0.
		trendRecord(4, 8, 1, 40),
		trendRecord(5, 18, 0, 999),
		trendRecord(6, 8, 0, 0),
		trendRecord(7, 9, 0, 80),
		trendRecord(8, 31, 0, 0),
		trendRecord(9, 8, 0, 1000),
	}
	index[6].flags &^= tagFlagPayloadComplete | tagFlagPreviousTagSizePresent | tagFlagPreviousTagSizeValid
	index[6].DataSize, index[6].AvailableDataSize = 50, 10
	index[7].flags &^= tagFlagPreviousTagSizeValid
	page, err := TrendTags(index, 0, MaxTrendPoints)
	if err != nil || page.ContextStart != 0 || page.ContextScanned != 0 || page.Next != 10 || len(page.Points) != 10 {
		t.Fatalf("window: %+v %v", page, err)
	}
	for _, tc := range []struct {
		id       int
		previous uint64
		dts      int64
		delta    int64
	}{{3, 0, 0, math.MaxUint32}, {4, 2, 40, 0}, {6, 3, math.MaxUint32, -math.MaxUint32}, {7, 1, 100, -20}, {9, 6, 0, 1000}} {
		point := page.Points[tc.id]
		if point.Context != "found" || point.PreviousTagID == nil || *point.PreviousTagID != tc.previous || point.PreviousDTS == nil || *point.PreviousDTS != tc.dts || point.DeltaMS == nil || *point.DeltaMS != tc.delta {
			t.Fatalf("Tag %d predecessor: %+v", tc.id, point)
		}
	}
	for _, id := range []int{0, 1, 2, 5, 8} {
		if point := page.Points[id]; point.Context != "start" || point.PreviousTagID != nil || point.PreviousDTS != nil || point.DeltaMS != nil {
			t.Fatalf("first track Tag %d: %+v", id, point)
		}
	}
	if !page.Points[3].Filtered || page.Points[3].TypeCode != 8 || page.Points[6].PayloadComplete || page.Points[6].PreviousTagSizeState != "missing" || page.Points[6].DataSize != 50 || page.Points[6].AvailableDataSize != 10 || page.Points[7].PreviousTagSizeState != "invalid" || page.Points[8].PreviousTagSizeState != "valid" {
		t.Fatalf("container facts: %+v", page.Points)
	}
	*page.Points[3].PreviousTagID = 999
	*page.Points[3].PreviousDTS = 99
	*page.Points[3].DeltaMS = 123
	if *page.Points[4].PreviousTagID != 2 || *page.Points[9].PreviousDTS != 0 {
		t.Fatal("point predecessor pointers alias each other")
	}
	again, err := TrendTags(index, 0, MaxTrendPoints)
	if err != nil || *again.Points[3].PreviousTagID != 0 || *again.Points[3].DeltaMS != math.MaxUint32 || index[0].DTS != 0 {
		t.Fatal("detached page mutation reached the index")
	}
}

func TestTrendTagsCrossBlockPredecessorAndEarlyStop(t *testing.T) {
	index := &chunkedTagIndex{limit: 1030}
	for id := uint64(0); id < 1030; id++ {
		record := trendRecord(id, 18, 0, int64(id))
		if id == 1020 || id == 1024 || id == 1026 {
			record.RawTypeFlags = 8
		}
		if id == 1023 || id == 1025 {
			record.RawTypeFlags = 9
		}
		index.append(record)
	}
	counted := &countedFilterIndex{TagIndex: index}
	page, err := TrendTags(counted, 1024, 3)
	if err != nil || page.Start != 1024 || page.Limit != 3 || page.Total != 1030 || page.Next != 1027 || page.ContextStart != 1020 || page.ContextScanned != 4 || counted.gets != 7 {
		t.Fatalf("bounded early stop: %+v %v visits=%d", page, err, counted.gets)
	}
	for i, previous := range []uint64{1020, 1023, 1024} {
		point := page.Points[i]
		if point.Context != "found" || point.TagID != uint64(1024+i) || point.PreviousTagID == nil || *point.PreviousTagID != previous || *point.PreviousDTS != int64(previous) || *point.DeltaMS != int64(point.TagID-previous) {
			t.Fatalf("closest track predecessor: %+v", point)
		}
	}
}

func TestTrendTagsHardVisitBudgetAndMissingContext(t *testing.T) {
	index := make(sliceTagIndex, 5512)
	for i := range index {
		index[i] = trendRecord(uint64(i), 18, 0, int64(i))
	}
	index[903] = trendRecord(903, 8, 1, 1) // One record outside the allowed context.
	for i := 5000; i < len(index); i++ {
		index[i] = trendRecord(uint64(i), 8, uint32((i-5000)/2+1), int64(i))
	}
	counted := &countedFilterIndex{TagIndex: index}
	page, err := TrendTags(counted, 5000, MaxTrendPoints)
	if err != nil || len(page.Points) != 512 || page.ContextScanned != 4096 || page.ContextStart != 904 || counted.gets != MaxTrendPoints+TrendContextScanLimit {
		t.Fatalf("hard visit bound: points=%d context=%d/%d visits=%d err=%v", len(page.Points), page.ContextStart, page.ContextScanned, counted.gets, err)
	}
	for i, point := range page.Points {
		if i%2 == 0 {
			if point.Context != "budget_exhausted" || point.PreviousTagID != nil || point.PreviousDTS != nil || point.DeltaMS != nil {
				t.Fatalf("unknown context: %+v", point)
			}
		} else if point.Context != "found" || *point.PreviousTagID != point.TagID-1 || *point.DeltaMS != 1 {
			t.Fatalf("window recovery: %+v", point)
		}
	}
	for _, start := range []uint64{4096, 4097} {
		index[start] = trendRecord(start, 9, 7, 5)
		p, err := TrendTags(index, start, 1)
		want := "start"
		if start > 4096 {
			want = "budget_exhausted"
		}
		// Restore the previous iteration so it does not create a predecessor.
		index[start] = trendRecord(start, 18, 0, int64(start))
		if err != nil || p.ContextScanned != 4096 || p.ContextStart != start-4096 || p.Points[0].Context != want {
			t.Fatalf("exact lookback boundary: %+v %v", p, err)
		}
	}
	index[0] = trendRecord(0, 9, 7, 2)
	index[4096] = trendRecord(4096, 9, 7, 5)
	p, err := TrendTags(index, 4096, 1)
	if err != nil || p.ContextScanned != 4096 || p.Points[0].Context != "found" || *p.Points[0].PreviousTagID != 0 || *p.Points[0].DeltaMS != 3 {
		t.Fatalf("last budget visit omitted: %+v %v", p, err)
	}
}

func TestTrendTagsBoundsAndEmptyWindow(t *testing.T) {
	index := sliceTagIndex{trendRecord(0, 8, 0, 0)}
	for _, tc := range []struct {
		start uint64
		count uint32
	}{{0, 0}, {0, 513}, {2, 1}, {math.MaxUint64, 1}} {
		if _, err := TrendTags(index, tc.start, tc.count); err == nil {
			t.Fatalf("accepted invalid bounds: %+v", tc)
		}
	}
	for _, source := range []TagIndex{index, sliceTagIndex{}} {
		counted := &countedFilterIndex{TagIndex: source}
		page, err := TrendTags(counted, source.Len(), MaxTrendPoints)
		if err != nil || counted.gets != 0 || page.ContextScanned != 0 || page.ContextStart != source.Len() || page.Start != page.Next || page.Next != page.Total || page.Points == nil || len(page.Points) != 0 {
			t.Fatalf("EOF: %+v %v visits=%d", page, err, counted.gets)
		}
	}
	page, err := TrendTags(index, 0, 1)
	if err != nil || !reflect.DeepEqual(page.Points, []TagTrendPoint{{TagID: 0, Offset: 13, TypeCode: 8, PayloadComplete: true, PreviousTagSizeState: "valid", Context: "start"}}) {
		t.Fatalf("one record: %+v %v", page, err)
	}
}
