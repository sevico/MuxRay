package analysis

import (
	"context"
	"fmt"
	"io"

	"github.com/sevico/muxray/internal/cursor"
	"github.com/sevico/muxray/internal/flv"
	"github.com/sevico/muxray/internal/mp3"
)

const (
	DefaultMaxMP3FrameEvents uint32 = 256
	HardMaxMP3FrameEvents    uint32 = 65_536
	DefaultMaxMP3Frames      uint64 = 1_000_000
	HardMaxMP3Frames         uint64 = 16_000_000
	// All nine supported MPEG sampling frequencies divide this value exactly.
	mp3DurationTimescale uint64 = 14_112_000
)

// OptionsV3 opts in to sequential MP3 frame coverage over the entire index.
// MaxMP3Frames bounds attempted headers, independently of retained events.
// Each attempted header needs at most four bytes; bodies stay in the source.
// The existing v2 first-header reads and AAC/AVC budgets remain independent.
type OptionsV3 struct {
	OptionsV2
	MaxMP3FrameEvents uint32
	MaxMP3Frames      uint64
}

func (o OptionsV3) Normalized() (OptionsV3, error) {
	var err error
	if o.OptionsV2, err = o.OptionsV2.Normalized(); err != nil {
		return OptionsV3{}, err
	}
	if o.MaxMP3FrameEvents == 0 {
		o.MaxMP3FrameEvents = DefaultMaxMP3FrameEvents
	}
	if o.MaxMP3FrameEvents > HardMaxMP3FrameEvents {
		return OptionsV3{}, fmt.Errorf("analysis: max MP3 frame events %d exceeds hard limit %d", o.MaxMP3FrameEvents, HardMaxMP3FrameEvents)
	}
	if o.MaxMP3Frames == 0 {
		o.MaxMP3Frames = DefaultMaxMP3Frames
	}
	if o.MaxMP3Frames > HardMaxMP3Frames {
		return OptionsV3{}, fmt.Errorf("analysis: max MP3 frames %d exceeds hard limit %d", o.MaxMP3Frames, HardMaxMP3Frames)
	}
	return o, nil
}

type ResultV3 struct {
	ResultV2
	MP3Frames MP3Frames
}

// MP3Frames counts complete declared frame boundaries, not decoded audio or
// CRC/side-info validity. Complete means the scan finished without errors and
// every audio Tag had a supported identity, with all known MP3 SoundData framed
// successfully. ScanComplete preserves scan.Complete independently of errors.
// FrameBytes + UnresolvedBytes equals SoundDataBytes on a successful analysis;
// error returns may contain only a prefix and must never be published complete.
type MP3Frames struct {
	Complete                                                               bool
	ScanComplete                                                           bool
	TagCount, CompleteTagCount, PartialTagCount                            uint64
	UnknownAudioTagCount, UnsupportedAudioTagCount                         uint64
	SoundDataBytes, FrameBytes, UnresolvedBytes                            uint64
	AttemptedHeaderCount, HeaderBytesRead, CompleteFrameCount, SampleCount uint64
	ParameterChangeCount, BitrateChangeCount                               uint64
	TruncatedHeaderCount, MalformedHeaderCount, UnsupportedLayerCount      uint64
	TruncatedFrameCount, FreeFormatCount, BudgetStoppedTagCount            uint64
	MaxFrames                                                              uint64
	MaxEvents                                                              uint32
	EventCount                                                             uint64
	Events                                                                 []MP3FrameEvent
	EventsTruncated                                                        bool
	DurationSeconds                                                        MP3Duration
	Rates                                                                  []MP3RateSamples
}

// MP3Duration is an exact reduced fraction. It sums samples/rate for observed
// complete frames, including rate changes; it is not the file's playback time.
type MP3Duration struct{ Numerator, Denominator uint64 }
type MP3RateSamples struct {
	SamplingFrequencyHz     uint32
	FrameCount, SampleCount uint64
}

// FrameIndex is zero-based inside its Tag. DTS is the containing Tag timestamp,
// never an invented frame timestamp. A stop event spans the unresolved suffix.
type MP3FrameEvent struct {
	TagID                                                                         uint64
	FrameIndex                                                                    uint32
	DTS                                                                           int64
	SourceSpan                                                                    cursor.Span
	Status                                                                        string
	Parameters                                                                    MP3Parameters
	ParametersPresent                                                             bool
	BitrateKbps                                                                   uint16
	BitratePresent                                                                bool
	ComparedToPrevious, ParametersChangedFromPrevious, BitrateChangedFromPrevious bool
}

func AnalyzeV3(ctx context.Context, source io.ReaderAt, scan flv.ScanResult, options OptionsV3) (ResultV3, error) {
	return AnalyzeV3WithProgress(ctx, source, scan, options, nil)
}
func AnalyzeV3WithProgress(ctx context.Context, source io.ReaderAt, scan flv.ScanResult, options OptionsV3, progress func(Progress)) (ResultV3, error) {
	result, _, err := analyzeV3(ctx, source, scan, options, progress, false)
	return result, err
}

// AnalyzeV3WithCurves adds frame observations without changing the existing
// Tag-window curve classification, DTS coverage, or byte-count semantics.
func AnalyzeV3WithCurves(ctx context.Context, source io.ReaderAt, scan flv.ScanResult, options OptionsV3, progress func(Progress)) (ResultV3, *MediaCurves, error) {
	return analyzeV3(ctx, source, scan, options, progress, true)
}
func analyzeV3(ctx context.Context, source io.ReaderAt, scan flv.ScanResult, options OptionsV3, progress func(Progress), curves bool) (ResultV3, *MediaCurves, error) {
	normalized, err := options.Normalized()
	if err != nil {
		return ResultV3{}, nil, err
	}
	total := scan.Index().Len()
	if total > flv.HardMaxTags {
		return ResultV3{}, nil, fmt.Errorf("analysis: index exceeds hard tag limit %d", flv.HardMaxTags)
	}
	frames := mp3FrameState{summary: MP3Frames{ScanComplete: scan.Complete, MaxFrames: normalized.MaxMP3Frames, MaxEvents: normalized.MaxMP3FrameEvents, Events: []MP3FrameEvent{}, Rates: []MP3RateSamples{}}}
	state := summaryV2State{headerLimit: normalized.MaxMP3HeaderEvents, frames: &frames}
	if curves {
		state.curves = &mediaCurveCollector{result: MediaCurves{IndexedTags: total, Points: make([]MediaCurvePoint, 0, int((total+CurveWindowTags-1)/CurveWindowTags))}}
	}
	base, err := analyze(ctx, source, scan, normalized.Options, &state, progress)
	if err == nil && ctx != nil && ctx.Err() != nil {
		err = ctx.Err()
	}
	frames.finish(err == nil && !scan.HasErrors())
	result := ResultV3{ResultV2: ResultV2{Result: base, AudioCodecs: state.audioCodecs, MP3: state.mp3}, MP3Frames: frames.summary}
	result.Complete = result.Complete && result.MP3Frames.Complete && err == nil
	if err != nil || !curves {
		return result, nil, err
	}
	state.curves.finish()
	return result, &state.curves.result, nil
}

type mp3FrameState struct {
	summary       MP3Frames
	previous      MP3FrameEvent
	havePrevious  bool
	durationTicks uint64
}

func (a *analyzer) visitMP3Frames(tag flv.TagRecord, first mp3.Result) error {
	f := a.v2.frames
	s := &f.summary
	s.TagCount++
	s.SoundDataBytes += uint64(tag.DataSize - 1)
	if f.havePrevious && tag.DTS < f.previous.DTS {
		f.havePrevious = false
	}
	offset := tag.PayloadSpan().ByteOffset + 1
	remaining := tag.DataSize - 1
	var frameIndex uint32
	// Empty SoundData is a truncated header, not a successfully empty sequence.
	for {
		if err := a.ctx.Err(); err != nil {
			return err
		}
		event := MP3FrameEvent{TagID: tag.ID, FrameIndex: frameIndex, DTS: tag.DTS, SourceSpan: cursor.Span{ByteOffset: offset, BitLength: remaining * 8}}
		if s.AttemptedHeaderCount == s.MaxFrames {
			s.BudgetStoppedTagCount++
			f.stop(event, "frameBudget", uint64(remaining))
			return nil
		}
		s.AttemptedHeaderCount++
		parsed := first
		if frameIndex != 0 {
			var err error
			parsed, err = mp3.ParseFrameHeader(a.source, cursor.Span{ByteOffset: offset, BitLength: min(remaining, 4) * 8}, mp3.Options{InspectFrames: true})
			if err != nil {
				return fmt.Errorf("analysis: parse MP3 tag %d frame %d: %w", tag.ID, frameIndex, err)
			}
		}
		if remaining >= 4 {
			s.HeaderBytesRead += 4
		}
		switch {
		case remaining < 4:
			s.TruncatedHeaderCount++
			f.stop(event, "truncatedHeader", uint64(remaining))
			return nil
		case !parsed.Complete:
			status := "malformedHeader"
			if parsed.Header.SyncWord == 0x7ff && (parsed.Header.Layer == mp3.LayerI || parsed.Header.Layer == mp3.LayerII) {
				status = "unsupportedLayer"
				s.UnsupportedLayerCount++
			} else {
				s.MalformedHeaderCount++
			}
			f.stop(event, status, uint64(remaining))
			return nil
		}
		header := parsed.Header
		event.ParametersPresent = true
		event.Parameters = MP3Parameters{Version: header.Version, SamplingFrequencyHz: header.SamplingFrequencyHz, ChannelMode: header.ChannelMode, Channels: header.Channels, SamplesPerFrame: header.SamplesPerFrame}
		event.BitratePresent, event.BitrateKbps = header.BitratePresent, header.BitrateKbps
		if !header.FrameLengthPresent {
			s.FreeFormatCount++
			f.stop(event, "freeFormat", uint64(remaining))
			return nil
		}
		if header.FrameLengthBytes > remaining {
			s.TruncatedFrameCount++
			f.stop(event, "truncatedFrame", uint64(remaining))
			return nil
		}
		event.Status, event.SourceSpan.BitLength = "complete", header.FrameLengthBytes*8
		if f.havePrevious {
			event.ComparedToPrevious = true
			event.ParametersChangedFromPrevious = event.Parameters != f.previous.Parameters
			event.BitrateChangedFromPrevious = event.BitrateKbps != f.previous.BitrateKbps
			if event.ParametersChangedFromPrevious {
				s.ParameterChangeCount++
			}
			if event.BitrateChangedFromPrevious {
				s.BitrateChangeCount++
			}
		}
		f.previous, f.havePrevious = event, true
		s.CompleteFrameCount++
		s.FrameBytes += uint64(header.FrameLengthBytes)
		s.SampleCount += uint64(header.SamplesPerFrame)
		f.durationTicks += uint64(header.SamplesPerFrame) * (mp3DurationTimescale / uint64(header.SamplingFrequencyHz))
		f.addRate(header.SamplingFrequencyHz, uint64(header.SamplesPerFrame))
		f.retain(event)
		remaining -= header.FrameLengthBytes
		if remaining == 0 {
			s.CompleteTagCount++
			return nil
		}
		offset += uint64(header.FrameLengthBytes)
		frameIndex++
	}
}

func (f *mp3FrameState) stop(event MP3FrameEvent, status string, remaining uint64) {
	event.Status = status
	f.summary.PartialTagCount++
	f.summary.UnresolvedBytes += remaining
	f.havePrevious = false
	f.retain(event)
}
func (f *mp3FrameState) retain(event MP3FrameEvent) {
	f.summary.EventCount++
	if uint32(len(f.summary.Events)) < f.summary.MaxEvents {
		f.summary.Events = append(f.summary.Events, event)
	} else {
		f.summary.EventsTruncated = true
	}
}
func (f *mp3FrameState) addRate(rate uint32, samples uint64) {
	for i := range f.summary.Rates {
		if f.summary.Rates[i].SamplingFrequencyHz == rate {
			f.summary.Rates[i].FrameCount++
			f.summary.Rates[i].SampleCount += samples
			return
		}
	}
	f.summary.Rates = append(f.summary.Rates, MP3RateSamples{SamplingFrequencyHz: rate, FrameCount: 1, SampleCount: samples})
}
func (f *mp3FrameState) finish(success bool) {
	s := &f.summary
	s.Complete = success && s.ScanComplete && s.PartialTagCount == 0 && s.UnknownAudioTagCount == 0 && s.UnsupportedAudioTagCount == 0
	numerator, denominator := f.durationTicks, mp3DurationTimescale
	a, b := numerator, denominator
	for b != 0 {
		a, b = b, a%b
	}
	s.DurationSeconds = MP3Duration{Numerator: numerator / a, Denominator: denominator / a}
}
