// Package analysis derives bounded, cross-tag media summaries from an FLV
// container index. It deliberately keeps payload bytes in the source and
// retains only bounded keyframe and configuration-event prefixes.
package analysis

import (
	"fmt"

	"github.com/sevico/muxray/internal/aac"
	"github.com/sevico/muxray/internal/avc"
)

// Options configures hard resource budgets. Zero values select defaults.
type Options struct {
	MaxKeyframes           uint32
	MaxConfigurationEvents uint32
	MaxConfigurationBytes  uint64
	MaxDiagnostics         uint32
	AAC                    aac.Options
	AVC                    avc.Options
}

// Progress counts completed Tag visits, not bytes read or partially visited
// Tags. Total is the frozen index length, including a canceled scan's prefix.
type Progress struct {
	Processed uint64
	Total     uint64
}

const (
	DefaultMaxKeyframes           uint32 = 4_096
	HardMaxKeyframes              uint32 = 65_536
	DefaultMaxConfigurationEvents uint32 = 256
	HardMaxConfigurationEvents    uint32 = 65_536
	DefaultMaxConfigurationBytes  uint64 = 256 << 20
	HardMaxConfigurationBytes     uint64 = 4 << 30
	DefaultMaxDiagnostics         uint32 = 1_024
	MinDiagnostics                uint32 = 2
	HardMaxDiagnostics            uint32 = 65_536
)

// Normalized applies defaults and rejects values outside package hard limits.
func (o Options) Normalized() (Options, error) {
	if o.MaxKeyframes == 0 {
		o.MaxKeyframes = DefaultMaxKeyframes
	} else if o.MaxKeyframes > HardMaxKeyframes {
		return Options{}, fmt.Errorf("analysis: max keyframes %d exceeds hard limit %d", o.MaxKeyframes, HardMaxKeyframes)
	}
	if o.MaxConfigurationEvents == 0 {
		o.MaxConfigurationEvents = DefaultMaxConfigurationEvents
	} else if o.MaxConfigurationEvents > HardMaxConfigurationEvents {
		return Options{}, fmt.Errorf("analysis: max configuration events %d exceeds hard limit %d", o.MaxConfigurationEvents, HardMaxConfigurationEvents)
	}
	if o.MaxConfigurationBytes == 0 {
		o.MaxConfigurationBytes = DefaultMaxConfigurationBytes
	} else if o.MaxConfigurationBytes > HardMaxConfigurationBytes {
		return Options{}, fmt.Errorf("analysis: max configuration bytes %d exceeds hard limit %d", o.MaxConfigurationBytes, HardMaxConfigurationBytes)
	}
	if o.MaxDiagnostics == 0 {
		o.MaxDiagnostics = DefaultMaxDiagnostics
	} else if o.MaxDiagnostics < MinDiagnostics {
		return Options{}, fmt.Errorf("analysis: max diagnostics %d is below minimum %d", o.MaxDiagnostics, MinDiagnostics)
	} else if o.MaxDiagnostics > HardMaxDiagnostics {
		return Options{}, fmt.Errorf("analysis: max diagnostics %d exceeds hard limit %d", o.MaxDiagnostics, HardMaxDiagnostics)
	}

	var err error
	if o.AAC, err = o.AAC.Normalized(); err != nil {
		return Options{}, err
	}
	if o.AVC, err = o.AVC.Normalized(); err != nil {
		return Options{}, err
	}
	return o, nil
}

// Result is the deterministic summary of all indexed FLV tags.
type Result struct {
	Complete    bool
	Tags        TagCounts
	Audio       Track
	Video       Track
	VideoPTS    Timeline
	Composition CompositionTime
	Keyframes   Keyframes
	GOP         GOP
	AAC         AACConfigurations
	AVC         AVCConfigurations
	AVSync      AVSync
	Diagnostics []Diagnostic
}

// HasErrors reports whether analysis was incomplete or emitted an error.
func (r Result) HasErrors() bool {
	if !r.Complete {
		return true
	}
	for _, item := range r.Diagnostics {
		if item.Severity == SeverityError {
			return true
		}
	}
	return false
}

type TagCounts struct {
	Total         uint64
	Audio         uint64
	Video         uint64
	Script        uint64
	Other         uint64
	DataSizeBytes uint64
}

// Track summarizes media-bearing tags. Configuration, end-of-sequence, and
// command tags are excluded from MediaTagCount and MediaDataSizeBytes.
type Track struct {
	TagCount             uint64
	IndexedDataSizeBytes uint64
	MediaTagCount        uint64
	MediaDataSizeBytes   uint64
	ExcludedTagCount     uint64
	UnclassifiedTagCount uint64
	Timing               Timeline
	Bitrate              Bitrate
}

// Timeline uses source order for First/Last/previous-value comparisons and
// numeric order for Min/Max/Range.
type Timeline struct {
	Present             bool
	SampleCount         uint64
	FirstMS             int64
	LastMS              int64
	MinMS               int64
	MaxMS               int64
	RangeMS             uint64
	ForwardDeltaTotalMS uint64
	RegressionCount     uint64
	DuplicateCount      uint64
	LargestForwardGapMS uint64
}

type Bitrate struct {
	Present              bool
	Basis                string
	Bytes                uint64
	CoveredIntervalMS    uint64
	AverageBitsPerSecond uint64
}

type CompositionTime struct {
	Present       bool
	SampleCount   uint64
	NegativeCount uint64
	ZeroCount     uint64
	PositiveCount uint64
	MinMS         int32
	MaxMS         int32
}

type Keyframe struct {
	TagID              uint64
	Offset             uint64
	DTS                int64
	PTS                int64
	PTSPresent         bool
	DataSize           uint32
	CodecID            uint8
	GOPDurationMS      uint64
	GOPDurationPresent bool
}

type Keyframes struct {
	DeclaredCount uint64
	Entries       []Keyframe
	Truncated     bool
}

type GOP struct {
	Count                    uint64
	ClosedDurationCount      uint64
	NonPositiveDurationCount uint64
	MinDurationMS            uint64
	MaxDurationMS            uint64
	AverageDurationMS        uint64
}

type AACConfiguration struct {
	TagID                             uint64
	Offset                            uint64
	DTS                               int64
	FingerprintSHA256                 string
	ChangedFromPrevious               bool
	ComparedToPrevious                bool
	Complete                          bool
	AudioObjectTypePresent            bool
	AudioObjectType                   uint8
	AudioObjectTypeName               string
	SampleRatePresent                 bool
	SamplingFrequencyHz               uint32
	ExtensionSamplingFrequencyPresent bool
	ExtensionSamplingFrequencyHz      uint32
	ChannelConfigurationPresent       bool
	ChannelConfiguration              uint8
	ChannelConfigurationName          string
	ChannelCountPresent               bool
	ChannelCount                      uint8
}

type AACConfigurations struct {
	EventCount           uint64
	AnalyzedCount        uint64
	SkippedByBudgetCount uint64
	ChangeCount          uint64
	Events               []AACConfiguration
	Truncated            bool
}

type AVCConfiguration struct {
	TagID                   uint64
	Offset                  uint64
	DTS                     int64
	FingerprintSHA256       string
	ChangedFromPrevious     bool
	ComparedToPrevious      bool
	Complete                bool
	ProfilePresent          bool
	ProfileIDC              uint8
	ProfileCompatibility    uint8
	LevelIDC                uint8
	LengthSizePresent       bool
	LengthSize              uint8
	FirstCompleteSPSPresent bool
	FirstCompleteSPSIndex   uint32
	FirstCompleteSPSID      uint32
	CompleteSPSCount        uint32
	DistinctSPSDimensions   uint32
	CodedWidth              uint64
	CodedHeight             uint64
	DisplayWidth            uint64
	DisplayHeight           uint64
}

type AVCConfigurations struct {
	EventCount           uint64
	AnalyzedCount        uint64
	SkippedByBudgetCount uint64
	ChangeCount          uint64
	Events               []AVCConfiguration
	Truncated            bool
}

// AVSync compares AVC presentation timestamps with audio decode timestamps.
// It does not model decoder priming, codec delay, or decoded sample duration.
type AVSync struct {
	Present            bool
	Basis              string
	StartExtremaSkewMS int64
	EndExtremaSkewMS   int64
	RangesOverlap      bool
	OverlapMS          uint64
}

type Severity string

const (
	SeverityWarning Severity = "warning"
	SeverityError   Severity = "error"
)

const (
	CodeAudioHeaderTruncated   = "MEDIA_AUDIO_HEADER_TRUNCATED"
	CodeAACPacketTypeTruncated = "MEDIA_AAC_PACKET_TYPE_TRUNCATED"
	CodeVideoHeaderTruncated   = "MEDIA_VIDEO_HEADER_TRUNCATED"
	CodeAVCFramingTruncated    = "MEDIA_AVC_FRAMING_TRUNCATED"
	CodeAACPacketTypeUnknown   = "MEDIA_AAC_PACKET_TYPE_UNKNOWN"
	CodeAVCPacketTypeUnknown   = "MEDIA_AVC_PACKET_TYPE_UNKNOWN"
	CodeFilteredTagSkipped     = "MEDIA_FILTERED_TAG_SKIPPED"
	CodeIncompleteTagSkipped   = "MEDIA_INCOMPLETE_TAG_SKIPPED"
	CodeAudioCodecUnsupported  = "MEDIA_AUDIO_CODEC_UNSUPPORTED"
	CodeVideoCodecUnsupported  = "MEDIA_VIDEO_CODEC_UNSUPPORTED"
	CodeConfigurationByteLimit = "MEDIA_CONFIGURATION_BYTE_LIMIT_REACHED"
	CodeDiagnosticLimitReached = "MEDIA_DIAGNOSTIC_LIMIT_REACHED"
)

type Diagnostic struct {
	Code           string
	Severity       Severity
	TagID          *uint64
	Offset         uint64
	BitOffset      uint8
	Message        string
	Expected       *uint64
	Actual         *uint64
	RecoveryOffset *uint64
}
