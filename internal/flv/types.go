// Package flv provides bounded, streaming-oriented FLV container scanning.
package flv

import (
	"fmt"

	"github.com/sevico/muxray/internal/cursor"
)

const (
	HeaderSize    uint64 = 9
	TagHeaderSize uint64 = 11
)

// TagType is the five-bit FLV tag type stored in a tag header.
type TagType uint8

const (
	TagTypeAudio  TagType = 8
	TagTypeVideo  TagType = 9
	TagTypeScript TagType = 18
)

func (t TagType) String() string {
	switch t {
	case TagTypeAudio:
		return "audio"
	case TagTypeVideo:
		return "video"
	case TagTypeScript:
		return "script"
	default:
		return "unknown"
	}
}

// Known reports whether the tag type is defined by the base FLV format.
func (t TagType) Known() bool {
	return t == TagTypeAudio || t == TagTypeVideo || t == TagTypeScript
}

// Header is the fixed FLV header plus its declared data offset.
type Header struct {
	Span          cursor.Span
	Version       uint8
	TypeFlags     uint8
	HasAudio      bool
	HasVideo      bool
	DataOffset    uint32
	ExtraDataSize uint64
}

type tagRecordFlags uint16

const (
	tagFlagPayloadComplete tagRecordFlags = 1 << iota
	tagFlagPreviousTagSizePresent
	tagFlagPreviousTagSizeValid
)

// TagRecord is a compact container-level index entry. Source spans and derived
// values are exposed by methods so every consumer uses the same calculation
// without storing several redundant Span values per tag.
type TagRecord struct {
	ID                uint64
	Offset            uint64
	DTS               int64
	DataSize          uint32
	AvailableDataSize uint32
	StreamID          uint32
	PreviousTagSize   uint32
	flags             tagRecordFlags
	RawTypeFlags      uint8
}

func (r TagRecord) Type() TagType {
	return TagType(r.RawTypeFlags & 0x1f)
}

func (r TagRecord) Filter() bool {
	return r.RawTypeFlags&0x20 != 0
}

func (r TagRecord) PayloadComplete() bool {
	return r.flags&tagFlagPayloadComplete != 0
}

func (r TagRecord) PreviousTagSizePresent() bool {
	return r.flags&tagFlagPreviousTagSizePresent != 0
}

func (r TagRecord) PreviousTagSizeValid() bool {
	return r.flags&tagFlagPreviousTagSizeValid != 0
}

func (r TagRecord) ExpectedPreviousTagSize() uint32 {
	return uint32(TagHeaderSize) + r.DataSize
}

func (r TagRecord) Span() cursor.Span {
	return cursor.Span{
		ByteOffset: r.Offset,
		BitLength:  uint32((TagHeaderSize + uint64(r.AvailableDataSize)) * 8),
	}
}

func (r TagRecord) HeaderSpan() cursor.Span {
	return cursor.Span{ByteOffset: r.Offset, BitLength: uint32(TagHeaderSize * 8)}
}

func (r TagRecord) PayloadSpan() cursor.Span {
	return cursor.Span{
		ByteOffset: r.Offset + TagHeaderSize,
		BitLength:  r.AvailableDataSize * 8,
	}
}

func (r TagRecord) PreviousTagSizeSpan() (cursor.Span, bool) {
	if !r.PreviousTagSizePresent() {
		return cursor.Span{}, false
	}
	return cursor.Span{
		ByteOffset: r.Offset + TagHeaderSize + uint64(r.DataSize),
		BitLength:  32,
	}, true
}

// ScanResult contains the deterministic result of one bounded FLV scan.
type ScanResult struct {
	SourceSize       uint64
	Header           Header
	HeaderPresent    bool
	PreviousTagSize0 uint32
	Tags             []TagRecord // Legacy Scan only; new consumers should use Index().
	Diagnostics      []Diagnostic
	BytesScanned     uint64
	Complete         bool
	index            *chunkedTagIndex
}

// HasErrors reports whether the scan is incomplete or produced an error diagnostic.
func (r ScanResult) HasErrors() bool {
	if !r.Complete {
		return true
	}
	for _, diagnostic := range r.Diagnostics {
		if diagnostic.Severity == SeverityError {
			return true
		}
	}
	return false
}

// Options sets hard resource limits for a scan. Zero values select defaults.
type Options struct {
	MaxTags        uint64
	MaxDiagnostics uint32
}

const (
	DefaultMaxTags        uint64 = 1_000_000
	HardMaxTags           uint64 = 1_000_000
	DefaultMaxDiagnostics uint32 = 1_024
	MinDiagnostics        uint32 = 2
	HardMaxDiagnostics    uint32 = 65_536
)

func (o Options) normalized() (Options, error) {
	if o.MaxTags == 0 {
		o.MaxTags = DefaultMaxTags
	} else if o.MaxTags > HardMaxTags {
		return Options{}, fmt.Errorf("flv: max tags %d exceeds hard limit %d", o.MaxTags, HardMaxTags)
	}
	if o.MaxDiagnostics == 0 {
		o.MaxDiagnostics = DefaultMaxDiagnostics
	} else if o.MaxDiagnostics < MinDiagnostics {
		return Options{}, fmt.Errorf("flv: max diagnostics %d is below minimum %d", o.MaxDiagnostics, MinDiagnostics)
	} else if o.MaxDiagnostics > HardMaxDiagnostics {
		return Options{}, fmt.Errorf("flv: max diagnostics %d exceeds hard limit %d", o.MaxDiagnostics, HardMaxDiagnostics)
	}
	return o, nil
}
