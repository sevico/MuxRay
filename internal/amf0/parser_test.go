package amf0

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"reflect"
	"testing"
	"unsafe"

	"github.com/sevico/muxray/internal/cursor"
)

func TestArenaRecordSizeBounds(t *testing.T) {
	nodeSize := unsafe.Sizeof(Node{})
	propertySize := unsafe.Sizeof(Property{})
	t.Logf("Node=%d bytes Property=%d bytes", nodeSize, propertySize)
	if nodeSize > 384 {
		t.Fatalf("Node grew to %d bytes; review AMF0 detail memory budget", nodeSize)
	}
	if propertySize > 96 {
		t.Fatalf("Property grew to %d bytes; review AMF0 detail memory budget", propertySize)
	}
	const parserArenaBudget = 32 << 20
	hardArenaBytes := uint64(HardMaxNodes)*(uint64(nodeSize)+uint64(unsafe.Sizeof(NodeID(0)))) +
		uint64(HardMaxContainerEntries)*uint64(propertySize)
	if hardArenaBytes > parserArenaBudget {
		t.Fatalf("hard-limit AMF0 arenas require %d bytes, budget is %d", hardArenaBytes, parserArenaBudget)
	}
}

func TestInitialArenaCapacityIsBounded(t *testing.T) {
	for _, test := range []struct {
		payload  uint64
		maxNodes uint32
		want     int
	}{
		{payload: 0, maxNodes: 100, want: 0},
		{payload: 1, maxNodes: 100, want: 1},
		{payload: 16, maxNodes: 100, want: 2},
		{payload: HardMaxPayloadBytes, maxNodes: 10, want: 10},
		{payload: HardMaxPayloadBytes, maxNodes: HardMaxNodes, want: 4_096},
	} {
		if got := initialArenaCapacity(test.payload, test.maxNodes); got != test.want {
			t.Errorf("initialArenaCapacity(%d, %d) = %d, want %d", test.payload, test.maxNodes, got, test.want)
		}
	}
}

func TestNextArenaCapacityDoublesWithinHardLimit(t *testing.T) {
	for _, test := range []struct {
		current int
		hard    int
		want    int
	}{
		{current: 0, hard: 10, want: 1},
		{current: 1, hard: 10, want: 2},
		{current: 4, hard: 10, want: 8},
		{current: 8, hard: 10, want: 10},
		{current: 10, hard: 10, want: 10},
	} {
		if got := nextArenaCapacity(test.current, test.hard); got != test.want {
			t.Errorf("nextArenaCapacity(%d, %d) = %d, want %d", test.current, test.hard, got, test.want)
		}
	}
}

func TestParseSequenceScalarsAndAbsoluteSpans(t *testing.T) {
	numberBits := math.Float64bits(-29.97)
	payload := []byte{byte(TypeNumber)}
	payload = appendU64(payload, numberBits)
	payload = append(payload, byte(TypeBoolean), 1)
	payload = appendShortString(payload, TypeString, []byte("hi"))
	payload = append(payload, byte(TypeNull), byte(TypeUndefined), byte(TypeUnsupported))
	payload = appendLongString(payload, TypeLongString, []byte("long"))
	payload = appendLongString(payload, TypeXMLDocument, []byte("<x/>"))

	const base = 7
	result := parseAt(t, base, payload, Options{})
	if !result.Complete || result.HasErrors() {
		t.Fatalf("unexpected result: complete=%v diagnostics=%+v", result.Complete, result.Diagnostics)
	}
	if result.BytesConsumed != uint64(len(payload)) || result.StringBytes != 10 {
		t.Fatalf("unexpected counters: %+v", result)
	}
	if len(result.Roots) != 8 || len(result.Nodes) != 8 {
		t.Fatalf("roots=%v nodes=%d", result.Roots, len(result.Nodes))
	}
	for index, node := range result.Nodes {
		if node.ID != NodeID(index) || node.ParentID != NoNode || node.PropertyID != NoProperty {
			t.Fatalf("node %d has invalid identity: %+v", index, node)
		}
	}
	number := result.Nodes[0]
	if number.NumberBits != numberBits {
		t.Fatalf("number bits=%016x want=%016x", number.NumberBits, numberBits)
	}
	assertSpan(t, number.MarkerSpan, base, 1)
	assertSpan(t, number.NumberSpan, base+1, 8)
	assertSpan(t, number.Span, base, 9)
	boolean := result.Nodes[1]
	if boolean.BooleanRaw != 1 || !boolean.BooleanValue {
		t.Fatalf("unexpected boolean: %+v", boolean)
	}
	short := result.Nodes[2]
	if short.String != "hi" || short.StringLength != 2 || short.StringOmitted {
		t.Fatalf("unexpected short string: %+v", short)
	}
	assertSpan(t, short.StringLengthSpan, base+12, 2)
	assertSpan(t, short.StringSpan, base+14, 2)
	long := result.Nodes[6]
	if long.String != "long" || long.StringLength != 4 {
		t.Fatalf("unexpected long string: %+v", long)
	}
	xml := result.Nodes[7]
	if xml.String != "<x/>" || !xml.ReferenceValid && xml.ReferencedNodeID != NoNode {
		t.Fatalf("unexpected XML document: %+v", xml)
	}
	assertAllSpansWithin(t, result, base, len(payload))
}

func TestParseCompositeArenaPropertiesAndReferences(t *testing.T) {
	object := []byte{byte(TypeObject)}
	object = appendProperty(object, []byte("a"), appendU64([]byte{byte(TypeNumber)}, math.Float64bits(1.5)))
	object = appendProperty(object, nil, []byte{byte(TypeBoolean), 1})
	object = append(object, 0, 0, byte(TypeObjectEnd))
	object = append(object, byte(TypeReference), 0, 0)

	result := parseAt(t, 0, object, Options{})
	if !result.Complete || result.HasErrors() {
		t.Fatalf("unexpected result: %+v", result)
	}
	if len(result.Roots) != 2 || len(result.Nodes) != 4 || len(result.Properties) != 2 {
		t.Fatalf("unexpected arena sizes: roots=%v nodes=%d properties=%d", result.Roots, len(result.Nodes), len(result.Properties))
	}
	container := result.Nodes[0]
	if container.Type != TypeObject || container.FirstChild != 1 || container.ChildCount != 2 || !container.HasTerminator || container.ActualCount != 2 {
		t.Fatalf("unexpected object: %+v", container)
	}
	if result.Nodes[1].NextSibling != 2 || result.Nodes[2].NextSibling != NoNode {
		t.Fatalf("invalid sibling chain: %+v %+v", result.Nodes[1], result.Nodes[2])
	}
	first := result.Properties[0]
	if first.ParentID != 0 || first.ValueID != 1 || first.Name != "a" || first.NameLength != 1 || first.NameOmitted {
		t.Fatalf("unexpected first property: %+v", first)
	}
	if result.Nodes[1].PropertyID != first.ID {
		t.Fatalf("node/property link mismatch: %+v", result.Nodes[1])
	}
	second := result.Properties[1]
	if second.Name != "" || second.NameLength != 0 || second.ValueID != 2 {
		t.Fatalf("empty-name property was treated as terminator: %+v", second)
	}
	assertSpan(t, container.TerminatorSpan, len(object)-6, 3)
	reference := result.Nodes[3]
	if reference.Type != TypeReference || reference.Reference != 0 || !reference.ReferenceValid || reference.ReferencedNodeID != 0 {
		t.Fatalf("unexpected reference: %+v", reference)
	}
}

func TestParseArraysDateAndTypedObject(t *testing.T) {
	payload := []byte{byte(TypeECMAArray)}
	payload = appendU32(payload, 2)
	payload = appendProperty(payload, []byte("x"), []byte{byte(TypeNull)})
	payload = append(payload, 0, 0, byte(TypeObjectEnd))
	payload = append(payload, byte(TypeStrictArray))
	payload = appendU32(payload, 2)
	payload = append(payload, byte(TypeUndefined), byte(TypeBoolean), 0)
	payload = append(payload, byte(TypeDate))
	payload = appendU64(payload, math.Float64bits(-1234.5))
	payload = appendU16(payload, 0xffc4)
	payload = append(payload, byte(TypeTypedObject))
	payload = appendU16(payload, 1)
	payload = append(payload, 'C')
	payload = appendProperty(payload, []byte("p"), []byte{byte(TypeString), 0, 1, 'v'})
	payload = append(payload, 0, 0, byte(TypeObjectEnd))

	result := parseAt(t, 3, payload, Options{})
	if !result.Complete || result.HasErrors() {
		t.Fatalf("warnings should not be errors: complete=%v diagnostics=%+v", result.Complete, result.Diagnostics)
	}
	assertDiagnostic(t, result, CodeECMAArrayCountMismatch, SeverityWarning)
	assertDiagnostic(t, result, CodeDateTimezoneNonZero, SeverityWarning)
	if len(result.Roots) != 4 {
		t.Fatalf("roots=%v", result.Roots)
	}
	ecma := result.Nodes[result.Roots[0]]
	if !ecma.DeclaredCountPresent || ecma.DeclaredCount != 2 || ecma.ActualCount != 1 || !ecma.HasTerminator {
		t.Fatalf("unexpected ECMA Array: %+v", ecma)
	}
	assertSpan(t, ecma.CountSpan, 4, 4)
	assertSpan(t, ecma.TerminatorSpan, 12, 3)
	assertSpan(t, result.Properties[0].Span, 8, 4)
	strict := result.Nodes[result.Roots[1]]
	if strict.DeclaredCount != 2 || strict.ActualCount != 2 || strict.ChildCount != 2 {
		t.Fatalf("unexpected Strict Array: %+v", strict)
	}
	assertSpan(t, strict.CountSpan, 16, 4)
	date := result.Nodes[result.Roots[2]]
	if date.DateTimeBits != math.Float64bits(-1234.5) || date.TimeZoneMinutes != -60 {
		t.Fatalf("unexpected date: %+v", date)
	}
	assertSpan(t, date.DateMillisecondsSpan, 24, 8)
	assertSpan(t, date.DateTimezoneSpan, 32, 2)
	typed := result.Nodes[result.Roots[3]]
	if typed.ClassName != "C" || typed.ClassNameLength != 1 || typed.ClassNameOmitted || !typed.HasTerminator || typed.ActualCount != 1 {
		t.Fatalf("unexpected typed object: %+v", typed)
	}
	assertSpan(t, typed.ClassNameLengthSpan, 35, 2)
	assertSpan(t, typed.ClassNameSpan, 37, 1)
	assertSpan(t, typed.TerminatorSpan, 45, 3)
	if result.Properties[len(result.Properties)-1].Name != "p" {
		t.Fatalf("typed-object property missing: %+v", result.Properties)
	}
	assertSpan(t, result.Properties[len(result.Properties)-1].Span, 38, 7)
}

func TestParseWarningsPreserveRawValues(t *testing.T) {
	payload := []byte{byte(TypeBoolean), 2}
	payload = appendShortString(payload, TypeString, []byte{'a', 0xff})
	payload = append(payload, byte(TypeReference), 0, 9)

	result := parseAt(t, 0, payload, Options{})
	if !result.Complete || result.HasErrors() {
		t.Fatalf("warnings unexpectedly failed parse: %+v", result)
	}
	for _, code := range []string{CodeBooleanNonCanonical, CodeUTF8Invalid, CodeReferenceOutOfRange} {
		assertDiagnostic(t, result, code, SeverityWarning)
	}
	if diagnostic := assertDiagnostic(t, result, CodeUTF8Invalid, SeverityWarning); diagnostic.Offset != 6 {
		t.Fatalf("invalid UTF-8 offset = %d, want first invalid byte 6", diagnostic.Offset)
	}
	if result.Nodes[0].BooleanRaw != 2 || !result.Nodes[0].BooleanValue {
		t.Fatalf("boolean raw value lost: %+v", result.Nodes[0])
	}
	if !bytes.Equal([]byte(result.Nodes[1].String), []byte{'a', 0xff}) {
		t.Fatalf("invalid UTF-8 was rewritten: %x", []byte(result.Nodes[1].String))
	}
	if result.Nodes[2].ReferenceValid || result.Nodes[2].ReferencedNodeID != NoNode {
		t.Fatalf("invalid reference became valid: %+v", result.Nodes[2])
	}
}

func TestParseFatalMarkersRecoverAtPayloadEnd(t *testing.T) {
	for _, test := range []struct {
		name   string
		marker byte
		code   string
	}{
		{name: "movie clip", marker: byte(TypeMovieClip), code: CodeTypeUnsupported},
		{name: "record set", marker: byte(TypeRecordSet), code: CodeTypeUnsupported},
		{name: "AMF3", marker: byte(TypeAVMPlus), code: CodeTypeUnsupported},
		{name: "object end", marker: byte(TypeObjectEnd), code: CodeObjectEndUnexpected},
		{name: "unknown", marker: 0xff, code: CodeMarkerUnknown},
	} {
		t.Run(test.name, func(t *testing.T) {
			payload := []byte{test.marker, 0xaa, 0xbb, 0xcc}
			result := parseAt(t, 11, payload, Options{})
			if result.Complete || !result.HasErrors() || result.BytesConsumed != 1 {
				t.Fatalf("fatal marker did not recover: %+v", result)
			}
			diagnostic := assertDiagnostic(t, result, test.code, SeverityError)
			if diagnostic.RecoveryOffset == nil || *diagnostic.RecoveryOffset != 11+uint64(len(payload)) {
				t.Fatalf("recovery offset=%v", diagnostic.RecoveryOffset)
			}
			if len(result.Nodes) != 1 || result.Nodes[0].Span.BitLength != 8 {
				t.Fatalf("recovery bytes leaked into node span: %+v", result.Nodes)
			}
			if result.Nodes[0].Complete {
				t.Fatalf("fatal node was marked complete: %+v", result.Nodes[0])
			}
		})
	}
}

func TestParseTruncationDiagnostics(t *testing.T) {
	tests := []struct {
		name    string
		payload []byte
		code    string
	}{
		{name: "number", payload: append([]byte{byte(TypeNumber)}, make([]byte, 7)...), code: CodeNumberTruncated},
		{name: "boolean", payload: []byte{byte(TypeBoolean)}, code: CodeBooleanTruncated},
		{name: "short string length", payload: []byte{byte(TypeString), 0}, code: CodeStringLengthTruncated},
		{name: "short string data", payload: []byte{byte(TypeString), 0, 2, 'a'}, code: CodeStringDataTruncated},
		{name: "long string length", payload: []byte{byte(TypeLongString), 0, 0, 0}, code: CodeStringLengthTruncated},
		{name: "XML data", payload: []byte{byte(TypeXMLDocument), 0, 0, 0, 2, 'x'}, code: CodeStringDataTruncated},
		{name: "reference", payload: []byte{byte(TypeReference), 0}, code: CodeReferenceTruncated},
		{name: "ECMA count", payload: []byte{byte(TypeECMAArray), 0, 0, 0}, code: CodeArrayCountTruncated},
		{name: "strict count", payload: []byte{byte(TypeStrictArray), 0, 0}, code: CodeArrayCountTruncated},
		{name: "date milliseconds", payload: append([]byte{byte(TypeDate)}, make([]byte, 7)...), code: CodeDateMillisecondsTruncated},
		{name: "date timezone", payload: append(appendU64([]byte{byte(TypeDate)}, 0), 0), code: CodeDateTimezoneTruncated},
		{name: "typed class length", payload: []byte{byte(TypeTypedObject), 0}, code: CodeTypedClassLengthTruncated},
		{name: "typed class data", payload: []byte{byte(TypeTypedObject), 0, 2, 'C'}, code: CodeTypedClassDataTruncated},
		{name: "object key length", payload: []byte{byte(TypeObject), 0}, code: CodeObjectKeyLengthTruncated},
		{name: "object key data", payload: []byte{byte(TypeObject), 0, 2, 'a'}, code: CodeObjectKeyDataTruncated},
		{name: "object end", payload: []byte{byte(TypeObject)}, code: CodeObjectEndMissing},
		{name: "strict element marker", payload: []byte{byte(TypeStrictArray), 0, 0, 0, 1}, code: CodeMarkerTruncated},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := parseAt(t, 5, test.payload, Options{})
			if result.Complete || result.BytesConsumed != uint64(len(test.payload)) {
				t.Fatalf("truncated parse state: %+v", result)
			}
			diagnostic := assertDiagnostic(t, result, test.code, SeverityError)
			if diagnostic.RecoveryOffset == nil || *diagnostic.RecoveryOffset != 5+uint64(len(test.payload)) {
				t.Fatalf("recovery offset=%v", diagnostic.RecoveryOffset)
			}
			assertAllSpansWithin(t, result, 5, len(test.payload))
			if len(result.Nodes) > 0 && result.Nodes[0].Complete {
				t.Fatalf("truncated root was marked complete: %+v", result.Nodes[0])
			}
		})
	}
}

func TestParseLimits(t *testing.T) {
	t.Run("depth", func(t *testing.T) {
		payload := []byte{byte(TypeStrictArray), 0, 0, 0, 1, byte(TypeNull)}
		result := parseAt(t, 0, payload, Options{MaxDepth: 1})
		assertDiagnostic(t, result, CodeDepthLimitExceeded, SeverityError)
		if result.Complete || len(result.Nodes) != 1 || result.BytesConsumed != uint64(len(payload)) {
			t.Fatalf("unexpected depth result: %+v", result)
		}
	})

	t.Run("nodes", func(t *testing.T) {
		payload := []byte{byte(TypeNull), byte(TypeNull)}
		result := parseAt(t, 0, payload, Options{MaxNodes: 1})
		assertDiagnostic(t, result, CodeNodeLimitExceeded, SeverityError)
		if len(result.Nodes) != 1 || result.Complete {
			t.Fatalf("unexpected node-limit result: %+v", result)
		}
	})

	t.Run("strict entries", func(t *testing.T) {
		payload := []byte{byte(TypeStrictArray), 0, 0, 0, 2, byte(TypeNull), byte(TypeNull)}
		result := parseAt(t, 0, payload, Options{MaxContainerEntries: 1})
		assertDiagnostic(t, result, CodeContainerEntryLimitExceeded, SeverityError)
		if result.Complete || result.Nodes[0].ActualCount != 0 {
			t.Fatalf("unexpected entry-limit result: %+v", result)
		}
	})

	t.Run("object limit still accepts terminator", func(t *testing.T) {
		payload := []byte{byte(TypeObject)}
		payload = appendProperty(payload, []byte("a"), []byte{byte(TypeNull)})
		payload = append(payload, 0, 0, byte(TypeObjectEnd))
		result := parseAt(t, 0, payload, Options{MaxContainerEntries: 1})
		if !result.Complete || result.HasErrors() || !result.Nodes[0].HasTerminator {
			t.Fatalf("terminator rejected at exact limit: %+v", result)
		}
	})

	t.Run("object entries", func(t *testing.T) {
		payload := []byte{byte(TypeObject)}
		payload = appendProperty(payload, []byte("a"), []byte{byte(TypeNull)})
		payload = appendProperty(payload, []byte("b"), []byte{byte(TypeNull)})
		payload = append(payload, 0, 0, byte(TypeObjectEnd))
		result := parseAt(t, 0, payload, Options{MaxContainerEntries: 1})
		assertDiagnostic(t, result, CodeContainerEntryLimitExceeded, SeverityError)
		if result.Complete || len(result.Properties) != 1 {
			t.Fatalf("unexpected object entry result: %+v", result)
		}
	})

	t.Run("single string", func(t *testing.T) {
		payload := appendShortString(nil, TypeString, []byte("ab"))
		result := parseAt(t, 0, payload, Options{MaxStringBytes: 1})
		assertDiagnostic(t, result, CodeStringLimitExceeded, SeverityWarning)
		if !result.Complete || result.HasErrors() || !result.Nodes[0].StringOmitted || result.StringBytes != 0 {
			t.Fatalf("unexpected string limit result: %+v", result)
		}
	})

	t.Run("total strings", func(t *testing.T) {
		payload := appendShortString(nil, TypeString, []byte("ab"))
		payload = appendShortString(payload, TypeString, []byte("cd"))
		result := parseAt(t, 0, payload, Options{MaxTotalStringBytes: 3})
		assertDiagnostic(t, result, CodeStringBudgetExceeded, SeverityWarning)
		if !result.Complete || result.Nodes[0].String != "ab" || !result.Nodes[1].StringOmitted || result.StringBytes != 2 {
			t.Fatalf("unexpected total-string result: %+v", result)
		}
	})

	t.Run("property name budget", func(t *testing.T) {
		payload := []byte{byte(TypeObject)}
		payload = appendProperty(payload, []byte("ab"), []byte{byte(TypeNull)})
		payload = append(payload, 0, 0, byte(TypeObjectEnd))
		result := parseAt(t, 0, payload, Options{MaxTotalStringBytes: 1})
		assertDiagnostic(t, result, CodeStringBudgetExceeded, SeverityWarning)
		if !result.Complete || len(result.Properties) != 1 || !result.Properties[0].NameOmitted || result.StringBytes != 0 {
			t.Fatalf("unexpected property budget result: %+v", result)
		}
	})

	t.Run("diagnostics", func(t *testing.T) {
		payload := []byte{byte(TypeBoolean), 2, byte(TypeBoolean), 3, byte(TypeBoolean), 4}
		result := parseAt(t, 0, payload, Options{MaxDiagnostics: 2})
		if !result.Complete || len(result.Diagnostics) != 2 || result.Diagnostics[1].Code != CodeDiagnosticLimitReached || !result.HasErrors() {
			t.Fatalf("unexpected diagnostic limit result: %+v", result)
		}
	})

	t.Run("warning overflow then fatal", func(t *testing.T) {
		payload := []byte{
			byte(TypeBoolean), 2,
			byte(TypeBoolean), 3,
			byte(TypeBoolean), 4,
			0xff,
		}
		result := parseAt(t, 19, payload, Options{MaxDiagnostics: 2})
		if result.Complete || len(result.Diagnostics) != 2 {
			t.Fatalf("unexpected diagnostic result: %+v", result)
		}
		fatal := result.Diagnostics[0]
		marker := result.Diagnostics[1]
		if fatal.Code != CodeMarkerUnknown || fatal.RecoveryOffset == nil || *fatal.RecoveryOffset != 19+uint64(len(payload)) {
			t.Fatalf("fatal diagnostic was not retained: %+v", fatal)
		}
		if marker.Code != CodeDiagnosticLimitReached || marker.RecoveryOffset == nil || *marker.RecoveryOffset != 19+uint64(len(payload)) {
			t.Fatalf("limit marker lost recovery: %+v", marker)
		}
	})
}

func TestOptionsAndPayloadValidation(t *testing.T) {
	normalized, err := (Options{}).Normalized()
	if err != nil {
		t.Fatalf("Normalized: %v", err)
	}
	if normalized.MaxDepth != DefaultMaxDepth || normalized.MaxNodes != DefaultMaxNodes || normalized.MaxContainerEntries != DefaultMaxContainerEntries ||
		normalized.MaxStringBytes != DefaultMaxStringBytes || normalized.MaxTotalStringBytes != DefaultMaxTotalStringBytes || normalized.MaxDiagnostics != DefaultMaxDiagnostics {
		t.Fatalf("unexpected defaults: %+v", normalized)
	}
	invalid := []Options{
		{MaxDepth: HardMaxDepth + 1},
		{MaxNodes: HardMaxNodes + 1},
		{MaxContainerEntries: HardMaxContainerEntries + 1},
		{MaxStringBytes: HardMaxStringBytes + 1},
		{MaxTotalStringBytes: HardMaxTotalStringBytes + 1},
		{MaxDiagnostics: 1},
		{MaxDiagnostics: HardMaxDiagnostics + 1},
	}
	for _, options := range invalid {
		if _, err := options.Normalized(); err == nil {
			t.Errorf("accepted invalid options: %+v", options)
		}
	}

	if _, err := ParseSequence(nil, cursor.Span{}, Options{}); err == nil {
		t.Fatal("nil source accepted")
	}
	if _, err := ParseSequence(bytes.NewReader(nil), cursor.Span{BitOffset: 1}, Options{}); err == nil {
		t.Fatal("unaligned span accepted")
	}
	tooLarge := cursor.Span{BitLength: uint32((HardMaxPayloadBytes + 1) * 8)}
	if _, err := ParseSequence(zeroReaderAt{}, tooLarge, Options{}); err == nil {
		t.Fatal("oversized payload accepted")
	}
}

func TestReaderAtFailuresAndPayloadBoundary(t *testing.T) {
	want := errors.New("storage failed")
	result, err := ParseSequence(errorReaderAt{err: want}, cursor.Span{BitLength: 8}, Options{})
	if !errors.Is(err, want) || result.Complete || result.BytesConsumed != 0 {
		t.Fatalf("ReaderAt result=%+v error=%v", result, err)
	}

	data := appendShortString(nil, TypeString, []byte("ok"))
	fullEOF := fullReadEOFReaderAt{reader: bytes.NewReader(data)}
	result, err = ParseSequence(fullEOF, cursor.Span{BitLength: uint32(len(data) * 8)}, Options{})
	if err != nil || !result.Complete || result.Nodes[0].String != "ok" {
		t.Fatalf("full read with EOF failed: result=%+v error=%v", result, err)
	}

	guardData := []byte{0xaa, byte(TypeMarkerForTestUnknown), 0xbb, 0xcc, 0xdd, 0xee}
	guard := &guardReaderAt{data: guardData, allowedStart: 1, allowedEnd: 5}
	result, err = ParseSequence(guard, cursor.Span{ByteOffset: 1, BitLength: 32}, Options{})
	if err != nil {
		t.Fatalf("guard parse: %v", err)
	}
	assertDiagnostic(t, result, CodeMarkerUnknown, SeverityError)
	if guard.maxReadEnd != 2 {
		t.Fatalf("fatal recovery read skipped bytes: max read end=%d", guard.maxReadEnd)
	}
}

// TypeMarkerForTestUnknown is kept outside the AMF0 marker range while making
// the boundary test more readable.
const TypeMarkerForTestUnknown = 0xfe

func parseAt(t testing.TB, base int, payload []byte, options Options) Result {
	t.Helper()
	data := make([]byte, base+len(payload)+3)
	copy(data[base:], payload)
	result, err := ParseSequence(bytes.NewReader(data), cursor.Span{
		ByteOffset: uint64(base), BitLength: uint32(len(payload) * 8),
	}, options)
	if err != nil {
		t.Fatalf("ParseSequence: %v", err)
	}
	return result
}

func appendShortString(output []byte, marker Type, value []byte) []byte {
	output = append(output, byte(marker))
	output = appendU16(output, uint16(len(value)))
	return append(output, value...)
}

func appendLongString(output []byte, marker Type, value []byte) []byte {
	output = append(output, byte(marker))
	output = appendU32(output, uint32(len(value)))
	return append(output, value...)
}

func appendProperty(output, name, value []byte) []byte {
	output = appendU16(output, uint16(len(name)))
	output = append(output, name...)
	return append(output, value...)
}

func appendU16(output []byte, value uint16) []byte {
	return binary.BigEndian.AppendUint16(output, value)
}

func appendU32(output []byte, value uint32) []byte {
	return binary.BigEndian.AppendUint32(output, value)
}

func appendU64(output []byte, value uint64) []byte {
	return binary.BigEndian.AppendUint64(output, value)
}

func assertDiagnostic(t testing.TB, result Result, code string, severity Severity) Diagnostic {
	t.Helper()
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Code == code {
			if diagnostic.Severity != severity {
				t.Fatalf("diagnostic %s severity=%s want=%s", code, diagnostic.Severity, severity)
			}
			return diagnostic
		}
	}
	t.Fatalf("missing diagnostic %s in %+v", code, result.Diagnostics)
	return Diagnostic{}
}

func assertSpan(t testing.TB, span cursor.Span, offset, length int) {
	t.Helper()
	if span.ByteOffset != uint64(offset) || span.BitOffset != 0 || span.BitLength != uint32(length*8) {
		t.Fatalf("span=%+v want offset=%d length=%d", span, offset, length)
	}
}

func assertAllSpansWithin(t testing.TB, result Result, base, length int) {
	t.Helper()
	start := uint64(base)
	end := start + uint64(length)
	check := func(name string, span cursor.Span) {
		if err := span.Validate(); err != nil {
			t.Fatalf("%s invalid: %+v: %v", name, span, err)
		}
		spanStart, spanEnd, err := span.ByteRange()
		if err != nil || spanStart < start || spanEnd > end {
			t.Fatalf("%s outside payload [%d,%d): %+v", name, start, end, span)
		}
	}
	check("result", result.Span)
	for _, node := range result.Nodes {
		check("node", node.Span)
		check("marker", node.MarkerSpan)
		if node.HasTerminator {
			check("terminator", node.TerminatorSpan)
		}
		switch node.Type {
		case TypeNumber:
			check("number", node.NumberSpan)
		case TypeBoolean:
			check("boolean", node.BooleanSpan)
		case TypeString:
			check("string length", node.StringLengthSpan)
			if node.StringLengthSpan.BitLength == 16 {
				check("string data", node.StringSpan)
			}
		case TypeLongString, TypeXMLDocument:
			check("long string length", node.StringLengthSpan)
			if node.StringLengthSpan.BitLength == 32 {
				check("long string data", node.StringSpan)
			}
		case TypeReference:
			check("reference", node.ReferenceSpan)
		case TypeECMAArray, TypeStrictArray:
			check("array count", node.CountSpan)
		case TypeDate:
			check("date milliseconds", node.DateMillisecondsSpan)
			if node.DateMillisecondsSpan.BitLength == 64 {
				check("date timezone", node.DateTimezoneSpan)
			}
		case TypeTypedObject:
			check("class length", node.ClassNameLengthSpan)
			if node.ClassNameLengthSpan.BitLength == 16 {
				check("class data", node.ClassNameSpan)
			}
		}
	}
	for _, property := range result.Properties {
		check("property", property.Span)
		check("property name length", property.NameLengthSpan)
		check("property name", property.NameSpan)
	}
}

type errorReaderAt struct{ err error }

func (r errorReaderAt) ReadAt([]byte, int64) (int, error) { return 0, r.err }

type fullReadEOFReaderAt struct{ reader io.ReaderAt }

func (r fullReadEOFReaderAt) ReadAt(destination []byte, offset int64) (int, error) {
	read, _ := r.reader.ReadAt(destination, offset)
	return read, io.EOF
}

type zeroReaderAt struct{}

func (zeroReaderAt) ReadAt(destination []byte, _ int64) (int, error) {
	clear(destination)
	return len(destination), nil
}

type guardReaderAt struct {
	data         []byte
	allowedStart int64
	allowedEnd   int64
	maxReadEnd   int64
}

func (r *guardReaderAt) ReadAt(destination []byte, offset int64) (int, error) {
	end := offset + int64(len(destination))
	if offset < r.allowedStart || end > r.allowedEnd {
		return 0, errors.New("read crossed guarded payload")
	}
	if end > r.maxReadEnd {
		r.maxReadEnd = end
	}
	copy(destination, r.data[offset:end])
	return len(destination), nil
}

func TestParseDeterministic(t *testing.T) {
	payload := []byte{byte(TypeECMAArray), 0, 0, 0, 1}
	payload = appendProperty(payload, []byte("duration"), appendU64([]byte{byte(TypeNumber)}, math.Float64bits(1.25)))
	payload = append(payload, 0, 0, byte(TypeObjectEnd))
	first := parseAt(t, 4, payload, Options{})
	for iteration := 0; iteration < 3; iteration++ {
		if next := parseAt(t, 4, payload, Options{}); !reflect.DeepEqual(first, next) {
			t.Fatalf("parse changed on iteration %d\nfirst=%+v\nnext=%+v", iteration, first, next)
		}
	}
}

var benchmarkAMF0Result Result

func BenchmarkParseSequenceMetadata(b *testing.B) {
	const propertyCount = 1_000
	payload := appendShortString(nil, TypeString, []byte("onMetaData"))
	payload = append(payload, byte(TypeECMAArray))
	payload = appendU32(payload, propertyCount)
	for index := 0; index < propertyCount; index++ {
		value := appendU64([]byte{byte(TypeNumber)}, math.Float64bits(float64(index)))
		payload = appendProperty(payload, []byte("duration"), value)
	}
	payload = append(payload, 0, 0, byte(TypeObjectEnd))
	span := cursor.Span{BitLength: uint32(len(payload) * 8)}

	b.SetBytes(int64(len(payload)))
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		result, err := ParseSequence(bytes.NewReader(payload), span, Options{})
		if err != nil {
			b.Fatal(err)
		}
		if !result.Complete || len(result.Nodes) != propertyCount+2 {
			b.Fatalf("unexpected result: complete=%v nodes=%d", result.Complete, len(result.Nodes))
		}
		benchmarkAMF0Result = result
	}
}

func BenchmarkParseSequenceHardNodeBudget(b *testing.B) {
	payload := bytes.Repeat([]byte{byte(TypeNull)}, int(HardMaxNodes))
	span := cursor.Span{BitLength: uint32(len(payload) * 8)}
	b.SetBytes(int64(len(payload)))
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		result, err := ParseSequence(bytes.NewReader(payload), span, Options{MaxNodes: HardMaxNodes})
		if err != nil {
			b.Fatal(err)
		}
		if !result.Complete || len(result.Nodes) != int(HardMaxNodes) {
			b.Fatalf("unexpected hard-budget result: complete=%v nodes=%d", result.Complete, len(result.Nodes))
		}
		benchmarkAMF0Result = result
	}
}

func BenchmarkParseSequenceHardPropertyBudget(b *testing.B) {
	payload := make([]byte, 1, 1+(int(HardMaxNodes)-1)*3+3)
	payload[0] = byte(TypeObject)
	for range int(HardMaxNodes) - 1 {
		payload = append(payload, 0, 0, byte(TypeNull))
	}
	payload = append(payload, 0, 0, byte(TypeObjectEnd))
	span := cursor.Span{BitLength: uint32(len(payload) * 8)}
	b.SetBytes(int64(len(payload)))
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		result, err := ParseSequence(bytes.NewReader(payload), span, Options{
			MaxNodes: HardMaxNodes, MaxContainerEntries: HardMaxContainerEntries,
		})
		if err != nil {
			b.Fatal(err)
		}
		if !result.Complete || len(result.Nodes) != int(HardMaxNodes) || len(result.Properties) != int(HardMaxNodes)-1 {
			b.Fatalf("unexpected hard-property result: complete=%v nodes=%d properties=%d", result.Complete, len(result.Nodes), len(result.Properties))
		}
		benchmarkAMF0Result = result
	}
}
