package report

import (
	"errors"
	"fmt"

	"github.com/sevico/muxray/internal/avc"
	"github.com/sevico/muxray/internal/avccontext"
	"github.com/sevico/muxray/internal/flv"
)

type AVCSEIMessageSequence struct {
	RBSP             AVCRBSPViewDetail `json:"rbsp"`
	Messages         []AVCSEIMessage   `json:"messages"`
	RBSPTrailingBits *AVCMappedSpan    `json:"rbspTrailingBits,omitempty"`
	BytesRead        uint32            `json:"bytesRead"`
	SyntaxBits       uint32            `json:"syntaxBits"`
	FramingComplete  bool              `json:"framingComplete"`
	Complete         bool              `json:"complete"`
}

type AVCSEIMessage struct {
	Index                uint32                      `json:"index"`
	PayloadType          *AVCMappedUnsignedField     `json:"payloadType,omitempty"`
	PayloadSize          *AVCMappedUnsignedField     `json:"payloadSize,omitempty"`
	PayloadSpan          *AVCMappedSpan              `json:"payloadSpan,omitempty"`
	Kind                 string                      `json:"kind"`
	SyntaxSupported      bool                        `json:"syntaxSupported"`
	RecoveryPoint        *AVCSEIRecoveryPoint        `json:"recoveryPoint,omitempty"`
	UserDataUnregistered *AVCSEIUserDataUnregistered `json:"userDataUnregistered,omitempty"`
	BufferingPeriod      *AVCSEIBufferingPeriod      `json:"bufferingPeriod,omitempty"`
	PictureTiming        *AVCSEIPictureTiming        `json:"pictureTiming,omitempty"`
	PayloadComplete      bool                        `json:"payloadComplete"`
	Complete             bool                        `json:"complete"`
}

type AVCSEIRecoveryPoint struct {
	RecoveryFrameCnt      *AVCMappedUnsignedField `json:"recoveryFrameCnt,omitempty"`
	ExactMatchFlag        *AVCMappedBooleanField  `json:"exactMatchFlag,omitempty"`
	BrokenLinkFlag        *AVCMappedBooleanField  `json:"brokenLinkFlag,omitempty"`
	ChangingSliceGroupIDC *AVCMappedUnsignedField `json:"changingSliceGroupIdc,omitempty"`
	PayloadExtensionBits  *AVCMappedSpan          `json:"payloadExtensionBits,omitempty"`
	PayloadAlignmentBits  *AVCMappedSpan          `json:"payloadAlignmentBits,omitempty"`
}

type AVCSEIHexBytes struct {
	Hex  string        `json:"hex"`
	Span AVCMappedSpan `json:"span"`
}

type AVCSEIUserDataUnregistered struct {
	UUID             *AVCSEIHexBytes `json:"uuid,omitempty"`
	DataPreview      *AVCSEIHexBytes `json:"dataPreview,omitempty"`
	DataBytes        uint32          `json:"dataBytes"`
	PreviewTruncated bool            `json:"previewTruncated"`
}

func mapSEIHex(p *avc.SEIHexBytes) *AVCSEIHexBytes {
	if p == nil {
		return nil
	}
	return &AVCSEIHexBytes{Hex: p.Hex, Span: mapAVCMappedSpan(p.Span)}
}

func mapSEI(p *avc.SEIMessageSequence) *AVCSEIMessageSequence {
	if p == nil {
		return nil
	}
	r := &AVCSEIMessageSequence{RBSP: mapAVCRBSP(p.RBSP), Messages: make([]AVCSEIMessage, 0, len(p.Messages)), BytesRead: p.BytesRead, SyntaxBits: p.SyntaxBits, FramingComplete: p.FramingComplete, Complete: p.Complete}
	if p.RBSPTrailingBits != nil {
		s := mapAVCMappedSpan(*p.RBSPTrailingBits)
		r.RBSPTrailingBits = &s
	}
	for _, m := range p.Messages {
		n := AVCSEIMessage{Index: m.Index, PayloadType: mapVUIUint(m.PayloadType), PayloadSize: mapVUIUint(m.PayloadSize), Kind: m.Kind, SyntaxSupported: m.SyntaxSupported, PayloadComplete: m.PayloadComplete, Complete: m.Complete}
		// v5 keeps timing payloads opaque even when a caller supplies a deeper
		// parse. The CLI uses matching parser depth to preserve diagnostics too.
		if m.Kind == "bufferingPeriod" || m.Kind == "pictureTiming" {
			n.Kind, n.SyntaxSupported = "unknown", false
		}
		if m.PayloadSpan != nil {
			s := mapAVCMappedSpan(*m.PayloadSpan)
			n.PayloadSpan = &s
		}
		if v := m.RecoveryPoint; v != nil {
			n.RecoveryPoint = &AVCSEIRecoveryPoint{RecoveryFrameCnt: mapVUIUint(v.RecoveryFrameCnt), ExactMatchFlag: mapVUIBool(v.ExactMatchFlag), BrokenLinkFlag: mapVUIBool(v.BrokenLinkFlag), ChangingSliceGroupIDC: mapVUIUint(v.ChangingSliceGroupIDC)}
			if v.PayloadExtensionBits != nil {
				s := mapAVCMappedSpan(*v.PayloadExtensionBits)
				n.RecoveryPoint.PayloadExtensionBits = &s
			}
			if v.PayloadAlignmentBits != nil {
				s := mapAVCMappedSpan(*v.PayloadAlignmentBits)
				n.RecoveryPoint.PayloadAlignmentBits = &s
			}
		}
		if v := m.UserDataUnregistered; v != nil {
			n.UserDataUnregistered = &AVCSEIUserDataUnregistered{UUID: mapSEIHex(v.UUID), DataPreview: mapSEIHex(v.DataPreview), DataBytes: v.DataBytes, PreviewTruncated: v.PreviewTruncated}
		}
		r.Messages = append(r.Messages, n)
	}
	return r
}

var ErrDetailV5Unsupported = errors.New("report: detail schema version 5 supports only AVC")

func NewTagDetailV5(size uint64, tag flv.TagRecord, parsed flv.ParsedTagDetail, parameters avccontext.ParameterResolution) (ParsedTagDetailDocument, error) {
	switch p := parsed.(type) {
	case flv.AVCVideo:
		return NewAVCTagDetailV5(size, tag, p, parameters), nil
	case *flv.AVCVideo:
		if p != nil {
			return NewAVCTagDetailV5(size, tag, *p, parameters), nil
		}
	}
	return nil, fmt.Errorf("%w: %T", ErrDetailV5Unsupported, parsed)
}

// NewAVCTagDetailV5 adds SEI only in this explicit projection. The v1-v4
// projections intentionally omit it even when the caller parsed SEI evidence.
func NewAVCTagDetailV5(size uint64, tag flv.TagRecord, p flv.AVCVideo, parameters avccontext.ParameterResolution) AVCTagDetailDocument {
	d := NewAVCTagDetailV4(size, tag, p, parameters)
	d.SchemaVersion = 5
	attach := func(mapped []AVCNALUnitDetail, sets []avc.ParameterSet) {
		for i, set := range sets {
			mapped[i].SEI = mapSEI(set.SEI)
		}
	}
	if c := d.Detail.AVCDecoderConfigurationRecord; c != nil {
		attach(c.SequenceParameterSets, p.Config.SequenceParameterSets)
		attach(c.PictureParameterSets, p.Config.PictureParameterSets)
		if c.HighProfileExtension != nil {
			attach(c.HighProfileExtension.SequenceParameterSetExts, p.Config.SequenceParameterSetExts)
		}
	}
	if seq := d.Detail.NALUnitSequence; seq != nil {
		for i, unit := range p.NALUnits.NALUnits {
			seq.NALUnits[i].SEI = mapSEI(unit.SEI)
		}
	}
	return d
}
