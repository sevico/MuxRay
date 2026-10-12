package report

import (
	"errors"
	"fmt"
	"github.com/sevico/muxray/internal/avc"
	"github.com/sevico/muxray/internal/avccontext"
	"github.com/sevico/muxray/internal/flv"
)

type AVCSliceGroupRectangle struct {
	TopLeft     *AVCMappedUnsignedField `json:"topLeft,omitempty"`
	BottomRight *AVCMappedUnsignedField `json:"bottomRight,omitempty"`
}

func mapSliceGroupRectangle(p *avc.SliceGroupRectangle) *AVCSliceGroupRectangle {
	if p == nil {
		return nil
	}
	r := &AVCSliceGroupRectangle{}
	r.TopLeft = mapVUIUint(p.TopLeft)
	r.BottomRight = mapVUIUint(p.BottomRight)
	return r
}

type AVCScalingList struct {
	Index       uint32                  `json:"index"`
	PresentFlag *AVCMappedBooleanField  `json:"presentFlag,omitempty"`
	DeltaScales []*AVCMappedSignedField `json:"deltaScales,omitempty"`
	Values      []uint32                `json:"values,omitempty"`
	UseDefault  bool                    `json:"useDefault"`
}

func mapScalingList(p *avc.ScalingList) *AVCScalingList {
	if p == nil {
		return nil
	}
	r := &AVCScalingList{}
	r.Index = p.Index
	r.PresentFlag = mapVUIBool(p.PresentFlag)
	for _, v := range p.DeltaScales {
		r.DeltaScales = append(r.DeltaScales, mapPrefixSigned(v))
	}
	for _, v := range p.Values {
		r.Values = append(r.Values, v)
	}
	r.UseDefault = p.UseDefault
	return r
}

type AVCPPSFullDetails struct {
	SliceGroupMapType             *AVCMappedUnsignedField   `json:"sliceGroupMapType,omitempty"`
	RunLengthMinus1               []*AVCMappedUnsignedField `json:"runLengthMinus1,omitempty"`
	Rectangles                    []AVCSliceGroupRectangle  `json:"rectangles,omitempty"`
	SliceGroupChangeDirectionFlag *AVCMappedBooleanField    `json:"sliceGroupChangeDirectionFlag,omitempty"`
	SliceGroupChangeRateMinus1    *AVCMappedUnsignedField   `json:"sliceGroupChangeRateMinus1,omitempty"`
	PicSizeInMapUnitsMinus1       *AVCMappedUnsignedField   `json:"picSizeInMapUnitsMinus1,omitempty"`
	SliceGroupID                  []*AVCMappedUnsignedField `json:"sliceGroupId,omitempty"`
	ExtensionPresent              bool                      `json:"extensionPresent"`
	Transform8x8ModeFlag          *AVCMappedBooleanField    `json:"transform8x8ModeFlag,omitempty"`
	PicScalingMatrixPresentFlag   *AVCMappedBooleanField    `json:"picScalingMatrixPresentFlag,omitempty"`
	ScalingLists                  []AVCScalingList          `json:"scalingLists,omitempty"`
	SecondChromaQPIndexOffset     *AVCMappedSignedField     `json:"secondChromaQpIndexOffset,omitempty"`
	RBSPTrailingBits              *AVCMappedSpan            `json:"rbspTrailingBits,omitempty"`
	SyntaxBits                    uint32                    `json:"syntaxBits"`
	Complete                      bool                      `json:"complete"`
}

func mapPPSFullDetails(p *avc.PPSFullDetails) *AVCPPSFullDetails {
	if p == nil {
		return nil
	}
	r := &AVCPPSFullDetails{}
	r.SliceGroupMapType = mapVUIUint(p.SliceGroupMapType)
	for _, v := range p.RunLengthMinus1 {
		r.RunLengthMinus1 = append(r.RunLengthMinus1, mapVUIUint(v))
	}
	for _, v := range p.Rectangles {
		r.Rectangles = append(r.Rectangles, *mapSliceGroupRectangle(&v))
	}
	r.SliceGroupChangeDirectionFlag = mapVUIBool(p.SliceGroupChangeDirectionFlag)
	r.SliceGroupChangeRateMinus1 = mapVUIUint(p.SliceGroupChangeRateMinus1)
	r.PicSizeInMapUnitsMinus1 = mapVUIUint(p.PicSizeInMapUnitsMinus1)
	for _, v := range p.SliceGroupID {
		r.SliceGroupID = append(r.SliceGroupID, mapVUIUint(v))
	}
	r.ExtensionPresent = p.ExtensionPresent
	r.Transform8x8ModeFlag = mapVUIBool(p.Transform8x8ModeFlag)
	r.PicScalingMatrixPresentFlag = mapVUIBool(p.PicScalingMatrixPresentFlag)
	for _, v := range p.ScalingLists {
		r.ScalingLists = append(r.ScalingLists, *mapScalingList(&v))
	}
	r.SecondChromaQPIndexOffset = mapPrefixSigned(p.SecondChromaQPIndexOffset)
	if p.RBSPTrailingBits != nil {
		m := mapAVCMappedSpan(*p.RBSPTrailingBits)
		r.RBSPTrailingBits = &m
	}
	r.SyntaxBits = p.SyntaxBits
	r.Complete = p.Complete
	return r
}

type AVCReferenceListModification struct {
	ModificationOfPicNumsIDC *AVCMappedUnsignedField `json:"modificationOfPicNumsIdc,omitempty"`
	AbsDiffPicNumMinus1      *AVCMappedUnsignedField `json:"absDiffPicNumMinus1,omitempty"`
	LongTermPicNum           *AVCMappedUnsignedField `json:"longTermPicNum,omitempty"`
}

func mapReferenceListModification(p *avc.ReferenceListModification) *AVCReferenceListModification {
	if p == nil {
		return nil
	}
	r := &AVCReferenceListModification{}
	r.ModificationOfPicNumsIDC = mapVUIUint(p.ModificationOfPicNumsIDC)
	r.AbsDiffPicNumMinus1 = mapVUIUint(p.AbsDiffPicNumMinus1)
	r.LongTermPicNum = mapVUIUint(p.LongTermPicNum)
	return r
}

type AVCPredictionWeight struct {
	LumaWeightFlag   *AVCMappedBooleanField  `json:"lumaWeightFlag,omitempty"`
	ChromaWeightFlag *AVCMappedBooleanField  `json:"chromaWeightFlag,omitempty"`
	LumaWeight       *AVCMappedSignedField   `json:"lumaWeight,omitempty"`
	LumaOffset       *AVCMappedSignedField   `json:"lumaOffset,omitempty"`
	ChromaWeight     []*AVCMappedSignedField `json:"chromaWeight,omitempty"`
	ChromaOffset     []*AVCMappedSignedField `json:"chromaOffset,omitempty"`
}

func mapPredictionWeight(p *avc.PredictionWeight) *AVCPredictionWeight {
	if p == nil {
		return nil
	}
	r := &AVCPredictionWeight{}
	r.LumaWeightFlag = mapVUIBool(p.LumaWeightFlag)
	r.ChromaWeightFlag = mapVUIBool(p.ChromaWeightFlag)
	r.LumaWeight = mapPrefixSigned(p.LumaWeight)
	r.LumaOffset = mapPrefixSigned(p.LumaOffset)
	for _, v := range p.ChromaWeight {
		r.ChromaWeight = append(r.ChromaWeight, mapPrefixSigned(v))
	}
	for _, v := range p.ChromaOffset {
		r.ChromaOffset = append(r.ChromaOffset, mapPrefixSigned(v))
	}
	return r
}

type AVCMemoryManagementOperation struct {
	Operation                 *AVCMappedUnsignedField `json:"operation,omitempty"`
	DifferenceOfPicNumsMinus1 *AVCMappedUnsignedField `json:"differenceOfPicNumsMinus1,omitempty"`
	LongTermPicNum            *AVCMappedUnsignedField `json:"longTermPicNum,omitempty"`
	LongTermFrameIdx          *AVCMappedUnsignedField `json:"longTermFrameIdx,omitempty"`
	MaxLongTermFrameIdxPlus1  *AVCMappedUnsignedField `json:"maxLongTermFrameIdxPlus1,omitempty"`
}

func mapMemoryManagementOperation(p *avc.MemoryManagementOperation) *AVCMemoryManagementOperation {
	if p == nil {
		return nil
	}
	r := &AVCMemoryManagementOperation{}
	r.Operation = mapVUIUint(p.Operation)
	r.DifferenceOfPicNumsMinus1 = mapVUIUint(p.DifferenceOfPicNumsMinus1)
	r.LongTermPicNum = mapVUIUint(p.LongTermPicNum)
	r.LongTermFrameIdx = mapVUIUint(p.LongTermFrameIdx)
	r.MaxLongTermFrameIdxPlus1 = mapVUIUint(p.MaxLongTermFrameIdxPlus1)
	return r
}

type AVCSliceHeaderDetails struct {
	ReferenceCountsKnown          bool                           `json:"referenceCountsKnown"`
	RBSP                          AVCRBSPViewDetail              `json:"rbsp"`
	DirectSpatialMVPredFlag       *AVCMappedBooleanField         `json:"directSpatialMvPredFlag,omitempty"`
	NumRefIdxActiveOverrideFlag   *AVCMappedBooleanField         `json:"numRefIdxActiveOverrideFlag,omitempty"`
	NumRefIdxL0ActiveMinus1       *AVCMappedUnsignedField        `json:"numRefIdxL0ActiveMinus1,omitempty"`
	NumRefIdxL1ActiveMinus1       *AVCMappedUnsignedField        `json:"numRefIdxL1ActiveMinus1,omitempty"`
	NumRefIdxL0Active             uint32                         `json:"numRefIdxL0Active"`
	NumRefIdxL1Active             uint32                         `json:"numRefIdxL1Active"`
	RefPicListModificationFlagL0  *AVCMappedBooleanField         `json:"refPicListModificationFlagL0,omitempty"`
	RefPicListModificationFlagL1  *AVCMappedBooleanField         `json:"refPicListModificationFlagL1,omitempty"`
	RefPicListModificationsL0     []AVCReferenceListModification `json:"refPicListModificationsL0,omitempty"`
	RefPicListModificationsL1     []AVCReferenceListModification `json:"refPicListModificationsL1,omitempty"`
	LumaLog2WeightDenom           *AVCMappedUnsignedField        `json:"lumaLog2WeightDenom,omitempty"`
	ChromaLog2WeightDenom         *AVCMappedUnsignedField        `json:"chromaLog2WeightDenom,omitempty"`
	PredictionWeightsL0           []AVCPredictionWeight          `json:"predictionWeightsL0,omitempty"`
	PredictionWeightsL1           []AVCPredictionWeight          `json:"predictionWeightsL1,omitempty"`
	NoOutputOfPriorPicsFlag       *AVCMappedBooleanField         `json:"noOutputOfPriorPicsFlag,omitempty"`
	LongTermReferenceFlag         *AVCMappedBooleanField         `json:"longTermReferenceFlag,omitempty"`
	AdaptiveRefPicMarkingModeFlag *AVCMappedBooleanField         `json:"adaptiveRefPicMarkingModeFlag,omitempty"`
	MemoryManagementOperations    []AVCMemoryManagementOperation `json:"memoryManagementOperations,omitempty"`
	CABACInitIDC                  *AVCMappedUnsignedField        `json:"cabacInitIdc,omitempty"`
	SliceQPDelta                  *AVCMappedSignedField          `json:"sliceQpDelta,omitempty"`
	SPForSwitchFlag               *AVCMappedBooleanField         `json:"spForSwitchFlag,omitempty"`
	SliceQSDelta                  *AVCMappedSignedField          `json:"sliceQsDelta,omitempty"`
	DisableDeblockingFilterIDC    *AVCMappedUnsignedField        `json:"disableDeblockingFilterIdc,omitempty"`
	SliceAlphaC0OffsetDiv2        *AVCMappedSignedField          `json:"sliceAlphaC0OffsetDiv2,omitempty"`
	SliceBetaOffsetDiv2           *AVCMappedSignedField          `json:"sliceBetaOffsetDiv2,omitempty"`
	SliceGroupChangeCycle         *AVCMappedUnsignedField        `json:"sliceGroupChangeCycle,omitempty"`
	RBSPBitOffset                 uint32                         `json:"rbspBitOffset"`
	SyntaxBits                    uint32                         `json:"syntaxBits"`
	HeaderBits                    uint32                         `json:"headerBits"`
	BytesRead                     uint32                         `json:"bytesRead"`
	Complete                      bool                           `json:"complete"`
}

func mapSliceHeaderDetails(p *avc.SliceHeaderDetails) *AVCSliceHeaderDetails {
	if p == nil {
		return nil
	}
	r := &AVCSliceHeaderDetails{RBSP: mapAVCRBSP(p.RBSP), ReferenceCountsKnown: p.ReferenceCountsKnown}
	r.DirectSpatialMVPredFlag = mapVUIBool(p.DirectSpatialMVPredFlag)
	r.NumRefIdxActiveOverrideFlag = mapVUIBool(p.NumRefIdxActiveOverrideFlag)
	r.NumRefIdxL0ActiveMinus1 = mapVUIUint(p.NumRefIdxL0ActiveMinus1)
	r.NumRefIdxL1ActiveMinus1 = mapVUIUint(p.NumRefIdxL1ActiveMinus1)
	r.NumRefIdxL0Active = p.NumRefIdxL0Active
	r.NumRefIdxL1Active = p.NumRefIdxL1Active
	r.RefPicListModificationFlagL0 = mapVUIBool(p.RefPicListModificationFlagL0)
	r.RefPicListModificationFlagL1 = mapVUIBool(p.RefPicListModificationFlagL1)
	for _, v := range p.RefPicListModificationsL0 {
		r.RefPicListModificationsL0 = append(r.RefPicListModificationsL0, *mapReferenceListModification(&v))
	}
	for _, v := range p.RefPicListModificationsL1 {
		r.RefPicListModificationsL1 = append(r.RefPicListModificationsL1, *mapReferenceListModification(&v))
	}
	r.LumaLog2WeightDenom = mapVUIUint(p.LumaLog2WeightDenom)
	r.ChromaLog2WeightDenom = mapVUIUint(p.ChromaLog2WeightDenom)
	for _, v := range p.PredictionWeightsL0 {
		r.PredictionWeightsL0 = append(r.PredictionWeightsL0, *mapPredictionWeight(&v))
	}
	for _, v := range p.PredictionWeightsL1 {
		r.PredictionWeightsL1 = append(r.PredictionWeightsL1, *mapPredictionWeight(&v))
	}
	r.NoOutputOfPriorPicsFlag = mapVUIBool(p.NoOutputOfPriorPicsFlag)
	r.LongTermReferenceFlag = mapVUIBool(p.LongTermReferenceFlag)
	r.AdaptiveRefPicMarkingModeFlag = mapVUIBool(p.AdaptiveRefPicMarkingModeFlag)
	for _, v := range p.MemoryManagementOperations {
		r.MemoryManagementOperations = append(r.MemoryManagementOperations, *mapMemoryManagementOperation(&v))
	}
	r.CABACInitIDC = mapVUIUint(p.CABACInitIDC)
	r.SliceQPDelta = mapPrefixSigned(p.SliceQPDelta)
	r.SPForSwitchFlag = mapVUIBool(p.SPForSwitchFlag)
	r.SliceQSDelta = mapPrefixSigned(p.SliceQSDelta)
	r.DisableDeblockingFilterIDC = mapVUIUint(p.DisableDeblockingFilterIDC)
	r.SliceAlphaC0OffsetDiv2 = mapPrefixSigned(p.SliceAlphaC0OffsetDiv2)
	r.SliceBetaOffsetDiv2 = mapPrefixSigned(p.SliceBetaOffsetDiv2)
	r.SliceGroupChangeCycle = mapVUIUint(p.SliceGroupChangeCycle)
	r.RBSPBitOffset = p.RBSPBitOffset
	r.SyntaxBits = p.SyntaxBits
	r.HeaderBits = p.HeaderBits
	r.BytesRead = p.BytesRead
	r.Complete = p.Complete
	return r
}

var ErrDetailV4Unsupported = errors.New("report: detail schema version 4 supports only AVC")

func NewTagDetailV4(size uint64, tag flv.TagRecord, parsed flv.ParsedTagDetail, parameters avccontext.ParameterResolution) (ParsedTagDetailDocument, error) {
	switch p := parsed.(type) {
	case flv.AVCVideo:
		return NewAVCTagDetailV4(size, tag, p, parameters), nil
	case *flv.AVCVideo:
		if p != nil {
			return NewAVCTagDetailV4(size, tag, *p, parameters), nil
		}
	}
	return nil, fmt.Errorf("%w: %T", ErrDetailV4Unsupported, parsed)
}
func NewAVCTagDetailV4(size uint64, tag flv.TagRecord, p flv.AVCVideo, parameters avccontext.ParameterResolution) AVCTagDetailDocument {
	d := NewAVCTagDetailV3(size, tag, p, parameters)
	d.SchemaVersion = 4
	attach := func(mapped []AVCNALUnitDetail, sets []avc.ParameterSet) {
		for i, set := range sets {
			if set.PictureParameterSet != nil && set.PictureParameterSet.Deep != nil {
				mapped[i].PictureParameterSet.Full = mapPPSFullDetails(set.PictureParameterSet.Deep.Full)
			}
		}
	}
	if c := d.Detail.AVCDecoderConfigurationRecord; c != nil {
		attach(c.SequenceParameterSets, p.Config.SequenceParameterSets)
		attach(c.PictureParameterSets, p.Config.PictureParameterSets)
		if c.HighProfileExtension != nil {
			attach(c.HighProfileExtension.SequenceParameterSetExts, p.Config.SequenceParameterSetExts)
		}
	}
	if seq := d.Detail.NALUnitSequence; seq != nil {
		for i, unit := range p.NALUnits.NALUnits {
			if unit.PictureParameterSet != nil && unit.PictureParameterSet.Deep != nil {
				seq.NALUnits[i].PictureParameterSet.Full = mapPPSFullDetails(unit.PictureParameterSet.Deep.Full)
			}
			if unit.SliceHeader != nil {
				seq.NALUnits[i].SliceHeaderFull = mapSliceHeaderDetails(unit.SliceHeader.Full)
			}
		}
	}
	return d
}
