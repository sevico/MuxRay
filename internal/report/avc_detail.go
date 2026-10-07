package report

import (
	"io"

	"github.com/sevico/muxray/internal/amf0"
	"github.com/sevico/muxray/internal/avc"
	"github.com/sevico/muxray/internal/cursor"
	"github.com/sevico/muxray/internal/flv"
)

const (
	AVCTagDetailSchemaVersion = 1
	AVCTagDetailDocumentType  = "flv-avc-tag-detail"
	AVCVideoKind              = "avcVideo"
)

// AVCTagDetailDocument is the independently versioned public contract for one
// bounded FLV AVC Video Tag. NAL and parameter-set bytes remain source spans.
type AVCTagDetailDocument struct {
	SchemaVersion int                   `json:"schemaVersion"`
	DocumentType  string                `json:"documentType"`
	Source        Source                `json:"source"`
	Tag           Tag                   `json:"tag"`
	Detail        AVCVideoDetail        `json:"detail"`
	Diagnostics   []TagDetailDiagnostic `json:"diagnostics"`
}

func (AVCTagDetailDocument) parsedTagDetailDocument() {}

type AVCVideoDetail struct {
	Kind                          string                        `json:"kind"`
	Span                          Span                          `json:"span"`
	BitsConsumed                  uint64                        `json:"bitsConsumed"`
	Complete                      bool                          `json:"complete"`
	VideoTagHeader                *FLVVideoTagHeaderDetail      `json:"videoTagHeader,omitempty"`
	PacketType                    *AVCPacketTypeDetail          `json:"packetType,omitempty"`
	CompositionTime               *AVCCompositionTimeDetail     `json:"compositionTime,omitempty"`
	DataSpan                      *Span                         `json:"dataSpan,omitempty"`
	AVCDecoderConfigurationRecord *AVCConfigurationRecordDetail `json:"avcDecoderConfigurationRecord,omitempty"`
	NALUnitSequence               *AVCNALUnitSequenceDetail     `json:"nalUnitSequence,omitempty"`
	EndOfSequenceSpan             *Span                         `json:"endOfSequenceSpan,omitempty"`
}

type FLVVideoTagHeaderDetail struct {
	Raw       uint8         `json:"raw"`
	Span      Span          `json:"span"`
	FrameType AVCNamedField `json:"frameType"`
	CodecID   AVCNamedField `json:"codecId"`
}

type AVCNamedField struct {
	Value uint8  `json:"value"`
	Name  string `json:"name"`
	Span  Span   `json:"span"`
}

type AVCPacketTypeDetail struct {
	Value uint8  `json:"value"`
	Name  string `json:"name"`
	Span  Span   `json:"span"`
}

type AVCCompositionTimeDetail struct {
	Raw   uint32 `json:"raw"`
	Value int32  `json:"value"`
	Span  Span   `json:"span"`
}

type AVCUnsignedField struct {
	Value uint64 `json:"value"`
	Span  Span   `json:"span"`
}

type AVCBooleanField struct {
	Value bool `json:"value"`
	Span  Span `json:"span"`
}

type AVCMappedUnsignedField struct {
	Value uint64        `json:"value"`
	Span  AVCMappedSpan `json:"span"`
}

type AVCMappedSignedField struct {
	Value int64         `json:"value"`
	Span  AVCMappedSpan `json:"span"`
}

type AVCMappedBooleanField struct {
	Value bool          `json:"value"`
	Span  AVCMappedSpan `json:"span"`
}

type AVCMappedSpan struct {
	RBSPBitOffset uint32 `json:"rbspBitOffset"`
	BitLength     uint32 `json:"bitLength"`
	SourceSpans   []Span `json:"sourceSpans"`
}

type AVCRBSPSourceSegmentDetail struct {
	RBSPByteOffset uint32 `json:"rbspByteOffset"`
	RBSPByteLength uint32 `json:"rbspByteLength"`
	SourceSpan     Span   `json:"sourceSpan"`
}

type AVCRBSPViewDetail struct {
	EBSPSpan                 Span                         `json:"ebspSpan"`
	ByteLength               uint32                       `json:"byteLength"`
	SourceSegments           []AVCRBSPSourceSegmentDetail `json:"sourceSegments"`
	EmulationPreventionBytes []Span                       `json:"emulationPreventionBytes"`
}

type AVCNALHeaderDetail struct {
	Raw              uint8            `json:"raw"`
	Span             Span             `json:"span"`
	ForbiddenZeroBit AVCBooleanField  `json:"forbiddenZeroBit"`
	NALRefIDC        AVCUnsignedField `json:"nalRefIdc"`
	NALUnitType      AVCNamedField    `json:"nalUnitType"`
	Complete         bool             `json:"complete"`
}

type AVCNALUnitDetail struct {
	Index                uint32                   `json:"index"`
	Length               uint32                   `json:"length"`
	LengthSpan           Span                     `json:"lengthSpan"`
	Span                 Span                     `json:"span"`
	Header               *AVCNALHeaderDetail      `json:"header,omitempty"`
	SequenceParameterSet *AVCSequenceParameterSet `json:"sequenceParameterSet,omitempty"`
	PictureParameterSet  *AVCPictureParameterSet  `json:"pictureParameterSet,omitempty"`
	SummaryComplete      bool                     `json:"summaryComplete"`
}

type AVCConfigurationRecordDetail struct {
	Span                     Span                     `json:"span"`
	SyntaxSpan               Span                     `json:"syntaxSpan"`
	TrailingSpan             Span                     `json:"trailingSpan"`
	BytesConsumed            uint64                   `json:"bytesConsumed"`
	FramingComplete          bool                     `json:"framingComplete"`
	Complete                 bool                     `json:"complete"`
	ConfigurationVersion     *AVCUnsignedField        `json:"configurationVersion,omitempty"`
	AVCProfileIndication     *AVCUnsignedField        `json:"avcProfileIndication,omitempty"`
	ProfileCompatibility     *AVCUnsignedField        `json:"profileCompatibility,omitempty"`
	AVCLevelIndication       *AVCUnsignedField        `json:"avcLevelIndication,omitempty"`
	LengthSizeReserved       *AVCUnsignedField        `json:"lengthSizeReserved,omitempty"`
	LengthSizeMinusOne       *AVCUnsignedField        `json:"lengthSizeMinusOne,omitempty"`
	LengthSize               *uint8                   `json:"lengthSize,omitempty"`
	SPSCountReserved         *AVCUnsignedField        `json:"spsCountReserved,omitempty"`
	NumSequenceParameterSets *AVCUnsignedField        `json:"numSequenceParameterSets,omitempty"`
	SequenceParameterSets    []AVCNALUnitDetail       `json:"sequenceParameterSets"`
	NumPictureParameterSets  *AVCUnsignedField        `json:"numPictureParameterSets,omitempty"`
	PictureParameterSets     []AVCNALUnitDetail       `json:"pictureParameterSets"`
	HighProfileExtension     *AVCHighProfileExtension `json:"highProfileExtension,omitempty"`
}

type AVCHighProfileExtension struct {
	ChromaFormatReserved       *AVCUnsignedField  `json:"chromaFormatReserved,omitempty"`
	ChromaFormat               *AVCUnsignedField  `json:"chromaFormat,omitempty"`
	BitDepthLumaReserved       *AVCUnsignedField  `json:"bitDepthLumaReserved,omitempty"`
	BitDepthLumaMinus8         *AVCUnsignedField  `json:"bitDepthLumaMinus8,omitempty"`
	BitDepthChromaReserved     *AVCUnsignedField  `json:"bitDepthChromaReserved,omitempty"`
	BitDepthChromaMinus8       *AVCUnsignedField  `json:"bitDepthChromaMinus8,omitempty"`
	NumSequenceParameterSetExt *AVCUnsignedField  `json:"numSequenceParameterSetExt,omitempty"`
	SequenceParameterSetExts   []AVCNALUnitDetail `json:"sequenceParameterSetExts"`
}

type AVCNALUnitSequenceDetail struct {
	Span            Span               `json:"span"`
	SyntaxSpan      Span               `json:"syntaxSpan"`
	TrailingSpan    Span               `json:"trailingSpan"`
	BytesConsumed   uint64             `json:"bytesConsumed"`
	LengthSize      uint8              `json:"lengthSize"`
	NALUnits        []AVCNALUnitDetail `json:"nalUnits"`
	FramingComplete bool               `json:"framingComplete"`
	Complete        bool               `json:"complete"`
}

type AVCSPSHighProfileDetail struct {
	ChromaFormatIDC                 *AVCMappedUnsignedField `json:"chromaFormatIdc,omitempty"`
	SeparateColourPlaneFlag         *AVCMappedBooleanField  `json:"separateColourPlaneFlag,omitempty"`
	BitDepthLumaMinus8              *AVCMappedUnsignedField `json:"bitDepthLumaMinus8,omitempty"`
	BitDepthChromaMinus8            *AVCMappedUnsignedField `json:"bitDepthChromaMinus8,omitempty"`
	QPPrimeYZeroTransformBypassFlag *AVCMappedBooleanField  `json:"qpPrimeYZeroTransformBypassFlag,omitempty"`
	SequenceScalingMatrixPresent    *AVCMappedBooleanField  `json:"sequenceScalingMatrixPresentFlag,omitempty"`
}

type AVCSequenceParameterSet struct {
	VUI                            *AVCVUIDetail            `json:"vui,omitempty"`
	RBSP                           AVCRBSPViewDetail        `json:"rbsp"`
	ProfileIDC                     *AVCMappedUnsignedField  `json:"profileIdc,omitempty"`
	ConstraintSetFlags             *AVCMappedUnsignedField  `json:"constraintSetFlags,omitempty"`
	ReservedZero2Bits              *AVCMappedUnsignedField  `json:"reservedZero2Bits,omitempty"`
	LevelIDC                       *AVCMappedUnsignedField  `json:"levelIdc,omitempty"`
	SequenceParameterSetID         *AVCMappedUnsignedField  `json:"sequenceParameterSetId,omitempty"`
	HighProfile                    *AVCSPSHighProfileDetail `json:"highProfile,omitempty"`
	Log2MaxFrameNumMinus4          *AVCMappedUnsignedField  `json:"log2MaxFrameNumMinus4,omitempty"`
	PicOrderCntType                *AVCMappedUnsignedField  `json:"picOrderCntType,omitempty"`
	Log2MaxPicOrderCntLSBMinus4    *AVCMappedUnsignedField  `json:"log2MaxPicOrderCntLsbMinus4,omitempty"`
	DeltaPicOrderAlwaysZeroFlag    *AVCMappedBooleanField   `json:"deltaPicOrderAlwaysZeroFlag,omitempty"`
	OffsetForNonRefPic             *AVCMappedSignedField    `json:"offsetForNonRefPic,omitempty"`
	OffsetForTopToBottomField      *AVCMappedSignedField    `json:"offsetForTopToBottomField,omitempty"`
	NumRefFramesInPicOrderCntCycle *AVCMappedUnsignedField  `json:"numRefFramesInPicOrderCntCycle,omitempty"`
	MaxNumRefFrames                *AVCMappedUnsignedField  `json:"maxNumRefFrames,omitempty"`
	GapsInFrameNumValueAllowedFlag *AVCMappedBooleanField   `json:"gapsInFrameNumValueAllowedFlag,omitempty"`
	PicWidthInMbsMinus1            *AVCMappedUnsignedField  `json:"picWidthInMbsMinus1,omitempty"`
	PicHeightInMapUnitsMinus1      *AVCMappedUnsignedField  `json:"picHeightInMapUnitsMinus1,omitempty"`
	FrameMbsOnlyFlag               *AVCMappedBooleanField   `json:"frameMbsOnlyFlag,omitempty"`
	MBAdaptiveFrameFieldFlag       *AVCMappedBooleanField   `json:"mbAdaptiveFrameFieldFlag,omitempty"`
	Direct8x8InferenceFlag         *AVCMappedBooleanField   `json:"direct8x8InferenceFlag,omitempty"`
	FrameCroppingFlag              *AVCMappedBooleanField   `json:"frameCroppingFlag,omitempty"`
	FrameCropLeftOffset            *AVCMappedUnsignedField  `json:"frameCropLeftOffset,omitempty"`
	FrameCropRightOffset           *AVCMappedUnsignedField  `json:"frameCropRightOffset,omitempty"`
	FrameCropTopOffset             *AVCMappedUnsignedField  `json:"frameCropTopOffset,omitempty"`
	FrameCropBottomOffset          *AVCMappedUnsignedField  `json:"frameCropBottomOffset,omitempty"`
	VUIParametersPresentFlag       *AVCMappedBooleanField   `json:"vuiParametersPresentFlag,omitempty"`
	ChromaArrayType                *uint32                  `json:"chromaArrayType,omitempty"`
	CropUnitX                      *uint32                  `json:"cropUnitX,omitempty"`
	CropUnitY                      *uint32                  `json:"cropUnitY,omitempty"`
	CodedWidth                     *uint64                  `json:"codedWidth,omitempty"`
	CodedHeight                    *uint64                  `json:"codedHeight,omitempty"`
	DisplayWidth                   *uint64                  `json:"displayWidth,omitempty"`
	DisplayHeight                  *uint64                  `json:"displayHeight,omitempty"`
	SyntaxBits                     uint32                   `json:"syntaxBits"`
	RemainingRBSPBits              uint32                   `json:"remainingRbspBits"`
	Complete                       bool                     `json:"complete"`
}

type AVCPictureParameterSet struct {
	RBSP                                  AVCRBSPViewDetail       `json:"rbsp"`
	PictureParameterSetID                 *AVCMappedUnsignedField `json:"pictureParameterSetId,omitempty"`
	SequenceParameterSetID                *AVCMappedUnsignedField `json:"sequenceParameterSetId,omitempty"`
	EntropyCodingModeFlag                 *AVCMappedBooleanField  `json:"entropyCodingModeFlag,omitempty"`
	BottomFieldPicOrderInFramePresentFlag *AVCMappedBooleanField  `json:"bottomFieldPicOrderInFramePresentFlag,omitempty"`
	SyntaxBits                            uint32                  `json:"syntaxBits"`
	RemainingRBSPBits                     uint32                  `json:"remainingRbspBits"`
	Complete                              bool                    `json:"complete"`
}

func NewAVCTagDetail(sourceSize uint64, tag flv.TagRecord, detail flv.AVCVideo) AVCTagDetailDocument {
	document := AVCTagDetailDocument{
		SchemaVersion: AVCTagDetailSchemaVersion,
		DocumentType:  AVCTagDetailDocumentType,
		Source:        Source{SizeBytes: sourceSize},
		Tag:           mapTag(tag),
		Detail: AVCVideoDetail{
			Kind: AVCVideoKind, Span: mapSpan(detail.Span),
			BitsConsumed: detail.BitsConsumed, Complete: detail.Complete,
		},
		Diagnostics: make([]TagDetailDiagnostic, 0, len(detail.Diagnostics)),
	}
	if detail.HeaderPresent {
		document.Detail.VideoTagHeader = &FLVVideoTagHeaderDetail{
			Raw: detail.Header.Raw, Span: mapSpan(detail.Header.Span),
			FrameType: AVCNamedField{Value: detail.Header.FrameType, Name: frameTypeName(detail.Header.FrameType), Span: mapSpan(detail.Header.FrameTypeSpan)},
			CodecID:   AVCNamedField{Value: detail.Header.CodecID, Name: codecIDName(detail.Header.CodecID), Span: mapSpan(detail.Header.CodecIDSpan)},
		}
	}
	if detail.PacketTypePresent {
		document.Detail.PacketType = &AVCPacketTypeDetail{Value: uint8(detail.PacketType), Name: detail.PacketType.String(), Span: mapSpan(detail.PacketTypeSpan)}
	}
	if detail.CompositionTimePresent {
		document.Detail.CompositionTime = &AVCCompositionTimeDetail{
			Raw: uint32(detail.CompositionTime) & 0x00ff_ffff, Value: detail.CompositionTime, Span: mapSpan(detail.CompositionTimeSpan),
		}
		span := mapSpan(detail.DataSpan)
		document.Detail.DataSpan = &span
	}
	if detail.ConfigPresent {
		config := mapAVCConfigurationRecord(detail.Config)
		document.Detail.AVCDecoderConfigurationRecord = &config
	}
	if detail.NALUnitsPresent {
		sequence := mapAVCNALUnitSequence(detail.NALUnits)
		document.Detail.NALUnitSequence = &sequence
	}
	if detail.EndOfSequencePresent {
		span := mapSpan(detail.DataSpan)
		document.Detail.EndOfSequenceSpan = &span
	}
	for _, diagnostic := range detail.Diagnostics {
		document.Diagnostics = append(document.Diagnostics, TagDetailDiagnostic{
			Code: diagnostic.Code, Severity: amf0.Severity(diagnostic.Severity),
			Offset: diagnostic.Offset, BitOffset: diagnostic.BitOffset, Message: diagnostic.Message,
			Expected: cloneNumber(diagnostic.Expected), Actual: cloneNumber(diagnostic.Actual), RecoveryOffset: cloneNumber(diagnostic.RecoveryOffset),
		})
	}
	return document
}

func WriteAVCTagDetailJSON(output io.Writer, document AVCTagDetailDocument, pretty bool) error {
	return writeTagDetailJSON(output, document, pretty)
}

func mapAVCConfigurationRecord(config avc.ConfigurationRecord) AVCConfigurationRecordDetail {
	mapped := AVCConfigurationRecordDetail{
		Span: mapSpan(config.Span), SyntaxSpan: mapSpan(config.SyntaxSpan), TrailingSpan: mapSpan(config.TrailingSpan),
		BytesConsumed: config.BytesConsumed, FramingComplete: config.FramingComplete, Complete: config.Complete,
		SequenceParameterSets: mapAVCParameterSets(config.SequenceParameterSets),
		PictureParameterSets:  mapAVCParameterSets(config.PictureParameterSets),
	}
	mapped.ConfigurationVersion = avcUintField(config.ConfigurationVersion, config.ConfigurationVersionSpan)
	mapped.AVCProfileIndication = avcUintField(config.AVCProfileIndication, config.AVCProfileIndicationSpan)
	mapped.ProfileCompatibility = avcUintField(config.ProfileCompatibility, config.ProfileCompatibilitySpan)
	mapped.AVCLevelIndication = avcUintField(config.AVCLevelIndication, config.AVCLevelIndicationSpan)
	mapped.LengthSizeReserved = avcUintField(config.LengthSizeReserved, config.LengthSizeReservedSpan)
	mapped.LengthSizeMinusOne = avcUintField(config.LengthSizeMinusOne, config.LengthSizeMinusOneSpan)
	if config.LengthSize == 1 || config.LengthSize == 2 || config.LengthSize == 4 {
		value := config.LengthSize
		mapped.LengthSize = &value
	}
	mapped.SPSCountReserved = avcUintField(config.SPSCountReserved, config.SPSCountReservedSpan)
	mapped.NumSequenceParameterSets = avcUintField(config.NumSequenceParameterSets, config.NumSequenceParameterSetsSpan)
	mapped.NumPictureParameterSets = avcUintField(config.NumPictureParameterSets, config.NumPictureParameterSetsSpan)
	if config.HighProfileFieldsPresent {
		mapped.HighProfileExtension = &AVCHighProfileExtension{
			ChromaFormatReserved:       avcUintField(config.ChromaFormatReserved, config.ChromaFormatReservedSpan),
			ChromaFormat:               avcUintField(config.ChromaFormat, config.ChromaFormatSpan),
			BitDepthLumaReserved:       avcUintField(config.BitDepthLumaReserved, config.BitDepthLumaReservedSpan),
			BitDepthLumaMinus8:         avcUintField(config.BitDepthLumaMinus8, config.BitDepthLumaMinus8Span),
			BitDepthChromaReserved:     avcUintField(config.BitDepthChromaReserved, config.BitDepthChromaReservedSpan),
			BitDepthChromaMinus8:       avcUintField(config.BitDepthChromaMinus8, config.BitDepthChromaMinus8Span),
			NumSequenceParameterSetExt: avcUintField(config.NumSequenceParameterSetExt, config.NumSequenceParameterSetExtSpan),
			SequenceParameterSetExts:   mapAVCParameterSets(config.SequenceParameterSetExts),
		}
	}
	return mapped
}

func mapAVCParameterSets(items []avc.ParameterSet) []AVCNALUnitDetail {
	mapped := make([]AVCNALUnitDetail, 0, len(items))
	for _, item := range items {
		mapped = append(mapped, mapAVCNALUnit(item.Index, item.Length, item.LengthSpan, item.Span, item.Header, item.HeaderPresent, item.SequenceParameterSet, item.PictureParameterSet, item.SummaryComplete))
	}
	return mapped
}

func mapAVCNALUnitSequence(sequence avc.NALUnitSequence) AVCNALUnitSequenceDetail {
	mapped := AVCNALUnitSequenceDetail{
		Span: mapSpan(sequence.Span), SyntaxSpan: mapSpan(sequence.SyntaxSpan), TrailingSpan: mapSpan(sequence.TrailingSpan),
		BytesConsumed: sequence.BytesConsumed, LengthSize: sequence.LengthSize,
		NALUnits:        make([]AVCNALUnitDetail, 0, len(sequence.NALUnits)),
		FramingComplete: sequence.FramingComplete, Complete: sequence.Complete,
	}
	for _, item := range sequence.NALUnits {
		mapped.NALUnits = append(mapped.NALUnits, mapAVCNALUnit(item.Index, item.Length, item.LengthSpan, item.Span, item.Header, item.HeaderPresent, item.SequenceParameterSet, item.PictureParameterSet, item.SummaryComplete))
	}
	return mapped
}

func mapAVCNALUnit(index, length uint32, lengthSpan, span cursor.Span, header avc.NALHeader, headerPresent bool, sps *avc.SequenceParameterSet, pps *avc.PictureParameterSet, complete bool) AVCNALUnitDetail {
	mapped := AVCNALUnitDetail{Index: index, Length: length, LengthSpan: mapSpan(lengthSpan), Span: mapSpan(span), SummaryComplete: complete}
	if headerPresent {
		mapped.Header = &AVCNALHeaderDetail{
			Raw: header.Raw, Span: mapSpan(header.Span),
			ForbiddenZeroBit: AVCBooleanField{Value: header.ForbiddenZeroBit, Span: mapSpan(header.ForbiddenZeroBitSpan)},
			NALRefIDC:        AVCUnsignedField{Value: uint64(header.NALRefIDC), Span: mapSpan(header.NALRefIDCSpan)},
			NALUnitType:      AVCNamedField{Value: uint8(header.NALUnitType), Name: header.NALUnitTypeName, Span: mapSpan(header.NALUnitTypeSpan)},
			Complete:         header.Complete,
		}
	}
	if sps != nil {
		value := mapAVCSPS(*sps)
		mapped.SequenceParameterSet = &value
	}
	if pps != nil {
		value := mapAVCPPS(*pps)
		mapped.PictureParameterSet = &value
	}
	return mapped
}

func mapAVCSPS(sps avc.SequenceParameterSet) AVCSequenceParameterSet {
	mapped := AVCSequenceParameterSet{
		RBSP: mapAVCRBSP(sps.RBSP), SyntaxBits: sps.SyntaxBits,
		RemainingRBSPBits: sps.RemainingRBSPBits, Complete: sps.Complete,
	}
	mapped.ProfileIDC = avcMappedUintField(uint32(sps.ProfileIDC), sps.ProfileIDCSpan)
	mapped.ConstraintSetFlags = avcMappedUintField(uint32(sps.ConstraintSetFlags), sps.ConstraintSetFlagsSpan)
	mapped.ReservedZero2Bits = avcMappedUintField(uint32(sps.ReservedZero2Bits), sps.ReservedZero2BitsSpan)
	mapped.LevelIDC = avcMappedUintField(uint32(sps.LevelIDC), sps.LevelIDCSpan)
	mapped.SequenceParameterSetID = avcMappedUintField(sps.SequenceParameterSetID, sps.SequenceParameterSetIDSpan)
	if sps.HighProfileFieldsPresent {
		mapped.HighProfile = &AVCSPSHighProfileDetail{
			ChromaFormatIDC:                 avcMappedUintField(sps.ChromaFormatIDC, sps.ChromaFormatIDCSpan),
			BitDepthLumaMinus8:              avcMappedUintField(sps.BitDepthLumaMinus8, sps.BitDepthLumaMinus8Span),
			BitDepthChromaMinus8:            avcMappedUintField(sps.BitDepthChromaMinus8, sps.BitDepthChromaMinus8Span),
			QPPrimeYZeroTransformBypassFlag: avcMappedBoolField(sps.QPPrimeYZeroTransformBypassFlag, sps.QPPrimeYZeroTransformBypassFlagSpan),
			SequenceScalingMatrixPresent:    avcMappedBoolField(sps.SequenceScalingMatrixPresentFlag, sps.SequenceScalingMatrixPresentFlagSpan),
		}
		if sps.SeparateColourPlaneFlagPresent {
			mapped.HighProfile.SeparateColourPlaneFlag = avcMappedBoolField(sps.SeparateColourPlaneFlag, sps.SeparateColourPlaneFlagSpan)
		}
	}
	mapped.Log2MaxFrameNumMinus4 = avcMappedUintField(sps.Log2MaxFrameNumMinus4, sps.Log2MaxFrameNumMinus4Span)
	mapped.PicOrderCntType = avcMappedUintField(sps.PicOrderCntType, sps.PicOrderCntTypeSpan)
	if sps.Log2MaxPicOrderCntLSBMinus4Present {
		mapped.Log2MaxPicOrderCntLSBMinus4 = avcMappedUintField(sps.Log2MaxPicOrderCntLSBMinus4, sps.Log2MaxPicOrderCntLSBMinus4Span)
	}
	if sps.DeltaPicOrderAlwaysZeroFlagPresent {
		mapped.DeltaPicOrderAlwaysZeroFlag = avcMappedBoolField(sps.DeltaPicOrderAlwaysZeroFlag, sps.DeltaPicOrderAlwaysZeroFlagSpan)
		mapped.OffsetForNonRefPic = avcMappedSignedField(sps.OffsetForNonRefPic, sps.OffsetForNonRefPicSpan)
		mapped.OffsetForTopToBottomField = avcMappedSignedField(sps.OffsetForTopToBottomField, sps.OffsetForTopToBottomFieldSpan)
		mapped.NumRefFramesInPicOrderCntCycle = avcMappedUintField(sps.NumRefFramesInPicOrderCntCycle, sps.NumRefFramesInPicOrderCntCycleSpan)
	}
	mapped.MaxNumRefFrames = avcMappedUintField(sps.MaxNumRefFrames, sps.MaxNumRefFramesSpan)
	mapped.GapsInFrameNumValueAllowedFlag = avcMappedBoolField(sps.GapsInFrameNumValueAllowedFlag, sps.GapsInFrameNumValueAllowedFlagSpan)
	mapped.PicWidthInMbsMinus1 = avcMappedUintField(sps.PicWidthInMbsMinus1, sps.PicWidthInMbsMinus1Span)
	mapped.PicHeightInMapUnitsMinus1 = avcMappedUintField(sps.PicHeightInMapUnitsMinus1, sps.PicHeightInMapUnitsMinus1Span)
	mapped.FrameMbsOnlyFlag = avcMappedBoolField(sps.FrameMbsOnlyFlag, sps.FrameMbsOnlyFlagSpan)
	if sps.MBAdaptiveFrameFieldFlagPresent {
		mapped.MBAdaptiveFrameFieldFlag = avcMappedBoolField(sps.MBAdaptiveFrameFieldFlag, sps.MBAdaptiveFrameFieldFlagSpan)
	}
	mapped.Direct8x8InferenceFlag = avcMappedBoolField(sps.Direct8x8InferenceFlag, sps.Direct8x8InferenceFlagSpan)
	mapped.FrameCroppingFlag = avcMappedBoolField(sps.FrameCroppingFlag, sps.FrameCroppingFlagSpan)
	if sps.FrameCroppingFlag {
		mapped.FrameCropLeftOffset = avcMappedUintField(sps.FrameCropLeftOffset, sps.FrameCropLeftOffsetSpan)
		mapped.FrameCropRightOffset = avcMappedUintField(sps.FrameCropRightOffset, sps.FrameCropRightOffsetSpan)
		mapped.FrameCropTopOffset = avcMappedUintField(sps.FrameCropTopOffset, sps.FrameCropTopOffsetSpan)
		mapped.FrameCropBottomOffset = avcMappedUintField(sps.FrameCropBottomOffset, sps.FrameCropBottomOffsetSpan)
	}
	mapped.VUIParametersPresentFlag = avcMappedBoolField(sps.VUIParametersPresentFlag, sps.VUIParametersPresentFlagSpan)
	if sps.CodedWidth > 0 && sps.CodedHeight > 0 {
		mapped.ChromaArrayType = avcUint32(sps.ChromaArrayType)
		mapped.CropUnitX = avcUint32(sps.CropUnitX)
		mapped.CropUnitY = avcUint32(sps.CropUnitY)
		mapped.CodedWidth = avcUint64(sps.CodedWidth)
		mapped.CodedHeight = avcUint64(sps.CodedHeight)
	}
	if sps.DisplayWidth > 0 && sps.DisplayHeight > 0 {
		mapped.DisplayWidth = avcUint64(sps.DisplayWidth)
		mapped.DisplayHeight = avcUint64(sps.DisplayHeight)
	}
	return mapped
}

func mapAVCPPS(pps avc.PictureParameterSet) AVCPictureParameterSet {
	return AVCPictureParameterSet{
		RBSP:                                  mapAVCRBSP(pps.RBSP),
		PictureParameterSetID:                 avcMappedUintField(pps.PictureParameterSetID, pps.PictureParameterSetIDSpan),
		SequenceParameterSetID:                avcMappedUintField(pps.SequenceParameterSetID, pps.SequenceParameterSetIDSpan),
		EntropyCodingModeFlag:                 avcMappedBoolField(pps.EntropyCodingModeFlag, pps.EntropyCodingModeFlagSpan),
		BottomFieldPicOrderInFramePresentFlag: avcMappedBoolField(pps.BottomFieldPicOrderInFramePresentFlag, pps.BottomFieldPicOrderInFramePresentFlagSpan),
		SyntaxBits:                            pps.SyntaxBits, RemainingRBSPBits: pps.RemainingRBSPBits, Complete: pps.Complete,
	}
}

func mapAVCRBSP(view avc.RBSPView) AVCRBSPViewDetail {
	mapped := AVCRBSPViewDetail{
		EBSPSpan: mapSpan(view.EBSPSpan), ByteLength: view.ByteLength,
		SourceSegments:           make([]AVCRBSPSourceSegmentDetail, 0, len(view.SourceSegments)),
		EmulationPreventionBytes: make([]Span, 0, len(view.EmulationPreventionBytes)),
	}
	for _, segment := range view.SourceSegments {
		mapped.SourceSegments = append(mapped.SourceSegments, AVCRBSPSourceSegmentDetail{
			RBSPByteOffset: segment.RBSPByteOffset, RBSPByteLength: segment.RBSPByteLength, SourceSpan: mapSpan(segment.SourceSpan),
		})
	}
	for _, span := range view.EmulationPreventionBytes {
		mapped.EmulationPreventionBytes = append(mapped.EmulationPreventionBytes, mapSpan(span))
	}
	return mapped
}

func mapAVCMappedSpan(span avc.MappedSpan) AVCMappedSpan {
	mapped := AVCMappedSpan{RBSPBitOffset: span.RBSPBitOffset, BitLength: span.BitLength, SourceSpans: make([]Span, 0, len(span.SourceSpans))}
	for _, source := range span.SourceSpans {
		mapped.SourceSpans = append(mapped.SourceSpans, mapSpan(source))
	}
	return mapped
}

func avcUintField(value uint8, span cursor.Span) *AVCUnsignedField {
	if span.BitLength == 0 {
		return nil
	}
	return &AVCUnsignedField{Value: uint64(value), Span: mapSpan(span)}
}

func avcMappedUintField(value uint32, span avc.MappedSpan) *AVCMappedUnsignedField {
	if span.BitLength == 0 {
		return nil
	}
	return &AVCMappedUnsignedField{Value: uint64(value), Span: mapAVCMappedSpan(span)}
}

func avcMappedBoolField(value bool, span avc.MappedSpan) *AVCMappedBooleanField {
	if span.BitLength == 0 {
		return nil
	}
	return &AVCMappedBooleanField{Value: value, Span: mapAVCMappedSpan(span)}
}

func avcMappedSignedField(value int64, span avc.MappedSpan) *AVCMappedSignedField {
	if span.BitLength == 0 {
		return nil
	}
	return &AVCMappedSignedField{Value: value, Span: mapAVCMappedSpan(span)}
}

func avcUint32(value uint32) *uint32 { return &value }
func avcUint64(value uint64) *uint64 { return &value }

func frameTypeName(value uint8) string {
	switch value {
	case 1:
		return "keyFrame"
	case 2:
		return "interFrame"
	case 3:
		return "disposableInterFrame"
	case 4:
		return "generatedKeyFrame"
	case 5:
		return "videoInfoOrCommand"
	default:
		return "reserved"
	}
}

func codecIDName(value uint8) string {
	if value == flv.CodecIDAVC {
		return "AVC"
	}
	return "unknown"
}

var _ ParsedTagDetailDocument = AVCTagDetailDocument{}

// NewAVCTagDetailV2 publishes the explicitly requested SPS VUI extension.
// ParseVUI must be enabled for the selected Tag before projection. The default
// constructor deliberately leaves this extension out of the v1 contract.
func NewAVCTagDetailV2(sourceSize uint64, tag flv.TagRecord, detail flv.AVCVideo) AVCTagDetailDocument {
	result := NewAVCTagDetail(sourceSize, tag, detail)
	result.SchemaVersion = 2
	attach := func(mapped []AVCNALUnitDetail, parsed []avc.ParameterSet) {
		for i, item := range parsed {
			if item.SequenceParameterSet != nil && item.SequenceParameterSet.VUI != nil {
				mapped[i].SequenceParameterSet.VUI = mapAVCVUI(item.SequenceParameterSet.VUI)
			}
		}
	}
	if c := result.Detail.AVCDecoderConfigurationRecord; c != nil {
		attach(c.SequenceParameterSets, detail.Config.SequenceParameterSets)
		attach(c.PictureParameterSets, detail.Config.PictureParameterSets)
		if c.HighProfileExtension != nil {
			attach(c.HighProfileExtension.SequenceParameterSetExts, detail.Config.SequenceParameterSetExts)
		}
	}
	if s := result.Detail.NALUnitSequence; s != nil {
		for i, item := range detail.NALUnits.NALUnits {
			if item.SequenceParameterSet != nil && item.SequenceParameterSet.VUI != nil {
				s.NALUnits[i].SequenceParameterSet.VUI = mapAVCVUI(item.SequenceParameterSet.VUI)
			}
		}
	}
	return result
}

type AVCVUIDetail struct {
	Present                            bool                    `json:"present"`
	AspectRatioInfoPresentFlag         *AVCMappedBooleanField  `json:"aspectRatioInfoPresentFlag,omitempty"`
	AspectRatioIDC                     *AVCMappedUnsignedField `json:"aspectRatioIdc,omitempty"`
	SARWidth                           *AVCMappedUnsignedField `json:"sarWidth,omitempty"`
	SARHeight                          *AVCMappedUnsignedField `json:"sarHeight,omitempty"`
	OverscanInfoPresentFlag            *AVCMappedBooleanField  `json:"overscanInfoPresentFlag,omitempty"`
	OverscanAppropriateFlag            *AVCMappedBooleanField  `json:"overscanAppropriateFlag,omitempty"`
	VideoSignalTypePresentFlag         *AVCMappedBooleanField  `json:"videoSignalTypePresentFlag,omitempty"`
	VideoFormat                        *AVCMappedUnsignedField `json:"videoFormat,omitempty"`
	VideoFullRangeFlag                 *AVCMappedBooleanField  `json:"videoFullRangeFlag,omitempty"`
	ColourDescriptionPresentFlag       *AVCMappedBooleanField  `json:"colourDescriptionPresentFlag,omitempty"`
	ColourPrimaries                    *AVCMappedUnsignedField `json:"colourPrimaries,omitempty"`
	TransferCharacteristics            *AVCMappedUnsignedField `json:"transferCharacteristics,omitempty"`
	MatrixCoefficients                 *AVCMappedUnsignedField `json:"matrixCoefficients,omitempty"`
	ChromaLocInfoPresentFlag           *AVCMappedBooleanField  `json:"chromaLocInfoPresentFlag,omitempty"`
	ChromaSampleLocTypeTopField        *AVCMappedUnsignedField `json:"chromaSampleLocTypeTopField,omitempty"`
	ChromaSampleLocTypeBottomField     *AVCMappedUnsignedField `json:"chromaSampleLocTypeBottomField,omitempty"`
	TimingInfoPresentFlag              *AVCMappedBooleanField  `json:"timingInfoPresentFlag,omitempty"`
	NumUnitsInTick                     *AVCMappedUnsignedField `json:"numUnitsInTick,omitempty"`
	TimeScale                          *AVCMappedUnsignedField `json:"timeScale,omitempty"`
	FixedFrameRateFlag                 *AVCMappedBooleanField  `json:"fixedFrameRateFlag,omitempty"`
	NALHRDParametersPresentFlag        *AVCMappedBooleanField  `json:"nalHrdParametersPresentFlag,omitempty"`
	VCLHRDParametersPresentFlag        *AVCMappedBooleanField  `json:"vclHrdParametersPresentFlag,omitempty"`
	LowDelayHRDFlag                    *AVCMappedBooleanField  `json:"lowDelayHrdFlag,omitempty"`
	PicStructPresentFlag               *AVCMappedBooleanField  `json:"picStructPresentFlag,omitempty"`
	BitstreamRestrictionFlag           *AVCMappedBooleanField  `json:"bitstreamRestrictionFlag,omitempty"`
	MotionVectorsOverPicBoundariesFlag *AVCMappedBooleanField  `json:"motionVectorsOverPicBoundariesFlag,omitempty"`
	MaxBytesPerPicDenom                *AVCMappedUnsignedField `json:"maxBytesPerPicDenom,omitempty"`
	MaxBitsPerMBDenom                  *AVCMappedUnsignedField `json:"maxBitsPerMbDenom,omitempty"`
	Log2MaxMVLengthHorizontal          *AVCMappedUnsignedField `json:"log2MaxMvLengthHorizontal,omitempty"`
	Log2MaxMVLengthVertical            *AVCMappedUnsignedField `json:"log2MaxMvLengthVertical,omitempty"`
	MaxNumReorderFrames                *AVCMappedUnsignedField `json:"maxNumReorderFrames,omitempty"`
	MaxDecFrameBuffering               *AVCMappedUnsignedField `json:"maxDecFrameBuffering,omitempty"`

	NALHRD            *AVCHRDDetail `json:"nalHrd,omitempty"`
	VCLHRD            *AVCHRDDetail `json:"vclHrd,omitempty"`
	RBSPBitOffset     uint32        `json:"rbspBitOffset"`
	SyntaxBits        uint32        `json:"syntaxBits"`
	RemainingRBSPBits uint32        `json:"remainingRbspBits"`
	TrailingBitsValid bool          `json:"trailingBitsValid"`
	Complete          bool          `json:"complete"`
}

type AVCHRDDetail struct {
	CPBCntMinus1                       *AVCMappedUnsignedField `json:"cpbCntMinus1,omitempty"`
	BitRateScale                       *AVCMappedUnsignedField `json:"bitRateScale,omitempty"`
	CPBSizeScale                       *AVCMappedUnsignedField `json:"cpbSizeScale,omitempty"`
	InitialCPBRemovalDelayLengthMinus1 *AVCMappedUnsignedField `json:"initialCpbRemovalDelayLengthMinus1,omitempty"`
	CPBRemovalDelayLengthMinus1        *AVCMappedUnsignedField `json:"cpbRemovalDelayLengthMinus1,omitempty"`
	DPBOutputDelayLengthMinus1         *AVCMappedUnsignedField `json:"dpbOutputDelayLengthMinus1,omitempty"`
	TimeOffsetLength                   *AVCMappedUnsignedField `json:"timeOffsetLength,omitempty"`

	CPB      []AVCHRDCPBEntry `json:"cpb"`
	Complete bool             `json:"complete"`
}

type AVCHRDCPBEntry struct {
	BitRateValueMinus1 *AVCMappedUnsignedField `json:"bitRateValueMinus1,omitempty"`
	CPBSizeValueMinus1 *AVCMappedUnsignedField `json:"cpbSizeValueMinus1,omitempty"`
	CBRFlag            *AVCMappedBooleanField  `json:"cbrFlag,omitempty"`
}

func mapAVCVUI(v *avc.VUIParameters) *AVCVUIDetail {
	r := &AVCVUIDetail{Present: v.Present, RBSPBitOffset: v.RBSPBitOffset, SyntaxBits: v.SyntaxBits,
		RemainingRBSPBits: v.RemainingRBSPBits, TrailingBitsValid: v.TrailingBitsValid, Complete: v.Complete,
		NALHRD: mapAVCHRD(v.NALHRD), VCLHRD: mapAVCHRD(v.VCLHRD)}
	r.AspectRatioInfoPresentFlag = mapVUIBool(v.AspectRatioInfoPresentFlag)
	r.AspectRatioIDC = mapVUIUint(v.AspectRatioIDC)
	r.SARWidth = mapVUIUint(v.SARWidth)
	r.SARHeight = mapVUIUint(v.SARHeight)
	r.OverscanInfoPresentFlag = mapVUIBool(v.OverscanInfoPresentFlag)
	r.OverscanAppropriateFlag = mapVUIBool(v.OverscanAppropriateFlag)
	r.VideoSignalTypePresentFlag = mapVUIBool(v.VideoSignalTypePresentFlag)
	r.VideoFormat = mapVUIUint(v.VideoFormat)
	r.VideoFullRangeFlag = mapVUIBool(v.VideoFullRangeFlag)
	r.ColourDescriptionPresentFlag = mapVUIBool(v.ColourDescriptionPresentFlag)
	r.ColourPrimaries = mapVUIUint(v.ColourPrimaries)
	r.TransferCharacteristics = mapVUIUint(v.TransferCharacteristics)
	r.MatrixCoefficients = mapVUIUint(v.MatrixCoefficients)
	r.ChromaLocInfoPresentFlag = mapVUIBool(v.ChromaLocInfoPresentFlag)
	r.ChromaSampleLocTypeTopField = mapVUIUint(v.ChromaSampleLocTypeTopField)
	r.ChromaSampleLocTypeBottomField = mapVUIUint(v.ChromaSampleLocTypeBottomField)
	r.TimingInfoPresentFlag = mapVUIBool(v.TimingInfoPresentFlag)
	r.NumUnitsInTick = mapVUIUint(v.NumUnitsInTick)
	r.TimeScale = mapVUIUint(v.TimeScale)
	r.FixedFrameRateFlag = mapVUIBool(v.FixedFrameRateFlag)
	r.NALHRDParametersPresentFlag = mapVUIBool(v.NALHRDParametersPresentFlag)
	r.VCLHRDParametersPresentFlag = mapVUIBool(v.VCLHRDParametersPresentFlag)
	r.LowDelayHRDFlag = mapVUIBool(v.LowDelayHRDFlag)
	r.PicStructPresentFlag = mapVUIBool(v.PicStructPresentFlag)
	r.BitstreamRestrictionFlag = mapVUIBool(v.BitstreamRestrictionFlag)
	r.MotionVectorsOverPicBoundariesFlag = mapVUIBool(v.MotionVectorsOverPicBoundariesFlag)
	r.MaxBytesPerPicDenom = mapVUIUint(v.MaxBytesPerPicDenom)
	r.MaxBitsPerMBDenom = mapVUIUint(v.MaxBitsPerMBDenom)
	r.Log2MaxMVLengthHorizontal = mapVUIUint(v.Log2MaxMVLengthHorizontal)
	r.Log2MaxMVLengthVertical = mapVUIUint(v.Log2MaxMVLengthVertical)
	r.MaxNumReorderFrames = mapVUIUint(v.MaxNumReorderFrames)
	r.MaxDecFrameBuffering = mapVUIUint(v.MaxDecFrameBuffering)
	return r
}

func mapAVCHRD(h *avc.HRDParameters) *AVCHRDDetail {
	if h == nil {
		return nil
	}
	r := &AVCHRDDetail{CPB: make([]AVCHRDCPBEntry, 0, len(h.CPB)), Complete: h.Complete}
	r.CPBCntMinus1 = mapVUIUint(h.CPBCntMinus1)
	r.BitRateScale = mapVUIUint(h.BitRateScale)
	r.CPBSizeScale = mapVUIUint(h.CPBSizeScale)
	r.InitialCPBRemovalDelayLengthMinus1 = mapVUIUint(h.InitialCPBRemovalDelayLengthMinus1)
	r.CPBRemovalDelayLengthMinus1 = mapVUIUint(h.CPBRemovalDelayLengthMinus1)
	r.DPBOutputDelayLengthMinus1 = mapVUIUint(h.DPBOutputDelayLengthMinus1)
	r.TimeOffsetLength = mapVUIUint(h.TimeOffsetLength)

	for _, entry := range h.CPB {
		r.CPB = append(r.CPB, AVCHRDCPBEntry{mapVUIUint(entry.BitRateValueMinus1), mapVUIUint(entry.CPBSizeValueMinus1), mapVUIBool(entry.CBRFlag)})
	}
	return r
}

func mapVUIUint(field *avc.VUIUnsignedField) *AVCMappedUnsignedField {
	if field == nil {
		return nil
	}
	return avcMappedUintField(field.Value, field.Span)
}

func mapVUIBool(field *avc.VUIBooleanField) *AVCMappedBooleanField {
	if field == nil {
		return nil
	}
	return avcMappedBoolField(field.Value, field.Span)
}
