package timesearch

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/sevico/muxray/internal/flv"
)

type observedIndex struct {
	flv.TagIndex
	reads  int
	cancel context.CancelFunc
}

func (i *observedIndex) Get(id uint64) (flv.TagRecord, bool) {
	i.reads++
	if i.cancel != nil && i.reads == 10 {
		i.cancel()
	}
	return i.TagIndex.Get(id)
}

func records(count int) []flv.TagRecord {
	tags := make([]flv.TagRecord, count)
	for i := range tags {
		tags[i] = flv.TagRecord{ID: uint64(i), Offset: uint64(13 + 15*i), DTS: int64(i), RawTypeFlags: 9}
	}
	return tags
}
func search(t *testing.T, tags []flv.TagRecord, seed State, filter string, threshold uint32) (Page, State) {
	t.Helper()
	p, next, err := Search(t.Context(), (flv.ScanResult{Tags: tags}).Index(), seed, filter, threshold)
	if err != nil {
		t.Fatal(err)
	}
	return p, next
}
func TestTimeSearchCrossBatchAndSparsePredecessor(t *testing.T) {
	tags := records(10000)
	tags[4096].DTS = 4094
	tags[8192].DTS = 8190
	var state State
	var found []uint64
	for batch := 0; batch < 3; batch++ {
		page, next := search(t, tags, state, "regressions", 1000)
		if page.Start != uint64(batch*4096) || page.Scanned > MaxScanned || page.Exhausted != (batch == 2) {
			t.Fatalf("batch %+v", page)
		}
		for _, row := range page.Rows {
			if row.Kind != "regression" || row.Point.PreviousTagID == nil || *row.Point.PreviousTagID != row.Point.TagID-1 || *row.Point.DeltaMS != -1 {
				t.Fatalf("row %+v", row)
			}
			found = append(found, row.Point.TagID)
		}
		state = next
	}
	if !reflect.DeepEqual(found, []uint64{4096, 8192}) {
		t.Fatal(found)
	}
	tags = records(9001)
	for i := 1; i < len(tags)-1; i++ {
		tags[i].StreamID = 1
	}
	tags[0].DTS, tags[9000].DTS = 100, 50
	state = State{}
	for i := 0; i < 3; i++ {
		page, next := search(t, tags, state, "regressions", 1000)
		if i == 2 && (len(page.Rows) != 1 || *page.Rows[0].Point.PreviousTagID != 0 || *page.Rows[0].Point.DeltaMS != -50) {
			t.Fatalf("sparse %+v", page)
		}
		state = next
	}
}
func TestTimeSearchRowBudgetDoesNotSkipOrRepeat(t *testing.T) {
	tags := records(200)
	for i := range tags {
		tags[i].DTS = 10
	}
	var state State
	var ids []uint64
	for {
		page, next := search(t, tags, state, "duplicates", 1)
		if len(page.Rows) > MaxRows || page.Next-page.Start != uint64(page.Scanned) {
			t.Fatalf("bounds %+v", page)
		}
		if len(ids) == 0 && (page.Next != 65 || page.Scanned != 65) {
			t.Fatalf("first %+v", page)
		}
		for _, row := range page.Rows {
			ids = append(ids, row.Point.TagID)
		}
		state = next
		if page.Exhausted {
			break
		}
	}
	if len(ids) != 199 {
		t.Fatal(len(ids))
	}
	for i, id := range ids {
		if id != uint64(i+1) {
			t.Fatalf("at %d = %d", i, id)
		}
	}
}
func TestTimeSearchThresholdWrapTypeAndFilter(t *testing.T) {
	tags := records(7)
	values := []int64{0, 1000, 2001, 2001, 4294967295, 0, 0}
	for i := range tags {
		tags[i].DTS = values[i]
	}
	tags[6].RawTypeFlags = 8    // audio first record must not compare to video
	tags[3].RawTypeFlags = 0x29 // filtered video remains on the same track
	for _, tc := range []struct {
		filter string
		want   []string
	}{
		{"all", []string{"gap", "duplicate", "gap", "regression"}},
		{"regressions", []string{"regression"}}, {"duplicates", []string{"duplicate"}}, {"gaps", []string{"gap", "gap"}},
	} {
		page, _ := search(t, tags, State{}, tc.filter, 1000)
		var got []string
		for _, row := range page.Rows {
			got = append(got, row.Kind)
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("%s %v", tc.filter, got)
		}
		if tc.filter == "regressions" && *page.Rows[0].Point.DeltaMS != -4294967295 {
			t.Fatal("DTS was unwrapped")
		}
		if tc.filter == "duplicates" && !page.Rows[0].Point.Filtered {
			t.Fatal("filtered metadata lost")
		}
	}
	page, _ := search(t, tags, State{}, "gaps", ^uint32(0))
	if len(page.Rows) != 0 {
		t.Fatalf("maximum threshold %+v", page.Rows)
	}
}
func TestTimeSearchTrackBudgetUnknownAndDeterministicLRU(t *testing.T) {
	tags := records(515)
	for i := 0; i < 512; i++ {
		tags[i].StreamID = uint32(i)
	}
	tags[512].StreamID = 0 // refresh key 0; key 1 is now oldest
	tags[513].StreamID = 512
	tags[514].StreamID = 1
	page, state := search(t, tags, State{}, "duplicates", 1000)
	if len(page.Rows) != 2 || page.UnknownPredecessors != 2 || len(state.previous) != 512 || !state.evicted {
		t.Fatalf("budget %+v tracks=%d", page, len(state.previous))
	}
	for i, row := range page.Rows {
		if row.Kind != "unknown" || row.Point.TagID != uint64(513+i) || row.Point.Context != "budget_exhausted" || row.Point.DeltaMS != nil || row.Point.PreviousTagID != nil || row.Point.PreviousDTS != nil {
			t.Fatalf("unknown %+v", row)
		}
	}
	if _, found := state.previous[track{9, 0}]; !found {
		t.Fatal("recently used key evicted")
	}
	if _, found := state.previous[track{9, 2}]; found {
		t.Fatal("wrong eviction order")
	}
}
func TestTimeSearchCloneCancellationAndValidation(t *testing.T) {
	tags := records(5000)
	_, seed := search(t, tags, State{}, "all", 1000)
	copy := seed.Clone()
	copy.previous[track{9, 0}] = previous{0, 0}
	if seed.previous[track{9, 0}].id != 4095 {
		t.Fatal("shallow state clone")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, _, err := Search(ctx, (flv.ScanResult{Tags: tags}).Index(), seed, "all", 1000)
	if !errors.Is(err, context.Canceled) || seed.Next != 4096 || seed.previous[track{9, 0}].id != 4095 {
		t.Fatal("cancellation mutated input", err)
	}
	page, _ := search(t, tags, seed, "all", 1000)
	if page.Start != 4096 || !page.Exhausted || seed.Next != 4096 {
		t.Fatal("successful search mutated input")
	}
	for _, tc := range []struct {
		seed      State
		filter    string
		threshold uint32
	}{{State{}, "bad", 1}, {State{}, "all", 0}, {State{Next: 5001}, "all", 1}} {
		_, _, err = Search(t.Context(), (flv.ScanResult{Tags: tags}).Index(), tc.seed, tc.filter, tc.threshold)
		if !errors.Is(err, ErrInvalidInput) {
			t.Fatal(err)
		}
	}
	empty, _ := search(t, nil, State{}, "all", 1)
	if !empty.Exhausted || empty.Rows == nil || empty.Scanned != 0 {
		t.Fatalf("empty %+v", empty)
	}
}

func TestTimeSearchBoundedIndexAccessAndMidBatchCancel(t *testing.T) {
	tags := records(10000)
	index := &observedIndex{TagIndex: (flv.ScanResult{Tags: tags}).Index()}
	page, seed, err := Search(t.Context(), index, State{}, "all", 1000)
	if err != nil || index.reads != MaxScanned || page.Scanned != MaxScanned {
		t.Fatalf("index overread %d %+v %v", index.reads, page, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	index.reads, index.cancel = 0, cancel
	page, _, err = Search(ctx, index, seed, "all", 1000)
	if !errors.Is(err, context.Canceled) || index.reads != 10 || page.Rows != nil || seed.Next != 4096 || seed.previous[track{9, 0}].id != 4095 {
		t.Fatalf("canceled batch leaked %+v %v reads=%d", page, err, index.reads)
	}
	for i := range tags {
		tags[i].DTS = 0
	}
	index = &observedIndex{TagIndex: (flv.ScanResult{Tags: tags}).Index()}
	page, _, err = Search(t.Context(), index, State{}, "all", 1000)
	if err != nil || index.reads != 65 || len(page.Rows) != MaxRows {
		t.Fatalf("row budget overread %d %+v %v", index.reads, page, err)
	}
}
