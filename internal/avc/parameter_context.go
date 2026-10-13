package avc

import "github.com/sevico/muxray/internal/cursor"

// ParameterSetContext holds bounded parameter-set summaries. Its zero value
// is empty. Parsed summaries are immutable; updates replace slots. Callers
// must not mutate a context concurrently with Clone or Add methods.
type ParameterSetContext struct {
	sps            [32]*SequenceParameterSet
	pps            [256]*PictureParameterSet
	timingActive   *SPSTimingParameters
	timingEvidence cursor.Span
}

func NewParameterSetContext() *ParameterSetContext { return &ParameterSetContext{} }

func (c *ParameterSetContext) Clone() *ParameterSetContext {
	if c == nil {
		return NewParameterSetContext()
	}
	clone := *c
	return &clone
}

// CompactClone retains only immutable values needed by slice header
// parsing and optional SEI timing evidence. It owns no RBSP bytes, field
// mappings, full VUI, or source handle. The 32 SPS / 256 PPS slots and
// fixed-size pointees bound checkpoint storage.
// This is a context snapshot, not a substitute for the parsed parameter detail.
func (c *ParameterSetContext) CompactClone() *ParameterSetContext {
	out := NewParameterSetContext()
	if c == nil {
		return out
	}
	out.timingActive, out.timingEvidence = c.timingActive, c.timingEvidence
	for id, s := range c.sps {
		if s == nil {
			continue
		}
		out.sps[id] = &SequenceParameterSet{
			RBSP:                        RBSPView{EBSPSpan: s.RBSP.EBSPSpan},
			Timing:                      s.Timing,
			SequenceParameterSetID:      s.SequenceParameterSetID,
			BitDepthLumaMinus8:          s.BitDepthLumaMinus8,
			ChromaFormatIDC:             s.ChromaFormatIDC,
			MaxNumRefFrames:             s.MaxNumRefFrames,
			SeparateColourPlaneFlag:     s.SeparateColourPlaneFlag,
			Log2MaxFrameNumMinus4:       s.Log2MaxFrameNumMinus4,
			FrameMbsOnlyFlag:            s.FrameMbsOnlyFlag,
			PicOrderCntType:             s.PicOrderCntType,
			Log2MaxPicOrderCntLSBMinus4: s.Log2MaxPicOrderCntLSBMinus4,
			DeltaPicOrderAlwaysZeroFlag: s.DeltaPicOrderAlwaysZeroFlag,
			PicWidthInMbsMinus1:         s.PicWidthInMbsMinus1,
			PicHeightInMapUnitsMinus1:   s.PicHeightInMapUnitsMinus1,
			MBAdaptiveFrameFieldFlag:    s.MBAdaptiveFrameFieldFlag,
			Complete:                    s.Complete,
		}
	}
	for id, p := range c.pps {
		if p == nil {
			continue
		}
		out.pps[id] = &PictureParameterSet{
			RBSP:                                  RBSPView{EBSPSpan: p.RBSP.EBSPSpan},
			PictureParameterSetID:                 p.PictureParameterSetID,
			SequenceParameterSetID:                p.SequenceParameterSetID,
			BottomFieldPicOrderInFramePresentFlag: p.BottomFieldPicOrderInFramePresentFlag,
			Complete:                              p.Complete,
			EntropyCodingModeFlag:                 p.EntropyCodingModeFlag,
			Deep: &PPSDetails{
				PicInitQPMinus26:           &SignedField{Value: p.Deep.PicInitQPMinus26.Value},
				RedundantPicCntPresentFlag: &VUIBooleanField{Value: p.Deep.RedundantPicCntPresentFlag.Value},
				PrefixComplete:             p.Deep.PrefixComplete,
			},
		}

		if p.Deep.Full != nil {
			d := out.pps[id].Deep
			d.NumSliceGroupsMinus1 = compactUnsigned(p.Deep.NumSliceGroupsMinus1)
			d.NumRefIdxL0DefaultActiveMinus1 = compactUnsigned(p.Deep.NumRefIdxL0DefaultActiveMinus1)
			d.NumRefIdxL1DefaultActiveMinus1 = compactUnsigned(p.Deep.NumRefIdxL1DefaultActiveMinus1)
			d.WeightedPredFlag = compactBoolean(p.Deep.WeightedPredFlag)
			d.WeightedBipredIDC = compactUnsigned(p.Deep.WeightedBipredIDC)
			d.PicInitQSMinus26 = compactSigned(p.Deep.PicInitQSMinus26)
			d.DeblockingFilterControlPresentFlag = compactBoolean(p.Deep.DeblockingFilterControlPresentFlag)
			d.Full = &PPSFullDetails{Complete: p.Deep.Full.Complete, SliceGroupMapType: compactUnsigned(p.Deep.Full.SliceGroupMapType), SliceGroupChangeRateMinus1: compactUnsigned(p.Deep.Full.SliceGroupChangeRateMinus1)}
		}
	}
	return out
}

// AddConfiguration starts a fresh generation. Broken framing cannot supply
// a reliable parameter-set generation and leaves the context empty.
func (c *ParameterSetContext) AddConfiguration(record ConfigurationRecord) {
	*c = ParameterSetContext{}
	if !record.FramingComplete {
		return
	}
	for _, set := range record.SequenceParameterSets {
		c.AddNALUnit(NALUnit{Header: set.Header, HeaderPresent: set.HeaderPresent, SequenceParameterSet: set.SequenceParameterSet, PictureParameterSet: set.PictureParameterSet, SEI: set.SEI, SummaryComplete: set.SummaryComplete})
	}
	for _, set := range record.PictureParameterSets {
		c.AddNALUnit(NALUnit{Header: set.Header, HeaderPresent: set.HeaderPresent, SequenceParameterSet: set.SequenceParameterSet, PictureParameterSet: set.PictureParameterSet, SEI: set.SEI, SummaryComplete: set.SummaryComplete})
	}
}

// AddNALUnit replaces the slot even when a known-ID update is invalid. An
// unreadable ID clears that kind of set, so stale values cannot fill gaps.
func (c *ParameterSetContext) AddNALUnit(unit NALUnit) {
	if !unit.HeaderPresent {
		*c = ParameterSetContext{}
		return
	}
	if unit.Header.ForbiddenZeroBit {
		c.timingActive = nil
	}
	switch unit.Header.NALUnitType {
	case NALUnitTypeEndOfSequence, NALUnitTypeEndOfStream:
		*c = ParameterSetContext{}
	case NALUnitTypeSequenceParameterSet:
		s := unit.SequenceParameterSet
		if s == nil || s.SequenceParameterSetIDSpan.BitLength == 0 || s.SequenceParameterSetID > 31 || c.timingActive != nil && c.timingActive.SPSID == s.SequenceParameterSetID {
			c.timingActive = nil
		}
		if s == nil || s.SequenceParameterSetIDSpan.BitLength == 0 || s.SequenceParameterSetID > 31 {
			c.sps = [32]*SequenceParameterSet{}
			c.invalidateFullPPS(nil)
			return
		}
		previous := c.sps[s.SequenceParameterSetID]
		c.sps[s.SequenceParameterSetID] = nil
		if !unit.SummaryComplete || !s.Complete || s.Log2MaxFrameNumMinus4 > 12 || s.PicOrderCntType > 2 || s.Log2MaxPicOrderCntLSBMinus4 > 12 {
			c.invalidateFullPPS(&s.SequenceParameterSetID)
			return
		}
		// Full PPS FMO bounds and scaling-list layout were validated against
		// the preceding SPS. A changed dependency must not silently retain
		// that validation. Identical syntax declarations at a new span remain
		// usable; legacy PPS-prefix contexts keep their original behavior.
		if previous == nil || previous.PicWidthInMbsMinus1 != s.PicWidthInMbsMinus1 || previous.PicHeightInMapUnitsMinus1 != s.PicHeightInMapUnitsMinus1 || previous.ChromaFormatIDC != s.ChromaFormatIDC || previous.BitDepthLumaMinus8 != s.BitDepthLumaMinus8 {
			c.invalidateFullPPS(&s.SequenceParameterSetID)
		}
		copy := *s
		copy.RBSP = RBSPView{EBSPSpan: s.RBSP.EBSPSpan}
		copy.VUI = nil
		c.sps[s.SequenceParameterSetID] = &copy
	case NALUnitTypeSEI:
		if unit.SEI != nil {
			if !unit.SEI.timingValid {
				c.timingActive = nil
				return
			}
			for _, message := range unit.SEI.Messages {
				if message.Kind == "bufferingPeriod" {
					c.timingActive = nil
					if message.Complete && message.BufferingPeriod != nil && message.BufferingPeriod.Context.State == "resolved" {
						c.timingActive = message.BufferingPeriod.Context.Parameters
						c.timingEvidence = *message.BufferingPeriod.Context.EvidenceSpan
					}
				}
			}
		}
	case NALUnitTypeDataPartitionA, NALUnitTypeDataPartitionB, NALUnitTypeDataPartitionC, NALUnitTypeAuxiliaryCodedSlice, NALUnitTypeCodedSliceExtension, NALUnitTypeDepthViewCodedSliceExt:
		c.timingActive = nil
	case NALUnitTypeNonIDRSlice, NALUnitTypeIDRSlice:
		if c.timingActive != nil {
			s := unit.SliceHeader
			if s == nil || !s.PrefixComplete || s.ReferencedSPSID == nil || *s.ReferencedSPSID != c.timingActive.SPSID {
				c.timingActive = nil
			}
		}
	case NALUnitTypePictureParameterSet:
		p := unit.PictureParameterSet
		if p == nil || p.PictureParameterSetIDSpan.BitLength == 0 || p.PictureParameterSetID > 255 {
			c.pps = [256]*PictureParameterSet{}
			return
		}
		c.pps[p.PictureParameterSetID] = nil
		if !unit.SummaryComplete || !p.Complete || p.Deep == nil || !p.Deep.PrefixComplete || p.SequenceParameterSetID > 31 {
			return
		}
		copy := *p
		copy.RBSP = RBSPView{EBSPSpan: p.RBSP.EBSPSpan}
		c.pps[p.PictureParameterSetID] = &copy
	case NALUnitTypeAccessUnitDelimiter, NALUnitTypeFillerData:
	default:
		c.timingActive = nil
	}
}

// Counts reports usable individual SPS/PPS prefixes; a PPS can still lack
// its referenced SPS. UsablePPSCount counts only pairs ready for a slice.
func (c *ParameterSetContext) Counts() (sps, pps uint32) {
	if c == nil {
		return
	}
	for _, p := range c.sps {
		if p != nil {
			sps++
		}
	}
	for _, p := range c.pps {
		if p != nil {
			pps++
		}
	}
	return
}
func (c *ParameterSetContext) UsablePPSCount() (count uint32) {
	if c == nil {
		return
	}
	for _, p := range c.pps {
		if p != nil && c.sps[p.SequenceParameterSetID] != nil {
			count++
		}
	}
	return
}

func compactUnsigned(f *VUIUnsignedField) *VUIUnsignedField {
	if f == nil {
		return nil
	}
	return &VUIUnsignedField{Value: f.Value}
}
func compactBoolean(f *VUIBooleanField) *VUIBooleanField {
	if f == nil {
		return nil
	}
	return &VUIBooleanField{Value: f.Value}
}
func compactSigned(f *SignedField) *SignedField {
	if f == nil {
		return nil
	}
	return &SignedField{Value: f.Value}
}

func (c *ParameterSetContext) invalidateFullPPS(spsID *uint32) {
	for id, p := range c.pps {
		if p != nil && p.Deep != nil && p.Deep.Full != nil && (spsID == nil || p.SequenceParameterSetID == *spsID) {
			c.pps[id] = nil
		}
	}
}
