package detailview

import (
	"fmt"
	"strconv"

	"github.com/sevico/muxray/internal/avc"
)

func (b *builder) avcSEI(parent, name string, p *avc.SEIMessageSequence) {
	id := b.add(parent, "SEI messages "+name, "bounded SEI sequence", "Message boundaries and supported payload syntax; recovery and picture timing are not decoder state", nil)
	b.add(id, "seiFramingComplete", "derived boolean", strconv.FormatBool(p.FramingComplete), nil)
	b.add(id, "seiComplete", "derived boolean", strconv.FormatBool(p.Complete), nil)
	b.add(id, "seiBytesRead", "derived count", decimal(uint64(p.BytesRead)), nil)
	b.add(id, "seiSyntaxBits", "derived count", decimal(uint64(p.SyntaxBits)), nil)
	b.add(id, "seiMessageCount", "derived count", decimal(uint64(len(p.Messages))), nil)
	for _, m := range p.Messages {
		group := b.add(id, fmt.Sprintf("SEI message[%d]", m.Index), "SEI message", m.Kind, nil)
		if group == "" {
			break
		}
		b.add(group, "syntaxSupported", "derived boolean", strconv.FormatBool(m.SyntaxSupported), nil)
		b.add(group, "payloadComplete", "derived boolean", strconv.FormatBool(m.PayloadComplete), nil)
		b.add(group, "messageComplete", "derived boolean", strconv.FormatBool(m.Complete), nil)
		b.seiUint(group, "payloadType", m.PayloadType)
		b.seiUint(group, "payloadSize", m.PayloadSize)
		if m.PayloadSpan != nil {
			b.seiMapped(group, "seiPayload", "RBSP payload", "Inspected payload bytes; unknown types remain opaque", *m.PayloadSpan)
		}
		if r := m.RecoveryPoint; r != nil {
			b.seiUint(group, "recoveryFrameCnt", r.RecoveryFrameCnt)
			b.seiBool(group, "exactMatchFlag", r.ExactMatchFlag)
			b.seiBool(group, "brokenLinkFlag", r.BrokenLinkFlag)
			b.seiUint(group, "changingSliceGroupIdc", r.ChangingSliceGroupIDC)
			if r.PayloadExtensionBits != nil {
				b.seiMapped(group, "payloadExtensionBits", "RBSP reserved bits", "Reserved extension data", *r.PayloadExtensionBits)
			}
			if r.PayloadAlignmentBits != nil {
				b.seiMapped(group, "payloadAlignmentBits", "RBSP alignment", "Validated payload alignment bits", *r.PayloadAlignmentBits)
			}
		}
		if u := m.UserDataUnregistered; u != nil {
			if u.UUID != nil {
				b.seiMapped(group, "uuid", "RBSP hex bytes", u.UUID.Hex, u.UUID.Span)
			}
			if u.DataPreview != nil {
				b.seiMapped(group, "userDataPreview", "RBSP hex bytes", u.DataPreview.Hex, u.DataPreview.Span)
			}
			b.add(group, "userDataBytes", "derived count", decimal(uint64(u.DataBytes)), nil)
			b.add(group, "userDataPreviewTruncated", "derived boolean", strconv.FormatBool(u.PreviewTruncated), nil)
			if u.PreviewTruncated {
				b.detail.Truncated = true
			}
		}
	}
	if p.RBSPTrailingBits != nil {
		b.seiMapped(id, "seiTrailingBits", "RBSP trailing bits", "Validated trailing bits", *p.RBSPTrailingBits)
	}
}

// Discontiguous RBSP fields keep individual original spans. In particular,
// source selections never silently include emulation-prevention bytes.
func (b *builder) seiMapped(parent, name, kind, value string, s avc.MappedSpan) {
	id := b.add(parent, name, kind, value, mappedPrefixSpan(s))
	if id == "" || len(s.SourceSpans) <= 1 {
		return
	}
	for i, part := range s.SourceSpans {
		if b.add(id, fmt.Sprintf("%s.source[%d]", name, i), "RBSP source segment", "Original bytes excluding emulation-prevention bytes", span(part)) == "" {
			break
		}
	}
}

func (b *builder) seiUint(parent, name string, field *avc.VUIUnsignedField) {
	if field != nil {
		b.seiMapped(parent, name, "RBSP unsigned", decimal(uint64(field.Value)), field.Span)
	}
}

func (b *builder) seiBool(parent, name string, field *avc.VUIBooleanField) {
	if field != nil {
		b.seiMapped(parent, name, "RBSP boolean", strconv.FormatBool(field.Value), field.Span)
	}
}
