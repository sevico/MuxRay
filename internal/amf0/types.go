// Package amf0 provides bounded, source-span-preserving AMF0 parsing.
package amf0

import (
	"fmt"

	"github.com/sevico/muxray/internal/cursor"
)

// Type is an AMF0 type marker.
type Type uint8

const (
	TypeNumber      Type = 0x00
	TypeBoolean     Type = 0x01
	TypeString      Type = 0x02
	TypeObject      Type = 0x03
	TypeMovieClip   Type = 0x04
	TypeNull        Type = 0x05
	TypeUndefined   Type = 0x06
	TypeReference   Type = 0x07
	TypeECMAArray   Type = 0x08
	TypeObjectEnd   Type = 0x09
	TypeStrictArray Type = 0x0a
	TypeDate        Type = 0x0b
	TypeLongString  Type = 0x0c
	TypeUnsupported Type = 0x0d
	TypeRecordSet   Type = 0x0e
	TypeXMLDocument Type = 0x0f
	TypeTypedObject Type = 0x10
	TypeAVMPlus     Type = 0x11
)

func (t Type) String() string {
	switch t {
	case TypeNumber:
		return "number"
	case TypeBoolean:
		return "boolean"
	case TypeString:
		return "string"
	case TypeObject:
		return "object"
	case TypeMovieClip:
		return "movieClip"
	case TypeNull:
		return "null"
	case TypeUndefined:
		return "undefined"
	case TypeReference:
		return "reference"
	case TypeECMAArray:
		return "ecmaArray"
	case TypeObjectEnd:
		return "objectEnd"
	case TypeStrictArray:
		return "strictArray"
	case TypeDate:
		return "date"
	case TypeLongString:
		return "longString"
	case TypeUnsupported:
		return "unsupported"
	case TypeRecordSet:
		return "recordSet"
	case TypeXMLDocument:
		return "xmlDocument"
	case TypeTypedObject:
		return "typedObject"
	case TypeAVMPlus:
		return "avmPlus"
	default:
		return "unknown"
	}
}

// NodeID is a stable index into Result.Nodes.
type NodeID uint32

// NoNode denotes the absence of a node relationship.
const NoNode NodeID = ^NodeID(0)

// NoProperty denotes a node that is not the value of a named property.
const NoProperty uint32 = ^uint32(0)

// Node is one AMF0 value in a flat, source-ordered arena. Child and sibling
// relationships use integer IDs so callers never need a recursive pointer
// graph.
type Node struct {
	ID          NodeID
	ParentID    NodeID
	FirstChild  NodeID
	NextSibling NodeID
	ChildCount  uint32
	PropertyID  uint32

	Type       Type
	Complete   bool
	Span       cursor.Span
	MarkerSpan cursor.Span

	HasTerminator  bool
	TerminatorSpan cursor.Span

	NumberBits uint64
	NumberSpan cursor.Span

	BooleanRaw   uint8
	BooleanValue bool
	BooleanSpan  cursor.Span

	String           string
	StringLength     uint32
	StringOmitted    bool
	StringLengthSpan cursor.Span
	StringSpan       cursor.Span

	Reference        uint16
	ReferenceSpan    cursor.Span
	ReferenceValid   bool
	ReferencedNodeID NodeID

	DeclaredCountPresent bool
	DeclaredCount        uint32
	ActualCount          uint32
	CountSpan            cursor.Span

	DateTimeBits         uint64
	DateMillisecondsSpan cursor.Span
	TimeZoneMinutes      int16
	DateTimezoneSpan     cursor.Span

	ClassName           string
	ClassNameLength     uint16
	ClassNameOmitted    bool
	ClassNameLengthSpan cursor.Span
	ClassNameSpan       cursor.Span
}

// Property associates an Object, ECMA Array, or Typed Object property name
// with its value node. Span covers the encoded name length, name bytes, and
// the bytes consumed for the value.
type Property struct {
	ID             uint32
	ParentID       NodeID
	ValueID        NodeID
	Span           cursor.Span
	NameLengthSpan cursor.Span
	NameSpan       cursor.Span
	NameLength     uint16
	Name           string
	NameOmitted    bool
}

// Result is the bounded result of parsing all AMF0 values in one payload.
type Result struct {
	Span          cursor.Span
	Roots         []NodeID
	Nodes         []Node
	Properties    []Property
	Diagnostics   []Diagnostic
	BytesConsumed uint64
	Complete      bool
	StringBytes   uint64
}

// HasErrors reports whether parsing was incomplete or emitted an error
// diagnostic.
func (r Result) HasErrors() bool {
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

// Options configures hard resource budgets for one payload. Zero values use
// the exported defaults.
type Options struct {
	MaxDepth            uint32
	MaxNodes            uint32
	MaxContainerEntries uint32
	MaxStringBytes      uint32
	MaxTotalStringBytes uint64
	MaxDiagnostics      uint32
}

const (
	// HardMaxPayloadBytes bounds one generic AMF0 payload. FLV DataSize is one
	// byte smaller than this ceiling.
	HardMaxPayloadBytes uint64 = 16 << 20

	DefaultMaxDepth uint32 = 32
	HardMaxDepth    uint32 = 256

	DefaultMaxNodes uint32 = 16_384
	HardMaxNodes    uint32 = 65_536

	DefaultMaxContainerEntries uint32 = 16_384
	HardMaxContainerEntries    uint32 = 65_536

	DefaultMaxStringBytes uint32 = 1 << 20
	HardMaxStringBytes    uint32 = (16 << 20) - 1

	DefaultMaxTotalStringBytes uint64 = 8 << 20
	HardMaxTotalStringBytes    uint64 = 16 << 20

	DefaultMaxDiagnostics uint32 = 256
	MinDiagnostics        uint32 = 2
	HardMaxDiagnostics    uint32 = 4_096
)

// Normalized applies defaults and rejects values above the package hard
// limits. It is exported so adapters can validate options before opening or
// scheduling work.
func (o Options) Normalized() (Options, error) {
	var err error
	if o.MaxDepth, err = normalizeUint32("max depth", o.MaxDepth, DefaultMaxDepth, HardMaxDepth); err != nil {
		return Options{}, err
	}
	if o.MaxNodes, err = normalizeUint32("max nodes", o.MaxNodes, DefaultMaxNodes, HardMaxNodes); err != nil {
		return Options{}, err
	}
	if o.MaxContainerEntries, err = normalizeUint32("max container entries", o.MaxContainerEntries, DefaultMaxContainerEntries, HardMaxContainerEntries); err != nil {
		return Options{}, err
	}
	if o.MaxStringBytes, err = normalizeUint32("max string bytes", o.MaxStringBytes, DefaultMaxStringBytes, HardMaxStringBytes); err != nil {
		return Options{}, err
	}
	if o.MaxTotalStringBytes == 0 {
		o.MaxTotalStringBytes = DefaultMaxTotalStringBytes
	} else if o.MaxTotalStringBytes > HardMaxTotalStringBytes {
		return Options{}, fmt.Errorf("amf0: max total string bytes %d exceeds hard limit %d", o.MaxTotalStringBytes, HardMaxTotalStringBytes)
	}
	if o.MaxDiagnostics == 0 {
		o.MaxDiagnostics = DefaultMaxDiagnostics
	} else if o.MaxDiagnostics < MinDiagnostics {
		return Options{}, fmt.Errorf("amf0: max diagnostics %d is below minimum %d", o.MaxDiagnostics, MinDiagnostics)
	} else if o.MaxDiagnostics > HardMaxDiagnostics {
		return Options{}, fmt.Errorf("amf0: max diagnostics %d exceeds hard limit %d", o.MaxDiagnostics, HardMaxDiagnostics)
	}
	return o, nil
}

func normalizeUint32(name string, value, defaultValue, hardLimit uint32) (uint32, error) {
	if value == 0 {
		return defaultValue, nil
	}
	if value > hardLimit {
		return 0, fmt.Errorf("amf0: %s %d exceeds hard limit %d", name, value, hardLimit)
	}
	return value, nil
}
