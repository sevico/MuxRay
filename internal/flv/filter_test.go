package flv

import (
	"math"
	"reflect"
	"testing"
)

func filterBound(value uint32) *uint32 { return &value }
func allFilter() TagFilter             { return TagFilter{Type: "all", Structure: "all"} }

type countedFilterIndex struct {
	TagIndex
	gets uint32
}

func (s *countedFilterIndex) Get(id uint64) (TagRecord, bool) { s.gets++; return s.TagIndex.Get(id) }

func TestFilterTagsSparseBudgetAndBlockCrossing(t *testing.T) {
	index := &chunkedTagIndex{limit: 10000}
	for id := uint64(0); id < 10000; id++ {
		r := TagRecord{ID: id, RawTypeFlags: 18, DTS: 0, flags: tagFlagPayloadComplete | tagFlagPreviousTagSizePresent | tagFlagPreviousTagSizeValid}
		if id == 1023 || id == 1024 || id == 4096 || id == 9999 {
			r.RawTypeFlags = 8
		}
		index.append(r)
	}
	filter := TagFilter{Type: "audio", Structure: "all"}
	counted := &countedFilterIndex{TagIndex: index}
	first, err := FilterTags(counted, 0, 2, filter)
	if err != nil || first.Next != 1025 || first.Scanned != 1025 || first.Exhausted || len(first.Records) != 2 || first.Records[0].ID != 1023 || first.Records[1].ID != 1024 || counted.gets != first.Scanned {
		t.Fatalf("count stop: %+v %v gets=%d", first, err, counted.gets)
	}
	first.Records[0].ID = 0
	if original, _ := index.Get(1023); original.ID != 1023 {
		t.Fatal("page aliases index")
	}
	second, err := FilterTags(counted, first.Next, 100, filter)
	if err != nil || second.Next != 5121 || second.Scanned != 4096 || second.Exhausted || len(second.Records) != 1 || second.Records[0].ID != 4096 {
		t.Fatalf("budget stop: %+v %v", second, err)
	}
	third, err := FilterTags(counted, second.Next, 100, filter)
	if err != nil || third.Next != 9217 || third.Exhausted || third.Records == nil || len(third.Records) != 0 {
		t.Fatalf("empty continuation: %+v %v", third, err)
	}
	last, err := FilterTags(counted, third.Next, 100, filter)
	if err != nil || last.Next != 10000 || last.Scanned != 783 || !last.Exhausted || len(last.Records) != 1 || last.Records[0].ID != 9999 {
		t.Fatalf("EOF stop: %+v %v", last, err)
	}
	if counted.gets != 10000 {
		t.Fatalf("rescan or missing record: %d", counted.gets)
	}
}

func TestFilterTagsTimestampAndStructureSemantics(t *testing.T) {
	good := tagFlagPayloadComplete | tagFlagPreviousTagSizePresent | tagFlagPreviousTagSizeValid
	index := sliceTagIndex{
		{ID: 0, RawTypeFlags: 8, DTS: 100, flags: good},
		{ID: 1, RawTypeFlags: 9, DTS: 200, flags: good},
		{ID: 2, RawTypeFlags: 8, DTS: 100, flags: good &^ tagFlagPreviousTagSizeValid},
		{ID: 3, RawTypeFlags: 18, DTS: 0, flags: tagFlagPayloadComplete},
		{ID: 4, RawTypeFlags: 31, DTS: math.MaxUint32},
		{ID: 5, RawTypeFlags: 0x28, DTS: 99, flags: good}, // FLV Filter bit is independent of query.
	}
	for _, tc := range []struct {
		name   string
		filter TagFilter
		want   []uint64
	}{
		{"inclusive repeat regression", TagFilter{Type: "all", Structure: "all", DTSMin: filterBound(100), DTSMax: filterBound(100)}, []uint64{0, 2}},
		{"range", TagFilter{Type: "all", Structure: "all", DTSMin: filterBound(99), DTSMax: filterBound(100)}, []uint64{0, 2, 5}},
		{"zero", TagFilter{Type: "all", Structure: "all", DTSMax: filterBound(0)}, []uint64{3}},
		{"maximum", TagFilter{Type: "all", Structure: "all", DTSMin: filterBound(math.MaxUint32)}, []uint64{4}},
		{"audio masked type", TagFilter{Type: "audio", Structure: "all"}, []uint64{0, 2, 5}},
		{"video", TagFilter{Type: "video", Structure: "all"}, []uint64{1}},
		{"script", TagFilter{Type: "script", Structure: "all"}, []uint64{3}},
		{"other", TagFilter{Type: "other", Structure: "all"}, []uint64{4}},
		{"incomplete", TagFilter{Type: "all", Structure: "payload_incomplete"}, []uint64{4}},
		{"missing", TagFilter{Type: "all", Structure: "previous_size_missing"}, []uint64{3, 4}},
		{"invalid excludes missing", TagFilter{Type: "all", Structure: "previous_size_invalid"}, []uint64{2}},
		{"combined", TagFilter{Type: "audio", Structure: "previous_size_invalid", DTSMin: filterBound(100), DTSMax: filterBound(100)}, []uint64{2}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			page, err := FilterTags(index, 0, 100, tc.filter)
			var ids []uint64
			for _, r := range page.Records {
				ids = append(ids, r.ID)
			}
			if err != nil || !reflect.DeepEqual(ids, tc.want) || !page.Exhausted {
				t.Fatalf("got %v want %v err=%v", ids, tc.want, err)
			}
		})
	}
}

func TestFilterTagsBounds(t *testing.T) {
	index := sliceTagIndex{{ID: 0}}
	for _, tc := range []struct {
		start  uint64
		count  uint32
		filter TagFilter
	}{
		{0, 0, allFilter()}, {0, 101, allFilter()}, {2, 1, allFilter()}, {math.MaxUint64, 1, allFilter()},
		{0, 1, TagFilter{}}, {0, 1, TagFilter{Type: "unknown", Structure: "all"}}, {0, 1, TagFilter{Type: "all", Structure: "unknown"}},
		{0, 1, TagFilter{Type: "all", Structure: "all", DTSMin: filterBound(1), DTSMax: filterBound(0)}},
	} {
		if _, err := FilterTags(index, tc.start, tc.count, tc.filter); err == nil {
			t.Fatalf("accepted invalid %+v", tc)
		}
	}
	for _, idx := range []TagIndex{index, sliceTagIndex{}} {
		page, err := FilterTags(idx, idx.Len(), 100, allFilter())
		if err != nil || !page.Exhausted || page.Scanned != 0 || page.Next != idx.Len() || page.Records == nil || len(page.Records) != 0 {
			t.Fatalf("EOF: %+v %v", page, err)
		}
	}
	index = make(sliceTagIndex, 101)
	for i := range index {
		index[i].ID = uint64(i)
	}
	page, err := FilterTags(index, 0, 100, allFilter())
	if err != nil || len(page.Records) != 100 || page.Next != 100 || page.Exhausted {
		t.Fatalf("count cap: %+v %v", page, err)
	}
}
