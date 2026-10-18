// Package timesearch finds bounded timestamp observations in an immutable FLV
// index. It never reads source bytes, decodes media, or classifies damage.
package timesearch

import (
	"context"
	"errors"

	"github.com/sevico/muxray/internal/flv"
)

const (
	MaxScanned = 4096
	MaxRows    = 64
	MaxTracks  = 512
)

var ErrInvalidInput = errors.New("timesearch: invalid input")

type track struct {
	typ    uint8
	stream uint32
}
type previous struct {
	id  uint64
	dts int64
}

// State is a server-owned continuation. Clone before handing it to a worker;
// the search itself also clones so cancellation never changes its input.
type State struct {
	Next     uint64
	previous map[track]previous
	evicted  bool
}

func (s State) Clone() State {
	copy := s
	copy.previous = make(map[track]previous, len(s.previous))
	for key, value := range s.previous {
		copy.previous[key] = value
	}
	return copy
}

type Row struct {
	Kind  string
	Point flv.TagTrendPoint
}
type Page struct {
	Start, Next, Total           uint64
	Scanned, UnknownPredecessors uint32
	Exhausted, ScanComplete      bool
	Rows                         []Row
}

func ValidFilter(filter string) bool {
	return filter == "all" || filter == "regressions" || filter == "duplicates" || filter == "gaps"
}

// Search consumes a contiguous prefix of at most 4096 records or 64 matching
// observations. Next includes the last returned observation, so continuation
// neither repeats it nor skips records. Raw DTS is never unwrapped or sorted.
func Search(ctx context.Context, index flv.TagIndex, seed State, filter string, gapThreshold uint32) (Page, State, error) {
	if ctx == nil || index == nil || !ValidFilter(filter) || gapThreshold == 0 || seed.Next > index.Len() {
		return Page{}, State{}, ErrInvalidInput
	}
	state := seed.Clone()
	page := Page{Start: seed.Next, Next: seed.Next, Total: index.Len(), Rows: []Row{}}
	for page.Next < page.Total && page.Scanned < MaxScanned && len(page.Rows) < MaxRows {
		if err := ctx.Err(); err != nil {
			return Page{}, State{}, err
		}
		record, ok := index.Get(page.Next)
		if !ok || record.ID != page.Next || record.DTS < 0 || record.DTS > int64(^uint32(0)) {
			return Page{}, State{}, ErrInvalidInput
		}
		key := track{uint8(record.Type()), record.StreamID}
		prior, found := state.previous[key]
		if !found && len(state.previous) == MaxTracks {
			// Unique source IDs make the least recently used key deterministic.
			var oldest track
			oldestID := ^uint64(0)
			for candidate, value := range state.previous {
				if value.id < oldestID {
					oldest, oldestID = candidate, value.id
				}
			}
			delete(state.previous, oldest)
			state.evicted = true
		}
		point := flv.TagTrendPoint{TagID: record.ID, Offset: record.Offset, DTS: record.DTS, TypeCode: uint8(record.Type()), StreamID: record.StreamID,
			DataSize: record.DataSize, AvailableDataSize: record.AvailableDataSize, PayloadComplete: record.PayloadComplete(), Filtered: record.Filter(),
			PreviousTagSizeState: "valid", Context: "start"}
		if !record.PreviousTagSizePresent() {
			point.PreviousTagSizeState = "missing"
		} else if !record.PreviousTagSizeValid() {
			point.PreviousTagSizeState = "invalid"
		}
		kind := ""
		if found {
			delta := record.DTS - prior.dts
			point.Context, point.PreviousTagID, point.PreviousDTS, point.DeltaMS = "found", &prior.id, &prior.dts, &delta
			switch {
			case delta < 0:
				kind = "regression"
			case delta == 0:
				kind = "duplicate"
			case delta > int64(gapThreshold):
				kind = "gap"
			}
		} else if state.evicted {
			point.Context, kind = "budget_exhausted", "unknown"
			page.UnknownPredecessors++
		}
		if kind != "" && (kind == "unknown" || filter == "all" || filter == kind+"s") {
			page.Rows = append(page.Rows, Row{kind, point})
		}
		state.previous[key] = previous{record.ID, record.DTS}
		page.Next++
		page.Scanned++
	}
	if err := ctx.Err(); err != nil {
		return Page{}, State{}, err
	}
	state.Next = page.Next
	page.Exhausted = page.Next == page.Total
	return page, state, nil
}
