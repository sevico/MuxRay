package report

import (
	"encoding/json"
	"io"

	"github.com/sevico/muxray/internal/analysis"
	"github.com/sevico/muxray/internal/flv"
)

const (
	MediaSummarySchemaVersion = 1
	MediaSummaryDocumentType  = "flv-media-summary"
)

// MediaSummaryDocument is the independently versioned public contract for
// bounded cross-tag media analysis. Scan and codec-detail contracts remain
// unchanged when this document evolves.
type MediaSummaryDocument struct {
	SchemaVersion       int                      `json:"schemaVersion"`
	DocumentType        string                   `json:"documentType"`
	Source              Source                   `json:"source"`
	Scan                MediaSummaryScan         `json:"scan"`
	Summary             MediaSummary             `json:"summary"`
	ScanDiagnostics     []Diagnostic             `json:"scanDiagnostics"`
	AnalysisDiagnostics []MediaSummaryDiagnostic `json:"analysisDiagnostics"`
}

type MediaSummaryScan struct {
	BytesScanned    uint64 `json:"bytesScanned"`
	Complete        bool   `json:"complete"`
	IndexedTagCount uint64 `json:"indexedTagCount"`
}

type MediaSummary struct {
	Complete        bool                   `json:"complete"`
	Tags            MediaTagCounts         `json:"tags"`
	Audio           MediaTrackSummary      `json:"audio"`
	Video           MediaTrackSummary      `json:"video"`
	VideoPTS        *MediaTimeline         `json:"videoPts,omitempty"`
	CompositionTime *MediaCompositionTime  `json:"compositionTime,omitempty"`
	Keyframes       MediaKeyframes         `json:"keyframes"`
	GOP             MediaGOP               `json:"gop"`
	AAC             MediaAACConfigurations `json:"aacConfigurations"`
	AVC             MediaAVCConfigurations `json:"avcConfigurations"`
	AVSync          *MediaAVSync           `json:"avSync,omitempty"`
}

type MediaTagCounts struct {
	Total         uint64 `json:"total"`
	Audio         uint64 `json:"audio"`
	Video         uint64 `json:"video"`
	Script        uint64 `json:"script"`
	Other         uint64 `json:"other"`
	DataSizeBytes uint64 `json:"dataSizeBytes"`
}

type MediaTrackSummary struct {
	TagCount             uint64         `json:"tagCount"`
	IndexedDataSizeBytes uint64         `json:"indexedDataSizeBytes"`
	MediaTagCount        uint64         `json:"mediaTagCount"`
	MediaDataSizeBytes   uint64         `json:"mediaDataSizeBytes"`
	ExcludedTagCount     uint64         `json:"excludedTagCount"`
	UnclassifiedTagCount uint64         `json:"unclassifiedTagCount"`
	Timing               *MediaTimeline `json:"timing,omitempty"`
	Bitrate              *MediaBitrate  `json:"bitrate,omitempty"`
}

type MediaTimeline struct {
	SampleCount         uint64 `json:"sampleCount"`
	FirstMS             int64  `json:"firstMs"`
	LastMS              int64  `json:"lastMs"`
	MinMS               int64  `json:"minMs"`
	MaxMS               int64  `json:"maxMs"`
	RangeMS             uint64 `json:"rangeMs"`
	ForwardDeltaTotalMS uint64 `json:"forwardDeltaTotalMs"`
	RegressionCount     uint64 `json:"regressionCount"`
	DuplicateCount      uint64 `json:"duplicateCount"`
	LargestForwardGapMS uint64 `json:"largestForwardGapMs"`
}

type MediaBitrate struct {
	Basis                string `json:"basis"`
	Bytes                uint64 `json:"bytes"`
	CoveredIntervalMS    uint64 `json:"coveredIntervalMs"`
	AverageBitsPerSecond uint64 `json:"averageBitsPerSecond"`
}

type MediaCompositionTime struct {
	SampleCount   uint64 `json:"sampleCount"`
	NegativeCount uint64 `json:"negativeCount"`
	ZeroCount     uint64 `json:"zeroCount"`
	PositiveCount uint64 `json:"positiveCount"`
	MinMS         int32  `json:"minMs"`
	MaxMS         int32  `json:"maxMs"`
}

type MediaKeyframe struct {
	TagID         uint64  `json:"tagId"`
	Offset        uint64  `json:"offset"`
	DTS           int64   `json:"dts"`
	PTS           *int64  `json:"pts,omitempty"`
	DataSizeBytes uint32  `json:"dataSizeBytes"`
	CodecID       uint8   `json:"codecId"`
	GOPDurationMS *uint64 `json:"gopDurationMs,omitempty"`
}

type MediaKeyframes struct {
	DeclaredCount uint64          `json:"declaredCount"`
	Entries       []MediaKeyframe `json:"entries"`
	Truncated     bool            `json:"truncated"`
}

type MediaGOP struct {
	Count                    uint64 `json:"count"`
	ClosedDurationCount      uint64 `json:"closedDurationCount"`
	NonPositiveDurationCount uint64 `json:"nonPositiveDurationCount"`
	MinDurationMS            uint64 `json:"minDurationMs"`
	MaxDurationMS            uint64 `json:"maxDurationMs"`
	AverageDurationMS        uint64 `json:"averageDurationMs"`
}

type MediaAACConfiguration struct {
	TagID                        uint64                     `json:"tagId"`
	Offset                       uint64                     `json:"offset"`
	DTS                          int64                      `json:"dts"`
	FingerprintSHA256            string                     `json:"fingerprintSha256"`
	ChangedFromPrevious          *bool                      `json:"changedFromPrevious,omitempty"`
	Complete                     bool                       `json:"complete"`
	AudioObjectType              *MediaNamedUint8           `json:"audioObjectType,omitempty"`
	SamplingFrequencyHz          *uint32                    `json:"samplingFrequencyHz,omitempty"`
	ExtensionSamplingFrequencyHz *uint32                    `json:"extensionSamplingFrequencyHz,omitempty"`
	ChannelConfiguration         *MediaChannelConfiguration `json:"channelConfiguration,omitempty"`
}

type MediaNamedUint8 struct {
	Value uint8  `json:"value"`
	Name  string `json:"name"`
}

type MediaChannelConfiguration struct {
	Value        uint8  `json:"value"`
	Name         string `json:"name"`
	ChannelCount *uint8 `json:"channelCount,omitempty"`
}

type MediaAACConfigurations struct {
	EventCount           uint64                  `json:"eventCount"`
	AnalyzedCount        uint64                  `json:"analyzedCount"`
	SkippedByBudgetCount uint64                  `json:"skippedByBudgetCount"`
	ChangeCount          uint64                  `json:"changeCount"`
	Events               []MediaAACConfiguration `json:"events"`
	Truncated            bool                    `json:"truncated"`
}

type MediaAVCConfiguration struct {
	TagID                 uint64            `json:"tagId"`
	Offset                uint64            `json:"offset"`
	DTS                   int64             `json:"dts"`
	FingerprintSHA256     string            `json:"fingerprintSha256"`
	ChangedFromPrevious   *bool             `json:"changedFromPrevious,omitempty"`
	Complete              bool              `json:"complete"`
	Profile               *MediaAVCProfile  `json:"profile,omitempty"`
	LengthSize            *uint8            `json:"lengthSize,omitempty"`
	CompleteSPSCount      uint32            `json:"completeSpsCount"`
	DistinctSPSDimensions uint32            `json:"distinctSpsDimensions"`
	FirstCompleteSPS      *MediaAVCFirstSPS `json:"firstCompleteSps,omitempty"`
}

type MediaAVCProfile struct {
	ProfileIDC           uint8 `json:"profileIdc"`
	ProfileCompatibility uint8 `json:"profileCompatibility"`
	LevelIDC             uint8 `json:"levelIdc"`
}

type MediaAVCFirstSPS struct {
	Index         uint32 `json:"index"`
	SPSID         uint32 `json:"spsId"`
	CodedWidth    uint64 `json:"codedWidth"`
	CodedHeight   uint64 `json:"codedHeight"`
	DisplayWidth  uint64 `json:"displayWidth"`
	DisplayHeight uint64 `json:"displayHeight"`
}

type MediaAVCConfigurations struct {
	EventCount           uint64                  `json:"eventCount"`
	AnalyzedCount        uint64                  `json:"analyzedCount"`
	SkippedByBudgetCount uint64                  `json:"skippedByBudgetCount"`
	ChangeCount          uint64                  `json:"changeCount"`
	Events               []MediaAVCConfiguration `json:"events"`
	Truncated            bool                    `json:"truncated"`
}

type MediaAVSync struct {
	Basis              string `json:"basis"`
	StartExtremaSkewMS int64  `json:"startExtremaSkewMs"`
	EndExtremaSkewMS   int64  `json:"endExtremaSkewMs"`
	RangesOverlap      bool   `json:"rangesOverlap"`
	OverlapMS          uint64 `json:"overlapMs"`
}

type MediaSummaryDiagnostic struct {
	Code           string            `json:"code"`
	Severity       analysis.Severity `json:"severity"`
	TagID          *uint64           `json:"tagId,omitempty"`
	Offset         uint64            `json:"offset"`
	BitOffset      uint8             `json:"bitOffset"`
	Message        string            `json:"message"`
	Expected       *uint64           `json:"expected,omitempty"`
	Actual         *uint64           `json:"actual,omitempty"`
	RecoveryOffset *uint64           `json:"recoveryOffset,omitempty"`
}

func NewMediaSummary(scan flv.ScanResult, result analysis.Result) MediaSummaryDocument {
	document := MediaSummaryDocument{
		SchemaVersion: MediaSummarySchemaVersion,
		DocumentType:  MediaSummaryDocumentType,
		Source:        Source{SizeBytes: scan.SourceSize},
		Scan: MediaSummaryScan{
			BytesScanned: scan.BytesScanned, Complete: scan.Complete,
			IndexedTagCount: uint64(len(scan.Tags)),
		},
		Summary: MediaSummary{
			Complete: result.Complete,
			Tags: MediaTagCounts{
				Total: result.Tags.Total, Audio: result.Tags.Audio, Video: result.Tags.Video,
				Script: result.Tags.Script, Other: result.Tags.Other, DataSizeBytes: result.Tags.DataSizeBytes,
			},
			Audio:     mapMediaTrack(result.Audio),
			Video:     mapMediaTrack(result.Video),
			Keyframes: mapMediaKeyframes(result.Keyframes),
			GOP: MediaGOP{
				Count: result.GOP.Count, ClosedDurationCount: result.GOP.ClosedDurationCount,
				NonPositiveDurationCount: result.GOP.NonPositiveDurationCount,
				MinDurationMS:            result.GOP.MinDurationMS, MaxDurationMS: result.GOP.MaxDurationMS,
				AverageDurationMS: result.GOP.AverageDurationMS,
			},
			AAC: mapAACConfigurations(result.AAC),
			AVC: mapAVCConfigurations(result.AVC),
		},
		ScanDiagnostics:     make([]Diagnostic, 0, len(scan.Diagnostics)),
		AnalysisDiagnostics: make([]MediaSummaryDiagnostic, 0, len(result.Diagnostics)),
	}
	if result.VideoPTS.Present {
		value := mapMediaTimeline(result.VideoPTS)
		document.Summary.VideoPTS = &value
	}
	if result.Composition.Present {
		document.Summary.CompositionTime = &MediaCompositionTime{
			SampleCount:   result.Composition.SampleCount,
			NegativeCount: result.Composition.NegativeCount, ZeroCount: result.Composition.ZeroCount,
			PositiveCount: result.Composition.PositiveCount,
			MinMS:         result.Composition.MinMS, MaxMS: result.Composition.MaxMS,
		}
	}
	if result.AVSync.Present {
		document.Summary.AVSync = &MediaAVSync{
			Basis:              result.AVSync.Basis,
			StartExtremaSkewMS: result.AVSync.StartExtremaSkewMS,
			EndExtremaSkewMS:   result.AVSync.EndExtremaSkewMS,
			RangesOverlap:      result.AVSync.RangesOverlap, OverlapMS: result.AVSync.OverlapMS,
		}
	}
	for _, item := range scan.Diagnostics {
		document.ScanDiagnostics = append(document.ScanDiagnostics, mapDiagnostic(item))
	}
	for _, item := range result.Diagnostics {
		document.AnalysisDiagnostics = append(document.AnalysisDiagnostics, MediaSummaryDiagnostic{
			Code: item.Code, Severity: item.Severity, TagID: cloneNumber(item.TagID),
			Offset: item.Offset, BitOffset: item.BitOffset, Message: item.Message,
			Expected: cloneNumber(item.Expected), Actual: cloneNumber(item.Actual), RecoveryOffset: cloneNumber(item.RecoveryOffset),
		})
	}
	return document
}

func WriteMediaSummaryJSON(output io.Writer, document MediaSummaryDocument, pretty bool) error {
	encoder := json.NewEncoder(output)
	encoder.SetEscapeHTML(false)
	if pretty {
		encoder.SetIndent("", "  ")
	}
	return encoder.Encode(document)
}

func mapMediaTrack(track analysis.Track) MediaTrackSummary {
	mapped := MediaTrackSummary{
		TagCount: track.TagCount, IndexedDataSizeBytes: track.IndexedDataSizeBytes,
		MediaTagCount: track.MediaTagCount, MediaDataSizeBytes: track.MediaDataSizeBytes,
		ExcludedTagCount: track.ExcludedTagCount, UnclassifiedTagCount: track.UnclassifiedTagCount,
	}
	if track.Timing.Present {
		value := mapMediaTimeline(track.Timing)
		mapped.Timing = &value
	}
	if track.Bitrate.Present {
		mapped.Bitrate = &MediaBitrate{
			Basis: track.Bitrate.Basis, Bytes: track.Bitrate.Bytes, CoveredIntervalMS: track.Bitrate.CoveredIntervalMS,
			AverageBitsPerSecond: track.Bitrate.AverageBitsPerSecond,
		}
	}
	return mapped
}

func mapMediaTimeline(value analysis.Timeline) MediaTimeline {
	return MediaTimeline{
		SampleCount: value.SampleCount, FirstMS: value.FirstMS, LastMS: value.LastMS,
		MinMS: value.MinMS, MaxMS: value.MaxMS, RangeMS: value.RangeMS, ForwardDeltaTotalMS: value.ForwardDeltaTotalMS,
		RegressionCount: value.RegressionCount, DuplicateCount: value.DuplicateCount,
		LargestForwardGapMS: value.LargestForwardGapMS,
	}
}

func mapMediaKeyframes(value analysis.Keyframes) MediaKeyframes {
	mapped := MediaKeyframes{
		DeclaredCount: value.DeclaredCount,
		Entries:       make([]MediaKeyframe, 0, len(value.Entries)),
		Truncated:     value.Truncated,
	}
	for _, entry := range value.Entries {
		item := MediaKeyframe{
			TagID: entry.TagID, Offset: entry.Offset, DTS: entry.DTS,
			DataSizeBytes: entry.DataSize, CodecID: entry.CodecID,
		}
		if entry.PTSPresent {
			value := entry.PTS
			item.PTS = &value
		}
		if entry.GOPDurationPresent {
			value := entry.GOPDurationMS
			item.GOPDurationMS = &value
		}
		mapped.Entries = append(mapped.Entries, item)
	}
	return mapped
}

func mapAACConfigurations(value analysis.AACConfigurations) MediaAACConfigurations {
	mapped := MediaAACConfigurations{
		EventCount: value.EventCount, AnalyzedCount: value.AnalyzedCount,
		SkippedByBudgetCount: value.SkippedByBudgetCount, ChangeCount: value.ChangeCount,
		Events: make([]MediaAACConfiguration, 0, len(value.Events)), Truncated: value.Truncated,
	}
	for _, event := range value.Events {
		item := MediaAACConfiguration{
			TagID: event.TagID, Offset: event.Offset, DTS: event.DTS,
			FingerprintSHA256: event.FingerprintSHA256, Complete: event.Complete,
		}
		if event.ComparedToPrevious {
			changed := event.ChangedFromPrevious
			item.ChangedFromPrevious = &changed
		}
		if event.AudioObjectTypePresent {
			item.AudioObjectType = &MediaNamedUint8{Value: event.AudioObjectType, Name: event.AudioObjectTypeName}
		}
		if event.SampleRatePresent {
			value := event.SamplingFrequencyHz
			item.SamplingFrequencyHz = &value
		}
		if event.ExtensionSamplingFrequencyPresent {
			value := event.ExtensionSamplingFrequencyHz
			item.ExtensionSamplingFrequencyHz = &value
		}
		if event.ChannelConfigurationPresent {
			channel := &MediaChannelConfiguration{Value: event.ChannelConfiguration, Name: event.ChannelConfigurationName}
			if event.ChannelCountPresent {
				value := event.ChannelCount
				channel.ChannelCount = &value
			}
			item.ChannelConfiguration = channel
		}
		mapped.Events = append(mapped.Events, item)
	}
	return mapped
}

func mapAVCConfigurations(value analysis.AVCConfigurations) MediaAVCConfigurations {
	mapped := MediaAVCConfigurations{
		EventCount: value.EventCount, AnalyzedCount: value.AnalyzedCount,
		SkippedByBudgetCount: value.SkippedByBudgetCount, ChangeCount: value.ChangeCount,
		Events: make([]MediaAVCConfiguration, 0, len(value.Events)), Truncated: value.Truncated,
	}
	for _, event := range value.Events {
		item := MediaAVCConfiguration{
			TagID: event.TagID, Offset: event.Offset, DTS: event.DTS,
			FingerprintSHA256: event.FingerprintSHA256, Complete: event.Complete,
			CompleteSPSCount: event.CompleteSPSCount, DistinctSPSDimensions: event.DistinctSPSDimensions,
		}
		if event.ComparedToPrevious {
			changed := event.ChangedFromPrevious
			item.ChangedFromPrevious = &changed
		}
		if event.ProfilePresent {
			item.Profile = &MediaAVCProfile{
				ProfileIDC: event.ProfileIDC, ProfileCompatibility: event.ProfileCompatibility, LevelIDC: event.LevelIDC,
			}
		}
		if event.LengthSizePresent {
			value := event.LengthSize
			item.LengthSize = &value
		}
		if event.FirstCompleteSPSPresent {
			item.FirstCompleteSPS = &MediaAVCFirstSPS{
				Index: event.FirstCompleteSPSIndex, SPSID: event.FirstCompleteSPSID,
				CodedWidth: event.CodedWidth, CodedHeight: event.CodedHeight,
				DisplayWidth: event.DisplayWidth, DisplayHeight: event.DisplayHeight,
			}
		}
		mapped.Events = append(mapped.Events, item)
	}
	return mapped
}
