package detailview

import (
	"fmt"
	"strconv"

	"github.com/sevico/muxray/internal/avc"
)

func (b *builder) avcPPSFull(parent string, p *avc.PPSFullDetails) {
	id := b.add(parent, "PPS extended syntax", "RBSP syntax", "Slice-group syntax, optional scaling matrices and trailing bits; no picture reconstruction", nil)
	b.add(id, "ppsFullComplete", "derived boolean", strconv.FormatBool(p.Complete), nil)
	b.add(id, "ppsFullSyntaxBits", "derived count", decimal(uint64(p.SyntaxBits)), nil)
	b.add(id, "ppsExtensionPresent", "derived boolean", strconv.FormatBool(p.ExtensionPresent), nil)
	b.prefixUint(id, "sliceGroupMapType", p.SliceGroupMapType)
	b.prefixBool(id, "sliceGroupChangeDirectionFlag", p.SliceGroupChangeDirectionFlag)
	b.prefixUint(id, "sliceGroupChangeRateMinus1", p.SliceGroupChangeRateMinus1)
	b.prefixUint(id, "picSizeInMapUnitsMinus1", p.PicSizeInMapUnitsMinus1)
	b.prefixBool(id, "transform8x8ModeFlag", p.Transform8x8ModeFlag)
	b.prefixBool(id, "picScalingMatrixPresentFlag", p.PicScalingMatrixPresentFlag)
	b.prefixSigned(id, "secondChromaQpIndexOffset", p.SecondChromaQPIndexOffset)
	if p.RBSPTrailingBits != nil {
		b.add(id, "ppsTrailingBits", "RBSP trailing bits", "validated", mappedPrefixSpan(*p.RBSPTrailingBits))
	}
	for i, field := range p.RunLengthMinus1 {
		b.prefixUint(id, fmt.Sprintf("runLengthMinus1[%d]", i), field)
	}
	for i, rectangle := range p.Rectangles {
		b.prefixUint(id, fmt.Sprintf("topLeft[%d]", i), rectangle.TopLeft)
		b.prefixUint(id, fmt.Sprintf("bottomRight[%d]", i), rectangle.BottomRight)
	}
	for i, field := range p.SliceGroupID {
		b.prefixUint(id, fmt.Sprintf("sliceGroupId[%d]", i), field)
	}
	for _, list := range p.ScalingLists {
		group := b.add(id, fmt.Sprintf("Scaling list[%d]", list.Index), "RBSP syntax", "Values in syntax scan order; absent fallback matrices are not inferred", nil)
		b.prefixBool(group, "scalingListPresentFlag", list.PresentFlag)
		b.add(group, "useDefaultScalingMatrix", "derived boolean", strconv.FormatBool(list.UseDefault), nil)
		for i, delta := range list.DeltaScales {
			b.prefixSigned(group, fmt.Sprintf("deltaScale[%d]", i), delta)
		}
	}
}

func (b *builder) avcSliceFull(parent string, index uint32, p *avc.SliceHeaderDetails) {
	id := b.add(parent, fmt.Sprintf("Slice full header NAL[%d]", index), "RBSP syntax", "Ordinary slice_header() only; references and compressed picture data are not decoded", nil)
	b.add(id, "headerComplete", "derived boolean", strconv.FormatBool(p.Complete), nil)
	b.add(id, "headerBytesRead", "derived count", decimal(uint64(p.BytesRead)), nil)
	b.add(id, "headerBits", "derived count", decimal(uint64(p.HeaderBits)), nil)
	b.add(id, "remainingHeaderStartBit", "derived count", decimal(uint64(p.RBSPBitOffset)), nil)
	b.add(id, "remainingHeaderSyntaxBits", "derived count", decimal(uint64(p.SyntaxBits)), nil)
	b.prefixBool(id, "directSpatialMvPredFlag", p.DirectSpatialMVPredFlag)
	b.prefixBool(id, "numRefIdxActiveOverrideFlag", p.NumRefIdxActiveOverrideFlag)
	b.prefixUint(id, "numRefIdxL0ActiveMinus1", p.NumRefIdxL0ActiveMinus1)
	b.prefixUint(id, "numRefIdxL1ActiveMinus1", p.NumRefIdxL1ActiveMinus1)
	b.add(id, "referenceCountsKnown", "derived boolean", strconv.FormatBool(p.ReferenceCountsKnown), nil)
	if p.ReferenceCountsKnown {
		b.add(id, "effectiveNumRefIdxL0Active", "derived count", decimal(uint64(p.NumRefIdxL0Active)), nil)
		b.add(id, "effectiveNumRefIdxL1Active", "derived count", decimal(uint64(p.NumRefIdxL1Active)), nil)
	}
	b.prefixBool(id, "refPicListModificationFlagL0", p.RefPicListModificationFlagL0)
	b.prefixBool(id, "refPicListModificationFlagL1", p.RefPicListModificationFlagL1)
	b.avcReferenceList(id, "L0", p.RefPicListModificationsL0)
	b.avcReferenceList(id, "L1", p.RefPicListModificationsL1)
	b.prefixUint(id, "lumaLog2WeightDenom", p.LumaLog2WeightDenom)
	b.prefixUint(id, "chromaLog2WeightDenom", p.ChromaLog2WeightDenom)
	b.avcWeights(id, "L0", p.PredictionWeightsL0)
	b.avcWeights(id, "L1", p.PredictionWeightsL1)
	b.prefixBool(id, "noOutputOfPriorPicsFlag", p.NoOutputOfPriorPicsFlag)
	b.prefixBool(id, "longTermReferenceFlag", p.LongTermReferenceFlag)
	b.prefixBool(id, "adaptiveRefPicMarkingModeFlag", p.AdaptiveRefPicMarkingModeFlag)
	for i, operation := range p.MemoryManagementOperations {
		group := b.add(id, fmt.Sprintf("Reference marking[%d]", i), "RBSP syntax", "Memory-management declaration; DPB state is not reconstructed", nil)
		b.prefixUint(group, "memoryManagementControlOperation", operation.Operation)
		b.prefixUint(group, "differenceOfPicNumsMinus1", operation.DifferenceOfPicNumsMinus1)
		b.prefixUint(group, "longTermPicNum", operation.LongTermPicNum)
		b.prefixUint(group, "longTermFrameIdx", operation.LongTermFrameIdx)
		b.prefixUint(group, "maxLongTermFrameIdxPlus1", operation.MaxLongTermFrameIdxPlus1)
	}
	b.prefixUint(id, "cabacInitIdc", p.CABACInitIDC)
	b.prefixSigned(id, "sliceQpDelta", p.SliceQPDelta)
	b.prefixBool(id, "spForSwitchFlag", p.SPForSwitchFlag)
	b.prefixSigned(id, "sliceQsDelta", p.SliceQSDelta)
	b.prefixUint(id, "disableDeblockingFilterIdc", p.DisableDeblockingFilterIDC)
	b.prefixSigned(id, "sliceAlphaC0OffsetDiv2", p.SliceAlphaC0OffsetDiv2)
	b.prefixSigned(id, "sliceBetaOffsetDiv2", p.SliceBetaOffsetDiv2)
	b.prefixUint(id, "sliceGroupChangeCycle", p.SliceGroupChangeCycle)
}

func (b *builder) avcReferenceList(parent, name string, values []avc.ReferenceListModification) {
	for i, value := range values {
		id := b.add(parent, fmt.Sprintf("Reference list %s[%d]", name, i), "RBSP syntax", "Reference-list modification declaration", nil)
		b.prefixUint(id, "modificationOfPicNumsIdc", value.ModificationOfPicNumsIDC)
		b.prefixUint(id, "absDiffPicNumMinus1", value.AbsDiffPicNumMinus1)
		b.prefixUint(id, "longTermPicNum", value.LongTermPicNum)
	}
}

func (b *builder) avcWeights(parent, name string, values []avc.PredictionWeight) {
	for i, value := range values {
		id := b.add(parent, fmt.Sprintf("Prediction weight %s[%d]", name, i), "RBSP syntax", "Encoded prediction weights; omitted defaults are not encoded source fields", nil)
		b.prefixBool(id, "lumaWeightFlag", value.LumaWeightFlag)
		b.prefixSigned(id, "lumaWeight", value.LumaWeight)
		b.prefixSigned(id, "lumaOffset", value.LumaOffset)
		b.prefixBool(id, "chromaWeightFlag", value.ChromaWeightFlag)
		for component, field := range value.ChromaWeight {
			b.prefixSigned(id, fmt.Sprintf("chromaWeight[%d]", component), field)
		}
		for component, field := range value.ChromaOffset {
			b.prefixSigned(id, fmt.Sprintf("chromaOffset[%d]", component), field)
		}
	}
}
