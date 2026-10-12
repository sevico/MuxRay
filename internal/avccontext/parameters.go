package avccontext

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"

	"github.com/sevico/muxray/internal/avc"
	"github.com/sevico/muxray/internal/flv"
)

const (
	StateInvalidated                = "invalidated"
	DefaultParameterMaxTags  uint64 = 8192
	DefaultParameterMaxBytes uint64 = 1 << 20
)

// ParameterLimits bounds forward replay. Unlike the legacy framing search,
// zero selects a finite default for each limit.
type ParameterLimits struct {
	MaxTags  uint64
	MaxBytes uint64
}

// ParameterResolution describes the replayed prefix [StartTagID, NextTagID).
// SelectedTagID itself is excluded: its in-band updates belong to its parser.
// BytesRead includes repeated reads and the selected two-byte eligibility probe.
// A context is published only after the entire prefix has been replayed and its
// final NAL length width agrees with framing. Individual usable slots do not
// imply that the selected slice's referenced PPS/SPS pair is available.
// BarrierCount counts video tags that clear the complete parameter generation.
type ParameterResolution struct {
	State          string
	Context        *avc.ParameterSetContext
	StartTagID     uint64
	NextTagID      uint64
	SelectedTagID  uint64
	ScannedTags    uint64
	VideoTags      uint64
	Configurations uint64
	NALPackets     uint64
	BytesRead      uint64
	BarrierCount   uint64
	SPSCount       uint32
	PPSCount       uint32
	// Segment-only progress is absent from the legacy JSON projection.
	// StartTagID remains the framing origin; ResumeStartTagID is this segment's
	// first tag. Work counters count only this segment, including an attempted
	// partial Tag; NextTagID excludes that Tag and CanResume requires progress.
	ResumeStartTagID uint64
	CanResume        bool
	CheckpointCount  uint32
}

// ResolveParameters independently reconstructs bounded SPS/PPS state from the
// framing declaration to the selected tag. It never changes framing, guesses a
// length width, or reads ordinary slice bodies. Audio/script tags are harmless;
// unreadable video generations are barriers rather than permission to reuse an
// older avcC. Cancellation, I/O errors and exhausted budgets publish no context.
func ResolveParameters(ctx context.Context, source io.ReaderAt, selected flv.TagRecord, lookup Lookup, framing Resolution, options flv.AVCVideoOptions, limits ParameterLimits) (ParameterResolution, error) {
	return resolveParameters(ctx, source, selected, lookup, framing, options, limits, nil)
}

// ResolveParameterSegment performs at most one normal replay budget, starting
// from an immutable checkpoint when supplied. It returns a candidate snapshot
// only at a completed Tag boundary. The owner must validate source identity and
// request currency before retaining it; errors/cancellation return no snapshot.
func ResolveParameterSegment(ctx context.Context, source io.ReaderAt, selected flv.TagRecord, lookup Lookup, framing Resolution, options flv.AVCVideoOptions, limits ParameterLimits, checkpoint *ParameterCheckpoint) (ParameterResolution, *ParameterCheckpoint, error) {
	replay := parameterReplay{seed: checkpoint}
	result, err := resolveParameters(ctx, source, selected, lookup, framing, options, limits, &replay)
	return result, replay.next, err
}

type parameterReplay struct{ seed, next *ParameterCheckpoint }

func resolveParameters(ctx context.Context, source io.ReaderAt, selected flv.TagRecord, lookup Lookup, framing Resolution, options flv.AVCVideoOptions, limits ParameterLimits, replay *parameterReplay) (result ParameterResolution, err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	result = ParameterResolution{State: StateNotNeeded, SelectedTagID: selected.ID}
	if limits.MaxTags == 0 {
		limits.MaxTags = DefaultParameterMaxTags
	}
	if limits.MaxBytes == 0 {
		limits.MaxBytes = DefaultParameterMaxBytes
	}
	reader := parameterReader{ctx: ctx, source: source, result: &result, limit: limits.MaxBytes}
	header, kind, present, err := ReadFramingPrefix(&reader, selected)
	if err != nil {
		return parameterError(result, err)
	}
	if !present || header&0x0f != flv.CodecIDAVC || header>>4 == 5 || kind != uint8(flv.AVCPacketTypeNALU) {
		return result, ctx.Err()
	}
	result.State = StateMissing
	if framing.State != StateFound {
		if framing.State == StateBudgetExhausted {
			result.State = StateBudgetExhausted
		}
		return result, ctx.Err()
	}
	if lookup == nil || framing.SourceTagID >= selected.ID || !parameterWidthValid(framing.NALLengthSize) {
		return result, ErrInvalidInput
	}
	options.AVC, err = parameterOptions(options.AVC)
	if err != nil {
		return result, err
	}
	sets := avc.NewParameterSetContext()
	width := uint8(0)
	invalidated := false
	start := framing.SourceTagID
	if replay != nil && replay.seed != nil {
		if !replay.seed.matches(framing, selected.ID, options.AVC) {
			return result, ErrInvalidInput
		}
		start, width, invalidated = replay.seed.nextTagID, replay.seed.width, replay.seed.invalidated
		sets = replay.seed.sets.Clone()
	}
	result.StartTagID, result.NextTagID = framing.SourceTagID, start
	if replay != nil {
		result.ResumeStartTagID = start
		defer func() {
			if err != nil {
				return
			}
			result.CanResume = result.State == StateBudgetExhausted && result.NextTagID > start
			if result.NextTagID == start && start != selected.ID {
				return
			}
			replay.next = &ParameterCheckpoint{sourceTagID: framing.SourceTagID, lengthSpan: framing.SourceLengthSpan, declaredWidth: framing.NALLengthSize, options: options.AVC, nextTagID: result.NextTagID, sets: sets.CompactClone(), width: width, invalidated: invalidated}
		}()
	}
	for id := start; id < selected.ID; id++ {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if result.ScannedTags >= limits.MaxTags {
			return parameterError(result, errBudgetExhausted)
		}
		tag, err := lookup(id)
		if err != nil {
			return result, err
		}
		if tag.ID != id {
			return result, ErrInvalidInput
		}
		result.ScannedTags++
		if tag.Type() != flv.TagTypeVideo {
			result.NextTagID = id + 1
			continue
		}
		result.VideoTags++
		header, kind, present, err := ReadFramingPrefix(&reader, tag)
		if err != nil {
			return parameterError(result, err)
		}
		barrier := !present || header&0x0f != flv.CodecIDAVC || header>>4 == 5 || tag.DataSize < 5
		if !barrier && (kind == uint8(flv.AVCPacketTypeSequenceHeader) || kind == uint8(flv.AVCPacketTypeNALU) && width != 0) {
			bounded, err := payloadReader(&reader, tag)
			if err != nil {
				return result, err
			}
			options.NALLengthSize = width
			parseOptions := options
			if options.AVC.ParseFullHeaders {
				parseOptions.AVC.ParameterSets = sets
			}
			if kind == uint8(flv.AVCPacketTypeSequenceHeader) {
				result.Configurations++
			} else {
				result.NALPackets++
			}
			parsed, err := flv.ParseAVCVideo(bounded, tag, parseOptions)
			if err != nil {
				return parameterError(result, err)
			}
			if kind == uint8(flv.AVCPacketTypeSequenceHeader) {
				// Commit a replacement only after its whole read returns. A
				// budget-exhausted Tag cannot corrupt the preceding checkpoint.
				sets, width = avc.NewParameterSetContext(), 0
				if parsed.ConfigPresent && parsed.Config.FramingComplete && parameterWidthValid(parsed.Config.LengthSize) {
					width = parsed.Config.LengthSize
					sets.AddConfiguration(parsed.Config)
					invalidated = !parsed.Config.Complete
				} else {
					barrier = true
				}
			} else if !parsed.NALUnitsPresent || !parsed.NALUnits.FramingComplete {
				// An unvisited suffix could contain parameter replacements.
				barrier = true
			} else {
				nalBarrier := false
				for _, unit := range parsed.NALUnits.NALUnits {
					sets.AddNALUnit(unit)
					if !unit.HeaderPresent || unit.Header.NALUnitType == avc.NALUnitTypeEndOfSequence || unit.Header.NALUnitType == avc.NALUnitTypeEndOfStream {
						nalBarrier = true
					}
					if !unit.SummaryComplete || nalBarrier {
						invalidated = true
					}
				}
				if nalBarrier {
					result.BarrierCount++
				}
			}
		} else if !barrier && kind != uint8(flv.AVCPacketTypeNALU) {
			// EOS and unknown packet types cannot activate an earlier config.
			barrier = true
		}
		if barrier {
			sets, width = avc.NewParameterSetContext(), 0
			invalidated = true
			result.BarrierCount++
		}
		result.NextTagID = id + 1
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if width != framing.NALLengthSize {
		result.State = StateInvalidated
		return result, nil
	}
	result.SPSCount, result.PPSCount = sets.Counts()
	if result.SPSCount != 0 || result.PPSCount != 0 {
		result.State, result.Context = StateFound, sets
	} else if invalidated {
		result.State = StateInvalidated
	}
	return result, nil
}

func parameterWidthValid(width uint8) bool { return width == 1 || width == 2 || width == 4 }

func parameterError(result ParameterResolution, err error) (ParameterResolution, error) {
	if errors.Is(err, errBudgetExhausted) {
		result.State = StateBudgetExhausted
		return result, nil
	}
	return result, err
}

// Keep non-EOF source failures visible even when a ReaderAt returns a full
// buffer: low-level syntax readers legitimately accept full reads with EOF.
type parameterReader struct {
	ctx    context.Context
	source io.ReaderAt
	result *ParameterResolution
	limit  uint64
}

func (r *parameterReader) ReadAt(p []byte, offset int64) (int, error) {
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
	if r.result.BytesRead > r.limit || wanted > r.limit-r.result.BytesRead {
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
	if err != nil && !errors.Is(err, io.EOF) {
		return 0, err
	}
	return n, err
}
