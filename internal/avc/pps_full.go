package avc

import (
	"math"
	"math/bits"
)

func parsePPSFull(p *rbspParser, spsID uint32, context *ParameterSetContext) *PPSDetails {
	d := &PPSDetails{RBSPBitOffset: p.bitPosition, Full: &PPSFullDetails{}}
	f := d.Full
	defer func() {
		if d.RedundantPicCntPresentFlag == nil {
			d.SyntaxBits, d.RemainingRBSPBits = p.bitPosition-d.RBSPBitOffset, p.remainingBits()
		}
		f.SyntaxBits = p.bitPosition - d.RBSPBitOffset
		f.Complete = d.PrefixComplete && !p.fatal && !p.invalid && f.RBSPTrailingBits != nil
	}()
	d.NumSliceGroupsMinus1 = p.fullUE(7, CodePPSValueInvalid, "num_slice_groups_minus1")
	if d.NumSliceGroupsMinus1 == nil || p.fatal {
		return d
	}
	var sps *SequenceParameterSet
	if context != nil && spsID < 32 {
		sps = context.sps[spsID]
	}
	if d.NumSliceGroupsMinus1.Value > 0 {
		if sps == nil {
			p.fullMissingSPS(d.NumSliceGroupsMinus1.Span)
			return d
		}
		parseSliceGroups(p, d, sps)
	}
	parsePPSCommon(p, d)
	if sps != nil && d.PicInitQPMinus26 != nil && d.PicInitQPMinus26.Value < -26-6*int64(sps.BitDepthLumaMinus8) {
		p.addAt(d.PicInitQPMinus26.Span, Diagnostic{Code: CodePPSValueInvalid, Severity: SeverityError, Message: "pic_init_qp_minus26 is invalid for the referenced SPS bit depth"})
	}
	d.SyntaxBits, d.RemainingRBSPBits = p.bitPosition-d.RBSPBitOffset, p.remainingBits()
	d.PrefixComplete = !p.fatal && !p.invalid && d.RedundantPicCntPresentFlag != nil
	if !d.PrefixComplete {
		return d
	}
	if sps == nil {
		p.fullMissingSPS(d.NumSliceGroupsMinus1.Span)
		return d
	}
	f.ExtensionPresent = p.moreRBSPData()
	if f.ExtensionPresent {
		f.Transform8x8ModeFlag = p.vuiFlag()
		f.PicScalingMatrixPresentFlag = p.vuiFlag()
		if vuiTrue(f.PicScalingMatrixPresentFlag) {
			if sps == nil {
				p.fullMissingSPS(f.PicScalingMatrixPresentFlag.Span)
				return d
			}
			count := uint32(6)
			if vuiTrue(f.Transform8x8ModeFlag) {
				if sps.ChromaFormatIDC == 3 {
					count += 6
				} else {
					count += 2
				}
			}
			for i := uint32(0); i < count && !p.fatal; i++ {
				f.ScalingLists = append(f.ScalingLists, parseScalingList(p, i))
			}
		}
		f.SecondChromaQPIndexOffset = p.fullSE(-12, 12, CodePPSValueInvalid, "second_chroma_qp_index_offset")
	}
	if p.fatal {
		return d
	}
	start := p.bitPosition
	if p.trailingBitsFor("PPS") {
		m := p.mappedSpan(start, p.bitPosition-start)
		f.RBSPTrailingBits = &m
	}
	return d
}

func (p *rbspParser) fullMissingSPS(span MappedSpan) {
	p.addAt(span, Diagnostic{Code: CodePPSParameterSetMissing, Severity: SeverityError, Message: "PPS syntax needs its preceding SPS"})
	p.fatal = true
}

func parseSliceGroups(p *rbspParser, d *PPSDetails, s *SequenceParameterSet) {
	f := d.Full
	f.SliceGroupMapType = p.fullUE(6, CodePPSValueInvalid, "slice_group_map_type")
	if f.SliceGroupMapType == nil || p.fatal {
		return
	}
	width, height := uint64(s.PicWidthInMbsMinus1)+1, uint64(s.PicHeightInMapUnitsMinus1)+1
	if height > math.MaxUint64/width {
		p.semanticFatal(f.SliceGroupMapType.Span, CodePPSValueInvalid, "picture map size exceeds uint64", math.MaxUint64, 0)
		return
	}
	size := width * height
	maxSize := uint32(min(size-1, uint64(^uint32(0))))
	switch f.SliceGroupMapType.Value {
	case 0:
		for i := uint32(0); i <= d.NumSliceGroupsMinus1.Value && !p.fatal; i++ {
			f.RunLengthMinus1 = append(f.RunLengthMinus1, p.fullUE(maxSize, CodePPSValueInvalid, "run_length_minus1"))
		}
	case 2:
		for i := uint32(0); i < d.NumSliceGroupsMinus1.Value && !p.fatal; i++ {
			r := SliceGroupRectangle{TopLeft: p.fullUE(maxSize, CodePPSValueInvalid, "top_left"), BottomRight: p.fullUE(maxSize, CodePPSValueInvalid, "bottom_right")}
			f.Rectangles = append(f.Rectangles, r)
			if r.TopLeft != nil && r.BottomRight != nil && (r.TopLeft.Value > r.BottomRight.Value || uint64(r.TopLeft.Value)%(uint64(s.PicWidthInMbsMinus1)+1) > uint64(r.BottomRight.Value)%(uint64(s.PicWidthInMbsMinus1)+1)) {
				p.semanticFatal(r.BottomRight.Span, CodePPSValueInvalid, "slice group rectangle has reversed bounds", uint64(r.TopLeft.Value), uint64(r.BottomRight.Value))
			}
		}
	case 3, 4, 5:
		if d.NumSliceGroupsMinus1.Value != 1 {
			p.semanticFatal(d.NumSliceGroupsMinus1.Span, CodePPSValueInvalid, "changing slice groups require exactly two groups", 1, uint64(d.NumSliceGroupsMinus1.Value))
			return
		}
		f.SliceGroupChangeDirectionFlag = p.vuiFlag()
		f.SliceGroupChangeRateMinus1 = p.fullUE(maxSize, CodePPSValueInvalid, "slice_group_change_rate_minus1")
	case 6:
		f.PicSizeInMapUnitsMinus1 = p.fullUE(maxSize, CodePPSValueInvalid, "pic_size_in_map_units_minus1")
		n := f.PicSizeInMapUnitsMinus1
		if n == nil || p.fatal {
			return
		}
		if uint64(n.Value)+1 != size {
			p.semanticFatal(n.Span, CodePPSValueInvalid, "explicit slice group map must match SPS picture size", size-1, uint64(n.Value))
			return
		}
		if n.Value >= MaxFMOMapUnits {
			p.semanticFatal(n.Span, CodeHeaderSyntaxBudgetReached, "explicit slice group map exceeds retained syntax budget", uint64(MaxFMOMapUnits), uint64(n.Value)+1)
			return
		}
		for i := uint32(0); i <= n.Value && !p.fatal; i++ {
			v := p.vuiBits(uint8(bits.Len32(d.NumSliceGroupsMinus1.Value)))
			f.SliceGroupID = append(f.SliceGroupID, v)
			if v != nil && v.Value > d.NumSliceGroupsMinus1.Value {
				p.semanticFatal(v.Span, CodePPSValueInvalid, "slice_group_id exceeds group count", uint64(d.NumSliceGroupsMinus1.Value), uint64(v.Value))
			}
		}
	}
}

func parseScalingList(p *rbspParser, index uint32) ScalingList {
	r := ScalingList{Index: index, PresentFlag: p.vuiFlag()}
	if !vuiTrue(r.PresentFlag) {
		return r
	}
	count := 16
	if index >= 6 {
		count = 64
	}
	last, next := int64(8), int64(8)
	for i := 0; i < count && !p.fatal; i++ {
		if next != 0 {
			v := p.fullSE(-128, 127, CodePPSValueInvalid, "delta_scale")
			if v == nil {
				return r
			}
			r.DeltaScales = append(r.DeltaScales, v)
			next = (last + v.Value + 256) % 256
			if i == 0 && next == 0 {
				r.UseDefault = true
			}
		}
		value := next
		if next == 0 {
			value = last
		}
		r.Values = append(r.Values, uint32(value))
		last = value
	}
	return r
}

func (p *rbspParser) moreRBSPData() bool {
	n := p.remainingBits()
	if n == 0 {
		return false
	}
	for i := uint32(0); i < n; i++ {
		pos := p.bitPosition + i
		v := (p.data.bytes[pos/8] >> (7 - pos%8)) & 1
		if i == 0 {
			if v != 1 {
				return true
			}
		} else if v != 0 {
			return true
		}
	}
	return false
}
func (p *rbspParser) fullUE(maximum uint32, code, name string) *VUIUnsignedField {
	v := p.vuiUE()
	if v != nil && v.Value > maximum {
		p.semanticFatal(v.Span, code, name+" exceeds syntax range", uint64(maximum), uint64(v.Value))
	}
	return v
}
func (p *rbspParser) fullSE(low, high int64, code, name string) *SignedField {
	v := p.signedField()
	if v != nil && (v.Value < low || v.Value > high) {
		p.addAt(v.Span, Diagnostic{Code: code, Severity: SeverityError, Message: name + " is outside syntax range"})
		p.fatal = true
	}
	return v
}
