package flv

import "fmt"

const (
	MaxTrendPoints        uint32 = 512
	TrendContextScanLimit uint32 = 4096
)

// TagTrendPoint describes container metadata, not a decoded media frame. A
// predecessor has the same base tag type and StreamID, including configurations
// and filtered Tags; source order determines adjacency rather than sorted DTS.
type TagTrendPoint struct {
	TagID                uint64
	Offset               uint64
	DTS                  int64
	TypeCode             uint8
	StreamID             uint32
	DataSize             uint32
	AvailableDataSize    uint32
	PayloadComplete      bool
	PreviousTagSizeState string
	Filtered             bool
	PreviousTagID        *uint64
	PreviousDTS          *int64
	DeltaMS              *int64
	Context              string
}

// TagTrendPage is one continuous source-index window. Context records are only
// examined to establish preceding values and are not returned as visible points.
type TagTrendPage struct {
	Start          uint64
	Limit          uint32
	Total          uint64
	Next           uint64
	ContextStart   uint64
	ContextScanned uint32
	Points         []TagTrendPoint
}

type trendTrack struct {
	typeCode uint8
	streamID uint32
}

type trendPrevious struct {
	id  uint64
	dts int64
}

func trendKey(record TagRecord) trendTrack {
	return trendTrack{uint8(record.Type()), record.StreamID}
}

// TrendTags reads at most 512 window records and 4096 preceding index records.
// It does no source I/O, copies no whole index and keeps at most one predecessor
// for each distinct (type, StreamID) key in the visible window. Raw uint32 FLV
// DTS values are neither sorted nor unwrapped; signed deltas preserve resets.
func TrendTags(index TagIndex, start uint64, count uint32) (TagTrendPage, error) {
	if count == 0 || count > MaxTrendPoints {
		return TagTrendPage{}, fmt.Errorf("flv: trend count must be between 1 and %d", MaxTrendPoints)
	}
	total := index.Len()
	if start > total {
		return TagTrendPage{}, fmt.Errorf("flv: trend start exceeds indexed tag count")
	}
	length := min(uint64(count), total-start)
	page := TagTrendPage{Start: start, Limit: count, Total: total, Next: start + length, ContextStart: start, Points: make([]TagTrendPoint, 0, int(length))}
	if length == 0 {
		return page, nil
	}
	records := make([]TagRecord, 0, int(length))
	needed := make(map[trendTrack]struct{}, int(length))
	for id := start; id < page.Next; id++ {
		record, _ := index.Get(id)
		records = append(records, record)
		needed[trendKey(record)] = struct{}{}
	}
	previous := make(map[trendTrack]trendPrevious, len(needed))
	for page.ContextStart > 0 && page.ContextScanned < TrendContextScanLimit && len(previous) < len(needed) {
		page.ContextStart--
		page.ContextScanned++
		record, _ := index.Get(page.ContextStart)
		key := trendKey(record)
		if _, wanted := needed[key]; !wanted {
			continue
		}
		if _, found := previous[key]; !found {
			previous[key] = trendPrevious{record.ID, record.DTS}
		}
	}
	for _, record := range records {
		point := TagTrendPoint{TagID: record.ID, Offset: record.Offset, DTS: record.DTS, TypeCode: uint8(record.Type()), StreamID: record.StreamID,
			DataSize: record.DataSize, AvailableDataSize: record.AvailableDataSize, PayloadComplete: record.PayloadComplete(), Filtered: record.Filter(),
			PreviousTagSizeState: "valid", Context: "start"}
		if !record.PreviousTagSizePresent() {
			point.PreviousTagSizeState = "missing"
		} else if !record.PreviousTagSizeValid() {
			point.PreviousTagSizeState = "invalid"
		}
		key := trendKey(record)
		if predecessor, found := previous[key]; found {
			delta := record.DTS - predecessor.dts
			point.PreviousTagID, point.PreviousDTS, point.DeltaMS = &predecessor.id, &predecessor.dts, &delta
			point.Context = "found"
		} else if page.ContextStart != 0 {
			point.Context = "budget_exhausted"
		}
		page.Points = append(page.Points, point)
		previous[key] = trendPrevious{record.ID, record.DTS}
	}
	return page, nil
}
