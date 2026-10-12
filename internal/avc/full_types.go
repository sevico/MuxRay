package avc

// Full-header limits are independent of the legacy prefix budgets. They bound
// retained syntax, not supported picture size or compressed slice bodies.
const (
	MaxFullSliceHeaderBytes       uint32 = 4096
	MaxFullSliceHeaderTotalBytes  uint32 = 256 << 10
	MaxFMOMapUnits                uint32 = 4096
	MaxHeaderOperations                  = 64
	CodeHeaderSyntaxBudgetReached        = "AVC_HEADER_SYNTAX_BUDGET_REACHED"
	CodePPSParameterSetMissing           = "AVC_PPS_PARAMETER_SET_MISSING"
	CodeSliceHeaderUnsupported           = "AVC_SLICE_HEADER_UNSUPPORTED"
)

type SliceGroupRectangle struct{ TopLeft, BottomRight *VUIUnsignedField }
type ScalingList struct {
	Index       uint32
	PresentFlag *VUIBooleanField
	DeltaScales []*SignedField
	// Values are derived in syntax scan order, not raster order. Absent lists
	// have no inferred fallback matrix and UseDefault reflects delta_scale=-8.
	Values     []uint32
	UseDefault bool
}

type PPSFullDetails struct {
	SliceGroupMapType                                   *VUIUnsignedField
	RunLengthMinus1                                     []*VUIUnsignedField
	Rectangles                                          []SliceGroupRectangle
	SliceGroupChangeDirectionFlag                       *VUIBooleanField
	SliceGroupChangeRateMinus1, PicSizeInMapUnitsMinus1 *VUIUnsignedField
	SliceGroupID                                        []*VUIUnsignedField
	ExtensionPresent                                    bool
	Transform8x8ModeFlag, PicScalingMatrixPresentFlag   *VUIBooleanField
	ScalingLists                                        []ScalingList
	SecondChromaQPIndexOffset                           *SignedField
	RBSPTrailingBits                                    *MappedSpan
	SyntaxBits                                          uint32
	Complete                                            bool
}

type ReferenceListModification struct {
	ModificationOfPicNumsIDC, AbsDiffPicNumMinus1, LongTermPicNum *VUIUnsignedField
}
type PredictionWeight struct {
	LumaWeightFlag, ChromaWeightFlag *VUIBooleanField
	LumaWeight, LumaOffset           *SignedField
	ChromaWeight, ChromaOffset       []*SignedField
}
type MemoryManagementOperation struct {
	Operation, DifferenceOfPicNumsMinus1, LongTermPicNum, LongTermFrameIdx, MaxLongTermFrameIdxPlus1 *VUIUnsignedField
}

// SliceHeaderDetails ends exactly at slice_header(), before CABAC alignment
// and slice_data(). Completeness never claims decoded references or pictures.
type SliceHeaderDetails struct {
	RBSP                                                 RBSPView
	DirectSpatialMVPredFlag, NumRefIdxActiveOverrideFlag *VUIBooleanField
	NumRefIdxL0ActiveMinus1, NumRefIdxL1ActiveMinus1     *VUIUnsignedField
	// Effective counts include PPS defaults and therefore have no source spans.
	// They are meaningful only when ReferenceCountsKnown is true.
	ReferenceCountsKnown                                                          bool
	NumRefIdxL0Active, NumRefIdxL1Active                                          uint32
	RefPicListModificationFlagL0, RefPicListModificationFlagL1                    *VUIBooleanField
	RefPicListModificationsL0, RefPicListModificationsL1                          []ReferenceListModification
	LumaLog2WeightDenom, ChromaLog2WeightDenom                                    *VUIUnsignedField
	PredictionWeightsL0, PredictionWeightsL1                                      []PredictionWeight
	NoOutputOfPriorPicsFlag, LongTermReferenceFlag, AdaptiveRefPicMarkingModeFlag *VUIBooleanField
	MemoryManagementOperations                                                    []MemoryManagementOperation
	CABACInitIDC                                                                  *VUIUnsignedField
	SliceQPDelta                                                                  *SignedField
	SPForSwitchFlag                                                               *VUIBooleanField
	SliceQSDelta                                                                  *SignedField
	DisableDeblockingFilterIDC                                                    *VUIUnsignedField
	SliceAlphaC0OffsetDiv2, SliceBetaOffsetDiv2                                   *SignedField
	SliceGroupChangeCycle                                                         *VUIUnsignedField
	RBSPBitOffset, SyntaxBits, HeaderBits, BytesRead                              uint32
	Complete                                                                      bool
}
