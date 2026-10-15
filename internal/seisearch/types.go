// Package seisearch searches bounded SEI message evidence in source order.
// It retains compact parameter context, never complete NAL or RBSP payloads.
package seisearch

import (
	"errors"

	"github.com/sevico/muxray/internal/avc"
	"github.com/sevico/muxray/internal/cursor"
	"github.com/sevico/muxray/internal/flv"
)

const (
	MaxScanTags uint32 = 256
	MaxResults         = 64
	MaxBytes    uint64 = 1 << 20
	// One row per parsed message plus one aggregate coverage record per Tag.
	MaxPending = int(avc.MaxSEIMessagesPerParse) + 1
)

var ErrInvalidInput = errors.New("seisearch: invalid input")

type Lookup func(uint64) (flv.TagRecord, error)

// Row is one message, or a Tag-level incomplete-coverage record whose message
// indices are nil. SourceSpan is a physical source envelope (including any
// intervening emulation-prevention bytes), not a concatenated RBSP bit span.
// Parameter and selection spans can refer to earlier Tags in the same source.
type Row struct {
	TagID, Offset                      uint64
	DTS                                int64
	NALIndex, MessageIndex             *uint32
	PayloadType                        *uint64
	Kind, Status, ContextState, Reason string
	Complete                           bool
	SourceSpan, SPSSpan, EvidenceSpan  *cursor.Span
	SPSID                              *uint32
	ContextSource                      string
}

// State is trusted server-side continuation state. Its zero value starts at
// Tag zero. Bind it to one source and filter; never accept it from a client.
// The fixed AVC slots and at most MaxPending compact rows bound retained data.
type State struct {
	next    uint64
	width   uint8
	sets    *avc.ParameterSetContext
	pending []Row
	filter  string
}

// Position is the earliest Tag with work or undelivered rows. Multiple pages
// can have the same position when one Tag supplies more than MaxResults rows.
func (s State) Position() uint64 {
	if len(s.pending) != 0 {
		return s.pending[0].TagID
	}
	return s.next
}

// Clone isolates candidate work from the committed state, including row fields.
func (s State) Clone() State {
	s.sets = s.sets.CompactClone()
	if len(s.pending) != 0 {
		pending := make([]Row, len(s.pending))
		for i, row := range s.pending {
			pending[i] = cloneRow(row)
		}
		s.pending = pending
	}
	return s
}

type Page struct {
	Start, Next, Total      uint64
	Scanned                 uint32
	BytesRead               uint64
	Exhausted, ScanComplete bool
	Filter                  string
	Rows                    []Row
	Pending                 uint32
	// State is a candidate. Commit only after source identity is revalidated.
	State State
}

func ValidFilter(filter string) bool {
	return filter == "all" || filter == "bufferingPeriod" || filter == "pictureTiming" || filter == "unknown"
}

func copyPtr[T any](p *T) *T {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

func cloneRow(row Row) Row {
	row.NALIndex, row.MessageIndex = copyPtr(row.NALIndex), copyPtr(row.MessageIndex)
	row.PayloadType, row.SPSID = copyPtr(row.PayloadType), copyPtr(row.SPSID)
	row.SourceSpan, row.SPSSpan, row.EvidenceSpan = copyPtr(row.SourceSpan), copyPtr(row.SPSSpan), copyPtr(row.EvidenceSpan)
	return row
}
