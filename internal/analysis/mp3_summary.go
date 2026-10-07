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
	DefaultMaxMP3HeaderEvents uint32 = 256
	HardMaxMP3HeaderEvents    uint32 = 65_536
)

// OptionsV2 adds a retained MP3 observation budget to the existing budgets.
// Every MP3 fixed header is analyzed even after the retained prefix is full.
type OptionsV2 struct {
	Options            Options
	MaxMP3HeaderEvents uint32
}

func (o OptionsV2) Normalized() (OptionsV2, error) {
	var err error
	if o.Options, err = o.Options.Normalized(); err != nil {
		return OptionsV2{}, err
	}
	if o.MaxMP3HeaderEvents == 0 {
		o.MaxMP3HeaderEvents = DefaultMaxMP3HeaderEvents
	} else if o.MaxMP3HeaderEvents > HardMaxMP3HeaderEvents {
		return OptionsV2{}, fmt.Errorf("analysis: max MP3 header events %d exceeds hard limit %d", o.MaxMP3HeaderEvents, HardMaxMP3HeaderEvents)
	}
	return o, nil
}

// ResultV2 is deliberately distinct from Result so a v2 analysis is not
// implicitly passed to a v1 report constructor.
type ResultV2 struct {
	Result
	AudioCodecs AudioCodecCounts
	MP3         MP3Summary
}

// AudioCodecCounts partitions all audio Tags by trustworthy SoundFormat.
// SwitchCount compares consecutive known identities, including other codecs;
// unknown identities clear that comparison baseline.
type AudioCodecCounts struct {
	AACTagCount     uint64
	MP3TagCount     uint64
	OtherTagCount   uint64
	UnknownTagCount uint64
	SwitchCount     uint64
}

type MP3Summary struct {
	Track   Track
	Headers MP3Headers
}

// MP3Parameters contains only complete first-header facts. Equality of these
// values is a parameter comparison, not a decoded stream-configuration proof.
type MP3Parameters struct {
	Version             mp3.Version
	SamplingFrequencyHz uint32
	ChannelMode         mp3.ChannelMode
	Channels            uint8
	SamplesPerFrame     uint16
}

type MP3HeaderEvent struct {
	TagID                         uint64
	Offset                        uint64
	DTS                           int64
	Complete                      bool
	Parameters                    MP3Parameters
	BitrateMode                   mp3.BitrateMode
	BitrateKbps                   uint16
	ComparedToPrevious            bool
	ParametersChangedFromPrevious bool
	BitrateChangedFromPrevious    bool
}

type MP3Headers struct {
	EventCount           uint64
	ValidCount           uint64
	MalformedCount       uint64
	FreeFormatCount      uint64
	ParameterChangeCount uint64
	BitrateChangeCount   uint64
	Events               []MP3HeaderEvent
	Truncated            bool
}

// AnalyzeV2 opts in to MP3 classification without changing Analyze's v1 scope.
// It reads one FLV audio byte and at most four MPEG header bytes per MP3 Tag.
func AnalyzeV2(ctx context.Context, source io.ReaderAt, scan flv.ScanResult, options OptionsV2) (ResultV2, error) {
	return AnalyzeV2WithProgress(ctx, source, scan, options, nil)
}

// AnalyzeV2WithProgress preserves AnalyzeV2's result and error semantics while
// reporting completed visits synchronously: initially, every 1024 Tags, and at
// the final completed position. Equal consecutive progress is not repeated.
// An error may leave the returned aggregate midway through a Tag; progress
// excludes that Tag, and callers must not publish the aggregate as complete.
func AnalyzeV2WithProgress(ctx context.Context, source io.ReaderAt, scan flv.ScanResult, options OptionsV2, progress func(Progress)) (ResultV2, error) {
	normalized, err := options.Normalized()
	if err != nil {
		return ResultV2{}, err
	}
	state := summaryV2State{headerLimit: normalized.MaxMP3HeaderEvents}
	result, err := analyze(ctx, source, scan, normalized.Options, &state, progress)
	return ResultV2{Result: result, AudioCodecs: state.audioCodecs, MP3: state.mp3}, err
}

type summaryV2State struct {
	audioCodecs         AudioCodecCounts
	mp3                 MP3Summary
	headerLimit         uint32
	previousSoundFormat uint8
	haveSoundFormat     bool
	previousMP3Header   MP3HeaderEvent
	haveMP3Header       bool
	mp3Bitrate          bitrateAccumulator
	curves              *mediaCurveCollector
}

func (a *analyzer) unknownAudioV2() {
	if a.v2 == nil {
		return
	}
	a.v2.audioCodecs.UnknownTagCount++
	a.v2.haveSoundFormat = false
	a.v2.haveMP3Header = false
	a.breakAudioCoverageV2()
}

func (a *analyzer) observeAudioCodecV2(soundFormat uint8) {
	v2 := a.v2
	switch soundFormat {
	case flv.SoundFormatAAC:
		v2.audioCodecs.AACTagCount++
	case flv.SoundFormatMP3:
		v2.audioCodecs.MP3TagCount++
	default:
		v2.audioCodecs.OtherTagCount++
	}
	if v2.haveSoundFormat && soundFormat != v2.previousSoundFormat {
		v2.audioCodecs.SwitchCount++
		a.breakAudioCoverageV2()
		v2.haveMP3Header = false
	}
	v2.previousSoundFormat, v2.haveSoundFormat = soundFormat, true
}

// Discard only the uncovered group. Already committed intervals stay exact.
func (a *analyzer) breakAudioCoverageV2() {
	a.audioBitrate.present, a.audioBitrate.pendingBytes = false, 0
	a.v2.mp3Bitrate.present, a.v2.mp3Bitrate.pendingBytes = false, 0
}

func (a *analyzer) visitMP3(tag flv.TagRecord) error {
	v2 := a.v2
	v2.mp3.Track.TagCount++
	v2.mp3.Track.IndexedDataSizeBytes += uint64(tag.DataSize)
	parsed, err := mp3.ParseFrameHeader(a.source, cursor.Span{
		ByteOffset: tag.PayloadSpan().ByteOffset + 1,
		BitLength:  (tag.DataSize - 1) * 8,
	}, mp3.Options{})
	if err != nil {
		return fmt.Errorf("analysis: parse MP3 first header tag %d: %w", tag.ID, err)
	}
	for _, item := range parsed.Diagnostics {
		a.diagnostics.add(Diagnostic{
			Code: item.Code, Severity: Severity(item.Severity), TagID: tagID(tag.ID),
			Offset: item.Offset, BitOffset: item.BitOffset, Message: item.Message,
			Expected: cloneNumber(item.Expected), Actual: cloneNumber(item.Actual), RecoveryOffset: cloneNumber(item.RecoveryOffset),
		})
	}
	event := MP3HeaderEvent{TagID: tag.ID, Offset: tag.Offset, DTS: tag.DTS, Complete: parsed.Complete}
	headers := &v2.mp3.Headers
	headers.EventCount++
	if !parsed.Complete {
		headers.MalformedCount++
		v2.mp3.Track.UnclassifiedTagCount++
		a.result.Audio.UnclassifiedTagCount++
		a.result.Complete = false
		v2.haveMP3Header = false
		a.breakAudioCoverageV2()
	} else {
		headers.ValidCount++
		header := parsed.Header
		event.Parameters = MP3Parameters{
			Version: header.Version, SamplingFrequencyHz: header.SamplingFrequencyHz,
			ChannelMode: header.ChannelMode, Channels: header.Channels, SamplesPerFrame: header.SamplesPerFrame,
		}
		event.BitrateMode, event.BitrateKbps = header.BitrateMode, header.BitrateKbps
		if header.BitrateMode == mp3.BitrateModeFree {
			headers.FreeFormatCount++
		}
		if v2.haveMP3Header && tag.DTS < v2.previousMP3Header.DTS {
			v2.haveMP3Header = false
			a.breakAudioCoverageV2()
		}
		if v2.haveMP3Header {
			previous := v2.previousMP3Header
			event.ComparedToPrevious = true
			event.ParametersChangedFromPrevious = event.Parameters != previous.Parameters
			event.BitrateChangedFromPrevious = event.BitrateMode != previous.BitrateMode || event.BitrateKbps != previous.BitrateKbps
			if event.ParametersChangedFromPrevious {
				headers.ParameterChangeCount++
				a.breakAudioCoverageV2()
			}
			if event.BitrateChangedFromPrevious {
				headers.BitrateChangeCount++
			}
		}
		v2.previousMP3Header, v2.haveMP3Header = event, true
		a.addMedia(&a.result.Audio, &a.audioBitrate, tag)
		a.addMedia(&v2.mp3.Track, &v2.mp3Bitrate, tag)
	}
	if uint32(len(headers.Events)) < v2.headerLimit {
		headers.Events = append(headers.Events, event)
	} else {
		headers.Truncated = true
	}
	return nil
}
