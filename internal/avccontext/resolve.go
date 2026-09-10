// Package avccontext resolves AVC NAL length declarations from earlier indexed
// tags without guessing framing or retaining payloads or a history of results.
package avccontext

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"

	"github.com/sevico/muxray/internal/cursor"
	"github.com/sevico/muxray/internal/flv"
)

const (
	StateNotNeeded       = "not_needed"
	StateFound           = "found"
	StateMissing         = "missing"
	StateBudgetExhausted = "budget_exhausted"
)

var (
	ErrInvalidInput    = errors.New("avccontext: invalid source or indexed tag")
	ErrCounterOverflow = errors.New("avccontext: work counter overflow")
	errBudgetExhausted = errors.New("avccontext: work budget exhausted")
)

// Lookup returns one immutable indexed record by its global ID. It must not
// acquire payloads, duplicate a full index, or return a record for another ID.
type Lookup func(uint64) (flv.TagRecord, error)

// Limits bounds one resolution. Zero disables that individual limit, preserving
// the CLI's existing search over the complete, already bounded container index.
type Limits struct {
	MaxTags       uint64
	MaxCandidates uint32
	MaxBytes      uint64
}

type Resolution struct {
	State            string
	NALLengthSize    uint8
	SourceTagID      uint64
	SourceLengthSpan cursor.Span
	ScannedTags      uint64
	Candidates       uint32
	BytesRead        uint64
}

// Resolve searches backward for the nearest usable AVC sequence header only
// when selected is an AVC NALU tag. NALLengthSize in options is ignored: this is
// automatic lookup, and the caller handles an explicit override separately.
// FramingComplete permits a usable declaration even if SPS syntax is damaged.
// ScannedTags excludes selected; Candidates counts actual configuration parses.
// BytesRead counts bytes returned by the source, including repeated reads and
// the selected prefix. Budget exhaustion is a result state, never "missing".
func Resolve(ctx context.Context, source io.ReaderAt, selected flv.TagRecord, lookup Lookup, options flv.AVCVideoOptions, limits Limits) (Resolution, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	result := Resolution{State: StateNotNeeded}
	reader := workReader{ctx: ctx, source: source, result: &result, limit: limits.MaxBytes}
	header, packetType, present, err := ReadFramingPrefix(&reader, selected)
	if err != nil {
		return resolutionError(result, err)
	}
	if !present || header&0x0f != flv.CodecIDAVC || header>>4 == 5 || packetType != uint8(flv.AVCPacketTypeNALU) {
		return result, ctx.Err()
	}
	result.State = StateMissing
	options.NALLengthSize = 0
	for id := selected.ID; id > 0; {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if limits.MaxTags != 0 && result.ScannedTags >= limits.MaxTags {
			result.State = StateBudgetExhausted
			return result, nil
		}
		if lookup == nil {
			return result, ErrInvalidInput
		}
		id--
		candidate, err := lookup(id)
		if err != nil {
			return result, err
		}
		if candidate.ID != id {
			return result, ErrInvalidInput
		}
		if result.ScannedTags == math.MaxUint64 {
			return result, ErrCounterOverflow
		}
		result.ScannedTags++
		header, kind, present, err := ReadFramingPrefix(&reader, candidate)
		if err != nil {
			return resolutionError(result, err)
		}
		if !present || header&0x0f != flv.CodecIDAVC || header>>4 == 5 || kind != uint8(flv.AVCPacketTypeSequenceHeader) {
			continue
		}
		if limits.MaxCandidates != 0 && result.Candidates >= limits.MaxCandidates {
			result.State = StateBudgetExhausted
			return result, nil
		}
		if result.Candidates == math.MaxUint32 {
			return result, ErrCounterOverflow
		}
		result.Candidates++
		bounded, err := payloadReader(&reader, candidate)
		if err != nil {
			return result, err
		}
		parsed, err := flv.ParseAVCVideo(bounded, candidate, options)
		if err != nil {
			if isSelectionError(err) {
				continue
			}
			return resolutionError(result, err)
		}
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if parsed.ConfigPresent && parsed.Config.FramingComplete && (parsed.Config.LengthSize == 1 || parsed.Config.LengthSize == 2 || parsed.Config.LengthSize == 4) {
			result.State, result.NALLengthSize, result.SourceTagID, result.SourceLengthSpan = StateFound, parsed.Config.LengthSize, candidate.ID, parsed.Config.LengthSizeMinusOneSpan
			return result, nil
		}
	}
	return result, ctx.Err()
}

func resolutionError(result Resolution, err error) (Resolution, error) {
	if errors.Is(err, errBudgetExhausted) {
		result.State = StateBudgetExhausted
		return result, nil
	}
	return result, err
}

// ReadFramingPrefix is the shared two-byte FLV AVC probe. A full read with EOF
// is accepted, matching ReaderAt and the existing CLI context lookup behavior.
func ReadFramingPrefix(source io.ReaderAt, tag flv.TagRecord) (header uint8, packetType uint8, present bool, err error) {
	if tag.Type() != flv.TagTypeVideo || tag.Filter() || !tag.PayloadComplete() || tag.DataSize < 2 {
		return 0, 0, false, nil
	}
	reader, err := payloadReader(source, tag)
	if err != nil {
		return 0, 0, false, err
	}
	var prefix [2]byte
	n, readErr := reader.ReadAt(prefix[:], int64(tag.PayloadSpan().ByteOffset))
	if n == len(prefix) {
		return prefix[0], prefix[1], true, nil
	}
	if readErr != nil {
		return 0, 0, false, readErr
	}
	return 0, 0, false, io.ErrUnexpectedEOF
}

type spanReader struct {
	source     io.ReaderAt
	start, end uint64
}

func payloadReader(source io.ReaderAt, tag flv.TagRecord) (spanReader, error) {
	if source == nil || tag.DataSize > 0xffffff || tag.AvailableDataSize != tag.DataSize || tag.Offset > math.MaxInt64-flv.TagHeaderSize-uint64(tag.DataSize) {
		return spanReader{}, ErrInvalidInput
	}
	start := tag.Offset + flv.TagHeaderSize
	return spanReader{source: source, start: start, end: start + uint64(tag.DataSize)}, nil
}

func (r spanReader) ReadAt(p []byte, offset int64) (int, error) {
	if offset < 0 || uint64(offset) < r.start || uint64(offset) > r.end || uint64(len(p)) > r.end-uint64(offset) {
		return 0, ErrInvalidInput
	}
	return r.source.ReadAt(p, offset)
}

type workReader struct {
	ctx    context.Context
	source io.ReaderAt
	result *Resolution
	limit  uint64
}

func (r *workReader) ReadAt(p []byte, offset int64) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	if r.source == nil {
		return 0, ErrInvalidInput
	}
	wanted := uint64(len(p))
	if wanted > math.MaxUint64-r.result.BytesRead {
		return 0, ErrCounterOverflow
	}
	if r.limit != 0 && (r.result.BytesRead > r.limit || wanted > r.limit-r.result.BytesRead) {
		return 0, errBudgetExhausted
	}
	n, err := r.source.ReadAt(p, offset)
	if n < 0 || n > len(p) {
		return 0, fmt.Errorf("%w: invalid ReaderAt count", ErrInvalidInput)
	}
	r.result.BytesRead += uint64(n)
	if canceled := r.ctx.Err(); canceled != nil {
		return 0, canceled
	}
	return n, err
}

func isSelectionError(err error) bool {
	return errors.Is(err, flv.ErrAVCVideoWrongTagType) || errors.Is(err, flv.ErrAVCVideoFiltered) || errors.Is(err, flv.ErrAVCVideoPayloadIncomplete) || errors.Is(err, flv.ErrAVCVideoWrongCodecID) || errors.Is(err, flv.ErrAVCVideoCommandFrame)
}
