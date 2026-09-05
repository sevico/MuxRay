package report

import (
	"encoding/json"
	"io"

	"github.com/sevico/muxray/internal/analysis"
	"github.com/sevico/muxray/internal/flv"
	"github.com/sevico/muxray/internal/mp3"
)

const MediaSummaryV2SchemaVersion = 2

type MediaSummaryDocumentV2 struct {
	SchemaVersion       int                      `json:"schemaVersion"`
	DocumentType        string                   `json:"documentType"`
	Source              Source                   `json:"source"`
	Scan                MediaSummaryScan         `json:"scan"`
	Summary             MediaSummaryV2           `json:"summary"`
	ScanDiagnostics     []Diagnostic             `json:"scanDiagnostics"`
	AnalysisDiagnostics []MediaSummaryDiagnostic `json:"analysisDiagnostics"`
}

type MediaSummaryV2 struct {
	MediaSummary
	AudioCodecs MediaAudioCodecCounts `json:"audioCodecs"`
	MP3         MediaMP3Summary       `json:"mp3"`
}

type MediaAudioCodecCounts struct {
	AACTagCount     uint64 `json:"aacTagCount"`
	MP3TagCount     uint64 `json:"mp3TagCount"`
	OtherTagCount   uint64 `json:"otherTagCount"`
	UnknownTagCount uint64 `json:"unknownTagCount"`
	SwitchCount     uint64 `json:"switchCount"`
}

type MediaMP3Summary struct {
	Track   MediaTrackSummary `json:"track"`
	Headers MediaMP3Headers   `json:"headers"`
}

type MediaMP3Headers struct {
	EventCount           uint64                `json:"eventCount"`
	ValidCount           uint64                `json:"validCount"`
	MalformedCount       uint64                `json:"malformedCount"`
	FreeFormatCount      uint64                `json:"freeFormatCount"`
	ParameterChangeCount uint64                `json:"parameterChangeCount"`
	BitrateChangeCount   uint64                `json:"bitrateChangeCount"`
	Events               []MediaMP3HeaderEvent `json:"events"`
	Truncated            bool                  `json:"truncated"`
}

type MediaMP3HeaderEvent struct {
	TagID                         uint64              `json:"tagId"`
	Offset                        uint64              `json:"offset"`
	DTS                           int64               `json:"dts"`
	Complete                      bool                `json:"complete"`
	Parameters                    *MediaMP3Parameters `json:"parameters,omitempty"`
	BitrateMode                   mp3.BitrateMode     `json:"bitrateMode,omitempty"`
	BitrateKbps                   *uint16             `json:"bitrateKbps,omitempty"`
	ParametersChangedFromPrevious *bool               `json:"parametersChangedFromPrevious,omitempty"`
	BitrateChangedFromPrevious    *bool               `json:"bitrateChangedFromPrevious,omitempty"`
}

type MediaMP3Parameters struct {
	Version             MediaNamedUint8 `json:"version"`
	SamplingFrequencyHz uint32          `json:"samplingFrequencyHz"`
	ChannelMode         MediaNamedUint8 `json:"channelMode"`
	Channels            uint8           `json:"channels"`
	SamplesPerFrame     uint16          `json:"samplesPerFrame"`
}

func NewMediaSummaryV2(scan flv.ScanResult, result analysis.ResultV2) MediaSummaryDocumentV2 {
	base := NewMediaSummary(scan, result.Result)
	counts := result.AudioCodecs
	headers := result.MP3.Headers
	document := MediaSummaryDocumentV2{
		SchemaVersion: MediaSummaryV2SchemaVersion, DocumentType: MediaSummaryDocumentType,
		Source: base.Source, Scan: base.Scan,
		Summary: MediaSummaryV2{
			MediaSummary: base.Summary,
			AudioCodecs: MediaAudioCodecCounts{
				AACTagCount: counts.AACTagCount, MP3TagCount: counts.MP3TagCount,
				OtherTagCount: counts.OtherTagCount, UnknownTagCount: counts.UnknownTagCount, SwitchCount: counts.SwitchCount,
			},
			MP3: MediaMP3Summary{
				Track: mapMediaTrack(result.MP3.Track),
				Headers: MediaMP3Headers{
					EventCount: headers.EventCount, ValidCount: headers.ValidCount, MalformedCount: headers.MalformedCount,
					FreeFormatCount: headers.FreeFormatCount, ParameterChangeCount: headers.ParameterChangeCount,
					BitrateChangeCount: headers.BitrateChangeCount,
					Events:             make([]MediaMP3HeaderEvent, 0, len(headers.Events)), Truncated: headers.Truncated,
				},
			},
		},
		ScanDiagnostics: base.ScanDiagnostics, AnalysisDiagnostics: base.AnalysisDiagnostics,
	}
	for _, event := range headers.Events {
		item := MediaMP3HeaderEvent{TagID: event.TagID, Offset: event.Offset, DTS: event.DTS, Complete: event.Complete}
		if event.Complete {
			p := event.Parameters
			item.Parameters = &MediaMP3Parameters{
				Version:             MediaNamedUint8{Value: uint8(p.Version), Name: p.Version.String()},
				SamplingFrequencyHz: p.SamplingFrequencyHz,
				ChannelMode:         MediaNamedUint8{Value: uint8(p.ChannelMode), Name: p.ChannelMode.String()},
				Channels:            p.Channels, SamplesPerFrame: p.SamplesPerFrame,
			}
			item.BitrateMode = event.BitrateMode
			if event.BitrateMode == mp3.BitrateModeIndexed {
				value := event.BitrateKbps
				item.BitrateKbps = &value
			}
			if event.ComparedToPrevious {
				parametersChanged, bitrateChanged := event.ParametersChangedFromPrevious, event.BitrateChangedFromPrevious
				item.ParametersChangedFromPrevious, item.BitrateChangedFromPrevious = &parametersChanged, &bitrateChanged
			}
		}
		document.Summary.MP3.Headers.Events = append(document.Summary.MP3.Headers.Events, item)
	}
	return document
}

func WriteMediaSummaryV2JSON(output io.Writer, document MediaSummaryDocumentV2, pretty bool) error {
	encoder := json.NewEncoder(output)
	encoder.SetEscapeHTML(false)
	if pretty {
		encoder.SetIndent("", "  ")
	}
	return encoder.Encode(document)
}
