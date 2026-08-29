package report

import (
	"encoding/json"
	"fmt"
	"io"
	"math"

	"github.com/sevico/muxray/internal/amf0"
	"github.com/sevico/muxray/internal/cursor"
	"github.com/sevico/muxray/internal/flv"
)

const (
	TagDetailSchemaVersion = 1
	TagDetailDocumentType  = "flv-tag-detail"
	AMF0ScriptDataKind     = "amf0ScriptData"
)

// TagDetailDocument is the bounded, on-demand report for one indexed FLV tag.
// It is a separate public contract from the container scan report.
type TagDetailDocument struct {
	SchemaVersion int                   `json:"schemaVersion"`
	DocumentType  string                `json:"documentType"`
	Source        Source                `json:"source"`
	Tag           Tag                   `json:"tag"`
	Detail        AMF0ScriptDataDetail  `json:"detail"`
	Diagnostics   []TagDetailDiagnostic `json:"diagnostics"`
}

// AMF0ScriptDataDetail is a flat, source-ordered AMF0 syntax arena. Integer
// node relationships avoid a recursive public object graph.
type AMF0ScriptDataDetail struct {
	Kind            string         `json:"kind"`
	Span            Span           `json:"span"`
	BytesConsumed   uint64         `json:"bytesConsumed"`
	Complete        bool           `json:"complete"`
	EventName       *string        `json:"eventName,omitempty"`
	EventNameNodeID *uint32        `json:"eventNameNodeId,omitempty"`
	ValueNodeID     *uint32        `json:"valueNodeId,omitempty"`
	Roots           []uint32       `json:"roots"`
	Nodes           []AMF0Node     `json:"nodes"`
	Properties      []AMF0Property `json:"properties"`
	StringBytes     uint64         `json:"stringBytes"`
}

// AMF0Node represents one source value. Type-specific syntax appears only in
// the corresponding optional member.
type AMF0Node struct {
	ID             uint32         `json:"id"`
	ParentID       *uint32        `json:"parentId,omitempty"`
	FirstChildID   *uint32        `json:"firstChildId,omitempty"`
	NextSiblingID  *uint32        `json:"nextSiblingId,omitempty"`
	ChildCount     uint32         `json:"childCount"`
	PropertyID     *uint32        `json:"propertyId,omitempty"`
	TypeCode       uint8          `json:"typeCode"`
	TypeName       string         `json:"typeName"`
	Complete       bool           `json:"complete"`
	Span           Span           `json:"span"`
	MarkerSpan     Span           `json:"markerSpan"`
	TerminatorSpan *Span          `json:"terminatorSpan,omitempty"`
	Number         *IEEE754Value  `json:"number,omitempty"`
	Boolean        *AMF0Boolean   `json:"boolean,omitempty"`
	String         *AMF0String    `json:"string,omitempty"`
	Reference      *AMF0Reference `json:"reference,omitempty"`
	Count          *AMF0Count     `json:"count,omitempty"`
	Date           *AMF0Date      `json:"date,omitempty"`
	ClassName      *AMF0String    `json:"className,omitempty"`
}

type IEEE754Value struct {
	Bits  string   `json:"bits"`
	Class string   `json:"class"`
	Span  Span     `json:"span"`
	Value *float64 `json:"value,omitempty"`
}

type AMF0Boolean struct {
	Raw   uint8 `json:"raw"`
	Value bool  `json:"value"`
	Span  Span  `json:"span"`
}

type AMF0String struct {
	Length     uint32  `json:"length"`
	LengthSpan Span    `json:"lengthSpan"`
	Span       Span    `json:"span"`
	Omitted    bool    `json:"omitted"`
	Value      *string `json:"value,omitempty"`
}

type AMF0Reference struct {
	Index            uint16  `json:"index"`
	Span             Span    `json:"span"`
	Valid            bool    `json:"valid"`
	ReferencedNodeID *uint32 `json:"referencedNodeId,omitempty"`
}

type AMF0Count struct {
	Declared *AMF0Uint32Field `json:"declared,omitempty"`
	Actual   uint32           `json:"actual"`
}

type AMF0Uint32Field struct {
	Value uint32 `json:"value"`
	Span  Span   `json:"span"`
}

type AMF0Date struct {
	Milliseconds    IEEE754Value `json:"milliseconds"`
	TimezoneMinutes int16        `json:"timezoneMinutes"`
	TimezoneSpan    Span         `json:"timezoneSpan"`
}

type AMF0Property struct {
	ID           uint32     `json:"id"`
	ParentNodeID uint32     `json:"parentNodeId"`
	ValueNodeID  *uint32    `json:"valueNodeId,omitempty"`
	Span         Span       `json:"span"`
	Name         AMF0String `json:"name"`
}

type TagDetailDiagnostic struct {
	Code           string        `json:"code"`
	Severity       amf0.Severity `json:"severity"`
	Offset         uint64        `json:"offset"`
	BitOffset      uint8         `json:"bitOffset"`
	Message        string        `json:"message"`
	Expected       *uint64       `json:"expected,omitempty"`
	Actual         *uint64       `json:"actual,omitempty"`
	RecoveryOffset *uint64       `json:"recoveryOffset,omitempty"`
}

// NewScriptTagDetail maps one parsed ScriptData payload into the public tag
// detail contract. The returned slices preserve parser source order.
func NewScriptTagDetail(sourceSize uint64, tag flv.TagRecord, detail flv.ScriptData) TagDetailDocument {
	document := TagDetailDocument{
		SchemaVersion: TagDetailSchemaVersion,
		DocumentType:  TagDetailDocumentType,
		Source:        Source{SizeBytes: sourceSize},
		Tag:           mapTag(tag),
		Detail: AMF0ScriptDataDetail{
			Kind:          AMF0ScriptDataKind,
			Span:          mapSpan(detail.Span),
			BytesConsumed: detail.AMF.BytesConsumed,
			Complete:      detail.Complete,
			Roots:         make([]uint32, 0, len(detail.AMF.Roots)),
			Nodes:         make([]AMF0Node, 0, len(detail.AMF.Nodes)),
			Properties:    make([]AMF0Property, 0, len(detail.AMF.Properties)),
			StringBytes:   detail.AMF.StringBytes,
		},
		Diagnostics: make([]TagDetailDiagnostic, 0, len(detail.Diagnostics)),
	}

	if detail.EventNameNodeID != amf0.NoNode {
		document.Detail.EventNameNodeID = nodeID(detail.EventNameNodeID)
		if node, ok := amf0Node(detail.AMF, detail.EventNameNodeID); ok && node.Type == amf0.TypeString && node.Complete && !node.StringOmitted {
			value := detail.EventName
			document.Detail.EventName = &value
		}
	}
	if detail.ValueNodeID != amf0.NoNode {
		document.Detail.ValueNodeID = nodeID(detail.ValueNodeID)
	}

	for _, root := range detail.AMF.Roots {
		document.Detail.Roots = append(document.Detail.Roots, uint32(root))
	}
	for _, node := range detail.AMF.Nodes {
		document.Detail.Nodes = append(document.Detail.Nodes, mapAMF0Node(node))
	}
	for _, property := range detail.AMF.Properties {
		document.Detail.Properties = append(document.Detail.Properties, mapAMF0Property(property))
	}
	for _, diagnostic := range detail.Diagnostics {
		document.Diagnostics = append(document.Diagnostics, TagDetailDiagnostic{
			Code: diagnostic.Code, Severity: diagnostic.Severity,
			Offset: diagnostic.Offset, BitOffset: diagnostic.BitOffset,
			Message: diagnostic.Message, Expected: cloneNumber(diagnostic.Expected),
			Actual: cloneNumber(diagnostic.Actual), RecoveryOffset: cloneNumber(diagnostic.RecoveryOffset),
		})
	}
	return document
}

// WriteTagDetailJSON writes exactly one tag-detail document followed by a
// newline. Struct-only DTOs make both compact and pretty output deterministic.
func WriteTagDetailJSON(output io.Writer, document TagDetailDocument, pretty bool) error {
	encoder := json.NewEncoder(output)
	encoder.SetEscapeHTML(false)
	if pretty {
		encoder.SetIndent("", "  ")
	}
	return encoder.Encode(document)
}

func mapAMF0Node(node amf0.Node) AMF0Node {
	mapped := AMF0Node{
		ID: uint32(node.ID), ParentID: optionalNodeID(node.ParentID),
		FirstChildID: optionalNodeID(node.FirstChild), NextSiblingID: optionalNodeID(node.NextSibling),
		ChildCount: node.ChildCount, PropertyID: optionalPropertyID(node.PropertyID),
		TypeCode: uint8(node.Type), TypeName: node.Type.String(), Complete: node.Complete,
		Span: mapSpan(node.Span), MarkerSpan: mapSpan(node.MarkerSpan),
	}
	if node.HasTerminator {
		span := mapSpan(node.TerminatorSpan)
		mapped.TerminatorSpan = &span
	}

	switch node.Type {
	case amf0.TypeNumber:
		if node.NumberSpan.BitLength == 64 {
			value := mapIEEE754(node.NumberBits, node.NumberSpan)
			mapped.Number = &value
		}
	case amf0.TypeBoolean:
		if node.BooleanSpan.BitLength == 8 {
			mapped.Boolean = &AMF0Boolean{Raw: node.BooleanRaw, Value: node.BooleanValue, Span: mapSpan(node.BooleanSpan)}
		}
	case amf0.TypeString, amf0.TypeLongString, amf0.TypeXMLDocument:
		lengthBits := uint32(32)
		if node.Type == amf0.TypeString {
			lengthBits = 16
		}
		if completeStringField(node.StringLength, node.StringLengthSpan, node.StringSpan, lengthBits) {
			value := mapAMF0String(node.StringLength, node.StringLengthSpan, node.StringSpan, node.String, node.StringOmitted)
			mapped.String = &value
		}
	case amf0.TypeReference:
		if node.ReferenceSpan.BitLength == 16 {
			mapped.Reference = &AMF0Reference{
				Index: node.Reference, Span: mapSpan(node.ReferenceSpan), Valid: node.ReferenceValid,
				ReferencedNodeID: optionalNodeID(node.ReferencedNodeID),
			}
		}
	case amf0.TypeObject, amf0.TypeECMAArray, amf0.TypeStrictArray, amf0.TypeTypedObject:
		mapped.Count = &AMF0Count{Actual: node.ActualCount}
		if node.DeclaredCountPresent {
			mapped.Count.Declared = &AMF0Uint32Field{Value: node.DeclaredCount, Span: mapSpan(node.CountSpan)}
		}
	}
	if node.Type == amf0.TypeDate && node.DateMillisecondsSpan.BitLength == 64 && node.DateTimezoneSpan.BitLength == 16 {
		mapped.Date = &AMF0Date{
			Milliseconds:    mapIEEE754(node.DateTimeBits, node.DateMillisecondsSpan),
			TimezoneMinutes: node.TimeZoneMinutes, TimezoneSpan: mapSpan(node.DateTimezoneSpan),
		}
	}
	if node.Type == amf0.TypeTypedObject && completeStringField(uint32(node.ClassNameLength), node.ClassNameLengthSpan, node.ClassNameSpan, 16) {
		value := mapAMF0String(uint32(node.ClassNameLength), node.ClassNameLengthSpan, node.ClassNameSpan, node.ClassName, node.ClassNameOmitted)
		mapped.ClassName = &value
	}
	return mapped
}

func completeStringField(length uint32, lengthSpan, valueSpan cursor.Span, lengthBits uint32) bool {
	return lengthSpan.BitLength == lengthBits && uint64(valueSpan.BitLength) == uint64(length)*8
}

func mapAMF0Property(property amf0.Property) AMF0Property {
	return AMF0Property{
		ID: uint32(property.ID), ParentNodeID: uint32(property.ParentID),
		ValueNodeID: optionalNodeID(property.ValueID), Span: mapSpan(property.Span),
		Name: mapAMF0String(uint32(property.NameLength), property.NameLengthSpan, property.NameSpan, property.Name, property.NameOmitted),
	}
}

func mapAMF0String(length uint32, lengthSpan, valueSpan cursor.Span, value string, omitted bool) AMF0String {
	mapped := AMF0String{
		Length: length, LengthSpan: mapSpan(lengthSpan), Span: mapSpan(valueSpan), Omitted: omitted,
	}
	if !omitted {
		copy := value
		mapped.Value = &copy
	}
	return mapped
}

func mapIEEE754(bits uint64, spanValue cursor.Span) IEEE754Value {
	value := math.Float64frombits(bits)
	mapped := IEEE754Value{
		Bits: ieee754Bits(bits), Class: ieee754Class(value), Span: mapSpan(spanValue),
	}
	if mapped.Class == "finite" {
		mapped.Value = &value
	}
	return mapped
}

func optionalNodeID(id amf0.NodeID) *uint32 {
	if id == amf0.NoNode {
		return nil
	}
	return nodeID(id)
}

func nodeID(id amf0.NodeID) *uint32 {
	value := uint32(id)
	return &value
}

func optionalPropertyID(id uint32) *uint32 {
	if id == amf0.NoProperty {
		return nil
	}
	value := id
	return &value
}

func amf0Node(result amf0.Result, id amf0.NodeID) (amf0.Node, bool) {
	index := uint64(id)
	if index >= uint64(len(result.Nodes)) {
		return amf0.Node{}, false
	}
	return result.Nodes[index], true
}

func ieee754Class(value float64) string {
	switch {
	case math.IsNaN(value):
		return "nan"
	case math.IsInf(value, 1):
		return "positiveInfinity"
	case math.IsInf(value, -1):
		return "negativeInfinity"
	default:
		return "finite"
	}
}

func ieee754Bits(bits uint64) string { return fmt.Sprintf("%016x", bits) }
