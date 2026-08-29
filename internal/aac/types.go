// Package aac provides bounded, source-span-preserving MPEG-4
// AudioSpecificConfig parsing.
package aac

import (
	"fmt"

	"github.com/sevico/muxray/internal/cursor"
)

// AudioObjectType is the decoded MPEG-4 audioObjectType value.
type AudioObjectType uint8

const (
	AudioObjectTypeNull           AudioObjectType = 0
	AudioObjectTypeAACMain        AudioObjectType = 1
	AudioObjectTypeAACLC          AudioObjectType = 2
	AudioObjectTypeAACSSR         AudioObjectType = 3
	AudioObjectTypeAACLTP         AudioObjectType = 4
	AudioObjectTypeSBR            AudioObjectType = 5
	AudioObjectTypeAACScalable    AudioObjectType = 6
	AudioObjectTypeTwinVQ         AudioObjectType = 7
	AudioObjectTypeCELP           AudioObjectType = 8
	AudioObjectTypeHVXC           AudioObjectType = 9
	AudioObjectTypeTTSI           AudioObjectType = 12
	AudioObjectTypeMainSynthesis  AudioObjectType = 13
	AudioObjectTypeWavetable      AudioObjectType = 14
	AudioObjectTypeGeneralMIDI    AudioObjectType = 15
	AudioObjectTypeAlgorithmic    AudioObjectType = 16
	AudioObjectTypeERAACLC        AudioObjectType = 17
	AudioObjectTypeERAACLTP       AudioObjectType = 19
	AudioObjectTypeERAACScalable  AudioObjectType = 20
	AudioObjectTypeERTwinVQ       AudioObjectType = 21
	AudioObjectTypeERBSAC         AudioObjectType = 22
	AudioObjectTypeERAACLD        AudioObjectType = 23
	AudioObjectTypeERCELP         AudioObjectType = 24
	AudioObjectTypeERHVXC         AudioObjectType = 25
	AudioObjectTypeERHILN         AudioObjectType = 26
	AudioObjectTypeERParametric   AudioObjectType = 27
	AudioObjectTypeSSC            AudioObjectType = 28
	AudioObjectTypePS             AudioObjectType = 29
	AudioObjectTypeMPEGSurround   AudioObjectType = 30
	AudioObjectTypeEscape         AudioObjectType = 31
	AudioObjectTypeMPEGLayer1     AudioObjectType = 32
	AudioObjectTypeMPEGLayer2     AudioObjectType = 33
	AudioObjectTypeMPEGLayer3     AudioObjectType = 34
	AudioObjectTypeDST            AudioObjectType = 35
	AudioObjectTypeALS            AudioObjectType = 36
	AudioObjectTypeSLS            AudioObjectType = 37
	AudioObjectTypeSLSNonCore     AudioObjectType = 38
	AudioObjectTypeERAACELD       AudioObjectType = 39
	AudioObjectTypeSMRSimple      AudioObjectType = 40
	AudioObjectTypeSMRMain        AudioObjectType = 41
	AudioObjectTypeUSAC           AudioObjectType = 42
	AudioObjectTypeSAOC           AudioObjectType = 43
	AudioObjectTypeLDMPEGSurround AudioObjectType = 44
)

func (t AudioObjectType) String() string {
	switch t {
	case AudioObjectTypeNull:
		return "null"
	case AudioObjectTypeAACMain:
		return "aacMain"
	case AudioObjectTypeAACLC:
		return "aacLC"
	case AudioObjectTypeAACSSR:
		return "aacSSR"
	case AudioObjectTypeAACLTP:
		return "aacLTP"
	case AudioObjectTypeSBR:
		return "sbr"
	case AudioObjectTypeAACScalable:
		return "aacScalable"
	case AudioObjectTypeTwinVQ:
		return "twinVQ"
	case AudioObjectTypeCELP:
		return "celp"
	case AudioObjectTypeHVXC:
		return "hvxc"
	case 10, 11, 18:
		return "reserved"
	case AudioObjectTypeTTSI:
		return "ttsi"
	case AudioObjectTypeMainSynthesis:
		return "mainSynthesis"
	case AudioObjectTypeWavetable:
		return "wavetableSynthesis"
	case AudioObjectTypeGeneralMIDI:
		return "generalMIDI"
	case AudioObjectTypeAlgorithmic:
		return "algorithmicSynthesisAndAudioFX"
	case AudioObjectTypeERAACLC:
		return "erAACLC"
	case AudioObjectTypeERAACLTP:
		return "erAACLTP"
	case AudioObjectTypeERAACScalable:
		return "erAACScalable"
	case AudioObjectTypeERTwinVQ:
		return "erTwinVQ"
	case AudioObjectTypeERBSAC:
		return "erBSAC"
	case AudioObjectTypeERAACLD:
		return "erAACLD"
	case AudioObjectTypeERCELP:
		return "erCELP"
	case AudioObjectTypeERHVXC:
		return "erHVXC"
	case AudioObjectTypeERHILN:
		return "erHILN"
	case AudioObjectTypeERParametric:
		return "erParametric"
	case AudioObjectTypeSSC:
		return "ssc"
	case AudioObjectTypePS:
		return "ps"
	case AudioObjectTypeMPEGSurround:
		return "mpegSurround"
	case AudioObjectTypeEscape:
		return "escape"
	case AudioObjectTypeMPEGLayer1:
		return "mpeg1Layer1"
	case AudioObjectTypeMPEGLayer2:
		return "mpeg1Layer2"
	case AudioObjectTypeMPEGLayer3:
		return "mpeg1Layer3"
	case AudioObjectTypeDST:
		return "dst"
	case AudioObjectTypeALS:
		return "als"
	case AudioObjectTypeSLS:
		return "sls"
	case AudioObjectTypeSLSNonCore:
		return "slsNonCore"
	case AudioObjectTypeERAACELD:
		return "erAACELD"
	case AudioObjectTypeSMRSimple:
		return "smrSimple"
	case AudioObjectTypeSMRMain:
		return "smrMain"
	case AudioObjectTypeUSAC:
		return "usac"
	case AudioObjectTypeSAOC:
		return "saoc"
	case AudioObjectTypeLDMPEGSurround:
		return "ldMpegSurround"
	default:
		return "reserved"
	}
}

// Known reports whether t identifies a standardized, non-reserved audio
// object type. Known does not imply that this package implements its specific
// configuration syntax.
func (t AudioObjectType) Known() bool {
	return t <= AudioObjectTypeLDMPEGSurround && t != AudioObjectTypeNull &&
		t != 10 && t != 11 && t != 18 && t != AudioObjectTypeEscape
}

// ObjectTypeField preserves both parts of GetAudioObjectType(). Raw is the
// five-bit value; Escape is the six-bit suffix when Raw is 31.
type ObjectTypeField struct {
	Raw        uint8
	RawSpan    cursor.Span
	Escaped    bool
	Escape     uint8
	EscapeSpan cursor.Span
	Value      AudioObjectType
	Name       string
	Span       cursor.Span
	Complete   bool
}

// FrequencyField preserves the samplingFrequencyIndex and, for index 15, the
// explicit 24-bit value. Hertz is the resolved frequency.
type FrequencyField struct {
	Index         uint8
	IndexSpan     cursor.Span
	Explicit      bool
	ExplicitValue uint32
	ExplicitSpan  cursor.Span
	Hertz         uint32
	Complete      bool
}

// ChannelConfigurationField preserves the four-bit channelConfiguration.
type ChannelConfigurationField struct {
	Value    uint8
	Name     string
	Span     cursor.Span
	Complete bool
}

// GASpecificConfig is the supported, fixed-size subset of
// GASpecificConfig(). Conditional fields carry a corresponding Present flag.
type GASpecificConfig struct {
	Span     cursor.Span
	Complete bool

	FrameLengthFlag     bool
	FrameLengthFlagSpan cursor.Span

	DependsOnCoreCoder     bool
	DependsOnCoreCoderSpan cursor.Span
	CoreCoderDelayPresent  bool
	CoreCoderDelay         uint16
	CoreCoderDelaySpan     cursor.Span

	ExtensionFlag     bool
	ExtensionFlagSpan cursor.Span

	LayerNrPresent bool
	LayerNr        uint8
	LayerNrSpan    cursor.Span

	NumOfSubFramePresent bool
	NumOfSubFrame        uint8
	NumOfSubFrameSpan    cursor.Span
	LayerLength          uint16
	LayerLengthSpan      cursor.Span

	ResilienceFlagsPresent         bool
	SectionDataResilienceFlag      bool
	SectionDataResilienceFlagSpan  cursor.Span
	ScalefactorDataResilienceFlag  bool
	ScalefactorDataResilienceSpan  cursor.Span
	SpectralDataResilienceFlag     bool
	SpectralDataResilienceFlagSpan cursor.Span

	ExtensionFlag3Present bool
	ExtensionFlag3        bool
	ExtensionFlag3Span    cursor.Span
}

// Result is the bounded parse result for one AudioSpecificConfig payload.
// AudioObjectType is the effective/core type; InitialAudioObjectType preserves
// the first field before an explicit SBR/PS extension rewrites it.
type Result struct {
	Span cursor.Span

	InitialAudioObjectType ObjectTypeField
	AudioObjectType        ObjectTypeField
	SamplingFrequency      FrequencyField
	ChannelConfiguration   ChannelConfigurationField

	ExplicitSBR                          bool
	ExplicitPS                           bool
	ExtensionAudioObjectType             AudioObjectType
	ExtensionSamplingFrequency           FrequencyField
	ExtensionChannelConfiguration        ChannelConfigurationField
	ExtensionChannelConfigurationPresent bool

	GASpecific        GASpecificConfig
	GASpecificPresent bool

	EPConfigPresent  bool
	EPConfigComplete bool
	EPConfig         uint8
	EPConfigSpan     cursor.Span

	SyntaxSpan   cursor.Span
	TrailingSpan cursor.Span
	BitsConsumed uint64
	TrailingBits uint64
	Diagnostics  []Diagnostic
	Complete     bool
}

// HasErrors reports whether parsing was incomplete or emitted an error
// diagnostic.
func (r Result) HasErrors() bool {
	if !r.Complete {
		return true
	}
	for _, diagnostic := range r.Diagnostics {
		if diagnostic.Severity == SeverityError {
			return true
		}
	}
	return false
}

// Options configures hard resource budgets. Zero values select defaults.
type Options struct {
	MaxDiagnostics uint32
}

const (
	// HardMaxPayloadBytes follows the maximum FLV tag payload scale while
	// preventing callers from presenting an effectively unbounded range.
	HardMaxPayloadBytes uint64 = 16 << 20

	DefaultMaxDiagnostics uint32 = 32
	MinDiagnostics        uint32 = 2
	HardMaxDiagnostics    uint32 = 1_024
)

// Normalized applies defaults and rejects values outside package hard limits.
func (o Options) Normalized() (Options, error) {
	if o.MaxDiagnostics == 0 {
		o.MaxDiagnostics = DefaultMaxDiagnostics
	} else if o.MaxDiagnostics < MinDiagnostics {
		return Options{}, fmt.Errorf("aac: max diagnostics %d is below minimum %d", o.MaxDiagnostics, MinDiagnostics)
	} else if o.MaxDiagnostics > HardMaxDiagnostics {
		return Options{}, fmt.Errorf("aac: max diagnostics %d exceeds hard limit %d", o.MaxDiagnostics, HardMaxDiagnostics)
	}
	return o, nil
}

func channelConfigurationName(value uint8) string {
	switch value {
	case 0:
		return "programConfigElement"
	case 1:
		return "mono"
	case 2:
		return "stereo"
	case 3:
		return "3.0"
	case 4:
		return "4.0"
	case 5:
		return "5.0"
	case 6:
		return "5.1"
	case 7:
		return "7.1"
	default:
		return "reserved"
	}
}
