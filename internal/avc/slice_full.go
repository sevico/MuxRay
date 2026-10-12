package avc

import "math/bits"

func parseSliceRemainder(p *rbspParser, s *SliceHeader, h NALHeader, context *ParameterSetContext) {
	f := s.Full
	pps := context.pps[s.PictureParameterSetID.Value]
	d := pps.Deep
	sps := context.sps[pps.SequenceParameterSetID]
	if d.Full == nil || !d.Full.Complete {
		p.addAt(s.PictureParameterSetID.Span, Diagnostic{Code: CodeSliceParameterSetMissing, Severity: SeverityError, Message: "complete slice header needs a fully inspected PPS"})
		p.fatal = true
		return
	}
	kind := s.SliceType.Value % 5
	isB, isI, isSP, isSI := kind == 1, kind == 2, kind == 3, kind == 4
	f.ReferenceCountsKnown = isI || isSI
	if isB {
		f.DirectSpatialMVPredFlag = p.vuiFlag()
	}
	if !isI && !isSI {
		f.NumRefIdxL0Active = d.NumRefIdxL0DefaultActiveMinus1.Value + 1
		if isB {
			f.NumRefIdxL1Active = d.NumRefIdxL1DefaultActiveMinus1.Value + 1
		}
		f.NumRefIdxActiveOverrideFlag = p.vuiFlag()
		if vuiTrue(f.NumRefIdxActiveOverrideFlag) {
			maximum := uint32(15)
			if vuiTrue(s.FieldPicFlag) {
				maximum = 31
			}
			f.NumRefIdxL0ActiveMinus1 = p.fullUE(maximum, CodeSliceValueInvalid, "num_ref_idx_l0_active_minus1")
			if f.NumRefIdxL0ActiveMinus1 != nil && !p.fatal {
				f.NumRefIdxL0Active = f.NumRefIdxL0ActiveMinus1.Value + 1
			}
			if isB {
				f.NumRefIdxL1ActiveMinus1 = p.fullUE(maximum, CodeSliceValueInvalid, "num_ref_idx_l1_active_minus1")
				if f.NumRefIdxL1ActiveMinus1 != nil && !p.fatal {
					f.NumRefIdxL1Active = f.NumRefIdxL1ActiveMinus1.Value + 1
				}
			}
		}
		maximum := uint32(16)
		if vuiTrue(s.FieldPicFlag) {
			maximum = 32
		}
		if !p.fatal && (f.NumRefIdxL0Active > maximum || f.NumRefIdxL1Active > maximum) {
			p.semanticFatal(s.PictureParameterSetID.Span, CodeSliceValueInvalid, "PPS default active references exceed picture/field bound", uint64(maximum), uint64(max(f.NumRefIdxL0Active, f.NumRefIdxL1Active)))
		}
		if p.fatal {
			return
		}
		f.ReferenceCountsKnown = true
		f.RefPicListModificationFlagL0 = p.vuiFlag()
		if vuiTrue(f.RefPicListModificationFlagL0) {
			f.RefPicListModificationsL0 = parseReferenceList(p, s, sps)
		}
		if isB {
			f.RefPicListModificationFlagL1 = p.vuiFlag()
			if vuiTrue(f.RefPicListModificationFlagL1) {
				f.RefPicListModificationsL1 = parseReferenceList(p, s, sps)
			}
		}
	}
	if (vuiTrue(d.WeightedPredFlag) && (kind == 0 || isSP)) || (d.WeightedBipredIDC.Value == 1 && isB) {
		f.LumaLog2WeightDenom = p.fullUE(7, CodeSliceValueInvalid, "luma_log2_weight_denom")
		chroma := !sps.SeparateColourPlaneFlag && sps.ChromaFormatIDC != 0
		if chroma {
			f.ChromaLog2WeightDenom = p.fullUE(7, CodeSliceValueInvalid, "chroma_log2_weight_denom")
		}
		f.PredictionWeightsL0 = parsePredictionWeights(p, f.NumRefIdxL0Active, chroma)
		if isB {
			f.PredictionWeightsL1 = parsePredictionWeights(p, f.NumRefIdxL1Active, chroma)
		}
	}
	if h.NALRefIDC != 0 {
		if h.NALUnitType == NALUnitTypeIDRSlice {
			f.NoOutputOfPriorPicsFlag = p.vuiFlag()
			f.LongTermReferenceFlag = p.vuiFlag()
		} else {
			f.AdaptiveRefPicMarkingModeFlag = p.vuiFlag()
			if vuiTrue(f.AdaptiveRefPicMarkingModeFlag) {
				f.MemoryManagementOperations = parseMemoryManagement(p)
			}
		}
	}
	if pps.EntropyCodingModeFlag && !isI && !isSI {
		f.CABACInitIDC = p.fullUE(2, CodeSliceValueInvalid, "cabac_init_idc")
	}
	f.SliceQPDelta = p.fullSE(-51-6*int64(sps.BitDepthLumaMinus8), 51+6*int64(sps.BitDepthLumaMinus8), CodeSliceValueInvalid, "slice_qp_delta")
	if f.SliceQPDelta != nil {
		qp := 26 + d.PicInitQPMinus26.Value + f.SliceQPDelta.Value
		if qp < -6*int64(sps.BitDepthLumaMinus8) || qp > 51 {
			p.addAt(f.SliceQPDelta.Span, Diagnostic{Code: CodeSliceValueInvalid, Severity: SeverityError, Message: "derived SliceQPY is outside SPS bit-depth range"})
		}
	}
	if isSP || isSI {
		if isSP {
			f.SPForSwitchFlag = p.vuiFlag()
		}
		f.SliceQSDelta = p.fullSE(-51, 51, CodeSliceValueInvalid, "slice_qs_delta")
		if f.SliceQSDelta != nil {
			qs := 26 + d.PicInitQSMinus26.Value + f.SliceQSDelta.Value
			if qs < 0 || qs > 51 {
				p.addAt(f.SliceQSDelta.Span, Diagnostic{Code: CodeSliceValueInvalid, Severity: SeverityError, Message: "derived SliceQSY is outside 0..51"})
			}
		}
	}
	if vuiTrue(d.DeblockingFilterControlPresentFlag) {
		f.DisableDeblockingFilterIDC = p.fullUE(2, CodeSliceValueInvalid, "disable_deblocking_filter_idc")
		if f.DisableDeblockingFilterIDC != nil && f.DisableDeblockingFilterIDC.Value != 1 {
			f.SliceAlphaC0OffsetDiv2 = p.fullSE(-6, 6, CodeSliceValueInvalid, "slice_alpha_c0_offset_div2")
			f.SliceBetaOffsetDiv2 = p.fullSE(-6, 6, CodeSliceValueInvalid, "slice_beta_offset_div2")
		}
	}
	if d.NumSliceGroupsMinus1.Value > 0 && d.Full.SliceGroupMapType != nil && d.Full.SliceGroupMapType.Value >= 3 && d.Full.SliceGroupMapType.Value <= 5 {
		size := (uint64(sps.PicWidthInMbsMinus1) + 1) * (uint64(sps.PicHeightInMapUnitsMinus1) + 1)
		rate := uint64(d.Full.SliceGroupChangeRateMinus1.Value) + 1
		maximum := (size + rate - 1) / rate
		width := bits.Len64(maximum)
		if width > 32 {
			p.semanticFatal(s.PictureParameterSetID.Span, CodeHeaderSyntaxBudgetReached, "slice group cycle exceeds 32-bit field budget", 32, uint64(width))
			return
		}
		f.SliceGroupChangeCycle = p.vuiBits(uint8(width))
		if f.SliceGroupChangeCycle != nil && uint64(f.SliceGroupChangeCycle.Value) > maximum {
			p.semanticFatal(f.SliceGroupChangeCycle.Span, CodeSliceValueInvalid, "slice_group_change_cycle exceeds picture bound", maximum, uint64(f.SliceGroupChangeCycle.Value))
		}
	}
}

func parseReferenceList(p *rbspParser, s *SliceHeader, sps *SequenceParameterSet) []ReferenceListModification {
	var out []ReferenceListModification
	for i := 0; i < MaxHeaderOperations && !p.fatal; i++ {
		r := ReferenceListModification{ModificationOfPicNumsIDC: p.fullUE(3, CodeSliceValueInvalid, "modification_of_pic_nums_idc")}
		if r.ModificationOfPicNumsIDC == nil {
			return out
		}
		if p.fatal {
			return append(out, r)
		}
		switch r.ModificationOfPicNumsIDC.Value {
		case 0, 1:
			maximum := uint32(1) << (sps.Log2MaxFrameNumMinus4 + 4)
			if vuiTrue(s.FieldPicFlag) {
				maximum *= 2
			}
			r.AbsDiffPicNumMinus1 = p.fullUE(maximum-1, CodeSliceValueInvalid, "abs_diff_pic_num_minus1")
		case 2:
			r.LongTermPicNum = p.vuiUE()
		}
		out = append(out, r)
		if r.ModificationOfPicNumsIDC.Value == 3 {
			return out
		}
	}
	if !p.fatal {
		p.fail(CodeHeaderSyntaxBudgetReached, "reference-list modification operation limit reached", MaxHeaderOperations, MaxHeaderOperations)
	}
	return out
}
func parsePredictionWeights(p *rbspParser, count uint32, chroma bool) []PredictionWeight {
	var out []PredictionWeight
	for i := uint32(0); i < count && !p.fatal; i++ {
		r := PredictionWeight{LumaWeightFlag: p.vuiFlag()}
		if vuiTrue(r.LumaWeightFlag) {
			r.LumaWeight = p.fullSE(-128, 127, CodeSliceValueInvalid, "luma_weight")
			r.LumaOffset = p.fullSE(-128, 127, CodeSliceValueInvalid, "luma_offset")
		}
		if chroma {
			r.ChromaWeightFlag = p.vuiFlag()
			if vuiTrue(r.ChromaWeightFlag) {
				for j := 0; j < 2 && !p.fatal; j++ {
					r.ChromaWeight = append(r.ChromaWeight, p.fullSE(-128, 127, CodeSliceValueInvalid, "chroma_weight"))
					r.ChromaOffset = append(r.ChromaOffset, p.fullSE(-128, 127, CodeSliceValueInvalid, "chroma_offset"))
				}
			}
		}
		out = append(out, r)
	}
	return out
}
func parseMemoryManagement(p *rbspParser) []MemoryManagementOperation {
	var out []MemoryManagementOperation
	for i := 0; i < MaxHeaderOperations && !p.fatal; i++ {
		r := MemoryManagementOperation{Operation: p.fullUE(6, CodeSliceValueInvalid, "memory_management_control_operation")}
		if r.Operation == nil {
			return out
		}
		if p.fatal {
			return append(out, r)
		}
		op := r.Operation.Value
		if op == 1 || op == 3 {
			r.DifferenceOfPicNumsMinus1 = p.vuiUE()
		}
		if op == 2 {
			r.LongTermPicNum = p.vuiUE()
		}
		if op == 3 || op == 6 {
			r.LongTermFrameIdx = p.vuiUE()
		}
		if op == 4 {
			r.MaxLongTermFrameIdxPlus1 = p.vuiUE()
		}
		out = append(out, r)
		if op == 0 {
			return out
		}
	}
	if !p.fatal {
		p.fail(CodeHeaderSyntaxBudgetReached, "memory-management operation limit reached", MaxHeaderOperations, MaxHeaderOperations)
	}
	return out
}
