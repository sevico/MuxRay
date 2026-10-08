package detailview

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/sevico/muxray/internal/avc"
	"github.com/sevico/muxray/internal/cursor"
)

func (b *builder) avcEvidence(parent string, frameType uint8, frameTypeSpan cursor.Span, sequence *avc.NALUnitSequence) {
	r := avc.CompareKeyframeEvidence(frameType, sequence)
	id := b.add(parent, "Keyframe evidence", "bounded NAL evidence", r.DeclarationRelation, nil)
	b.encoded(id, "declaredFrameType", "uint4", decimal(uint64(frameType)), frameTypeSpan)
	b.add(id, "observedIDR", "derived enum", r.ObservedIDR, nil)
	b.add(id, "declarationRelation", "derived enum", r.DeclarationRelation, nil)
	b.add(id, "nalHeaderCoverageComplete", "derived boolean", strconv.FormatBool(r.NALHeaderCoverageComplete), nil)
	b.add(id, "sliceTypeCoverageComplete", "derived boolean", strconv.FormatBool(r.SliceTypeCoverageComplete), nil)
	names := make([]string, 0, len(r.SliceTypes))
	for _, s := range r.SliceTypes {
		names = append(names, s.Name)
	}
	b.add(id, "observedSliceTypes", "derived set", strings.Join(names, ","), nil)
	for _, f := range []struct {
		name  string
		value uint32
	}{
		{"retainedNALUnits", r.RetainedNALUnits}, {"inspectedNALHeaders", r.InspectedNALHeaders},
		{"unclassifiedNALHeaders", r.UnclassifiedNALHeaders}, {"observedSliceHeaders", r.ObservedSliceHeaders},
	} {
		b.add(id, f.name, "derived count", decimal(uint64(f.value)), nil)
	}
	for _, f := range []struct {
		name     string
		evidence *avc.NALTypeEvidence
	}{{"evidenceNALType", r.FirstNAL}, {"evidenceIDRNALType", r.FirstIDR}} {
		if n := f.evidence; n != nil {
			b.encoded(id, f.name, "uint5", fmt.Sprintf("%d (%s; NAL[%d])", n.Type, n.Type, n.NALIndex), n.Span)
		}
	}
	for _, s := range r.SliceTypes {
		b.add(id, "evidenceSliceType"+s.Name, "RBSP unsigned",
			fmt.Sprintf("%d (%s; NAL[%d])", s.Field.Value, s.Name, s.NALIndex), mappedPrefixSpan(s.Field.Span))
	}
	b.add(id, "evidenceScope", "scope", "Selected Tag only; IDR means observed NAL type 5. Slice types are a set, not a picture type; I slices do not imply IDR. Extension headers, complete pictures and independent decoding are not verified.", nil)
}
