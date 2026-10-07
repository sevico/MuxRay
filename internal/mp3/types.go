// Package mp3 provides bounded, source-span-preserving MPEG Audio Layer III
// frame-header parsing.
package mp3

import (
	"fmt"

	"github.com/sevico/muxray/internal/cursor"
)

// Version is the two-bit MPEG Audio version ID stored in a frame header.
type Version uint8

const (
	VersionMPEG25   Version = 0
	VersionReserved Version = 1
	VersionMPEG2    Version = 2
	VersionMPEG1    Version = 3
)

func (version Version) String() string {
	switch version {
	case VersionMPEG25:
		return "mpeg2.5"
	case VersionMPEG2:
		return "mpeg2"
	case VersionMPEG1:
		return "mpeg1"
	default:
		return "reserved"
	}
}

// Layer is the two-bit MPEG Audio layer field in its encoded representation.
type Layer uint8

const (
	LayerReserved Layer = 0
	LayerIII      Layer = 1
	LayerII       Layer = 2
	LayerI        Layer = 3
)

func (layer Layer) String() string {
	switch layer {
	case LayerIII:
		return "layerIII"
	case LayerII:
		return "layerII"
	case LayerI:
		return "layerI"
	default:
		return "reserved"
	}
}

// ChannelMode is the two-bit MPEG Audio channel-mode field.
type ChannelMode uint8

const (
	ChannelModeStereo        ChannelMode = 0
	ChannelModeJointStereo   ChannelMode = 1
	ChannelModeDualChannel   ChannelMode = 2
	ChannelModeSingleChannel ChannelMode = 3
)

func (mode ChannelMode) String() string {
	switch mode {
	case ChannelModeStereo:
		return "stereo"
	case ChannelModeJointStereo:
		return "jointStereo"
	case ChannelModeDualChannel:
		return "dualChannel"
	case ChannelModeSingleChannel:
		return "singleChannel"
	default:
		return "unknown"
	}
}

// Emphasis is the two-bit MPEG Audio emphasis field.
type Emphasis uint8

const (
	EmphasisNone           Emphasis = 0
	EmphasisFiftyFifteenUS Emphasis = 1
	EmphasisReserved       Emphasis = 2
	EmphasisCCITTJ17       Emphasis = 3
)

func (emphasis Emphasis) String() string {
	switch emphasis {
	case EmphasisNone:
		return "none"
	case EmphasisFiftyFifteenUS:
		return "50/15us"
	case EmphasisCCITTJ17:
		return "ccittJ17"
	default:
		return "reserved"
	}
}

// BitrateMode distinguishes table-indexed headers from free-format syntax and
// the forbidden bitrate index.
type BitrateMode string

const (
	BitrateModeFree    BitrateMode = "free"
	BitrateModeIndexed BitrateMode = "indexed"
	BitrateModeInvalid BitrateMode = "invalid"
)

// FrameHeader preserves the fixed MPEG Audio header fields and only publishes
// derived values when their prerequisite fields are valid.
type FrameHeader struct {
	Raw  uint32
	Span cursor.Span

	SyncWord     uint16
	SyncWordSpan cursor.Span

	Version     Version
	VersionSpan cursor.Span
	Layer       Layer
	LayerSpan   cursor.Span

	ProtectionBit     uint8
	ProtectionBitSpan cursor.Span
	CRCProtected      bool

	BitrateIndex     uint8
	BitrateIndexSpan cursor.Span
	BitrateMode      BitrateMode
	BitrateKbps      uint16
	BitratePresent   bool

	SamplingFrequencyIndex     uint8
	SamplingFrequencyIndexSpan cursor.Span
	SamplingFrequencyHz        uint32
	SamplingFrequencyPresent   bool

	Padding     bool
	PaddingSpan cursor.Span
	Private     bool
	PrivateSpan cursor.Span

	ChannelMode       ChannelMode
	ChannelModeSpan   cursor.Span
	ModeExtension     uint8
	ModeExtensionSpan cursor.Span
	Channels          uint8

	Copyright     bool
	CopyrightSpan cursor.Span
	Original      bool
	OriginalSpan  cursor.Span
	Emphasis      Emphasis
	EmphasisSpan  cursor.Span

	SamplesPerFrame        uint16
	SamplesPerFramePresent bool
	FrameLengthBytes       uint32
	FrameLengthPresent     bool
	Complete               bool
}

// Result is the bounded parse result for the first frame header in one MP3
// SoundData range. Bytes after the fixed header are never read.
type Result struct {
	Span          cursor.Span
	Header        FrameHeader
	HeaderPresent bool
	BitsConsumed  uint64
	Diagnostics   []Diagnostic
	Complete      bool
}

// HasErrors reports whether parsing was incomplete or emitted an error.
func (result Result) HasErrors() bool {
	return !result.Complete || hasError(result.Diagnostics)
}

// Options configures diagnostic and optional same-tag frame budgets.
// Zero budget values select defaults; InspectFrames is opt-in.
type Options struct {
	MaxDiagnostics uint32
	// InspectFrames opts FLV detail into bounded same-tag frame inspection.
	InspectFrames bool
	MaxFrames     uint32
}

const (
	HardMaxPayloadBytes   uint64 = 16 << 20
	DefaultMaxDiagnostics uint32 = 32
	MinDiagnostics        uint32 = 2
	HardMaxDiagnostics    uint32 = 1_024
	DefaultMaxFrames      uint32 = 64
	HardMaxFrames         uint32 = 1_024
	MaxFrameScanBytes     uint64 = 1 << 20
)

// Normalized applies defaults and rejects values outside hard limits.
func (options Options) Normalized() (Options, error) {
	if options.MaxFrames == 0 {
		options.MaxFrames = DefaultMaxFrames
	} else if options.MaxFrames > HardMaxFrames {
		return Options{}, fmt.Errorf("mp3: max frames %d exceeds hard limit %d", options.MaxFrames, HardMaxFrames)
	}
	if options.MaxDiagnostics == 0 {
		options.MaxDiagnostics = DefaultMaxDiagnostics
	} else if options.MaxDiagnostics < MinDiagnostics {
		return Options{}, fmt.Errorf("mp3: max diagnostics %d is below minimum %d", options.MaxDiagnostics, MinDiagnostics)
	} else if options.MaxDiagnostics > HardMaxDiagnostics {
		return Options{}, fmt.Errorf("mp3: max diagnostics %d exceeds hard limit %d", options.MaxDiagnostics, HardMaxDiagnostics)
	}
	return options, nil
}
