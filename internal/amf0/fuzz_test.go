package amf0

import (
	"bytes"
	"math"
	"reflect"
	"testing"

	"github.com/sevico/muxray/internal/cursor"
)

func FuzzAMF0(f *testing.F) {
	f.Add([]byte{})
	f.Add(appendU64([]byte{byte(TypeNumber)}, math.Float64bits(1.25)))
	f.Add([]byte{byte(TypeBoolean), 1})
	f.Add(appendShortString(nil, TypeString, []byte("onMetaData")))
	f.Add([]byte{byte(TypeNull), byte(TypeUndefined), byte(TypeUnsupported)})
	f.Add([]byte{byte(TypeObject), 0, 0, byte(TypeObjectEnd)})
	object := []byte{byte(TypeObject)}
	object = appendProperty(object, []byte("x"), []byte{byte(TypeBoolean), 0})
	object = append(object, 0, 0, byte(TypeObjectEnd))
	f.Add(object)
	f.Add([]byte{byte(TypeECMAArray), 0, 0, 0, 0, 0, 0, byte(TypeObjectEnd)})
	f.Add([]byte{byte(TypeStrictArray), 0, 0, 0, 2, byte(TypeNull), byte(TypeUndefined)})
	f.Add(appendU16(appendU64([]byte{byte(TypeDate)}, 0), 0))
	f.Add(appendLongString(nil, TypeLongString, []byte("long")))
	f.Add(appendLongString(nil, TypeXMLDocument, []byte("<x/>")))
	typed := []byte{byte(TypeTypedObject), 0, 1, 'C', 0, 0, byte(TypeObjectEnd), byte(TypeReference), 0, 0}
	f.Add(typed)

	options := Options{
		MaxDepth: 16, MaxNodes: 1_024, MaxContainerEntries: 1_024,
		MaxStringBytes: 64 << 10, MaxTotalStringBytes: 256 << 10,
		MaxDiagnostics: 64,
	}
	normalized, err := options.Normalized()
	if err != nil {
		f.Fatalf("normalize fuzz options: %v", err)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		if uint64(len(data)) > HardMaxPayloadBytes {
			return
		}
		const base = 3
		source := make([]byte, base+len(data)+2)
		copy(source[base:], data)
		span := cursor.Span{ByteOffset: base, BitLength: uint32(len(data) * 8)}
		result, err := ParseSequence(bytes.NewReader(source), span, options)
		if err != nil {
			t.Fatalf("ParseSequence: %v", err)
		}
		assertFuzzInvariants(t, result, normalized, base, len(data))

		repeated, err := ParseSequence(bytes.NewReader(source), span, options)
		if err != nil {
			t.Fatalf("repeat ParseSequence: %v", err)
		}
		if !reflect.DeepEqual(result, repeated) {
			t.Fatalf("non-deterministic parse\nfirst=%+v\nsecond=%+v", result, repeated)
		}
	})
}

func assertFuzzInvariants(t *testing.T, result Result, options Options, base, length int) {
	t.Helper()
	if result.BytesConsumed > uint64(length) {
		t.Fatalf("bytes consumed %d > payload %d", result.BytesConsumed, length)
	}
	if result.Complete && result.BytesConsumed != uint64(length) {
		t.Fatalf("complete parse consumed %d of %d bytes", result.BytesConsumed, length)
	}
	if uint32(len(result.Nodes)) > options.MaxNodes {
		t.Fatalf("nodes %d > limit %d", len(result.Nodes), options.MaxNodes)
	}
	if uint32(len(result.Diagnostics)) > options.MaxDiagnostics {
		t.Fatalf("diagnostics %d > limit %d", len(result.Diagnostics), options.MaxDiagnostics)
	}
	if result.StringBytes > options.MaxTotalStringBytes {
		t.Fatalf("stored strings %d > budget %d", result.StringBytes, options.MaxTotalStringBytes)
	}
	assertAllSpansWithin(t, result, base, length)

	validNode := func(id NodeID) bool { return id == NoNode || uint64(id) < uint64(len(result.Nodes)) }
	validProperty := func(id uint32) bool { return id == NoProperty || uint64(id) < uint64(len(result.Properties)) }
	for index, node := range result.Nodes {
		if node.ID != NodeID(index) {
			t.Fatalf("node %d has id %d", index, node.ID)
		}
		if !validNode(node.ParentID) || !validNode(node.FirstChild) || !validNode(node.NextSibling) || !validNode(node.ReferencedNodeID) {
			t.Fatalf("node %d has invalid relationship: %+v", index, node)
		}
		if !validProperty(node.PropertyID) {
			t.Fatalf("node %d has invalid property id %d", index, node.PropertyID)
		}
		if result.Complete && !node.Complete {
			t.Fatalf("complete result contains incomplete node %d: %+v", index, node)
		}
		assertCompleteNodeFields(t, node)
		seen := make(map[NodeID]struct{}, node.ChildCount)
		child := node.FirstChild
		for child != NoNode {
			if _, exists := seen[child]; exists {
				t.Fatalf("node %d child chain contains cycle at %d", index, child)
			}
			seen[child] = struct{}{}
			if result.Nodes[child].ParentID != node.ID {
				t.Fatalf("child %d parent=%d want=%d", child, result.Nodes[child].ParentID, node.ID)
			}
			child = result.Nodes[child].NextSibling
		}
		if uint32(len(seen)) != node.ChildCount {
			t.Fatalf("node %d child count=%d chain=%d", index, node.ChildCount, len(seen))
		}
	}
	for index, property := range result.Properties {
		if property.ID != uint32(index) || uint64(property.ParentID) >= uint64(len(result.Nodes)) {
			t.Fatalf("invalid property identity: %+v", property)
		}
		if property.ValueID != NoNode {
			if uint64(property.ValueID) >= uint64(len(result.Nodes)) || result.Nodes[property.ValueID].PropertyID != property.ID {
				t.Fatalf("invalid property value link: %+v", property)
			}
		}
	}
	end := uint64(base + length)
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Offset < uint64(base) || diagnostic.Offset > end || diagnostic.BitOffset > 7 {
			t.Fatalf("diagnostic outside payload: %+v", diagnostic)
		}
		if diagnostic.RecoveryOffset != nil && *diagnostic.RecoveryOffset != end {
			t.Fatalf("recovery offset=%d want=%d", *diagnostic.RecoveryOffset, end)
		}
	}
}

func assertCompleteNodeFields(t *testing.T, node Node) {
	t.Helper()
	if !node.Complete {
		return
	}
	switch node.Type {
	case TypeNumber:
		if node.NumberSpan.BitLength != 64 {
			t.Fatalf("complete number has partial span: %+v", node)
		}
	case TypeBoolean:
		if node.BooleanSpan.BitLength != 8 {
			t.Fatalf("complete boolean has partial span: %+v", node)
		}
	case TypeString:
		if node.StringLengthSpan.BitLength != 16 || uint64(node.StringSpan.BitLength) != uint64(node.StringLength)*8 {
			t.Fatalf("complete string has partial fields: %+v", node)
		}
	case TypeLongString, TypeXMLDocument:
		if node.StringLengthSpan.BitLength != 32 || uint64(node.StringSpan.BitLength) != uint64(node.StringLength)*8 {
			t.Fatalf("complete long string has partial fields: %+v", node)
		}
	case TypeReference:
		if node.ReferenceSpan.BitLength != 16 {
			t.Fatalf("complete reference has partial span: %+v", node)
		}
	case TypeObject, TypeECMAArray:
		if !node.HasTerminator {
			t.Fatalf("complete object-like node has no terminator: %+v", node)
		}
	case TypeStrictArray:
		if !node.DeclaredCountPresent || node.CountSpan.BitLength != 32 || node.ActualCount != node.DeclaredCount {
			t.Fatalf("complete strict array has partial count/elements: %+v", node)
		}
	case TypeDate:
		if node.DateMillisecondsSpan.BitLength != 64 || node.DateTimezoneSpan.BitLength != 16 {
			t.Fatalf("complete date has partial fields: %+v", node)
		}
	case TypeTypedObject:
		if node.ClassNameLengthSpan.BitLength != 16 || uint64(node.ClassNameSpan.BitLength) != uint64(node.ClassNameLength)*8 || !node.HasTerminator {
			t.Fatalf("complete typed object has partial fields: %+v", node)
		}
	case TypeMovieClip, TypeObjectEnd, TypeRecordSet, TypeAVMPlus:
		t.Fatalf("unsupported/fatal marker was marked complete: %+v", node)
	}
}
