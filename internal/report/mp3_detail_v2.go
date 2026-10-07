package report

import (
	"github.com/sevico/muxray/internal/flv"
	"github.com/sevico/muxray/internal/mp3"
)

const MP3TagDetailSchemaVersionV2 = 2

// MP3TagDetailDocumentV2 adds opt-in same-tag inspection. The v1 projection
// stays frozen; neither version decodes compressed audio.
type MP3TagDetailDocumentV2 struct {
	SchemaVersion int                   `json:"schemaVersion"`
	DocumentType  string                `json:"documentType"`
	Source        Source                `json:"source"`
	Tag           Tag                   `json:"tag"`
	Detail        MP3AudioDetailV2      `json:"detail"`
	Diagnostics   []TagDetailDiagnostic `json:"diagnostics"`
}

func (MP3TagDetailDocumentV2) parsedTagDetailDocument() {}

type MP3AudioDetailV2 struct {
	MP3AudioDetail
	Frames *MP3FrameSequenceDetail `json:"frames,omitempty"`
}
type MP3FrameSequenceDetail struct {
	Span            Span             `json:"span"`
	MaxFrames       uint32           `json:"maxFrames"`
	MaxBytes        uint64           `json:"maxBytes"`
	BytesCovered    uint64           `json:"bytesCovered"`
	BytesRead       uint64           `json:"bytesRead"`
	CompleteFrames  uint32           `json:"completeFrames"`
	UninspectedSpan Span             `json:"uninspectedSpan"`
	StopReason      string           `json:"stopReason"`
	Complete        bool             `json:"complete"`
	Items           []MP3FrameDetail `json:"items"`
	VBR             *MP3VBRDetail    `json:"vbr,omitempty"`
}
type MP3FrameDetail struct {
	Index            uint32               `json:"index"`
	Span             Span                 `json:"span"`
	Header           MP3FrameHeaderDetail `json:"header"`
	BoundaryComplete bool                 `json:"boundaryComplete"`
	CRC              MP3CRCDetail         `json:"crc"`
}
type MP3CRCDetail struct {
	Status   string  `json:"status"`
	Span     *Span   `json:"span,omitempty"`
	Stored   *uint16 `json:"stored,omitempty"`
	Computed *uint16 `json:"computed,omitempty"`
}
type MP3VBRDetail struct {
	Kind           string  `json:"kind"`
	Span           Span    `json:"span"`
	Complete       bool    `json:"complete"`
	Flags          *uint32 `json:"flags,omitempty"`
	Version        *uint16 `json:"version,omitempty"`
	Delay          *uint16 `json:"delay,omitempty"`
	Quality        *uint32 `json:"quality,omitempty"`
	DeclaredFrames *uint32 `json:"declaredFrames,omitempty"`
	DeclaredBytes  *uint32 `json:"declaredBytes,omitempty"`
	TOCSpan        *Span   `json:"tocSpan,omitempty"`
}

func NewMP3TagDetailV2(size uint64, tag flv.TagRecord, parsed flv.MP3Audio) MP3TagDetailDocumentV2 {
	v1 := NewMP3TagDetail(size, tag, parsed)
	out := MP3TagDetailDocumentV2{SchemaVersion: 2, DocumentType: v1.DocumentType, Source: v1.Source, Tag: v1.Tag, Detail: MP3AudioDetailV2{MP3AudioDetail: v1.Detail}, Diagnostics: v1.Diagnostics}
	if parsed.Frames == nil {
		return out
	}
	p := parsed.Frames
	out.Detail.UnparsedDataSpan = nil // Inspected headers are interleaved with opaque audio bodies.
	f := &MP3FrameSequenceDetail{Span: mapSpan(p.Span), MaxFrames: p.MaxFrames, MaxBytes: p.MaxBytes, BytesCovered: p.BytesCovered, BytesRead: p.BytesRead, CompleteFrames: p.CompleteFrames, UninspectedSpan: mapSpan(p.UninspectedSpan), StopReason: p.StopReason, Complete: p.Complete, Items: make([]MP3FrameDetail, 0, len(p.Frames))}
	for _, item := range p.Frames {
		crc := MP3CRCDetail{Status: item.CRC.Status}
		if !item.CRC.Span.Empty() {
			s := mapSpan(item.CRC.Span)
			crc.Span = &s
		}
		if item.CRC.ValuesPresent {
			stored, computed := item.CRC.Stored, item.CRC.Computed
			crc.Stored = &stored
			crc.Computed = &computed
		}
		f.Items = append(f.Items, MP3FrameDetail{Index: item.Index, Span: mapSpan(item.Span), Header: mapMP3FrameHeader(item.Header), BoundaryComplete: item.BoundaryComplete, CRC: crc})
	}
	f.VBR = mapMP3VBR(p.VBR)
	out.Detail.Frames = f
	return out
}
func mapMP3VBR(p *mp3.VBRHeader) *MP3VBRDetail {
	if p == nil {
		return nil
	}
	v := &MP3VBRDetail{Kind: p.Kind, Span: mapSpan(p.Span), Complete: p.Complete, Flags: p.Flags, Version: p.Version, Delay: p.Delay, Quality: p.Quality, DeclaredFrames: p.DeclaredFrames, DeclaredBytes: p.DeclaredBytes}
	if p.TOCSpan != nil {
		s := mapSpan(*p.TOCSpan)
		v.TOCSpan = &s
	}
	return v
}

var _ ParsedTagDetailDocument = MP3TagDetailDocumentV2{}
