package avc

import (
	"io"

	"github.com/sevico/muxray/internal/cursor"
)

func parseNALHeader(source io.ReaderAt, span cursor.Span, diagnostics *diagnosticCollector) (NALHeader, error) {
	start, end, err := span.ByteRange()
	if err != nil {
		return NALHeader{}, err
	}
	if start == end {
		diagnostics.add(Diagnostic{
			Code: CodeNALHeaderTruncated, Severity: SeverityError,
			Offset: start, Message: "NAL unit header is missing",
			Expected: number(1), Actual: number(0), RecoveryOffset: number(end),
		})
		return NALHeader{}, nil
	}
	var raw [1]byte
	if err := readExactAt(source, start, raw[:]); err != nil {
		return NALHeader{}, err
	}
	header := NALHeader{
		Raw: raw[0], Span: cursor.Span{ByteOffset: start, BitLength: 8},
		ForbiddenZeroBit:     raw[0]&0x80 != 0,
		ForbiddenZeroBitSpan: cursor.Span{ByteOffset: start, BitLength: 1},
		NALRefIDC:            (raw[0] >> 5) & 0x03,
		NALRefIDCSpan:        cursor.Span{ByteOffset: start, BitOffset: 1, BitLength: 2},
		NALUnitType:          NALUnitType(raw[0] & 0x1f),
		NALUnitTypeSpan:      cursor.Span{ByteOffset: start, BitOffset: 3, BitLength: 5},
		Complete:             true,
	}
	header.NALUnitTypeName = header.NALUnitType.String()
	if header.ForbiddenZeroBit {
		diagnostics.add(Diagnostic{
			Code: CodeForbiddenZeroBitSet, Severity: SeverityError,
			Offset: start, Message: "forbidden_zero_bit must be zero",
			Expected: number(0), Actual: number(1), RecoveryOffset: number(end),
		})
	}
	return header, nil
}

func parseParameterSetSummary(source io.ReaderAt, span cursor.Span, options Options, budget *parseBudget, diagnostics *diagnosticCollector) (NALHeader, *SequenceParameterSet, *PictureParameterSet, bool, error) {
	header, err := parseNALHeader(source, span, diagnostics)
	if err != nil || !header.Complete {
		return header, nil, nil, false, err
	}
	switch header.NALUnitType {
	case NALUnitTypeSequenceParameterSet:
		summary, parseErr := parseSequenceParameterSet(source, span, options, budget, diagnostics)
		complete := summary.Complete && (summary.VUI == nil || summary.VUI.Complete)
		return header, &summary, nil, complete && !header.ForbiddenZeroBit, parseErr
	case NALUnitTypePictureParameterSet:
		summary, parseErr := parsePictureParameterSet(source, span, options, budget, diagnostics)
		complete := summary.Complete && (summary.Deep == nil || summary.Deep.PrefixComplete)
		if options.ParseFullHeaders {
			complete = complete && summary.Deep != nil && summary.Deep.Full != nil && summary.Deep.Full.Complete
		}
		return header, nil, &summary, complete && !header.ForbiddenZeroBit, parseErr
	default:
		return header, nil, nil, !header.ForbiddenZeroBit, nil
	}
}

func parseSequenceParameterSet(source io.ReaderAt, span cursor.Span, options Options, budget *parseBudget, diagnostics *diagnosticCollector) (SequenceParameterSet, error) {
	data, err := decodeRBSP(source, span, options, budget, diagnostics)
	result := SequenceParameterSet{RBSP: data.view}
	if err != nil || !data.valid || len(data.bytes) == 0 {
		if err == nil && len(data.bytes) == 0 && data.valid {
			diagnostics.add(Diagnostic{
				Code: CodeRBSPSyntaxTruncated, Severity: SeverityError,
				Offset: data.end, Message: "SPS RBSP is missing",
				Expected: number(1), Actual: number(0), RecoveryOffset: number(data.end),
			})
		}
		return result, err
	}
	p := newRBSPParser(data, diagnostics)

	value, mapped, ok := p.readBits(8)
	result.ProfileIDC, result.ProfileIDCSpan = uint8(value), mapped
	if !ok {
		return finishSPS(result, p), nil
	}
	value, mapped, ok = p.readBits(6)
	result.ConstraintSetFlags, result.ConstraintSetFlagsSpan = uint8(value), mapped
	if !ok {
		return finishSPS(result, p), nil
	}
	value, mapped, ok = p.readBits(2)
	result.ReservedZero2Bits, result.ReservedZero2BitsSpan = uint8(value), mapped
	if !ok {
		return finishSPS(result, p), nil
	}
	if value != 0 {
		p.addAt(mapped, Diagnostic{
			Code: CodeSPSReservedBitsUnexpected, Severity: SeverityWarning,
			Message: "SPS reserved_zero_2bits should be zero", Expected: number(0), Actual: number(value),
		})
	}
	value, mapped, ok = p.readBits(8)
	result.LevelIDC, result.LevelIDCSpan = uint8(value), mapped
	if !ok {
		return finishSPS(result, p), nil
	}
	ue, mapped, ok := p.readUE()
	result.SequenceParameterSetID, result.SequenceParameterSetIDSpan = ue, mapped
	if !ok {
		return finishSPS(result, p), nil
	}
	if ue > 31 {
		p.addAt(mapped, Diagnostic{
			Code: CodeSPSIDOutOfRange, Severity: SeverityError,
			Message: "seq_parameter_set_id exceeds 31", Expected: number(31), Actual: number(uint64(ue)),
		})
	}

	result.ChromaFormatIDC = 1
	if highProfileSyntax(result.ProfileIDC) {
		result.HighProfileFieldsPresent = true
		ue, mapped, ok = p.readUE()
		result.ChromaFormatIDC, result.ChromaFormatIDCSpan = ue, mapped
		if !ok {
			return finishSPS(result, p), nil
		}
		if ue > 3 {
			p.semanticFatal(mapped, CodeSPSChromaFormatInvalid, "chroma_format_idc exceeds 3", 3, uint64(ue))
			return finishSPS(result, p), nil
		}
		if ue == 3 {
			result.SeparateColourPlaneFlagPresent = true
			value, mapped, ok = p.readBits(1)
			result.SeparateColourPlaneFlag, result.SeparateColourPlaneFlagSpan = value != 0, mapped
			if !ok {
				return finishSPS(result, p), nil
			}
		}
		ue, mapped, ok = p.readUE()
		result.BitDepthLumaMinus8, result.BitDepthLumaMinus8Span = ue, mapped
		if !ok {
			return finishSPS(result, p), nil
		}
		if ue > 6 {
			p.addAt(mapped, Diagnostic{
				Code: CodeSPSBitDepthInvalid, Severity: SeverityError,
				Message: "bit_depth_luma_minus8 exceeds 6", Expected: number(6), Actual: number(uint64(ue)),
			})
		}
		ue, mapped, ok = p.readUE()
		result.BitDepthChromaMinus8, result.BitDepthChromaMinus8Span = ue, mapped
		if !ok {
			return finishSPS(result, p), nil
		}
		if ue > 6 {
			p.addAt(mapped, Diagnostic{
				Code: CodeSPSBitDepthInvalid, Severity: SeverityError,
				Message: "bit_depth_chroma_minus8 exceeds 6", Expected: number(6), Actual: number(uint64(ue)),
			})
		}
		value, mapped, ok = p.readBits(1)
		result.QPPrimeYZeroTransformBypassFlag, result.QPPrimeYZeroTransformBypassFlagSpan = value != 0, mapped
		if !ok {
			return finishSPS(result, p), nil
		}
		value, mapped, ok = p.readBits(1)
		result.SequenceScalingMatrixPresentFlag, result.SequenceScalingMatrixPresentFlagSpan = value != 0, mapped
		if !ok {
			return finishSPS(result, p), nil
		}
		if value != 0 && !p.skipScalingMatrices(result.ChromaFormatIDC) {
			return finishSPS(result, p), nil
		}
	}

	ue, mapped, ok = p.readUE()
	result.Log2MaxFrameNumMinus4, result.Log2MaxFrameNumMinus4Span = ue, mapped
	if !ok {
		return finishSPS(result, p), nil
	}
	if ue > 12 {
		p.addAt(mapped, Diagnostic{
			Code: CodeSPSLog2MaxFrameNumInvalid, Severity: SeverityError,
			Message: "log2_max_frame_num_minus4 exceeds 12", Expected: number(12), Actual: number(uint64(ue)),
		})
	}
	ue, mapped, ok = p.readUE()
	result.PicOrderCntType, result.PicOrderCntTypeSpan = ue, mapped
	if !ok {
		return finishSPS(result, p), nil
	}
	switch ue {
	case 0:
		result.Log2MaxPicOrderCntLSBMinus4Present = true
		ue, mapped, ok = p.readUE()
		result.Log2MaxPicOrderCntLSBMinus4, result.Log2MaxPicOrderCntLSBMinus4Span = ue, mapped
		if !ok {
			return finishSPS(result, p), nil
		}
		if ue > 12 {
			p.addAt(mapped, Diagnostic{
				Code: CodeSPSLog2MaxPicOrderInvalid, Severity: SeverityError,
				Message: "log2_max_pic_order_cnt_lsb_minus4 exceeds 12", Expected: number(12), Actual: number(uint64(ue)),
			})
		}
	case 1:
		result.DeltaPicOrderAlwaysZeroFlagPresent = true
		value, mapped, ok = p.readBits(1)
		result.DeltaPicOrderAlwaysZeroFlag, result.DeltaPicOrderAlwaysZeroFlagSpan = value != 0, mapped
		if !ok {
			return finishSPS(result, p), nil
		}
		se, seSpan, parsed := p.readSE()
		result.OffsetForNonRefPic, result.OffsetForNonRefPicSpan = se, seSpan
		if !parsed {
			return finishSPS(result, p), nil
		}
		se, seSpan, parsed = p.readSE()
		result.OffsetForTopToBottomField, result.OffsetForTopToBottomFieldSpan = se, seSpan
		if !parsed {
			return finishSPS(result, p), nil
		}
		ue, mapped, ok = p.readUE()
		result.NumRefFramesInPicOrderCntCycle, result.NumRefFramesInPicOrderCntCycleSpan = ue, mapped
		if !ok {
			return finishSPS(result, p), nil
		}
		if ue > 255 {
			p.semanticFatal(mapped, CodeSPSReferenceCycleTooLarge, "num_ref_frames_in_pic_order_cnt_cycle exceeds 255", 255, uint64(ue))
			return finishSPS(result, p), nil
		}
		for index := uint32(0); index < ue; index++ {
			if _, _, parsed = p.readSE(); !parsed {
				return finishSPS(result, p), nil
			}
		}
	case 2:
	default:
		p.semanticFatal(mapped, CodeSPSPicOrderCntTypeInvalid, "pic_order_cnt_type exceeds 2", 2, uint64(ue))
		return finishSPS(result, p), nil
	}

	ue, mapped, ok = p.readUE()
	result.MaxNumRefFrames, result.MaxNumRefFramesSpan = ue, mapped
	if !ok {
		return finishSPS(result, p), nil
	}
	value, mapped, ok = p.readBits(1)
	result.GapsInFrameNumValueAllowedFlag, result.GapsInFrameNumValueAllowedFlagSpan = value != 0, mapped
	if !ok {
		return finishSPS(result, p), nil
	}
	ue, mapped, ok = p.readUE()
	result.PicWidthInMbsMinus1, result.PicWidthInMbsMinus1Span = ue, mapped
	if !ok {
		return finishSPS(result, p), nil
	}
	ue, mapped, ok = p.readUE()
	result.PicHeightInMapUnitsMinus1, result.PicHeightInMapUnitsMinus1Span = ue, mapped
	if !ok {
		return finishSPS(result, p), nil
	}
	value, mapped, ok = p.readBits(1)
	result.FrameMbsOnlyFlag, result.FrameMbsOnlyFlagSpan = value != 0, mapped
	if !ok {
		return finishSPS(result, p), nil
	}
	if !result.FrameMbsOnlyFlag {
		result.MBAdaptiveFrameFieldFlagPresent = true
		value, mapped, ok = p.readBits(1)
		result.MBAdaptiveFrameFieldFlag, result.MBAdaptiveFrameFieldFlagSpan = value != 0, mapped
		if !ok {
			return finishSPS(result, p), nil
		}
	}
	value, mapped, ok = p.readBits(1)
	result.Direct8x8InferenceFlag, result.Direct8x8InferenceFlagSpan = value != 0, mapped
	if !ok {
		return finishSPS(result, p), nil
	}
	value, mapped, ok = p.readBits(1)
	result.FrameCroppingFlag, result.FrameCroppingFlagSpan = value != 0, mapped
	if !ok {
		return finishSPS(result, p), nil
	}
	if result.FrameCroppingFlag {
		ue, mapped, ok = p.readUE()
		result.FrameCropLeftOffset, result.FrameCropLeftOffsetSpan = ue, mapped
		if !ok {
			return finishSPS(result, p), nil
		}
		ue, mapped, ok = p.readUE()
		result.FrameCropRightOffset, result.FrameCropRightOffsetSpan = ue, mapped
		if !ok {
			return finishSPS(result, p), nil
		}
		ue, mapped, ok = p.readUE()
		result.FrameCropTopOffset, result.FrameCropTopOffsetSpan = ue, mapped
		if !ok {
			return finishSPS(result, p), nil
		}
		ue, mapped, ok = p.readUE()
		result.FrameCropBottomOffset, result.FrameCropBottomOffsetSpan = ue, mapped
		if !ok {
			return finishSPS(result, p), nil
		}
	}
	value, mapped, ok = p.readBits(1)
	result.VUIParametersPresentFlag, result.VUIParametersPresentFlagSpan = value != 0, mapped
	if !ok {
		return finishSPS(result, p), nil
	}

	deriveDimensions(&result, p)
	result = finishSPS(result, p)
	if options.ParseVUI && result.Complete {
		result.VUI = parseVUI(p, result.VUIParametersPresentFlag, result.MaxNumRefFrames)
		if options.ParseSEITiming {
			result.Timing = summarizeSPSTiming(&result)
		}
	}
	return result, nil
}

func finishSPS(result SequenceParameterSet, p *rbspParser) SequenceParameterSet {
	result.SyntaxBits = p.bitPosition
	result.RemainingRBSPBits = p.remainingBits()
	result.Complete = !p.fatal && !p.invalid && p.data.valid
	return result
}

func parsePictureParameterSet(source io.ReaderAt, span cursor.Span, options Options, budget *parseBudget, diagnostics *diagnosticCollector) (PictureParameterSet, error) {
	data, err := decodeRBSP(source, span, options, budget, diagnostics)
	result := PictureParameterSet{RBSP: data.view}
	if err != nil || !data.valid || len(data.bytes) == 0 {
		if err == nil && len(data.bytes) == 0 && data.valid {
			diagnostics.add(Diagnostic{
				Code: CodeRBSPSyntaxTruncated, Severity: SeverityError,
				Offset: data.end, Message: "PPS RBSP is missing",
				Expected: number(1), Actual: number(0), RecoveryOffset: number(data.end),
			})
		}
		return result, err
	}
	p := newRBSPParser(data, diagnostics)
	value, mapped, ok := p.readUE()
	result.PictureParameterSetID, result.PictureParameterSetIDSpan = value, mapped
	if !ok {
		return finishPPS(result, p), nil
	}
	if value > 255 {
		p.addAt(mapped, Diagnostic{
			Code: CodePPSIDOutOfRange, Severity: SeverityError,
			Message: "pic_parameter_set_id exceeds 255", Expected: number(255), Actual: number(uint64(value)),
		})
	}
	value, mapped, ok = p.readUE()
	result.SequenceParameterSetID, result.SequenceParameterSetIDSpan = value, mapped
	if !ok {
		return finishPPS(result, p), nil
	}
	if value > 31 {
		p.addAt(mapped, Diagnostic{
			Code: CodePPSReferencedSPSOutOfRange, Severity: SeverityError,
			Message: "PPS seq_parameter_set_id exceeds 31", Expected: number(31), Actual: number(uint64(value)),
		})
	}
	bit, mapped, ok := p.readBits(1)
	result.EntropyCodingModeFlag, result.EntropyCodingModeFlagSpan = bit != 0, mapped
	if !ok {
		return finishPPS(result, p), nil
	}
	bit, mapped, ok = p.readBits(1)
	result.BottomFieldPicOrderInFramePresentFlag, result.BottomFieldPicOrderInFramePresentFlagSpan = bit != 0, mapped
	if !ok {
		return finishPPS(result, p), nil
	}
	result = finishPPS(result, p)
	if options.ParsePPS && result.Complete {
		if options.ParseFullHeaders {
			result.Deep = parsePPSFull(p, result.SequenceParameterSetID, options.ParameterSets)
		} else {
			result.Deep = parsePPSDetails(p)
		}
	}
	return result, nil
}

func finishPPS(result PictureParameterSet, p *rbspParser) PictureParameterSet {
	result.SyntaxBits = p.bitPosition
	result.RemainingRBSPBits = p.remainingBits()
	result.Complete = !p.fatal && !p.invalid && p.data.valid
	return result
}

func (p *rbspParser) skipScalingMatrices(chromaFormatIDC uint32) bool {
	count := 8
	if chromaFormatIDC == 3 {
		count = 12
	}
	for index := 0; index < count; index++ {
		present, _, ok := p.readBits(1)
		if !ok {
			return false
		}
		if present == 0 {
			continue
		}
		size := 16
		if index >= 6 {
			size = 64
		}
		lastScale, nextScale := int64(8), int64(8)
		for element := 0; element < size; element++ {
			if nextScale != 0 {
				delta, _, parsed := p.readSE()
				if !parsed {
					return false
				}
				nextScale = (lastScale + delta + 256) % 256
			}
			if nextScale != 0 {
				lastScale = nextScale
			}
		}
	}
	return true
}

func (p *rbspParser) semanticFatal(span MappedSpan, code, message string, expected, actual uint64) {
	p.addAt(span, Diagnostic{
		Code: code, Severity: SeverityError, Message: message,
		Expected: number(expected), Actual: number(actual),
	})
	p.fatal = true
	p.invalid = true
}

func deriveDimensions(result *SequenceParameterSet, p *rbspParser) {
	frameFactor := uint64(2)
	if result.FrameMbsOnlyFlag {
		frameFactor = 1
	}
	result.CodedWidth = (uint64(result.PicWidthInMbsMinus1) + 1) * 16
	result.CodedHeight = frameFactor * (uint64(result.PicHeightInMapUnitsMinus1) + 1) * 16
	result.ChromaArrayType = result.ChromaFormatIDC
	if result.SeparateColourPlaneFlag {
		result.ChromaArrayType = 0
	}
	switch result.ChromaArrayType {
	case 0:
		result.CropUnitX = 1
		result.CropUnitY = uint32(frameFactor)
	case 1:
		result.CropUnitX = 2
		result.CropUnitY = uint32(2 * frameFactor)
	case 2:
		result.CropUnitX = 2
		result.CropUnitY = uint32(frameFactor)
	case 3:
		result.CropUnitX = 1
		result.CropUnitY = uint32(frameFactor)
	}
	cropX := (uint64(result.FrameCropLeftOffset) + uint64(result.FrameCropRightOffset)) * uint64(result.CropUnitX)
	cropY := (uint64(result.FrameCropTopOffset) + uint64(result.FrameCropBottomOffset)) * uint64(result.CropUnitY)
	if cropX >= result.CodedWidth || cropY >= result.CodedHeight {
		p.addAt(result.FrameCroppingFlagSpan, Diagnostic{
			Code: CodeSPSCropInvalid, Severity: SeverityError,
			Message: "SPS crop offsets exceed coded dimensions",
		})
		return
	}
	result.DisplayWidth = result.CodedWidth - cropX
	result.DisplayHeight = result.CodedHeight - cropY
}

func highProfileSyntax(profile uint8) bool {
	switch profile {
	case 44, 83, 86, 100, 110, 118, 122, 128, 134, 135, 138, 139, 144, 244:
		return true
	default:
		return false
	}
}
