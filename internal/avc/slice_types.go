package avc

import "github.com/sevico/muxray/internal/cursor"

// SignedField retains a successfully decoded signed Exp-Golomb field.
type SignedField struct {
	Value int64
	Span  MappedSpan
}

// PPSDetails expands only the PPS prefix through
// redundant_pic_cnt_present_flag. PrefixComplete does not validate optional
// transform/scaling extensions or rbsp_trailing_bits. Slice groups are
// explicitly unsupported in this bounded analysis.
type PPSDetails struct {
	NumSliceGroupsMinus1                                                                     *VUIUnsignedField
	NumRefIdxL0DefaultActiveMinus1, NumRefIdxL1DefaultActiveMinus1                           *VUIUnsignedField
	WeightedPredFlag                                                                         *VUIBooleanField
	WeightedBipredIDC                                                                        *VUIUnsignedField
	PicInitQPMinus26, PicInitQSMinus26, ChromaQPIndexOffset                                  *SignedField
	DeblockingFilterControlPresentFlag, ConstrainedIntraPredFlag, RedundantPicCntPresentFlag *VUIBooleanField
	RBSPBitOffset, SyntaxBits, RemainingRBSPBits                                             uint32
	PrefixComplete                                                                           bool
}

const (
	MaxSliceHeaderBytes           uint32 = 256
	MaxSliceHeaderTotalBytes      uint32 = 64 << 10
	CodePPSSliceGroupsUnsupported        = "AVC_PPS_SLICE_GROUPS_UNSUPPORTED"
	CodePPSValueInvalid                  = "AVC_PPS_VALUE_INVALID"
	CodeSliceValueInvalid                = "AVC_SLICE_VALUE_INVALID"
	CodeSliceParameterSetMissing         = "AVC_SLICE_PARAMETER_SET_MISSING"
	CodeSliceHeaderBudgetReached         = "AVC_SLICE_HEADER_BUDGET_REACHED"
)

// SliceHeader is the slice_header() prefix through redundant_pic_cnt only.
// A complete prefix is not a complete slice header or a decoded picture.
// RBSP covers only inspected bytes, never the entire slice body.
type SliceHeader struct {
	RBSP                                                         RBSPView
	FirstMBInSlice, SliceType, PictureParameterSetID             *VUIUnsignedField
	SliceTypeName                                                string
	ColourPlaneID, FrameNum                                      *VUIUnsignedField
	FieldPicFlag, BottomFieldFlag                                *VUIBooleanField
	IDRPicID, PicOrderCntLSB                                     *VUIUnsignedField
	DeltaPicOrderCntBottom, DeltaPicOrderCnt0, DeltaPicOrderCnt1 *SignedField
	RedundantPicCnt                                              *VUIUnsignedField
	// Referenced spans identify the originating parameter-set NALs, which
	// may precede this Tag. They are not spans of fields in this slice.
	ReferencedSPSID                      *uint32
	ReferencedSPSSpan, ReferencedPPSSpan *cursor.Span
	SyntaxBits, BytesRead                uint32
	PrefixComplete                       bool
}
