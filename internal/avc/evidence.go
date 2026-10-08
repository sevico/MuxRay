package avc

import "github.com/sevico/muxray/internal/cursor"

// NALTypeEvidence identifies one original five-bit NAL type field.
type NALTypeEvidence struct {
	NALIndex uint32
	Type     NALUnitType
	Span     cursor.Span
}

// SliceTypeEvidence retains the first observed slice_type of one normalized
// type. Values 5..9 normalize to P, B, I, SP, SI without making a picture claim.
type SliceTypeEvidence struct {
	NALIndex uint32
	Name     string
	Field    VUIUnsignedField
}

// KeyframeEvidence compares an FLV FrameType declaration with bounded NAL
// observations. It neither validates an access unit nor proves decodability.
// Coverage concerns headers and slice_type fields, not the full slice prefix
// or payload. SliceTypes contains at most five representatives in P,B,I,SP,SI
// order; all source spans refer to the existing parse result.
type KeyframeEvidence struct {
	DeclaredFrameType         uint8
	ObservedIDR               string // yes, no, unknown
	DeclarationRelation       string
	NALHeaderCoverageComplete bool
	SliceTypeCoverageComplete bool
	RetainedNALUnits          uint32
	InspectedNALHeaders       uint32
	UnclassifiedNALHeaders    uint32
	ObservedSliceHeaders      uint32
	FirstNAL, FirstIDR        *NALTypeEvidence
	SliceTypes                []SliceTypeEvidence
}

// CompareKeyframeEvidence does not read source data or change its input. A nil
// sequence means NAL boundaries are unavailable. A positive observation can
// survive incomplete coverage; a negative requires every NAL to have a usable
// header. Reserved/unspecified and extension types are conservatively unknown:
// their extension header IDR semantics are outside this parser's scope.
func CompareKeyframeEvidence(frameType uint8, sequence *NALUnitSequence) KeyframeEvidence {
	r := KeyframeEvidence{DeclaredFrameType: frameType, ObservedIDR: "unknown"}
	if sequence != nil {
		r.RetainedNALUnits = uint32(len(sequence.NALUnits))
		r.NALHeaderCoverageComplete = sequence.FramingComplete
		r.SliceTypeCoverageComplete = sequence.FramingComplete
		var types [5]*SliceTypeEvidence
		for _, n := range sequence.NALUnits {
			if n.HeaderPresent && n.Header.Complete {
				r.InspectedNALHeaders++
			}
			if !n.HeaderPresent || !n.Header.Complete || !evidenceHeaderUsable(n.Header) {
				r.UnclassifiedNALHeaders++
				r.NALHeaderCoverageComplete, r.SliceTypeCoverageComplete = false, false
				continue
			}
			h := NALTypeEvidence{NALIndex: n.Index, Type: n.Header.NALUnitType, Span: n.Header.NALUnitTypeSpan}
			if r.FirstNAL == nil {
				r.FirstNAL = &h
			}
			if h.Type == NALUnitTypeIDRSlice && r.FirstIDR == nil {
				r.FirstIDR = &h
			}
			switch h.Type {
			case NALUnitTypeNonIDRSlice, NALUnitTypeIDRSlice:
				p := n.SliceHeader
				if p == nil || p.SliceType == nil || p.SliceType.Value > 9 {
					r.SliceTypeCoverageComplete = false
					continue
				}
				r.ObservedSliceHeaders++
				index := p.SliceType.Value % 5
				if types[index] == nil {
					types[index] = &SliceTypeEvidence{NALIndex: n.Index,
						Name: [...]string{"P", "B", "I", "SP", "SI"}[index], Field: *p.SliceType}
				}
			case NALUnitTypeDataPartitionA, NALUnitTypeDataPartitionB, NALUnitTypeDataPartitionC:
				// Data partitions are not parsed as ordinary slice headers.
				r.SliceTypeCoverageComplete = false
			}
		}
		for _, item := range types {
			if item != nil {
				r.SliceTypes = append(r.SliceTypes, *item)
			}
		}
		if r.FirstIDR != nil {
			r.ObservedIDR = "yes"
		} else if r.NALHeaderCoverageComplete {
			r.ObservedIDR = "no"
		}
	}
	r.DeclarationRelation = "undetermined"
	if frameType != 1 && frameType != 2 {
		r.DeclarationRelation = "other_declaration"
	} else if r.ObservedIDR != "unknown" {
		if frameType == 1 {
			r.DeclarationRelation = "keyframe_"
		} else {
			r.DeclarationRelation = "inter_"
		}
		if r.ObservedIDR == "yes" {
			r.DeclarationRelation += "with_idr"
		} else {
			r.DeclarationRelation += "without_idr"
		}
	}
	return r
}

func evidenceHeaderUsable(h NALHeader) bool {
	if h.ForbiddenZeroBit || h.NALUnitType < 1 || h.NALUnitType > 13 {
		return false
	}
	switch h.NALUnitType {
	case NALUnitTypeIDRSlice, NALUnitTypeSequenceParameterSet, NALUnitTypePictureParameterSet:
		return h.NALRefIDC != 0
	case NALUnitTypeSEI, NALUnitTypeAccessUnitDelimiter, NALUnitTypeEndOfSequence, NALUnitTypeEndOfStream, NALUnitTypeFillerData:
		return h.NALRefIDC == 0
	default:
		return true
	}
}
