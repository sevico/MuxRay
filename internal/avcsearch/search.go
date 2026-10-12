// Package avcsearch compares FLV declarations with bounded NAL-header evidence.
// It never reads NAL bodies or reconstructs pictures/parameter sets.
package avcsearch

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"math"

	"github.com/sevico/muxray/internal/avc"
	"github.com/sevico/muxray/internal/avccontext"
	"github.com/sevico/muxray/internal/cursor"
	"github.com/sevico/muxray/internal/flv"
)

const (
	MaxScanTags   uint32 = 256
	MaxResults           = 64
	MaxBytes      uint64 = 1 << 20
	MaxNALHeaders        = 256
)

var ErrInvalidInput = errors.New("avcsearch: invalid input")
var errBudget = errors.New("avcsearch: byte limit")
var errFraming = errors.New("avcsearch: incomplete framing")

type Lookup func(uint64) (flv.TagRecord, error)
type Row struct {
	TagID                                    uint64
	DTS                                      int64
	Offset                                   uint64
	DeclaredFrameType                        *uint8
	ObservedIDR, DeclarationRelation, Reason string
	Span                                     *cursor.Span
}
type Page struct {
	Start, Next, Total      uint64
	Scanned                 uint32
	BytesRead               uint64
	Exhausted, ScanComplete bool
	Filter                  string
	Rows                    []Row
}

func ValidFilter(filter string) bool {
	return filter == "all" || filter == "mismatches" || filter == "unknown"
}

// Search visits source-index positions [start,next). A result limit stops after
// consuming its last row, so continuation neither repeats nor omits records.
// A terminal partial container index remains explicitly ScanComplete=false.
// Cancellation/source errors return no page, never a falsely completed cursor.
func Search(ctx context.Context, source io.ReaderAt, lookup Lookup, index *avccontext.CandidateIndex, start, total uint64, scanComplete bool, filter string) (*Page, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if source == nil || lookup == nil || index == nil || start > total || total > flv.HardMaxTags || !ValidFilter(filter) {
		return nil, ErrInvalidInput
	}
	r := reader{ctx: ctx, source: source}
	p := &Page{Start: start, Next: start, Total: total, ScanComplete: scanComplete, Filter: filter, Rows: make([]Row, 0, MaxResults)}
	width := uint8(0)
	if start < total {
		id, found, err := index.PreviousFramingEvent(ctx, start)
		if err != nil {
			return nil, err
		}
		if found {
			tag, err := lookup(id)
			if err != nil {
				return nil, err
			}
			if tag.ID != id {
				return nil, ErrInvalidInput
			}
			_, _, err = r.inspect(tag, &width)
			if err != nil {
				return nil, err
			}
		}
	}
	for p.Next < total && p.Scanned < MaxScanTags && len(p.Rows) < MaxResults {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		tag, err := lookup(p.Next)
		if err != nil {
			return nil, err
		}
		if tag.ID != p.Next {
			return nil, ErrInvalidInput
		}
		row, eligible, err := r.inspect(tag, &width)
		if err != nil && !errors.Is(err, errBudget) {
			return nil, err
		}
		p.Next++
		p.Scanned++
		if eligible && (filter == "all" || filter == "unknown" && row.ObservedIDR == "unknown" || filter == "mismatches" && (row.DeclarationRelation == "keyframe_without_idr" || row.DeclarationRelation == "inter_with_idr")) {
			p.Rows = append(p.Rows, row)
		}
		if errors.Is(err, errBudget) {
			break
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	p.BytesRead = r.bytes
	p.Exhausted = p.Next == total
	return p, nil
}

type reader struct {
	ctx    context.Context
	source io.ReaderAt
	bytes  uint64
}

func (r *reader) read(offset uint64, b []byte) error {
	if err := r.ctx.Err(); err != nil {
		return err
	}
	if offset > math.MaxInt64 || uint64(len(b)) > math.MaxInt64-offset {
		return ErrInvalidInput
	}
	if uint64(len(b)) > MaxBytes-r.bytes {
		return errBudget
	}
	n, err := r.source.ReadAt(b, int64(offset))
	if n < 0 || n > len(b) {
		return io.ErrUnexpectedEOF
	}
	r.bytes += uint64(n)
	if canceled := r.ctx.Err(); canceled != nil {
		return canceled
	}
	// EOF accompanying every requested byte is valid; any other source failure
	// remains observable even if ReadAt also filled the destination buffer.
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	if n != len(b) {
		return io.ErrUnexpectedEOF
	}
	return nil
}
func (r *reader) inspect(tag flv.TagRecord, width *uint8) (Row, bool, error) {
	row := Row{TagID: tag.ID, DTS: tag.DTS, Offset: tag.Offset, ObservedIDR: "unknown", DeclarationRelation: "undetermined"}
	if tag.Type() != flv.TagTypeVideo {
		return row, false, nil
	}
	if tag.Filter() {
		*width = 0
		row.Reason = "filtered"
		return row, true, nil
	}
	if tag.DataSize > 0xffffff || tag.AvailableDataSize > tag.DataSize || tag.Offset > math.MaxInt64-flv.TagHeaderSize-uint64(tag.AvailableDataSize) {
		return row, false, ErrInvalidInput
	}
	base := tag.PayloadSpan().ByteOffset
	if tag.AvailableDataSize == 0 {
		*width = 0
		row.Reason = "truncated_payload"
		return row, true, nil
	}
	var prefix [2]byte
	n := min(uint32(2), tag.AvailableDataSize)
	if err := r.read(base, prefix[:n]); err != nil {
		row.Reason = "byte_limit"
		return row, true, err
	}
	if prefix[0]&15 != flv.CodecIDAVC {
		*width = 0
		return row, false, nil
	}
	frame := prefix[0] >> 4
	row.DeclaredFrameType = &frame
	row.DeclarationRelation = avc.CompareKeyframeEvidence(frame, nil).DeclarationRelation
	row.Span = &cursor.Span{ByteOffset: base, BitLength: 4}
	if frame == 5 {
		*width = 0
		return row, false, nil
	}
	if n < 2 {
		*width = 0
		row.Reason = "truncated_payload"
		return row, true, nil
	}
	if !tag.PayloadComplete() || tag.AvailableDataSize != tag.DataSize {
		*width = 0
		row.Reason = "truncated_payload"
		return row, true, nil
	}
	switch flv.AVCPacketType(prefix[1]) {
	case flv.AVCPacketTypeSequenceHeader:
		*width = 0
		if tag.DataSize < 5 {
			return row, false, nil
		}
		value, err := r.configuration(base+5, base+uint64(tag.DataSize))
		if errors.Is(err, errFraming) {
			return row, false, nil
		}
		if err != nil {
			return row, false, err
		}
		*width = value
		return row, false, nil
	case flv.AVCPacketTypeEndOfSequence:
		*width = 0
		return row, false, nil
	case flv.AVCPacketTypeNALU:
	default:
		*width = 0
		row.Reason = "unsupported_packet"
		return row, true, nil
	}
	if tag.DataSize < 5 {
		*width = 0
		row.Reason = "truncated_payload"
		return row, true, nil
	}
	if *width == 0 {
		row.Reason = "missing_configuration"
		return row, true, nil
	}
	sequence := &avc.NALUnitSequence{LengthSize: *width}
	pos, end := base+5, base+uint64(tag.DataSize)
	row.Reason = "complete"
	var stopErr error
	for pos < end {
		if len(sequence.NALUnits) >= MaxNALHeaders {
			row.Reason = "nal_limit"
			break
		}
		if uint64(*width) > end-pos {
			row.Reason = "invalid_framing"
			break
		}
		var length [4]byte
		if err := r.read(pos, length[4-int(*width):]); err != nil {
			stopErr = err
			row.Reason = "byte_limit"
			break
		}
		size := uint64(binary.BigEndian.Uint32(length[:]))
		pos += uint64(*width)
		if size == 0 || size > end-pos {
			row.Reason = "invalid_framing"
			break
		}
		var raw [1]byte
		if err := r.read(pos, raw[:]); err != nil {
			stopErr = err
			row.Reason = "byte_limit"
			break
		}
		h := avc.NALHeader{Raw: raw[0], Complete: true, ForbiddenZeroBit: raw[0]&128 != 0, NALRefIDC: raw[0] >> 5 & 3, NALUnitType: avc.NALUnitType(raw[0] & 31), NALUnitTypeSpan: cursor.Span{ByteOffset: pos, BitOffset: 3, BitLength: 5}}
		sequence.NALUnits = append(sequence.NALUnits, avc.NALUnit{Index: uint32(len(sequence.NALUnits)), HeaderPresent: true, Header: h})
		pos += size
	}
	sequence.FramingComplete = pos == end && row.Reason == "complete" && len(sequence.NALUnits) > 0
	if len(sequence.NALUnits) == 0 && row.Reason == "complete" {
		row.Reason = "invalid_framing"
	}
	evidence := avc.CompareKeyframeEvidence(frame, sequence)
	row.ObservedIDR, row.DeclarationRelation = evidence.ObservedIDR, evidence.DeclarationRelation
	if row.Reason == "complete" && !evidence.NALHeaderCoverageComplete {
		row.Reason = "unclassified_header"
	}
	if evidence.FirstIDR != nil {
		span := evidence.FirstIDR.Span
		row.Span = &span
	} else if evidence.FirstNAL != nil {
		span := evidence.FirstNAL.Span
		row.Span = &span
	}
	return row, true, stopErr
}

// configuration validates avcC length framing and skips every parameter-set
// body. Width validity does not claim SPS/PPS syntax or decoder usability.
func (r *reader) configuration(pos, end uint64) (uint8, error) {
	read := func(b []byte) error {
		if uint64(len(b)) > end-pos {
			return errFraming
		}
		if err := r.read(pos, b); err != nil {
			return err
		}
		pos += uint64(len(b))
		return nil
	}
	var head [6]byte
	if err := read(head[:]); err != nil {
		return 0, err
	}
	if head[0] != 1 || head[4]&3 == 2 {
		return 0, errFraming
	}
	sets := func(count uint8) error {
		for i := 0; i < int(count); i++ {
			var b [2]byte
			if err := read(b[:]); err != nil {
				return err
			}
			size := uint64(binary.BigEndian.Uint16(b[:]))
			if size == 0 || size > end-pos {
				return errFraming
			}
			pos += size
		}
		return nil
	}
	if err := sets(head[5] & 31); err != nil {
		return 0, err
	}
	var count [1]byte
	if err := read(count[:]); err != nil {
		return 0, err
	}
	if err := sets(count[0]); err != nil {
		return 0, err
	}
	if head[1] != 66 && head[1] != 77 && head[1] != 88 {
		var ext [4]byte
		if err := read(ext[:]); err != nil {
			return 0, err
		}
		if err := sets(ext[3]); err != nil {
			return 0, err
		}
	}
	if pos != end {
		return 0, errFraming
	}
	return (head[4] & 3) + 1, nil
}
