package avc

import (
	"fmt"
	"io"
	"math"

	"github.com/sevico/muxray/internal/cursor"
)

func parseSliceHeader(source io.ReaderAt, span cursor.Span, header NALHeader, context *ParameterSetContext, budget *parseBudget, diagnostics *diagnosticCollector) (*SliceHeader, error) {
	start, end, err := span.ByteRange()
	if err != nil {
		return nil, err
	}
	s := &SliceHeader{}
	p := newRBSPParser(rbspData{valid: true, end: end}, diagnostics)
	loader := slicePrefixLoader{source: source, position: start + 1, start: start + 1, end: end, budget: budget, p: p}
	p.ensure = loader.ensure
	parseSlicePrefix(p, s, header, context)
	s.SyntaxBits, s.BytesRead = p.bitPosition, uint32(loader.position-loader.start)
	p.data.view.ByteLength = uint32(len(p.data.bytes))
	p.data.view.EBSPSpan = cursor.Span{ByteOffset: loader.start, BitLength: s.BytesRead * 8}
	p.data.view.SourceSegments = sourceSegments(p.data.sourceOffsets)
	s.RBSP = p.data.view
	s.PrefixComplete = !p.fatal && !p.invalid && !header.ForbiddenZeroBit
	return s, loader.err
}

func parseSlicePrefix(p *rbspParser, s *SliceHeader, header NALHeader, context *ParameterSetContext) {
	s.FirstMBInSlice = p.vuiUE()
	s.SliceType = p.vuiUE()
	if s.SliceType != nil {
		if s.SliceType.Value > 9 {
			p.semanticFatal(s.SliceType.Span, CodeSliceValueInvalid, "slice_type exceeds 9", 9, uint64(s.SliceType.Value))
			return
		}
		s.SliceTypeName = [...]string{"P", "B", "I", "SP", "SI"}[s.SliceType.Value%5]
	}
	s.PictureParameterSetID = p.vuiUE()
	if s.PictureParameterSetID == nil {
		return
	}
	if s.PictureParameterSetID.Value > 255 {
		p.semanticFatal(s.PictureParameterSetID.Span, CodeSliceValueInvalid, "pic_parameter_set_id exceeds 255", 255, uint64(s.PictureParameterSetID.Value))
		return
	}
	pps := context.pps[s.PictureParameterSetID.Value]
	if pps == nil || context.sps[pps.SequenceParameterSetID] == nil {
		p.addAt(s.PictureParameterSetID.Span, Diagnostic{Code: CodeSliceParameterSetMissing, Severity: SeverityError, Message: "slice prefix needs a valid preceding PPS and its referenced SPS"})
		p.fatal = true
		return
	}
	sps := context.sps[pps.SequenceParameterSetID]
	s.ReferencedSPSID = new(uint32)
	*s.ReferencedSPSID = sps.SequenceParameterSetID
	s.ReferencedSPSSpan = parameterNALSpan(sps.RBSP)
	s.ReferencedPPSSpan = parameterNALSpan(pps.RBSP)
	if pps.Deep.PicInitQPMinus26.Value < -26-6*int64(sps.BitDepthLumaMinus8) {
		p.addAt(s.PictureParameterSetID.Span, Diagnostic{Code: CodeSliceValueInvalid, Severity: SeverityError, Message: "referenced PPS pic_init_qp_minus26 is invalid for the SPS bit depth"})
		p.fatal = true
		return
	}
	if sps.SeparateColourPlaneFlag {
		s.ColourPlaneID = p.vuiBits(2)
		if s.ColourPlaneID != nil && s.ColourPlaneID.Value > 2 {
			p.vuiInvalid(s.ColourPlaneID, CodeSliceValueInvalid, "colour_plane_id exceeds 2", 2)
		}
	}
	s.FrameNum = p.vuiBits(uint8(sps.Log2MaxFrameNumMinus4 + 4))
	fieldPic := false
	if !sps.FrameMbsOnlyFlag {
		s.FieldPicFlag = p.vuiFlag()
		fieldPic = vuiTrue(s.FieldPicFlag)
		if fieldPic {
			s.BottomFieldFlag = p.vuiFlag()
		}
	}
	if header.NALUnitType == NALUnitTypeIDRSlice {
		s.IDRPicID = p.vuiUE()
		if s.IDRPicID != nil && s.IDRPicID.Value > 65535 {
			p.vuiInvalid(s.IDRPicID, CodeSliceValueInvalid, "idr_pic_id exceeds 65535", 65535)
		}
		if s.FrameNum != nil && s.FrameNum.Value != 0 {
			p.vuiInvalid(s.FrameNum, CodeSliceValueInvalid, "IDR frame_num must be zero", 0)
		}
		if s.SliceType != nil && s.SliceType.Value%5 != 2 && s.SliceType.Value%5 != 4 {
			p.vuiInvalid(s.SliceType, CodeSliceValueInvalid, "IDR slice_type must be I or SI", 2)
		}
	}
	switch sps.PicOrderCntType {
	case 0:
		s.PicOrderCntLSB = p.vuiBits(uint8(sps.Log2MaxPicOrderCntLSBMinus4 + 4))
		if pps.BottomFieldPicOrderInFramePresentFlag && !fieldPic {
			s.DeltaPicOrderCntBottom = p.signedField()
		}
	case 1:
		if !sps.DeltaPicOrderAlwaysZeroFlag {
			s.DeltaPicOrderCnt0 = p.signedField()
			if pps.BottomFieldPicOrderInFramePresentFlag && !fieldPic {
				s.DeltaPicOrderCnt1 = p.signedField()
			}
		}
	}
	for _, f := range []*SignedField{s.DeltaPicOrderCntBottom, s.DeltaPicOrderCnt0, s.DeltaPicOrderCnt1} {
		if f != nil && (f.Value < math.MinInt32+1 || f.Value > math.MaxInt32) {
			p.addAt(f.Span, Diagnostic{Code: CodeSliceValueInvalid, Severity: SeverityError, Message: "slice delta picture order count exceeds signed 32-bit syntax range"})
		}
	}
	if pps.Deep.RedundantPicCntPresentFlag.Value {
		s.RedundantPicCnt = p.vuiUE()
		if s.RedundantPicCnt != nil && s.RedundantPicCnt.Value > 127 {
			p.vuiInvalid(s.RedundantPicCnt, CodeSliceValueInvalid, "redundant_pic_cnt exceeds 127", 127)
		}
	}
	if s.FirstMBInSlice != nil && !p.fatal {
		mbWidth := uint64(sps.PicWidthInMbsMinus1) + 1
		mbHeight := uint64(sps.PicHeightInMapUnitsMinus1) + 1
		if !sps.FrameMbsOnlyFlag && !fieldPic {
			mbHeight *= 2
		}
		firstMB := uint64(s.FirstMBInSlice.Value)
		if sps.MBAdaptiveFrameFieldFlag && !fieldPic {
			firstMB *= 2
		}
		if firstMB/mbWidth >= mbHeight {
			p.addAt(s.FirstMBInSlice.Span, Diagnostic{Code: CodeSliceValueInvalid, Severity: SeverityError, Message: "first_mb_in_slice exceeds the referenced picture dimensions"})
		}
	}
}

func parameterNALSpan(view RBSPView) *cursor.Span {
	if view.EBSPSpan.ByteOffset == 0 {
		return nil
	}
	span := cursor.Span{ByteOffset: view.EBSPSpan.ByteOffset - 1, BitLength: view.EBSPSpan.BitLength + 8}
	return &span
}

// slicePrefixLoader de-escapes a byte only when the next requested field
// needs it. Invalid escaping in opaque slice data cannot invalidate a prefix.
type slicePrefixLoader struct {
	source               io.ReaderAt
	position, start, end uint64
	zeroCount            int
	budget               *parseBudget
	p                    *rbspParser
	err                  error
}

func (l *slicePrefixLoader) ensure(width uint32) bool {
	for l.p.remainingBits() < width {
		value, offset, ok := l.readByte()
		if !ok {
			return false
		}
		if l.zeroCount >= 2 && value == 3 {
			escapeOffset := offset
			value, offset, ok = l.readByte()
			if !ok {
				return false
			}
			if value > 3 {
				return l.fail(CodeEmulationPreventionInvalid, "slice prefix has an invalid emulation-prevention byte", escapeOffset)
			}
			if !l.budget.takeMappingEntry() {
				return l.fail(CodeRBSPMappingLimitReached, "slice prefix exceeds the cumulative RBSP mapping budget", escapeOffset)
			}
			l.p.data.view.EmulationPreventionBytes = append(l.p.data.view.EmulationPreventionBytes, cursor.Span{ByteOffset: escapeOffset, BitLength: 8})
			l.zeroCount = 0
		} else if l.zeroCount >= 2 && value <= 2 {
			return l.fail(CodeEmulationPreventionInvalid, "slice prefix contains an unescaped start-code sequence", offset)
		}
		if len(l.p.data.sourceOffsets) == 0 || offset != l.p.data.sourceOffsets[len(l.p.data.sourceOffsets)-1]+1 {
			if !l.budget.takeMappingEntry() {
				return l.fail(CodeRBSPMappingLimitReached, "slice prefix exceeds the cumulative RBSP mapping budget", offset)
			}
		}
		l.p.data.bytes = append(l.p.data.bytes, value)
		l.p.data.sourceOffsets = append(l.p.data.sourceOffsets, offset)
		if value == 0 {
			l.zeroCount++
		} else {
			l.zeroCount = 0
		}
	}
	return true
}

func (l *slicePrefixLoader) readByte() (byte, uint64, bool) {
	offset := l.position
	if offset >= l.end {
		l.fail(CodeRBSPSyntaxTruncated, "slice header prefix is truncated", offset)
		return 0, offset, false
	}
	if offset-l.start >= uint64(MaxSliceHeaderBytes) || l.budget.sliceBytesRead >= MaxSliceHeaderTotalBytes {
		l.fail(CodeSliceHeaderBudgetReached, "slice header prefix exceeds the byte-read budget", offset)
		return 0, offset, false
	}
	var one [1]byte
	if offset > math.MaxInt64 {
		l.err = fmt.Errorf("avc: slice prefix offset %d exceeds int64", offset)
		l.p.fatal = true
		return 0, offset, false
	}
	// ReaderAt implementations may return all requested bytes with a non-EOF
	// error (including cancellation); it must not disappear behind a full read.
	n, err := l.source.ReadAt(one[:], int64(offset))
	if n > 0 {
		l.position++
		l.budget.sliceBytesRead++
	}
	if err != nil && err != io.EOF || n != 1 {
		if err == nil {
			err = io.ErrUnexpectedEOF
		}
		l.err = fmt.Errorf("avc: read slice prefix at byte %d: %w", offset, err)
		l.p.fatal = true
		return 0, offset, false
	}
	return one[0], offset, true
}

func (l *slicePrefixLoader) fail(code, message string, offset uint64) bool {
	l.p.diagnostics.add(Diagnostic{Code: code, Severity: SeverityError, Offset: offset, Message: message, RecoveryOffset: number(l.end)})
	l.p.fatal = true
	return false
}
