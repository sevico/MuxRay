package avccontext

import (
	"context"
	"io"
	"math/bits"

	"github.com/sevico/muxray/internal/cursor"
	"github.com/sevico/muxray/internal/flv"
)

// CandidateIndex records sequence-header candidates and video framing barriers,
// never parsed widths. Legacy candidate lookup ignores the separate barriers.
// Its zero value is ready for sequential Observe calls by one scan worker.
// After that worker stops it is immutable and may be read concurrently. The
// caller owns the source identity and must discard the index with that source.
type CandidateIndex struct {
	words   []uint64
	events  []uint64 // configuration candidates and video framing barriers
	covered uint64   // all records strictly before this ID were observed
}

// Observe is called once after each container record is committed. A failed or
// canceled probe does not advance coverage. A final committed but unobserved
// record can still resolve its context because only its predecessors are needed.
// At most two bytes per eligible record are read; storage is at most 125,000 B
// per bitset for the scanner's one-million-record hard limit. The second bitset
// records framing barriers for header-only batch searches, without extra reads.
func (x *CandidateIndex) Observe(ctx context.Context, source io.ReaderAt, tag flv.TagRecord) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if x == nil || tag.ID != x.covered || tag.ID >= flv.HardMaxTags {
		return ErrInvalidInput
	}
	header, packet, present, err := ReadFramingPrefix(cursor.WithContext(ctx, source), tag)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if present && isConfiguration(header, packet) {
		if x.words == nil {
			x.words = make([]uint64, (flv.HardMaxTags+63)/64)
		}
		x.words[tag.ID/64] |= uint64(1) << (tag.ID % 64)
	}
	if tag.Type() == flv.TagTypeVideo && (!present || tag.DataSize < 5 || isConfiguration(header, packet) || header&15 != flv.CodecIDAVC || header>>4 == 5 || packet != uint8(flv.AVCPacketTypeNALU)) {
		if x.events == nil {
			x.events = make([]uint64, (flv.HardMaxTags+63)/64)
		}
		x.events[tag.ID/64] |= uint64(1) << (tag.ID % 64)
	}
	x.covered++
	return nil
}

// previous returns the greatest candidate ID strictly below before. Unobserved
// predecessors are an invalid query, not proof that a configuration is missing.
func (x *CandidateIndex) previous(ctx context.Context, before uint64) (uint64, bool, error) {
	if err := ctx.Err(); err != nil {
		return 0, false, err
	}
	if x == nil || before > x.covered {
		return 0, false, ErrInvalidInput
	}
	if before == 0 || x.words == nil {
		return 0, false, nil
	}
	id := before - 1
	word := id / 64
	value := x.words[word] & (^uint64(0) >> (63 - id%64))
	for {
		if err := ctx.Err(); err != nil {
			return 0, false, err
		}
		if value != 0 {
			return word*64 + uint64(63-bits.LeadingZeros64(value)), true, nil
		}
		if word == 0 {
			return 0, false, nil
		}
		word--
		value = x.words[word]
	}
}

func isConfiguration(header, packet uint8) bool {
	return header&0x0f == flv.CodecIDAVC && header>>4 != 5 && packet == uint8(flv.AVCPacketTypeSequenceHeader)
}

// PreviousFramingEvent returns the nearest configuration or framing barrier
// strictly before before. Unlike legacy configuration lookup, it cannot skip a
// malformed configuration, codec switch, filtered/truncated video or FLV EOS.
// The consumer must re-read that event, bound all reads and verify source identity.
func (x *CandidateIndex) PreviousFramingEvent(ctx context.Context, before uint64) (uint64, bool, error) {
	if x == nil {
		return 0, false, ErrInvalidInput
	}
	view := CandidateIndex{words: x.events, covered: x.covered}
	return view.previous(ctx, before)
}
