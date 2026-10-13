// Package avc provides bounded, source-span-preserving AVC/H.264
// configuration and NAL-unit parsing.
package avc

import (
	"fmt"

	"github.com/sevico/muxray/internal/cursor"
)

// Options configures hard resource budgets. Zero values select defaults.
type Options struct {
	// ParseVUI expands SPS VUI and validates SPS trailing bits. The default
	// retains the original phase-one result and diagnostic contract.
	ParseVUI bool
	// ParsePPS expands the necessary PPS prefix; ParseSliceHeaders reads only
	// the slice header prefix through redundant_pic_cnt and also enables
	// ParsePPS for in-band parameter replacements. Both default off.
	ParsePPS          bool
	ParseSliceHeaders bool
	// ParseFullHeaders expands PPS syntax and the remainder of requested slice
	// headers. ParseSliceHeaders must also be set to inspect slice NAL units.
	ParseFullHeaders bool
	// ParseSEI inspects selected SEI NAL units with independent byte/message
	// budgets. It does not enable parameter-set or slice parsing.
	ParseSEI bool
	// ParameterSets is cloned for each parse; in-band updates never mutate it.
	ParameterSets    *ParameterSetContext
	MaxDiagnostics   uint32
	MaxParameterSets uint32
	MaxNALUnits      uint32
	// MaxParameterSetBytes bounds one SPS/PPS EBSP materialized for a
	// phase-one summary. Ordinary slice NAL units are never materialized.
	MaxParameterSetBytes uint32
	// MaxRBSPMappingEntries is a cumulative budget per Parse call for source
	// segments plus removed emulation-prevention byte records.
	MaxRBSPMappingEntries uint32
}

const (
	HardMaxPayloadBytes uint64 = 16 << 20

	DefaultMaxDiagnostics uint32 = 64
	MinDiagnostics        uint32 = 2
	HardMaxDiagnostics    uint32 = 1_024

	DefaultMaxParameterSets uint32 = 512
	HardMaxParameterSets    uint32 = 541 // 31 SPS + 255 PPS + 255 SPS extensions.

	DefaultMaxNALUnits uint32 = 4_096
	HardMaxNALUnits    uint32 = 16_384

	DefaultMaxParameterSetBytes uint32 = 64 << 10
	HardMaxParameterSetBytes    uint32 = 1 << 20

	DefaultMaxRBSPMappingEntries uint32 = 16_384
	HardMaxRBSPMappingEntries    uint32 = 65_536
)

// Normalized applies defaults and rejects values outside package hard limits.
func (o Options) Normalized() (Options, error) {
	if o.ParseSliceHeaders || o.ParseFullHeaders {
		o.ParsePPS = true
	}
	if o.MaxDiagnostics == 0 {
		o.MaxDiagnostics = DefaultMaxDiagnostics
	} else if o.MaxDiagnostics < MinDiagnostics {
		return Options{}, fmt.Errorf("avc: max diagnostics %d is below minimum %d", o.MaxDiagnostics, MinDiagnostics)
	} else if o.MaxDiagnostics > HardMaxDiagnostics {
		return Options{}, fmt.Errorf("avc: max diagnostics %d exceeds hard limit %d", o.MaxDiagnostics, HardMaxDiagnostics)
	}
	if o.MaxParameterSets == 0 {
		o.MaxParameterSets = DefaultMaxParameterSets
	} else if o.MaxParameterSets > HardMaxParameterSets {
		return Options{}, fmt.Errorf("avc: max parameter sets %d exceeds hard limit %d", o.MaxParameterSets, HardMaxParameterSets)
	}
	if o.MaxNALUnits == 0 {
		o.MaxNALUnits = DefaultMaxNALUnits
	} else if o.MaxNALUnits > HardMaxNALUnits {
		return Options{}, fmt.Errorf("avc: max NAL units %d exceeds hard limit %d", o.MaxNALUnits, HardMaxNALUnits)
	}
	if o.MaxParameterSetBytes == 0 {
		o.MaxParameterSetBytes = DefaultMaxParameterSetBytes
	} else if o.MaxParameterSetBytes > HardMaxParameterSetBytes {
		return Options{}, fmt.Errorf("avc: max parameter-set bytes %d exceeds hard limit %d", o.MaxParameterSetBytes, HardMaxParameterSetBytes)
	}
	if o.MaxRBSPMappingEntries == 0 {
		o.MaxRBSPMappingEntries = DefaultMaxRBSPMappingEntries
	} else if o.MaxRBSPMappingEntries > HardMaxRBSPMappingEntries {
		return Options{}, fmt.Errorf("avc: max RBSP mapping entries %d exceeds hard limit %d", o.MaxRBSPMappingEntries, HardMaxRBSPMappingEntries)
	}
	return o, nil
}

// NALUnitType is the five-bit nal_unit_type from an AVC NAL header.
type NALUnitType uint8

const (
	NALUnitTypeNonIDRSlice                NALUnitType = 1
	NALUnitTypeDataPartitionA             NALUnitType = 2
	NALUnitTypeDataPartitionB             NALUnitType = 3
	NALUnitTypeDataPartitionC             NALUnitType = 4
	NALUnitTypeIDRSlice                   NALUnitType = 5
	NALUnitTypeSEI                        NALUnitType = 6
	NALUnitTypeSequenceParameterSet       NALUnitType = 7
	NALUnitTypePictureParameterSet        NALUnitType = 8
	NALUnitTypeAccessUnitDelimiter        NALUnitType = 9
	NALUnitTypeEndOfSequence              NALUnitType = 10
	NALUnitTypeEndOfStream                NALUnitType = 11
	NALUnitTypeFillerData                 NALUnitType = 12
	NALUnitTypeSequenceParameterSetExt    NALUnitType = 13
	NALUnitTypePrefix                     NALUnitType = 14
	NALUnitTypeSubsetSequenceParameterSet NALUnitType = 15
	NALUnitTypeDepthParameterSet          NALUnitType = 16
	NALUnitTypeAuxiliaryCodedSlice        NALUnitType = 19
	NALUnitTypeCodedSliceExtension        NALUnitType = 20
	NALUnitTypeDepthViewCodedSliceExt     NALUnitType = 21
)

func (t NALUnitType) String() string {
	switch t {
	case NALUnitTypeNonIDRSlice:
		return "nonIDRSlice"
	case NALUnitTypeDataPartitionA:
		return "dataPartitionA"
	case NALUnitTypeDataPartitionB:
		return "dataPartitionB"
	case NALUnitTypeDataPartitionC:
		return "dataPartitionC"
	case NALUnitTypeIDRSlice:
		return "idrSlice"
	case NALUnitTypeSEI:
		return "sei"
	case NALUnitTypeSequenceParameterSet:
		return "sequenceParameterSet"
	case NALUnitTypePictureParameterSet:
		return "pictureParameterSet"
	case NALUnitTypeAccessUnitDelimiter:
		return "accessUnitDelimiter"
	case NALUnitTypeEndOfSequence:
		return "endOfSequence"
	case NALUnitTypeEndOfStream:
		return "endOfStream"
	case NALUnitTypeFillerData:
		return "fillerData"
	case NALUnitTypeSequenceParameterSetExt:
		return "sequenceParameterSetExtension"
	case NALUnitTypePrefix:
		return "prefix"
	case NALUnitTypeSubsetSequenceParameterSet:
		return "subsetSequenceParameterSet"
	case NALUnitTypeDepthParameterSet:
		return "depthParameterSet"
	case NALUnitTypeAuxiliaryCodedSlice:
		return "auxiliaryCodedSlice"
	case NALUnitTypeCodedSliceExtension:
		return "codedSliceExtension"
	case NALUnitTypeDepthViewCodedSliceExt:
		return "depthViewCodedSliceExtension"
	case 17, 18, 22, 23:
		return "reserved"
	case 0, 24, 25, 26, 27, 28, 29, 30, 31:
		return "unspecified"
	default:
		return "unknown"
	}
}

// NALHeader preserves the first byte and each of its source bit ranges.
type NALHeader struct {
	Raw                  uint8
	Span                 cursor.Span
	ForbiddenZeroBit     bool
	ForbiddenZeroBitSpan cursor.Span
	NALRefIDC            uint8
	NALRefIDCSpan        cursor.Span
	NALUnitType          NALUnitType
	NALUnitTypeName      string
	NALUnitTypeSpan      cursor.Span
	Complete             bool
}

// MappedSpan identifies a bit range in de-escaped RBSP space and the one or
// more ranges that contain those bits in the original EBSP source. A field
// crossing an emulation-prevention byte therefore has multiple SourceSpans;
// callers must not infer source contiguity from RBSPBitOffset.
type MappedSpan struct {
	RBSPBitOffset uint32
	BitLength     uint32
	SourceSpans   []cursor.Span
}

// RBSPSourceSegment maps a contiguous run of RBSP bytes back to its original
// contiguous EBSP source range.
type RBSPSourceSegment struct {
	RBSPByteOffset uint32
	RBSPByteLength uint32
	SourceSpan     cursor.Span
}

// RBSPView records the deterministic de-escaping map. EBSPSpan excludes the
// one-byte NAL header. EmulationPreventionBytes contains the removed 0x03
// source bytes, in source order.
type RBSPView struct {
	EBSPSpan                 cursor.Span
	ByteLength               uint32
	SourceSegments           []RBSPSourceSegment
	EmulationPreventionBytes []cursor.Span
}

// SequenceParameterSet is the phase-one SPS syntax and dimension summary.
// Complete means all fields through vui_parameters_present_flag were decoded;
// it does not claim that optional VUI syntax was expanded.
type SequenceParameterSet struct {
	RBSP RBSPView
	// VUI is nil unless ParseVUI is enabled and phase-one syntax is complete.
	// Its independent completeness includes the SPS rbsp_trailing_bits.
	VUI *VUIParameters

	ProfileIDC                 uint8
	ProfileIDCSpan             MappedSpan
	ConstraintSetFlags         uint8
	ConstraintSetFlagsSpan     MappedSpan
	ReservedZero2Bits          uint8
	ReservedZero2BitsSpan      MappedSpan
	LevelIDC                   uint8
	LevelIDCSpan               MappedSpan
	SequenceParameterSetID     uint32
	SequenceParameterSetIDSpan MappedSpan

	HighProfileFieldsPresent             bool
	ChromaFormatIDC                      uint32
	ChromaFormatIDCSpan                  MappedSpan
	SeparateColourPlaneFlag              bool
	SeparateColourPlaneFlagPresent       bool
	SeparateColourPlaneFlagSpan          MappedSpan
	BitDepthLumaMinus8                   uint32
	BitDepthLumaMinus8Span               MappedSpan
	BitDepthChromaMinus8                 uint32
	BitDepthChromaMinus8Span             MappedSpan
	QPPrimeYZeroTransformBypassFlag      bool
	QPPrimeYZeroTransformBypassFlagSpan  MappedSpan
	SequenceScalingMatrixPresentFlag     bool
	SequenceScalingMatrixPresentFlagSpan MappedSpan

	Log2MaxFrameNumMinus4              uint32
	Log2MaxFrameNumMinus4Span          MappedSpan
	PicOrderCntType                    uint32
	PicOrderCntTypeSpan                MappedSpan
	Log2MaxPicOrderCntLSBMinus4        uint32
	Log2MaxPicOrderCntLSBMinus4Present bool
	Log2MaxPicOrderCntLSBMinus4Span    MappedSpan
	DeltaPicOrderAlwaysZeroFlag        bool
	DeltaPicOrderAlwaysZeroFlagPresent bool
	DeltaPicOrderAlwaysZeroFlagSpan    MappedSpan
	OffsetForNonRefPic                 int64
	OffsetForNonRefPicSpan             MappedSpan
	OffsetForTopToBottomField          int64
	OffsetForTopToBottomFieldSpan      MappedSpan
	NumRefFramesInPicOrderCntCycle     uint32
	NumRefFramesInPicOrderCntCycleSpan MappedSpan

	MaxNumRefFrames                    uint32
	MaxNumRefFramesSpan                MappedSpan
	GapsInFrameNumValueAllowedFlag     bool
	GapsInFrameNumValueAllowedFlagSpan MappedSpan
	PicWidthInMbsMinus1                uint32
	PicWidthInMbsMinus1Span            MappedSpan
	PicHeightInMapUnitsMinus1          uint32
	PicHeightInMapUnitsMinus1Span      MappedSpan
	FrameMbsOnlyFlag                   bool
	FrameMbsOnlyFlagSpan               MappedSpan
	MBAdaptiveFrameFieldFlag           bool
	MBAdaptiveFrameFieldFlagPresent    bool
	MBAdaptiveFrameFieldFlagSpan       MappedSpan
	Direct8x8InferenceFlag             bool
	Direct8x8InferenceFlagSpan         MappedSpan
	FrameCroppingFlag                  bool
	FrameCroppingFlagSpan              MappedSpan
	FrameCropLeftOffset                uint32
	FrameCropLeftOffsetSpan            MappedSpan
	FrameCropRightOffset               uint32
	FrameCropRightOffsetSpan           MappedSpan
	FrameCropTopOffset                 uint32
	FrameCropTopOffsetSpan             MappedSpan
	FrameCropBottomOffset              uint32
	FrameCropBottomOffsetSpan          MappedSpan
	VUIParametersPresentFlag           bool
	VUIParametersPresentFlagSpan       MappedSpan

	ChromaArrayType   uint32
	CropUnitX         uint32
	CropUnitY         uint32
	CodedWidth        uint64
	CodedHeight       uint64
	DisplayWidth      uint64
	DisplayHeight     uint64
	SyntaxBits        uint32
	RemainingRBSPBits uint32
	Complete          bool
}

// PictureParameterSet is the phase-one PPS reference summary. Complete means
// the identifiers and the first two PPS flags were decoded; remaining PPS
// syntax stays in RemainingRBSPBits.
type PictureParameterSet struct {
	RBSP RBSPView
	Deep *PPSDetails

	PictureParameterSetID                     uint32
	PictureParameterSetIDSpan                 MappedSpan
	SequenceParameterSetID                    uint32
	SequenceParameterSetIDSpan                MappedSpan
	EntropyCodingModeFlag                     bool
	EntropyCodingModeFlagSpan                 MappedSpan
	BottomFieldPicOrderInFramePresentFlag     bool
	BottomFieldPicOrderInFramePresentFlagSpan MappedSpan
	SyntaxBits                                uint32
	RemainingRBSPBits                         uint32
	Complete                                  bool
}

// ParameterSet is one length-delimited entry in an avcC parameter-set array.
// Span covers the NAL bytes only; LengthSpan covers its preceding length.
type ParameterSet struct {
	Index                uint32
	Length               uint32
	LengthSpan           cursor.Span
	Span                 cursor.Span
	Header               NALHeader
	HeaderPresent        bool
	SequenceParameterSet *SequenceParameterSet
	PictureParameterSet  *PictureParameterSet
	SEI                  *SEIMessageSequence
	SummaryComplete      bool
}

// ConfigurationRecord is one AVCDecoderConfigurationRecord (avcC).
// FramingComplete concerns avcC lengths/counts only. Parameter-set summaries
// report their own completeness and may produce diagnostics independently.
type ConfigurationRecord struct {
	Span cursor.Span

	ConfigurationVersion     uint8
	ConfigurationVersionSpan cursor.Span
	AVCProfileIndication     uint8
	AVCProfileIndicationSpan cursor.Span
	ProfileCompatibility     uint8
	ProfileCompatibilitySpan cursor.Span
	AVCLevelIndication       uint8
	AVCLevelIndicationSpan   cursor.Span

	LengthSizeReserved           uint8
	LengthSizeReservedSpan       cursor.Span
	LengthSizeMinusOne           uint8
	LengthSizeMinusOneSpan       cursor.Span
	LengthSize                   uint8
	SPSCountReserved             uint8
	SPSCountReservedSpan         cursor.Span
	NumSequenceParameterSets     uint8
	NumSequenceParameterSetsSpan cursor.Span
	SequenceParameterSets        []ParameterSet
	NumPictureParameterSets      uint8
	NumPictureParameterSetsSpan  cursor.Span
	PictureParameterSets         []ParameterSet

	HighProfileFieldsPresent       bool
	ChromaFormatReserved           uint8
	ChromaFormatReservedSpan       cursor.Span
	ChromaFormat                   uint8
	ChromaFormatSpan               cursor.Span
	BitDepthLumaReserved           uint8
	BitDepthLumaReservedSpan       cursor.Span
	BitDepthLumaMinus8             uint8
	BitDepthLumaMinus8Span         cursor.Span
	BitDepthChromaReserved         uint8
	BitDepthChromaReservedSpan     cursor.Span
	BitDepthChromaMinus8           uint8
	BitDepthChromaMinus8Span       cursor.Span
	NumSequenceParameterSetExt     uint8
	NumSequenceParameterSetExtSpan cursor.Span
	SequenceParameterSetExts       []ParameterSet

	SyntaxSpan      cursor.Span
	TrailingSpan    cursor.Span
	BytesConsumed   uint64
	Diagnostics     []Diagnostic
	FramingComplete bool
	Complete        bool
}

// HasErrors reports whether parsing was incomplete or emitted an error.
func (r ConfigurationRecord) HasErrors() bool {
	if !r.Complete {
		return true
	}
	return hasError(r.Diagnostics)
}

// NALUnit is one length-prefixed NAL unit. Span excludes the length prefix.
type NALUnit struct {
	Index                uint32
	Length               uint32
	LengthSpan           cursor.Span
	Span                 cursor.Span
	Header               NALHeader
	HeaderPresent        bool
	SequenceParameterSet *SequenceParameterSet
	PictureParameterSet  *PictureParameterSet
	SliceHeader          *SliceHeader
	SEI                  *SEIMessageSequence
	SummaryComplete      bool
}

// NALUnitSequence is a bounded sequence of length-prefixed AVC NAL units.
type NALUnitSequence struct {
	Span            cursor.Span
	LengthSize      uint8
	NALUnits        []NALUnit
	SyntaxSpan      cursor.Span
	TrailingSpan    cursor.Span
	BytesConsumed   uint64
	Diagnostics     []Diagnostic
	FramingComplete bool
	Complete        bool
}

// HasErrors reports whether parsing was incomplete or emitted an error.
func (r NALUnitSequence) HasErrors() bool {
	if !r.Complete {
		return true
	}
	return hasError(r.Diagnostics)
}
