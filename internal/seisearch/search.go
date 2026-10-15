package seisearch

import (
	"context"
	"errors"
	"io"
	"math"

	"github.com/sevico/muxray/internal/avc"
	"github.com/sevico/muxray/internal/flv"
)

var errBudget = errors.New("seisearch: byte limit")

// Search consumes Tags in source order. It parses each bounded Tag once, then
// drains any remaining message rows without rereading the source. State is
// never mutated. Cancellation and source I/O failure return no candidate page.
func Search(ctx context.Context, source io.ReaderAt, lookup Lookup, state State, total uint64, scanComplete bool, filter string) (*Page, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if source == nil || lookup == nil || state.next > total || total > flv.HardMaxTags || !ValidFilter(filter) || state.filter != "" && state.filter != filter {
		return nil, ErrInvalidInput
	}
	s := state.Clone()
	s.filter = filter
	r := reader{ctx: ctx, source: source}
	p := &Page{Start: state.Position(), Total: total, ScanComplete: scanComplete, Filter: filter, Rows: make([]Row, 0, MaxResults)}
	for len(p.Rows) < MaxResults {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if len(s.pending) != 0 {
			n := min(MaxResults-len(p.Rows), len(s.pending))
			for _, row := range s.pending[:n] {
				p.Rows = append(p.Rows, cloneRow(row))
			}
			s.pending = s.pending[n:]
			if len(s.pending) == 0 {
				s.pending = nil
			}
			continue
		}
		if s.next == total || p.Scanned == MaxScanTags || r.bytes == MaxBytes {
			break
		}
		tag, err := lookup(s.next)
		if err != nil {
			return nil, err
		}
		if tag.ID != s.next {
			return nil, ErrInvalidInput
		}
		before := r.bytes
		// The candidate context is disposable until the whole Tag read succeeds.
		candidate := s
		candidate.sets = s.sets.Clone()
		rows, err := r.inspect(tag, &candidate)
		p.Scanned++
		if err != nil {
			if !errors.Is(err, errBudget) {
				return nil, err
			}
			if before != 0 {
				// Retry once with a full page budget; neither context nor the Tag
				// position is advanced by an interrupted parse.
				break
			}
			// A single Tag exceeds the entire batch budget. Mark its coverage
			// gap and move forward, so an opaque cursor cannot loop forever.
			candidate.clear()
			rows = []Row{coverageRow(tag, "byte_limit")}
		}
		candidate.next++
		candidate.sets = candidate.sets.CompactClone()
		candidate.pending = nil
		for _, row := range rows {
			if matches(row, filter) {
				candidate.pending = append(candidate.pending, row)
			}
		}
		s = candidate
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	p.BytesRead, p.Next, p.Pending = r.bytes, s.Position(), uint32(len(s.pending))
	p.Exhausted = s.next == total && len(s.pending) == 0
	p.State = s
	return p, nil
}

func (s *State) clear() {
	s.width = 0
	s.sets = avc.NewParameterSetContext()
}

func matches(row Row, filter string) bool {
	// Coverage gaps affect every filter: no claim is made that an uninspected
	// suffix lacks the requested message kind.
	return filter == "all" || row.Kind == "coverage" || filter != "unknown" && filter == row.Kind || filter == "unknown" && (!row.Complete || row.ContextState == "missing" || row.ContextState == "ambiguous")
}

type reader struct {
	ctx        context.Context
	source     io.ReaderAt
	bytes      uint64
	start, end uint64
	failure    error
}

func (r *reader) ReadAt(p []byte, offset int64) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	if r.failure != nil {
		return 0, r.failure
	}
	if offset < 0 || uint64(offset) < r.start || uint64(offset) > r.end || uint64(len(p)) > r.end-uint64(offset) {
		return 0, ErrInvalidInput
	}
	if uint64(len(p)) > MaxBytes-r.bytes {
		return 0, errBudget
	}
	n, err := r.source.ReadAt(p, offset)
	if n < 0 || n > len(p) {
		return 0, ErrInvalidInput
	}
	r.bytes += uint64(n)
	if canceled := r.ctx.Err(); canceled != nil {
		return n, canceled
	}
	if err != nil && !errors.Is(err, io.EOF) {
		// Some syntax readers accept a full read without inspecting its error.
		// Preserve genuine source failures across that compatibility boundary.
		r.failure = err
		return n, err
	}
	if n != len(p) {
		return n, io.ErrUnexpectedEOF
	}
	return n, nil
}

func (r *reader) inspect(tag flv.TagRecord, state *State) ([]Row, error) {
	if tag.Type() != flv.TagTypeVideo {
		return nil, nil
	}
	if tag.DataSize > 0xffffff || tag.AvailableDataSize > tag.DataSize || tag.Offset > math.MaxInt64-flv.TagHeaderSize-uint64(tag.AvailableDataSize) {
		return nil, ErrInvalidInput
	}
	r.start = tag.PayloadSpan().ByteOffset
	r.end = r.start + uint64(tag.AvailableDataSize)
	barrier := func(reason string) ([]Row, error) {
		state.clear()
		return []Row{coverageRow(tag, reason)}, nil
	}
	if tag.Filter() {
		return barrier("filtered_video")
	}
	if !tag.PayloadComplete() || tag.AvailableDataSize != tag.DataSize || tag.DataSize == 0 {
		return barrier("truncated_payload")
	}
	var prefix [2]byte
	n := min(uint32(2), tag.DataSize)
	if _, err := r.ReadAt(prefix[:n], int64(r.start)); err != nil {
		return nil, err
	}
	if prefix[0]&15 != flv.CodecIDAVC || prefix[0]>>4 == 5 {
		state.clear()
		return nil, nil
	}
	if tag.DataSize < 5 {
		return barrier("truncated_avc_header")
	}
	kind := flv.AVCPacketType(prefix[1])
	if kind == flv.AVCPacketTypeEndOfSequence {
		state.clear()
		return nil, nil
	}
	if kind != flv.AVCPacketTypeSequenceHeader && kind != flv.AVCPacketTypeNALU {
		return barrier("unsupported_packet")
	}
	if kind == flv.AVCPacketTypeNALU && state.width == 0 {
		return barrier("nal_length_unavailable")
	}
	options := flv.AVCVideoOptions{NALLengthSize: state.width, AVC: avc.Options{
		ParseSEI: true, ParseSEITiming: true, ParseFullHeaders: true, ParameterSets: state.sets,
	}}
	parsed, err := flv.ParseAVCVideo(r, tag, options)
	if r.failure != nil {
		return nil, r.failure
	}
	if err != nil {
		return nil, err
	}
	rows, incompleteSEI := messageRows(tag, parsed)
	reason := limitReason(parsed.Diagnostics)
	if kind == flv.AVCPacketTypeSequenceHeader {
		state.clear()
		if parsed.ConfigPresent && parsed.Config.FramingComplete && (parsed.Config.LengthSize == 1 || parsed.Config.LengthSize == 2 || parsed.Config.LengthSize == 4) {
			state.width = parsed.Config.LengthSize
			state.sets.AddConfiguration(parsed.Config)
		} else if reason == "" {
			reason = "configuration_incomplete"
		}
	} else if !parsed.NALUnitsPresent || !parsed.NALUnits.FramingComplete {
		state.clear()
		if reason == "" {
			reason = "nal_framing_incomplete"
		}
	} else {
		for _, unit := range parsed.NALUnits.NALUnits {
			state.sets.AddNALUnit(unit)
		}
	}
	if incompleteSEI && reason == "" {
		reason = "sei_framing_incomplete"
	}
	if reason != "" {
		// A restricted Tag cannot supply trustworthy persistent timing state.
		// Keep a fully established NAL width, but discard every old SPS/PPS.
		state.sets = avc.NewParameterSetContext()
		rows = append(rows, coverageRow(tag, reason))
	}
	return rows, nil
}
