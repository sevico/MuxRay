// Package mp3context locates physical Layer III main-data slots across FLV
// Tags. It never decodes audio or models consumption of a bit reservoir.
package mp3context

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"slices"

	"github.com/sevico/muxray/internal/cursor"
	"github.com/sevico/muxray/internal/flv"
	"github.com/sevico/muxray/internal/mp3"
)

var ErrInvalidInput = errors.New("mp3context: invalid input")
var errReadBudget = errors.New("mp3context: read budget exhausted")

const (
	MaxTags      uint64 = 512
	MaxAudioTags uint64 = 64
	MaxBytes     uint64 = 1 << 20
	MaxFrames    uint32 = 256
	MaxSources   uint32 = 64
)

type Lookup func(uint64) (flv.TagRecord, error)

// Zero limits select finite defaults; callers may only lower these limits.
type Limits struct {
	MaxTags, MaxAudioTags, MaxBytes, MaxReadBytes uint64
	MaxFrames, MaxSources                         uint32
}

type Source struct {
	TagID      uint64
	FrameIndex uint32
	Current    bool
	Span       cursor.Span
}

// Sources is the known physical suffix of the backward window, followed by
// the current frame's entire main-data slot. In an unknown result it is not a
// contiguous known prefix of the compressed audio. Selected preserves the
// one bounded selected-Tag parse for the detail projector; it owns no reader.
// CoverageBytes charges whole SoundData ranges before parsing, including any
// uninspected suffix, so neither traversal nor parsing can exceed its budget.
// ScannedTags and AudioTags count preceding Tags only; FramesInspected and
// BytesRead also include the selected Tag.
type Resolution struct {
	State, Reason                                    string
	TargetTagID                                      uint64
	TargetFrameIndex                                 uint32
	RequestedPreviousBytes, MissingPreviousBytes     *uint32
	ResolvedPreviousBytes                            uint32
	Part23Bits                                       *uint32
	AvailableBits                                    *uint64
	Sources                                          []Source
	ScannedTags, AudioTags, CoverageBytes, BytesRead uint64
	FramesInspected                                  uint32
	Limits                                           Limits
	Selected                                         *flv.MP3Audio
}

func normalize(l Limits) (Limits, error) {
	for _, pair := range []struct {
		value   *uint64
		maximum uint64
	}{
		{&l.MaxTags, MaxTags}, {&l.MaxAudioTags, MaxAudioTags}, {&l.MaxBytes, MaxBytes}, {&l.MaxReadBytes, MaxBytes},
	} {
		if *pair.value == 0 {
			*pair.value = pair.maximum
		}
		if *pair.value > pair.maximum {
			return l, ErrInvalidInput
		}
	}
	for _, pair := range []struct {
		value   *uint32
		maximum uint32
	}{{&l.MaxFrames, MaxFrames}, {&l.MaxSources, MaxSources}} {
		if *pair.value == 0 {
			*pair.value = pair.maximum
		}
		if *pair.value > pair.maximum {
			return l, ErrInvalidInput
		}
	}
	return l, nil
}

// Resolve inspects the selected Tag once, then visits previous Tags backwards
// until main_data_begin is covered. Video and script Tags do not break audio
// adjacency. A prior audio Tag must have its complete frame boundaries located
// from its beginning; unknown tails are never skipped or searched for sync.
// Errors and cancellation discard all output. Budget exhaustion is an explicit
// unknown result, with only the already established suffix retained.
func Resolve(ctx context.Context, source io.ReaderAt, selected flv.TagRecord, frameIndex uint32, lookup Lookup, limits Limits) (result Resolution, err error) {
	return resolveSelection(ctx, source, selected, frameIndex, lookup, limits, nil, nil)
}

// ResolvePage reuses a paginated selected parse and its bounded physical suffix.
// Cached slots are provenance only; they never imply decoded reservoir state.
func ResolvePage(ctx context.Context, source io.ReaderAt, selected flv.TagRecord, frameIndex uint32, lookup Lookup, limits Limits, parsed flv.MP3Audio, page mp3.FramePage) (Resolution, error) {
	return resolveSelection(ctx, source, selected, frameIndex, lookup, limits, &parsed, &page)
}

func resolveSelection(ctx context.Context, source io.ReaderAt, selected flv.TagRecord, frameIndex uint32, lookup Lookup, limits Limits, prepared *flv.MP3Audio, page *mp3.FramePage) (result Resolution, err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	limits, err = normalize(limits)
	if err != nil || source == nil || lookup == nil || (page == nil && frameIndex >= mp3.DefaultMaxFrames) || selected.Type() != flv.TagTypeAudio {
		return Resolution{}, ErrInvalidInput
	}
	result = Resolution{State: "unknown", Reason: "target_unavailable", TargetTagID: selected.ID, TargetFrameIndex: frameIndex, Limits: limits, Sources: []Source{}}
	defer func() {
		if err == nil {
			err = ctx.Err()
		}
		if err != nil {
			result = Resolution{}
			return
		}
		slices.Reverse(result.Sources)
		if result.RequestedPreviousBytes != nil {
			missing := *result.RequestedPreviousBytes - result.ResolvedPreviousBytes
			result.MissingPreviousBytes = &missing
		}
	}()
	reader := &boundedReader{ctx: ctx, source: source, result: &result}
	parse := func(tag flv.TagRecord, maximum uint32) (*flv.MP3Audio, string, error) {
		if tag.Filter() {
			return nil, "filtered_audio", nil
		}
		if !tag.PayloadComplete() || tag.AvailableDataSize != tag.DataSize {
			return nil, "truncated_audio", nil
		}
		if tag.DataSize == 0 {
			return nil, "audio_header_missing", nil
		}
		if tag.DataSize > 0xffffff || tag.Offset > math.MaxInt64-flv.TagHeaderSize-uint64(tag.DataSize) {
			return nil, "", ErrInvalidInput
		}
		reader.start = tag.Offset + flv.TagHeaderSize
		reader.end = reader.start + uint64(tag.DataSize)
		var prefix [1]byte
		if _, e := reader.ReadAt(prefix[:], int64(reader.start)); e != nil {
			return nil, "", e
		}
		if prefix[0]>>4 != flv.SoundFormatMP3 {
			return nil, "audio_codec_changed", nil
		}
		coverage := uint64(tag.DataSize - 1)
		if coverage > limits.MaxBytes-result.CoverageBytes {
			return nil, "coverage_budget", nil
		}
		if maximum == 0 {
			return nil, "frame_budget", nil
		}
		result.CoverageBytes += coverage
		if page != nil && tag.ID != selected.ID {
			var checkpoint *mp3.FrameCursor
			for {
				if maximum < mp3.FramePageSize {
					return nil, "frame_budget", nil
				}
				p, current, e := flv.ParseMP3AudioPage(ctx, reader, tag, checkpoint)
				if reader.failure != nil {
					return nil, "", reader.failure
				}
				if e != nil {
					return nil, "", e
				}
				count := uint32(len(current.Sequence.Frames))
				result.FramesInspected += count
				maximum -= count
				if current.Next == nil {
					// Keep a bounded physical suffix, including the previous
					// page when the terminal page contains only a few frames.
					frames := append(append([]mp3.Frame{}, current.PriorFrames...), current.Sequence.Frames...)
					if len(frames) > mp3.FramePageHistorySize {
						frames = frames[len(frames)-mp3.FramePageHistorySize:]
					}
					p.Frames.Frames = frames
					return &p, "", nil
				}
				checkpoint = current.Next
			}
		}
		p, e := flv.ParseMP3Audio(reader, tag, mp3.Options{InspectSideInfo: true, MaxFrames: maximum})
		if reader.failure != nil {
			return nil, "", reader.failure
		}
		if e != nil {
			return nil, "", e
		}
		result.FramesInspected += uint32(len(p.Frames.Frames))
		return &p, "", nil
	}
	var parsed *flv.MP3Audio
	var reason string
	if page == nil {
		parsed, reason, err = parse(selected, min(limits.MaxFrames, mp3.DefaultMaxFrames))
	} else {
		if prepared == nil || prepared.Frames == nil || prepared.TagID != selected.ID || page.StartIndex > frameIndex || uint64(frameIndex) >= uint64(page.StartIndex)+uint64(mp3.FramePageSize) {
			return result, ErrInvalidInput
		}
		parsed = prepared
		result.CoverageBytes, result.BytesRead = page.Sequence.BytesCovered, page.Sequence.BytesRead
		result.FramesInspected = uint32(len(page.Sequence.Frames))
		if result.CoverageBytes > limits.MaxBytes || result.BytesRead > limits.MaxReadBytes || result.FramesInspected > limits.MaxFrames {
			return result, ErrInvalidInput
		}
	}
	if errors.Is(err, errReadBudget) {
		result.Reason = "read_budget"
		return result, nil
	}
	if err != nil {
		return result, err
	}
	if reason != "" {
		result.Reason = reason
		return result, nil
	}
	result.Selected = parsed
	frames := parsed.Frames.Frames
	if page != nil {
		frames = append(append([]mp3.Frame{}, page.PriorFrames...), frames...)
	}
	targetPosition := -1
	for i, frame := range frames {
		if frame.Index == frameIndex {
			targetPosition = i
			break
		}
	}
	if targetPosition < 0 {
		if parsed.Frames.StopReason == "frame_limit" {
			result.Reason = "frame_budget"
		}
		return result, nil
	}
	target := frames[targetPosition]
	if reason = usable(target); reason != "" {
		result.Reason = reason
		return result, nil
	}
	back := target.SideInfo.MainDataBegin.Value
	result.RequestedPreviousBytes, result.Part23Bits = &back, target.Dependency.Part23Bits
	currentBytes := uint64(target.Dependency.MainDataSpan.BitLength / 8)
	available := currentBytes * 8
	result.AvailableBits = &available
	if currentBytes != 0 {
		result.Sources = append(result.Sources, Source{selected.ID, target.Index, true, target.Dependency.MainDataSpan})
	}
	if back == 0 {
		result.State, result.Reason = "resolved", "no_backward_reference"
		return result, nil
	}
	add := func(tagID uint64, frame mp3.Frame) bool {
		if why := usable(frame); why != "" {
			result.Reason = why
			return false
		}
		if frame.Header.Version != target.Header.Version || frame.Header.SamplingFrequencyHz != target.Header.SamplingFrequencyHz || frame.Header.Channels != target.Header.Channels {
			result.Reason = "parameters_changed"
			return false
		}
		s := frame.Dependency.MainDataSpan
		n := min(back-result.ResolvedPreviousBytes, s.BitLength/8)
		if n != 0 {
			if uint32(len(result.Sources)) >= limits.MaxSources {
				result.Reason = "source_limit"
				return false
			}
			s.ByteOffset += uint64(s.BitLength/8 - n)
			s.BitLength = n * 8
			result.Sources = append(result.Sources, Source{tagID, frame.Index, false, s})
			result.ResolvedPreviousBytes += n
			*result.AvailableBits += uint64(n) * 8
		}
		return true
	}
	for index := targetPosition - 1; index >= 0; index-- {
		if err = ctx.Err(); err != nil {
			return result, err
		}
		if !add(selected.ID, frames[index]) {
			return result, nil
		}
		if result.ResolvedPreviousBytes == back {
			result.State, result.Reason = "resolved", "within_tag"
			return result, nil
		}
	}
	if page != nil && len(frames) != 0 && frames[0].Index != 0 {
		result.Reason = "source_limit"
		return result, nil
	}
	for id := selected.ID; id > 0; {
		if err = ctx.Err(); err != nil {
			return result, err
		}
		if result.ScannedTags >= limits.MaxTags {
			result.Reason = "tag_budget"
			return result, nil
		}
		id--
		tag, e := lookup(id)
		if e != nil {
			return result, e
		}
		if tag.ID != id {
			return result, ErrInvalidInput
		}
		result.ScannedTags++
		if tag.Type() == flv.TagTypeVideo || tag.Type() == flv.TagTypeScript {
			continue
		}
		if tag.Type() != flv.TagTypeAudio {
			result.Reason = "unsupported_tag"
			return result, nil
		}
		if result.AudioTags >= limits.MaxAudioTags {
			result.Reason = "audio_tag_budget"
			return result, nil
		}
		result.AudioTags++
		if tag.StreamID != selected.StreamID {
			result.Reason = "stream_changed"
			return result, nil
		}
		maximum := limits.MaxFrames - result.FramesInspected
		if page == nil {
			maximum = min(maximum, mp3.DefaultMaxFrames)
		}
		previous, why, e := parse(tag, maximum)
		if errors.Is(e, errReadBudget) {
			result.Reason = "read_budget"
			return result, nil
		}
		if e != nil {
			return result, e
		}
		if why != "" {
			result.Reason = why
			return result, nil
		}
		sequence := previous.Frames
		if !sequence.UninspectedSpan.Empty() || len(sequence.Frames) == 0 {
			result.Reason = "unparsed_audio_suffix"
			if sequence.StopReason == "frame_limit" {
				result.Reason = "frame_budget"
			}
			return result, nil
		}
		for index := len(sequence.Frames) - 1; index >= 0; index-- {
			if err = ctx.Err(); err != nil {
				return result, err
			}
			if !add(id, sequence.Frames[index]) {
				return result, nil
			}
			if result.ResolvedPreviousBytes == back {
				result.State, result.Reason = "resolved", "across_tags"
				return result, nil
			}
		}
		if page != nil && len(sequence.Frames) != 0 && sequence.Frames[0].Index != 0 {
			result.Reason = "source_limit"
			return result, nil
		}
	}
	result.Reason = "start_of_file"
	return result, nil
}

func usable(f mp3.Frame) string {
	if !f.Header.Complete || !f.BoundaryComplete || f.Dependency == nil {
		return "invalid_frame"
	}
	if f.CRC.Status != "not_protected" && f.CRC.Status != "match" {
		return "crc_invalid"
	}
	if f.SideInfo == nil || !f.SideInfo.Complete || f.SideInfo.MainDataBegin == nil || f.Dependency.Part23Bits == nil {
		return "invalid_side_info"
	}
	if uint64(*f.Dependency.Part23Bits) > (uint64(f.SideInfo.MainDataBegin.Value)+uint64(f.Dependency.MainDataSpan.BitLength/8))*8 {
		return "invalid_main_data_length"
	}
	return ""
}

type boundedReader struct {
	ctx        context.Context
	source     io.ReaderAt
	result     *Resolution
	start, end uint64
	failure    error
}

func (r *boundedReader) ReadAt(p []byte, offset int64) (n int, err error) {
	if r.failure != nil {
		return 0, r.failure
	}
	defer func() {
		if err != nil {
			r.failure = err
		}
	}()
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	if offset < 0 || uint64(offset) < r.start || uint64(offset) > r.end || uint64(len(p)) > r.end-uint64(offset) {
		return 0, ErrInvalidInput
	}
	if uint64(len(p)) > r.result.Limits.MaxReadBytes-r.result.BytesRead {
		return 0, errReadBudget
	}
	n, err = r.source.ReadAt(p, offset)
	if n < 0 || n > len(p) {
		return 0, ErrInvalidInput
	}
	r.result.BytesRead += uint64(n)
	if canceled := r.ctx.Err(); canceled != nil {
		return n, canceled
	}
	if n == len(p) && errors.Is(err, io.EOF) {
		return n, nil
	}
	if n != len(p) && err == nil {
		err = io.ErrUnexpectedEOF
	}
	if err != nil {
		return n, fmt.Errorf("mp3context: read at %d: %w", offset, err)
	}
	return n, nil
}
