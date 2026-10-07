package avc

func parsePPSDetails(p *rbspParser) *PPSDetails {
	d := &PPSDetails{RBSPBitOffset: p.bitPosition}
	defer func() {
		d.SyntaxBits = p.bitPosition - d.RBSPBitOffset
		d.RemainingRBSPBits = p.remainingBits()
		d.PrefixComplete = !p.fatal && !p.invalid && d.RedundantPicCntPresentFlag != nil
	}()
	d.NumSliceGroupsMinus1 = p.vuiUE()
	if d.NumSliceGroupsMinus1 == nil {
		return d
	}
	if d.NumSliceGroupsMinus1.Value > 0 {
		code := CodePPSSliceGroupsUnsupported
		message := "PPS slice groups are outside this bounded prefix analysis"
		if d.NumSliceGroupsMinus1.Value > 7 {
			code, message = CodePPSValueInvalid, "num_slice_groups_minus1 exceeds 7"
		}
		p.semanticFatal(d.NumSliceGroupsMinus1.Span, code, message, 0, uint64(d.NumSliceGroupsMinus1.Value))
		return d
	}
	d.NumRefIdxL0DefaultActiveMinus1 = p.vuiUE()
	d.NumRefIdxL1DefaultActiveMinus1 = p.vuiUE()
	for _, f := range []*VUIUnsignedField{d.NumRefIdxL0DefaultActiveMinus1, d.NumRefIdxL1DefaultActiveMinus1} {
		if f != nil && f.Value > 31 {
			p.vuiInvalid(f, CodePPSValueInvalid, "default reference index exceeds 31", 31)
		}
	}
	d.WeightedPredFlag = p.vuiFlag()
	d.WeightedBipredIDC = p.vuiBits(2)
	if d.WeightedBipredIDC != nil && d.WeightedBipredIDC.Value > 2 {
		p.vuiInvalid(d.WeightedBipredIDC, CodePPSValueInvalid, "weighted_bipred_idc exceeds 2", 2)
	}
	d.PicInitQPMinus26 = p.signedField()
	d.PicInitQSMinus26 = p.signedField()
	d.ChromaQPIndexOffset = p.signedField()
	// The QP lower bound is checked against the largest supported bit depth.
	// Its narrower SPS-dependent constraint does not change prefix layout.
	p.checkSigned(d.PicInitQPMinus26, -62, 25, "pic_init_qp_minus26")
	p.checkSigned(d.PicInitQSMinus26, -26, 25, "pic_init_qs_minus26")
	p.checkSigned(d.ChromaQPIndexOffset, -12, 12, "chroma_qp_index_offset")
	d.DeblockingFilterControlPresentFlag = p.vuiFlag()
	d.ConstrainedIntraPredFlag = p.vuiFlag()
	d.RedundantPicCntPresentFlag = p.vuiFlag()
	return d
}

func (p *rbspParser) signedField() *SignedField {
	value, span, ok := p.readSE()
	if !ok {
		return nil
	}
	return &SignedField{Value: value, Span: span}
}

func (p *rbspParser) checkSigned(field *SignedField, low, high int64, name string) {
	if field != nil && (field.Value < low || field.Value > high) {
		p.addAt(field.Span, Diagnostic{Code: CodePPSValueInvalid, Severity: SeverityError, Message: name + " is outside its supported syntax range"})
	}
}
