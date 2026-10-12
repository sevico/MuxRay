package avc

// VUIUnsignedField and VUIBooleanField retain only successfully read syntax.
// Their mapped spans may cross emulation-prevention bytes in the source.
type VUIUnsignedField struct {
	Value uint32
	Span  MappedSpan
}

type VUIBooleanField struct {
	Value bool
	Span  MappedSpan
}

// VUIParameters is the optional SPS VUI syntax, not a decoded-picture or
// measured-frame-rate result. Present=false means the SPS declares no VUI;
// the extension still validates that SPS's rbsp_trailing_bits.
type VUIParameters struct {
	Present                                                         bool
	AspectRatioInfoPresentFlag                                      *VUIBooleanField
	AspectRatioIDC                                                  *VUIUnsignedField
	SARWidth, SARHeight                                             *VUIUnsignedField
	OverscanInfoPresentFlag, OverscanAppropriateFlag                *VUIBooleanField
	VideoSignalTypePresentFlag                                      *VUIBooleanField
	VideoFormat                                                     *VUIUnsignedField
	VideoFullRangeFlag, ColourDescriptionPresentFlag                *VUIBooleanField
	ColourPrimaries, TransferCharacteristics, MatrixCoefficients    *VUIUnsignedField
	ChromaLocInfoPresentFlag                                        *VUIBooleanField
	ChromaSampleLocTypeTopField, ChromaSampleLocTypeBottomField     *VUIUnsignedField
	TimingInfoPresentFlag                                           *VUIBooleanField
	NumUnitsInTick, TimeScale                                       *VUIUnsignedField
	FixedFrameRateFlag                                              *VUIBooleanField
	NALHRDParametersPresentFlag, VCLHRDParametersPresentFlag        *VUIBooleanField
	NALHRD, VCLHRD                                                  *HRDParameters
	LowDelayHRDFlag, PicStructPresentFlag, BitstreamRestrictionFlag *VUIBooleanField
	MotionVectorsOverPicBoundariesFlag                              *VUIBooleanField
	MaxBytesPerPicDenom, MaxBitsPerMBDenom                          *VUIUnsignedField
	Log2MaxMVLengthHorizontal, Log2MaxMVLengthVertical              *VUIUnsignedField
	MaxNumReorderFrames, MaxDecFrameBuffering                       *VUIUnsignedField
	// SyntaxBits excludes the base SPS and trailing bits. RemainingRBSPBits
	// is measured after attempting the tail; a complete result has zero left.
	RBSPBitOffset, SyntaxBits, RemainingRBSPBits uint32
	TrailingBitsValid, Complete                  bool
}

// HRDParameters retains at most the 32 CPB entries allowed by H.264. Declared
// HRD rates and cbr_flag do not establish the measured bitrate or its mode.
type HRDParameters struct {
	CPBCntMinus1, BitRateScale, CPBSizeScale                        *VUIUnsignedField
	CPB                                                             []HRDCPBEntry
	InitialCPBRemovalDelayLengthMinus1, CPBRemovalDelayLengthMinus1 *VUIUnsignedField
	DPBOutputDelayLengthMinus1, TimeOffsetLength                    *VUIUnsignedField
	Complete                                                        bool
}

type HRDCPBEntry struct {
	BitRateValueMinus1, CPBSizeValueMinus1 *VUIUnsignedField
	CBRFlag                                *VUIBooleanField
}

const (
	CodeVUISARInvalid            = "AVC_VUI_SAR_INVALID"
	CodeVUIChromaLocationInvalid = "AVC_VUI_CHROMA_LOCATION_INVALID"
	CodeVUITimingInvalid         = "AVC_VUI_TIMING_INVALID"
	CodeVUIHRDCountInvalid       = "AVC_VUI_HRD_COUNT_INVALID"
	CodeVUIHRDValueInvalid       = "AVC_VUI_HRD_VALUE_INVALID"
	CodeVUIRestrictionInvalid    = "AVC_VUI_RESTRICTION_INVALID"
	CodeRBSPTrailingBitsInvalid  = "AVC_RBSP_TRAILING_BITS_INVALID"
)

func parseVUI(p *rbspParser, present bool, maxNumRefFrames uint32) *VUIParameters {
	v := &VUIParameters{Present: present, RBSPBitOffset: p.bitPosition}
	if present {
		parseVUISyntax(p, v)
		if v.MaxDecFrameBuffering != nil && v.MaxDecFrameBuffering.Value < maxNumRefFrames {
			p.vuiInvalid(v.MaxDecFrameBuffering, CodeVUIRestrictionInvalid, "max_dec_frame_buffering is below max_num_ref_frames", maxNumRefFrames)
		}
	}
	v.SyntaxBits = p.bitPosition - v.RBSPBitOffset
	if !p.fatal {
		v.TrailingBitsValid = p.trailingBits()
	}
	v.RemainingRBSPBits = p.remainingBits()
	v.Complete = !p.fatal && !p.invalid && v.TrailingBitsValid
	return v
}

func parseVUISyntax(p *rbspParser, v *VUIParameters) {
	v.AspectRatioInfoPresentFlag = p.vuiFlag()
	if vuiTrue(v.AspectRatioInfoPresentFlag) {
		v.AspectRatioIDC = p.vuiBits(8)
		if v.AspectRatioIDC != nil && v.AspectRatioIDC.Value == 255 {
			v.SARWidth, v.SARHeight = p.vuiBits(16), p.vuiBits(16)
			// E.2.1 permits zero as unspecified; non-zero terms must be coprime.
			if v.SARWidth != nil && v.SARHeight != nil && v.SARWidth.Value != 0 && v.SARHeight.Value != 0 {
				x, y := v.SARWidth.Value, v.SARHeight.Value
				for y != 0 {
					x, y = y, x%y
				}
				if x != 1 {
					p.addAt(v.SARWidth.Span, Diagnostic{Code: CodeVUISARInvalid, Severity: SeverityError,
						Message: "greatest common divisor of non-zero extended SAR terms must be one", Expected: number(1), Actual: number(uint64(x))})
				}
			}
		}
	}
	v.OverscanInfoPresentFlag = p.vuiFlag()
	if vuiTrue(v.OverscanInfoPresentFlag) {
		v.OverscanAppropriateFlag = p.vuiFlag()
	}
	v.VideoSignalTypePresentFlag = p.vuiFlag()
	if vuiTrue(v.VideoSignalTypePresentFlag) {
		v.VideoFormat = p.vuiBits(3)
		v.VideoFullRangeFlag, v.ColourDescriptionPresentFlag = p.vuiFlag(), p.vuiFlag()
		if vuiTrue(v.ColourDescriptionPresentFlag) {
			v.ColourPrimaries, v.TransferCharacteristics, v.MatrixCoefficients = p.vuiBits(8), p.vuiBits(8), p.vuiBits(8)
		}
	}
	v.ChromaLocInfoPresentFlag = p.vuiFlag()
	if vuiTrue(v.ChromaLocInfoPresentFlag) {
		v.ChromaSampleLocTypeTopField, v.ChromaSampleLocTypeBottomField = p.vuiUE(), p.vuiUE()
		for _, field := range []*VUIUnsignedField{v.ChromaSampleLocTypeTopField, v.ChromaSampleLocTypeBottomField} {
			if field != nil && field.Value > 5 {
				p.vuiInvalid(field, CodeVUIChromaLocationInvalid, "chroma sample location exceeds 5", 5)
			}
		}
	}
	v.TimingInfoPresentFlag = p.vuiFlag()
	if vuiTrue(v.TimingInfoPresentFlag) {
		v.NumUnitsInTick, v.TimeScale, v.FixedFrameRateFlag = p.vuiBits(32), p.vuiBits(32), p.vuiFlag()
		for _, field := range []*VUIUnsignedField{v.NumUnitsInTick, v.TimeScale} {
			if field != nil && field.Value == 0 {
				p.vuiInvalid(field, CodeVUITimingInvalid, "VUI timing numerator and denominator must be non-zero", 1)
			}
		}
	}
	v.NALHRDParametersPresentFlag = p.vuiFlag()
	if vuiTrue(v.NALHRDParametersPresentFlag) {
		v.NALHRD = parseHRD(p)
	}
	v.VCLHRDParametersPresentFlag = p.vuiFlag()
	if vuiTrue(v.VCLHRDParametersPresentFlag) {
		v.VCLHRD = parseHRD(p)
	}
	if vuiTrue(v.NALHRDParametersPresentFlag) || vuiTrue(v.VCLHRDParametersPresentFlag) {
		v.LowDelayHRDFlag = p.vuiFlag()
	}
	if vuiTrue(v.LowDelayHRDFlag) {
		for _, h := range []*HRDParameters{v.NALHRD, v.VCLHRD} {
			if h != nil && h.CPBCntMinus1 != nil && h.CPBCntMinus1.Value != 0 {
				p.vuiInvalid(h.CPBCntMinus1, CodeVUIHRDValueInvalid, "low_delay_hrd_flag requires one CPB entry", 0)
			}
		}
	}
	if v.NALHRD != nil && v.VCLHRD != nil {
		for _, pair := range [][2]*VUIUnsignedField{
			{v.NALHRD.InitialCPBRemovalDelayLengthMinus1, v.VCLHRD.InitialCPBRemovalDelayLengthMinus1},
			{v.NALHRD.CPBRemovalDelayLengthMinus1, v.VCLHRD.CPBRemovalDelayLengthMinus1},
			{v.NALHRD.DPBOutputDelayLengthMinus1, v.VCLHRD.DPBOutputDelayLengthMinus1},
			{v.NALHRD.TimeOffsetLength, v.VCLHRD.TimeOffsetLength},
		} {
			if pair[0] != nil && pair[1] != nil && pair[0].Value != pair[1].Value {
				p.vuiInvalid(pair[1], CodeVUIHRDValueInvalid, "NAL and VCL HRD delay lengths must match", pair[0].Value)
			}
		}
	}
	v.PicStructPresentFlag, v.BitstreamRestrictionFlag = p.vuiFlag(), p.vuiFlag()
	if vuiTrue(v.BitstreamRestrictionFlag) {
		v.MotionVectorsOverPicBoundariesFlag = p.vuiFlag()
		v.MaxBytesPerPicDenom, v.MaxBitsPerMBDenom = p.vuiUE(), p.vuiUE()
		v.Log2MaxMVLengthHorizontal, v.Log2MaxMVLengthVertical = p.vuiUE(), p.vuiUE()
		v.MaxNumReorderFrames, v.MaxDecFrameBuffering = p.vuiUE(), p.vuiUE()
		for _, field := range []*VUIUnsignedField{v.MaxBytesPerPicDenom, v.MaxBitsPerMBDenom} {
			if field != nil && field.Value > 16 {
				p.vuiInvalid(field, CodeVUIRestrictionInvalid, "VUI bitstream restriction value exceeds 16", 16)
			}
		}
		for _, field := range []*VUIUnsignedField{v.Log2MaxMVLengthHorizontal, v.Log2MaxMVLengthVertical} {
			if field != nil && field.Value > 15 {
				p.vuiInvalid(field, CodeVUIRestrictionInvalid, "VUI motion vector length exceeds 15", 15)
			}
		}
		if v.MaxNumReorderFrames != nil && v.MaxDecFrameBuffering != nil && v.MaxNumReorderFrames.Value > v.MaxDecFrameBuffering.Value {
			p.vuiInvalid(v.MaxNumReorderFrames, CodeVUIRestrictionInvalid, "max_num_reorder_frames exceeds max_dec_frame_buffering", v.MaxDecFrameBuffering.Value)
		}
	}
}

func parseHRD(p *rbspParser) *HRDParameters {
	h := &HRDParameters{CPBCntMinus1: p.vuiUE(), CPB: []HRDCPBEntry{}}
	if h.CPBCntMinus1 == nil {
		return h
	}
	if h.CPBCntMinus1.Value > 31 {
		p.semanticFatal(h.CPBCntMinus1.Span, CodeVUIHRDCountInvalid, "cpb_cnt_minus1 exceeds 31", 31, uint64(h.CPBCntMinus1.Value))
		return h
	}
	h.BitRateScale, h.CPBSizeScale = p.vuiBits(4), p.vuiBits(4)
	valid := true
	for i := uint32(0); i <= h.CPBCntMinus1.Value && !p.fatal; i++ {
		entry := HRDCPBEntry{p.vuiUE(), p.vuiUE(), p.vuiFlag()}
		for _, field := range []*VUIUnsignedField{entry.BitRateValueMinus1, entry.CPBSizeValueMinus1} {
			if field != nil && field.Value == ^uint32(0) {
				p.vuiInvalid(field, CodeVUIHRDValueInvalid, "HRD value exceeds 2^32-2", ^uint32(0)-1)
				valid = false
			}
		}
		if i > 0 && entry.BitRateValueMinus1 != nil && entry.CPBSizeValueMinus1 != nil {
			previous := h.CPB[i-1]
			if entry.BitRateValueMinus1.Value <= previous.BitRateValueMinus1.Value {
				p.vuiInvalid(entry.BitRateValueMinus1, CodeVUIHRDValueInvalid, "HRD bit rates must strictly increase", previous.BitRateValueMinus1.Value)
				valid = false
			}
			if entry.CPBSizeValueMinus1.Value > previous.CPBSizeValueMinus1.Value {
				p.vuiInvalid(entry.CPBSizeValueMinus1, CodeVUIHRDValueInvalid, "HRD CPB sizes must not increase", previous.CPBSizeValueMinus1.Value)
				valid = false
			}
		}
		h.CPB = append(h.CPB, entry)
	}
	h.InitialCPBRemovalDelayLengthMinus1, h.CPBRemovalDelayLengthMinus1 = p.vuiBits(5), p.vuiBits(5)
	h.DPBOutputDelayLengthMinus1, h.TimeOffsetLength = p.vuiBits(5), p.vuiBits(5)
	h.Complete = !p.fatal && valid
	return h
}

func vuiTrue(field *VUIBooleanField) bool { return field != nil && field.Value }

func (p *rbspParser) vuiFlag() *VUIBooleanField {
	value, span, ok := p.readBits(1)
	if !ok {
		return nil
	}
	return &VUIBooleanField{value != 0, span}
}

func (p *rbspParser) vuiBits(width uint8) *VUIUnsignedField {
	value, span, ok := p.readBits(width)
	if !ok {
		return nil
	}
	return &VUIUnsignedField{uint32(value), span}
}

func (p *rbspParser) vuiUE() *VUIUnsignedField {
	if p.fatal {
		return nil
	}
	value, span, ok := p.readUE()
	if !ok {
		return nil
	}
	return &VUIUnsignedField{value, span}
}

func (p *rbspParser) vuiInvalid(field *VUIUnsignedField, code, message string, expected uint32) {
	p.addAt(field.Span, Diagnostic{Code: code, Severity: SeverityError, Message: message, Expected: number(uint64(expected)), Actual: number(uint64(field.Value))})
}

func (p *rbspParser) trailingBits() bool { return p.trailingBitsFor("SPS") }

func (p *rbspParser) trailingBitsFor(label string) bool {
	value, span, ok := p.readBits(1)
	if !ok {
		return false
	}
	if value != 1 {
		p.semanticFatal(span, CodeRBSPTrailingBitsInvalid, "rbsp_stop_one_bit must be one", 1, value)
		return false
	}
	for p.bitPosition%8 != 0 {
		value, span, ok = p.readBits(1)
		if !ok {
			return false
		}
		if value != 0 {
			p.semanticFatal(span, CodeRBSPTrailingBitsInvalid, "rbsp_alignment_zero_bit must be zero", 0, value)
			return false
		}
	}
	if p.remainingBits() != 0 {
		p.fail(CodeRBSPTrailingBitsInvalid, "unexpected data after "+label+" rbsp_trailing_bits", 0, uint64(p.remainingBits()))
		return false
	}
	return true
}
