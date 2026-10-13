package avc

// Syntax: ITU-T H.264 (08/2024), D.1.1-D.1.2 and D.2.1-D.2.2.
// Cross-check: https://ffmpeg.org/doxygen/trunk/cbs__h264__syntax__template_8c_source.html
// This parser deliberately requires explicit activation evidence, rather than
// adopting FFmpeg CBS's optional single-SPS fallback.

import "github.com/sevico/muxray/internal/cursor"

const (
	CodeSEITimingContextMissing   = "AVC_SEI_TIMING_CONTEXT_MISSING"
	CodeSEITimingContextAmbiguous = "AVC_SEI_TIMING_CONTEXT_AMBIGUOUS"
)

func summarizeSPSTiming(s *SequenceParameterSet) *SPSTimingParameters {
	if s == nil || !s.Complete || s.VUI == nil || !s.VUI.Complete {
		return nil
	}
	span := parameterNALSpan(s.RBSP)
	if span == nil {
		return nil
	}
	v := s.VUI
	t := &SPSTimingParameters{SPSID: s.SequenceParameterSetID, SPSSpan: *span, PicStructPresent: vuiTrue(v.PicStructPresentFlag), TimingInfoPresent: vuiTrue(v.TimingInfoPresentFlag), FixedFrameRate: vuiTrue(v.FixedFrameRateFlag)}
	if t.TimingInfoPresent {
		t.NumUnitsInTick, t.TimeScale = v.NumUnitsInTick.Value, v.TimeScale.Value
	}
	for _, pair := range []struct {
		src *HRDParameters
		dst *HRDTimingParameters
	}{{v.NALHRD, &t.NALHRD}, {v.VCLHRD, &t.VCLHRD}} {
		if pair.src == nil {
			continue
		}
		h := pair.src
		if !h.Complete {
			return nil
		}
		*pair.dst = HRDTimingParameters{Present: true, CPBCount: h.CPBCntMinus1.Value + 1, InitialCPBRemovalDelayLength: uint8(h.InitialCPBRemovalDelayLengthMinus1.Value + 1), CPBRemovalDelayLength: uint8(h.CPBRemovalDelayLengthMinus1.Value + 1), DPBOutputDelayLength: uint8(h.DPBOutputDelayLengthMinus1.Value + 1), TimeOffsetLength: uint8(h.TimeOffsetLength.Value)}
	}
	return t
}

func resolvedTiming(t *SPSTimingParameters, source string, evidence cursor.Span) SEITimingContext {
	id, span := t.SPSID, t.SPSSpan
	return SEITimingContext{State: "resolved", Source: source, SPSID: &id, SPSSpan: &span, EvidenceSpan: &evidence, Parameters: t}
}

func timingEvidence(m *SEIMessage) cursor.Span {
	if m.PayloadSpan == nil || len(m.PayloadSpan.SourceSpans) == 0 {
		return cursor.Span{}
	}
	spans := m.PayloadSpan.SourceSpans
	first, last := spans[0], spans[len(spans)-1]
	bits := (last.ByteOffset-first.ByteOffset)*8 + uint64(last.BitOffset) + uint64(last.BitLength) - uint64(first.BitOffset)
	return cursor.Span{ByteOffset: first.ByteOffset, BitOffset: first.BitOffset, BitLength: uint32(bits)}
}

func (p *rbspParser) missingTiming(state string) {
	code := CodeSEITimingContextMissing
	message := "SEI timing context unavailable; payload remains opaque"
	if state == "ambiguous" {
		code, message = CodeSEITimingContextAmbiguous, "SEI timing activation evidence conflicts; payload remains opaque"
	}
	p.fail(code, message, 0, 0)
}

func (p *rbspParser) seiBufferingPeriod(complete bool, c *ParameterSetContext, evidence cursor.Span) *SEIBufferingPeriod {
	r := &SEIBufferingPeriod{Context: SEITimingContext{State: "missing"}}
	r.SequenceParameterSetID = p.fullUE(31, CodeSEISyntaxInvalid, "seq_parameter_set_id")
	if r.SequenceParameterSetID == nil || p.fatal {
		return r
	}
	id := r.SequenceParameterSetID.Value
	r.Context.SPSID = &id
	var timing *SPSTimingParameters
	if c != nil && c.sps[id] != nil {
		timing = c.sps[id].Timing
	}
	if timing == nil {
		p.missingTiming("missing")
		return r
	}
	r.Context = resolvedTiming(timing, "bufferingPeriod", evidence)
	readDelays := func(h HRDTimingParameters) []SEICPBInitialDelay {
		if !h.Present {
			return nil
		}
		out := make([]SEICPBInitialDelay, 0, h.CPBCount)
		for i := uint32(0); i < h.CPBCount && !p.fatal; i++ {
			d := SEICPBInitialDelay{Index: i, InitialCPBRemovalDelay: p.vuiBits(h.InitialCPBRemovalDelayLength)}
			if d.InitialCPBRemovalDelay != nil && d.InitialCPBRemovalDelay.Value == 0 {
				p.semanticFatal(d.InitialCPBRemovalDelay.Span, CodeSEISyntaxInvalid, "initial_cpb_removal_delay must be nonzero", 1, 0)
			}
			d.InitialCPBRemovalDelayOffset = p.vuiBits(h.InitialCPBRemovalDelayLength)
			out = append(out, d)
		}
		return out
	}
	r.NALInitialDelays = readDelays(timing.NALHRD)
	r.VCLInitialDelays = readDelays(timing.VCLHRD)
	r.PayloadExtensionBits, r.PayloadAlignmentBits = p.seiPayloadTail(complete)
	return r
}

func (p *rbspParser) seiPictureTiming(r *SEIPictureTiming, complete bool, context SEITimingContext) {
	r.Context = context
	if context.State != "resolved" {
		p.missingTiming(context.State)
		return
	}
	t := context.Parameters
	h := t.NALHRD
	if !h.Present {
		h = t.VCLHRD
	}
	if h.Present {
		r.CPBRemovalDelay = p.vuiBits(h.CPBRemovalDelayLength)
		r.DPBOutputDelay = p.vuiBits(h.DPBOutputDelayLength)
	}
	if t.PicStructPresent {
		r.PicStruct = p.seiBoundedBits(4, 8, "pic_struct")
		if r.PicStruct == nil || p.fatal {
			return
		}
		n := [...]uint32{1, 1, 1, 2, 2, 3, 3, 2, 3}[r.PicStruct.Value]
		r.ClockTimestamps = make([]SEIClockTimestamp, 0, n)
		for i := uint32(0); i < n && !p.fatal; i++ {
			c := SEIClockTimestamp{Index: i, ClockTimestampFlag: p.vuiFlag()}
			if vuiTrue(c.ClockTimestampFlag) {
				p.seiClockTimestamp(&c, h)
			}
			r.ClockTimestamps = append(r.ClockTimestamps, c)
		}
	}
	r.PayloadExtensionBits, r.PayloadAlignmentBits = p.seiPayloadTail(complete)
}

func (p *rbspParser) seiBoundedBits(width uint8, maximum uint32, name string) *VUIUnsignedField {
	f := p.vuiBits(width)
	if f != nil && f.Value > maximum {
		p.semanticFatal(f.Span, CodeSEISyntaxInvalid, name+" exceeds syntax range", uint64(maximum), uint64(f.Value))
	}
	return f
}

func (p *rbspParser) seiClockTimestamp(c *SEIClockTimestamp, h HRDTimingParameters) {
	c.CTType = p.seiBoundedBits(2, 2, "ct_type")
	c.NuitFieldBasedFlag = p.vuiFlag()
	c.CountingType = p.seiBoundedBits(5, 6, "counting_type")
	c.FullTimestampFlag = p.vuiFlag()
	c.DiscontinuityFlag = p.vuiFlag()
	c.CntDroppedFlag = p.vuiFlag()
	c.NFrames = p.vuiBits(8)
	if vuiTrue(c.FullTimestampFlag) {
		c.SecondsValue = p.seiBoundedBits(6, 59, "seconds_value")
		c.MinutesValue = p.seiBoundedBits(6, 59, "minutes_value")
		c.HoursValue = p.seiBoundedBits(5, 23, "hours_value")
	} else {
		c.SecondsFlag = p.vuiFlag()
		if vuiTrue(c.SecondsFlag) {
			c.SecondsValue = p.seiBoundedBits(6, 59, "seconds_value")
			c.MinutesFlag = p.vuiFlag()
			if vuiTrue(c.MinutesFlag) {
				c.MinutesValue = p.seiBoundedBits(6, 59, "minutes_value")
				c.HoursFlag = p.vuiFlag()
				if vuiTrue(c.HoursFlag) {
					c.HoursValue = p.seiBoundedBits(5, 23, "hours_value")
				}
			}
		}
	}
	width := h.TimeOffsetLength
	if !h.Present {
		width = 24
	}
	if width != 0 {
		v, s, ok := p.readBits(width)
		if ok {
			signed := int64(v)
			if v&(uint64(1)<<(width-1)) != 0 {
				signed -= int64(uint64(1) << width)
			}
			c.TimeOffset = &SignedField{Value: signed, Span: s}
		}
	}
}

// A deferred picture message shares the bounded, already-read SEI payload.
// It snapshots at most 32 fixed-size SPS pointers, never re-reads source bytes,
// and is discarded at the end of this one NAL packet/configuration parse.
type pendingSEITiming struct {
	sequence   *SEIMessageSequence
	index      uint32
	parser     *rbspParser
	sets       [32]*SPSTimingParameters
	active     *SPSTimingParameters
	evidence   cursor.Span
	afterSlice bool
}

type timingSliceEvidence struct {
	parameters      *SPSTimingParameters
	span            cursor.Span
	seen, ambiguous bool
	firstMBZero     bool
	picture         timingPictureIdentity
}

// These are the already-read conditions for a new primary coded picture in
// H.264 7.4.1.2.4. Presence bits distinguish absent optional fields from zero.
type timingPictureIdentity struct {
	values         [10]int64
	present        uint16
	idr, reference bool
}

func timingIdentity(unit NALUnit) timingPictureIdentity {
	s := unit.SliceHeader
	key := timingPictureIdentity{idr: unit.Header.NALUnitType == NALUnitTypeIDRSlice, reference: unit.Header.NALRefIDC != 0}
	for i, f := range []*VUIUnsignedField{s.PictureParameterSetID, s.FrameNum, s.IDRPicID, s.PicOrderCntLSB, s.ColourPlaneID} {
		if f != nil {
			key.values[i] = int64(f.Value)
			key.present |= 1 << i
		}
	}
	for i, f := range []*VUIBooleanField{s.FieldPicFlag, s.BottomFieldFlag} {
		if f != nil {
			if f.Value {
				key.values[i+5] = 1
			}
			key.present |= 1 << (i + 5)
		}
	}
	for i, f := range []*SignedField{s.DeltaPicOrderCntBottom, s.DeltaPicOrderCnt0, s.DeltaPicOrderCnt1} {
		if f != nil {
			key.values[i+7] = f.Value
			key.present |= 1 << (i + 7)
		}
	}
	return key
}

func (b *parseBudget) rememberTimingSlice(unit NALUnit, c *ParameterSetContext) {
	s := unit.SliceHeader
	if s == nil || !s.PrefixComplete || s.ReferencedSPSID == nil {
		b.timingSlice.ambiguous = true
		return
	}
	if s.FirstMBInSlice != nil && s.FirstMBInSlice.Value == 0 {
		if b.timingSlice.firstMBZero {
			b.timingSlice.ambiguous = true
		}
		b.timingSlice.firstMBZero = true
	}
	var timing *SPSTimingParameters
	if c.sps[*s.ReferencedSPSID] != nil {
		timing = c.sps[*s.ReferencedSPSID].Timing
	}
	if timing == nil {
		b.timingSlice.ambiguous = true
		return
	}
	picture := timingIdentity(unit)
	if b.timingSlice.seen && (b.timingSlice.picture != picture || *b.timingSlice.parameters != *timing) {
		b.timingSlice.ambiguous = true
	}
	b.timingSlice.picture = picture
	b.timingSlice.seen = true
	b.timingSlice.parameters = timing
	b.timingSlice.span = unit.Span
}

func (b *parseBudget) finishSEITiming(c *ParameterSetContext, framingComplete bool) {
	for _, pending := range b.pendingTiming {
		m := &pending.sequence.Messages[pending.index]
		context := SEITimingContext{State: "missing"}
		if !framingComplete || b.timingSlice.ambiguous || pending.afterSlice {
			context.State = "ambiguous"
		} else if b.timingSlice.seen {
			t := b.timingSlice.parameters
			atMessage := pending.sets[t.SPSID]
			if atMessage != nil && *atMessage == *t {
				if pending.active != nil && *pending.active != *t {
					context.State = "ambiguous"
				} else {
					context = resolvedTiming(t, "slice", b.timingSlice.span)
				}
			}
		} else if pending.active != nil {
			if c != nil && c.timingActive != nil && *c.timingActive == *pending.active {
				context = resolvedTiming(pending.active, "bufferingPeriod", pending.evidence)
			} else {
				context.State = "ambiguous"
			}
		}
		pending.parser.seiPictureTiming(m.PictureTiming, m.PayloadComplete, context)
		m.Complete = m.PayloadComplete && !pending.parser.fatal && !pending.parser.invalid
		if !m.Complete {
			pending.sequence.Complete = false
		}
	}
	b.pendingTiming = nil
}
