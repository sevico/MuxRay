package report

import (
	"encoding/json"
	"io"

	"github.com/sevico/muxray/internal/analysis"
	"github.com/sevico/muxray/internal/flv"
)

const MediaSummaryV3SchemaVersion = 3

type MediaSummaryDocumentV3 struct {
	SchemaVersion       int                      `json:"schemaVersion"`
	DocumentType        string                   `json:"documentType"`
	Source              Source                   `json:"source"`
	Scan                MediaSummaryScan         `json:"scan"`
	Summary             MediaSummaryV3           `json:"summary"`
	ScanDiagnostics     []Diagnostic             `json:"scanDiagnostics"`
	AnalysisDiagnostics []MediaSummaryDiagnostic `json:"analysisDiagnostics"`
}
type MediaSummaryV3 struct {
	MediaSummaryV2
	MP3Frames MediaMP3Frames `json:"mp3Frames"`
}

type MediaMP3Frames struct {
	Complete                 bool                  `json:"complete"`
	ScanComplete             bool                  `json:"scanComplete"`
	TagCount                 uint64                `json:"tagCount"`
	CompleteTagCount         uint64                `json:"completeTagCount"`
	PartialTagCount          uint64                `json:"partialTagCount"`
	UnknownAudioTagCount     uint64                `json:"unknownAudioTagCount"`
	UnsupportedAudioTagCount uint64                `json:"unsupportedAudioTagCount"`
	SoundDataBytes           uint64                `json:"soundDataBytes"`
	FrameBytes               uint64                `json:"frameBytes"`
	UnresolvedBytes          uint64                `json:"unresolvedBytes"`
	AttemptedHeaderCount     uint64                `json:"attemptedHeaderCount"`
	HeaderBytesRead          uint64                `json:"headerBytesRead"`
	CompleteFrameCount       uint64                `json:"completeFrameCount"`
	SampleCount              uint64                `json:"sampleCount"`
	ParameterChangeCount     uint64                `json:"parameterChangeCount"`
	BitrateChangeCount       uint64                `json:"bitrateChangeCount"`
	TruncatedHeaderCount     uint64                `json:"truncatedHeaderCount"`
	MalformedHeaderCount     uint64                `json:"malformedHeaderCount"`
	UnsupportedLayerCount    uint64                `json:"unsupportedLayerCount"`
	TruncatedFrameCount      uint64                `json:"truncatedFrameCount"`
	FreeFormatCount          uint64                `json:"freeFormatCount"`
	BudgetStoppedTagCount    uint64                `json:"budgetStoppedTagCount"`
	MaxFrames                uint64                `json:"maxFrames"`
	MaxEvents                uint32                `json:"maxEvents"`
	EventCount               uint64                `json:"eventCount"`
	Events                   []MediaMP3FrameEvent  `json:"events"`
	EventsTruncated          bool                  `json:"eventsTruncated"`
	DurationSeconds          MediaMP3Duration      `json:"durationSeconds"`
	Rates                    []MediaMP3RateSamples `json:"rates"`
}
type MediaMP3Duration struct {
	Numerator   uint64 `json:"numerator"`
	Denominator uint64 `json:"denominator"`
}
type MediaMP3RateSamples struct {
	SamplingFrequencyHz uint32 `json:"samplingFrequencyHz"`
	FrameCount          uint64 `json:"frameCount"`
	SampleCount         uint64 `json:"sampleCount"`
}
type MediaMP3FrameEvent struct {
	TagID                         uint64              `json:"tagId"`
	FrameIndex                    uint32              `json:"frameIndex"`
	DTS                           int64               `json:"dts"`
	SourceSpan                    Span                `json:"sourceSpan"`
	Status                        string              `json:"status"`
	Parameters                    *MediaMP3Parameters `json:"parameters,omitempty"`
	BitrateKbps                   *uint16             `json:"bitrateKbps,omitempty"`
	ParametersChangedFromPrevious *bool               `json:"parametersChangedFromPrevious,omitempty"`
	BitrateChangedFromPrevious    *bool               `json:"bitrateChangedFromPrevious,omitempty"`
}

func NewMediaSummaryV3(scan flv.ScanResult, result analysis.ResultV3) MediaSummaryDocumentV3 {
	base := NewMediaSummaryV2(scan, result.ResultV2)
	s := result.MP3Frames
	out := MediaMP3Frames{
		Complete: s.Complete, ScanComplete: s.ScanComplete, TagCount: s.TagCount,
		CompleteTagCount: s.CompleteTagCount, PartialTagCount: s.PartialTagCount,
		UnknownAudioTagCount: s.UnknownAudioTagCount, UnsupportedAudioTagCount: s.UnsupportedAudioTagCount,
		SoundDataBytes: s.SoundDataBytes, FrameBytes: s.FrameBytes, UnresolvedBytes: s.UnresolvedBytes,
		AttemptedHeaderCount: s.AttemptedHeaderCount, HeaderBytesRead: s.HeaderBytesRead,
		CompleteFrameCount: s.CompleteFrameCount, SampleCount: s.SampleCount,
		ParameterChangeCount: s.ParameterChangeCount, BitrateChangeCount: s.BitrateChangeCount,
		TruncatedHeaderCount: s.TruncatedHeaderCount, MalformedHeaderCount: s.MalformedHeaderCount,
		UnsupportedLayerCount: s.UnsupportedLayerCount, TruncatedFrameCount: s.TruncatedFrameCount,
		FreeFormatCount: s.FreeFormatCount, BudgetStoppedTagCount: s.BudgetStoppedTagCount,
		MaxFrames: s.MaxFrames, MaxEvents: s.MaxEvents, EventCount: s.EventCount, EventsTruncated: s.EventsTruncated,
		DurationSeconds: MediaMP3Duration{Numerator: s.DurationSeconds.Numerator, Denominator: s.DurationSeconds.Denominator},
		Events:          make([]MediaMP3FrameEvent, 0, len(s.Events)), Rates: make([]MediaMP3RateSamples, 0, len(s.Rates)),
	}
	for _, rate := range s.Rates {
		out.Rates = append(out.Rates, MediaMP3RateSamples{SamplingFrequencyHz: rate.SamplingFrequencyHz, FrameCount: rate.FrameCount, SampleCount: rate.SampleCount})
	}
	for _, event := range s.Events {
		item := MediaMP3FrameEvent{TagID: event.TagID, FrameIndex: event.FrameIndex, DTS: event.DTS, SourceSpan: mapSpan(event.SourceSpan), Status: event.Status}
		if event.ParametersPresent {
			p := event.Parameters
			item.Parameters = &MediaMP3Parameters{Version: MediaNamedUint8{Value: uint8(p.Version), Name: p.Version.String()}, SamplingFrequencyHz: p.SamplingFrequencyHz, ChannelMode: MediaNamedUint8{Value: uint8(p.ChannelMode), Name: p.ChannelMode.String()}, Channels: p.Channels, SamplesPerFrame: p.SamplesPerFrame}
		}
		if event.BitratePresent {
			value := event.BitrateKbps
			item.BitrateKbps = &value
		}
		if event.ComparedToPrevious {
			changed, bitrate := event.ParametersChangedFromPrevious, event.BitrateChangedFromPrevious
			item.ParametersChangedFromPrevious, item.BitrateChangedFromPrevious = &changed, &bitrate
		}
		out.Events = append(out.Events, item)
	}
	return MediaSummaryDocumentV3{SchemaVersion: MediaSummaryV3SchemaVersion, DocumentType: MediaSummaryDocumentType, Source: base.Source, Scan: base.Scan, Summary: MediaSummaryV3{MediaSummaryV2: base.Summary, MP3Frames: out}, ScanDiagnostics: base.ScanDiagnostics, AnalysisDiagnostics: base.AnalysisDiagnostics}
}
func WriteMediaSummaryV3JSON(output io.Writer, document MediaSummaryDocumentV3, pretty bool) error {
	encoder := json.NewEncoder(output)
	encoder.SetEscapeHTML(false)
	if pretty {
		encoder.SetIndent("", "  ")
	}
	return encoder.Encode(document)
}
