package avc

// ParameterSetContext holds bounded parameter-set summaries. Its zero value
// is empty. Parsed summaries are immutable; updates replace slots. Callers
// must not mutate a context concurrently with Clone or Add methods.
type ParameterSetContext struct {
	sps [32]*SequenceParameterSet
	pps [256]*PictureParameterSet
}

func NewParameterSetContext() *ParameterSetContext { return &ParameterSetContext{} }

func (c *ParameterSetContext) Clone() *ParameterSetContext {
	if c == nil {
		return NewParameterSetContext()
	}
	clone := *c
	return &clone
}

// AddConfiguration starts a fresh generation. Broken framing cannot supply
// a reliable parameter-set generation and leaves the context empty.
func (c *ParameterSetContext) AddConfiguration(record ConfigurationRecord) {
	*c = ParameterSetContext{}
	if !record.FramingComplete {
		return
	}
	for _, set := range record.SequenceParameterSets {
		c.AddNALUnit(NALUnit{Header: set.Header, HeaderPresent: set.HeaderPresent, SequenceParameterSet: set.SequenceParameterSet, PictureParameterSet: set.PictureParameterSet, SummaryComplete: set.SummaryComplete})
	}
	for _, set := range record.PictureParameterSets {
		c.AddNALUnit(NALUnit{Header: set.Header, HeaderPresent: set.HeaderPresent, SequenceParameterSet: set.SequenceParameterSet, PictureParameterSet: set.PictureParameterSet, SummaryComplete: set.SummaryComplete})
	}
}

// AddNALUnit replaces the slot even when a known-ID update is invalid. An
// unreadable ID clears that kind of set, so stale values cannot fill gaps.
func (c *ParameterSetContext) AddNALUnit(unit NALUnit) {
	if !unit.HeaderPresent {
		*c = ParameterSetContext{}
		return
	}
	switch unit.Header.NALUnitType {
	case NALUnitTypeEndOfSequence, NALUnitTypeEndOfStream:
		*c = ParameterSetContext{}
	case NALUnitTypeSequenceParameterSet:
		s := unit.SequenceParameterSet
		if s == nil || s.SequenceParameterSetIDSpan.BitLength == 0 || s.SequenceParameterSetID > 31 {
			c.sps = [32]*SequenceParameterSet{}
			return
		}
		c.sps[s.SequenceParameterSetID] = nil
		if !unit.SummaryComplete || !s.Complete || s.Log2MaxFrameNumMinus4 > 12 || s.PicOrderCntType > 2 || s.Log2MaxPicOrderCntLSBMinus4 > 12 {
			return
		}
		copy := *s
		copy.RBSP = RBSPView{EBSPSpan: s.RBSP.EBSPSpan}
		copy.VUI = nil
		c.sps[s.SequenceParameterSetID] = &copy
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
