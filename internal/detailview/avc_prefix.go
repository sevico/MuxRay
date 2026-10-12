package detailview

import (
	"fmt"
	"strconv"

	"github.com/sevico/muxray/internal/avc"
	"github.com/sevico/muxray/internal/avccontext"
)

func (b *builder) avcParameterContext(parent string, p avccontext.ParameterResolution) {
	id := b.add(parent, "Parameter context", "bounded replay", p.State, nil)
	b.add(id, "contextCanContinue", "derived boolean", strconv.FormatBool(p.CanResume), nil)
	for _, f := range []struct {
		name  string
		value uint64
	}{
		{"startTagId", p.StartTagID}, {"nextTagId", p.NextTagID}, {"selectedTagId", p.SelectedTagID},
		{"contextSegmentStartTagId", p.ResumeStartTagID}, {"checkpointCount", uint64(p.CheckpointCount)},
		{"replayedTags", p.ScannedTags}, {"replayedVideoTags", p.VideoTags},
		{"replayedConfigurations", p.Configurations}, {"replayedNALPackets", p.NALPackets},
		{"contextBytesRead", p.BytesRead}, {"contextBarriers", p.BarrierCount},
		{"availableSPS", uint64(p.SPSCount)}, {"availablePPS", uint64(p.PPSCount)},
	} {
		b.add(id, f.name, "derived count", decimal(f.value), nil)
	}
	if p.State == avccontext.StateBudgetExhausted {
		b.detail.Truncated = true
	}
}

func (b *builder) avcPPSPrefix(parent string, p *avc.PPSDetails) {
	scope := "Through redundant_pic_cnt_present_flag; extensions and trailing bits are not validated"
	if p.Full != nil {
		scope = "Fields through redundant_pic_cnt_present_flag; see PPS extended syntax for remaining coverage"
	}
	id := b.add(parent, "PPS prefix", "RBSP syntax", scope, nil)
	b.add(id, "prefixComplete", "derived boolean", strconv.FormatBool(p.PrefixComplete), nil)
	b.prefixUint(id, "numSliceGroupsMinus1", p.NumSliceGroupsMinus1)
	b.prefixUint(id, "numRefIdxL0DefaultActiveMinus1", p.NumRefIdxL0DefaultActiveMinus1)
	b.prefixUint(id, "numRefIdxL1DefaultActiveMinus1", p.NumRefIdxL1DefaultActiveMinus1)
	b.prefixBool(id, "weightedPredFlag", p.WeightedPredFlag)
	b.prefixUint(id, "weightedBipredIdc", p.WeightedBipredIDC)
	b.prefixSigned(id, "picInitQpMinus26", p.PicInitQPMinus26)
	b.prefixSigned(id, "picInitQsMinus26", p.PicInitQSMinus26)
	b.prefixSigned(id, "chromaQpIndexOffset", p.ChromaQPIndexOffset)
	b.prefixBool(id, "deblockingFilterControlPresentFlag", p.DeblockingFilterControlPresentFlag)
	b.prefixBool(id, "constrainedIntraPredFlag", p.ConstrainedIntraPredFlag)
	b.prefixBool(id, "redundantPicCntPresentFlag", p.RedundantPicCntPresentFlag)
	b.add(id, "remainingRbspBits", "derived count", decimal(uint64(p.RemainingRBSPBits)), nil)
}

func (b *builder) avcSlicePrefix(parent string, index uint32, p *avc.SliceHeader) {
	scope := "Through redundant_pic_cnt only; remaining header and picture data are not decoded"
	if p.Full != nil {
		scope = "Fields through redundant_pic_cnt; see full header for remaining coverage"
	}
	id := b.add(parent, fmt.Sprintf("Slice prefix NAL[%d]", index), "RBSP syntax", scope, nil)
	b.add(id, "prefixComplete", "derived boolean", strconv.FormatBool(p.PrefixComplete), nil)
	b.prefixUint(id, "firstMbInSlice", p.FirstMBInSlice)
	b.prefixUint(id, "sliceType", p.SliceType)
	if p.SliceTypeName != "" {
		b.add(id, "sliceTypeName", "derived enum", p.SliceTypeName, nil)
	}
	b.prefixUint(id, "pictureParameterSetId", p.PictureParameterSetID)
	b.prefixUint(id, "colourPlaneId", p.ColourPlaneID)
	b.prefixUint(id, "frameNum", p.FrameNum)
	b.prefixBool(id, "fieldPicFlag", p.FieldPicFlag)
	b.prefixBool(id, "bottomFieldFlag", p.BottomFieldFlag)
	b.prefixUint(id, "idrPicId", p.IDRPicID)
	b.prefixUint(id, "picOrderCntLsb", p.PicOrderCntLSB)
	b.prefixSigned(id, "deltaPicOrderCntBottom", p.DeltaPicOrderCntBottom)
	b.prefixSigned(id, "deltaPicOrderCnt0", p.DeltaPicOrderCnt0)
	b.prefixSigned(id, "deltaPicOrderCnt1", p.DeltaPicOrderCnt1)
	b.prefixUint(id, "redundantPicCnt", p.RedundantPicCnt)
	if p.ReferencedSPSID != nil {
		b.add(id, "referencedSpsId", "derived reference", decimal(uint64(*p.ReferencedSPSID)), nil)
	}
	if p.ReferencedSPSSpan != nil {
		b.add(id, "referencedSPS", "source NAL", "Parameter source; may precede the selected Tag", span(*p.ReferencedSPSSpan))
	}
	if p.ReferencedPPSSpan != nil {
		b.add(id, "referencedPPS", "source NAL", "Parameter source; may precede the selected Tag", span(*p.ReferencedPPSSpan))
	}
	b.add(id, "prefixSyntaxBits", "derived count", decimal(uint64(p.SyntaxBits)), nil)
	b.add(id, "prefixBytesRead", "derived count", decimal(uint64(p.BytesRead)), nil)
}

func mappedPrefixSpan(s avc.MappedSpan) *Span {
	if len(s.SourceSpans) == 1 && s.SourceSpans[0].BitLength == s.BitLength {
		return span(s.SourceSpans[0])
	}
	return nil
}
func (b *builder) prefixUint(parent, name string, p *avc.VUIUnsignedField) {
	if p != nil {
		b.add(parent, name, "RBSP unsigned", decimal(uint64(p.Value)), mappedPrefixSpan(p.Span))
	}
}
func (b *builder) prefixSigned(parent, name string, p *avc.SignedField) {
	if p != nil {
		b.add(parent, name, "RBSP signed", strconv.FormatInt(p.Value, 10), mappedPrefixSpan(p.Span))
	}
}
func (b *builder) prefixBool(parent, name string, p *avc.VUIBooleanField) {
	if p != nil {
		b.add(parent, name, "RBSP boolean", strconv.FormatBool(p.Value), mappedPrefixSpan(p.Span))
	}
}
