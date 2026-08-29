package report

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/sevico/muxray/internal/amf0"
	"github.com/sevico/muxray/internal/flv"
)

func TestScriptTagDetailGoldenAndDeterminism(t *testing.T) {
	payload := appendAMF0String(nil, []byte("onMetaData"))
	payload = append(payload, byte(amf0.TypeECMAArray), 0, 0, 0, 2)
	payload = appendAMF0PropertyName(payload, []byte("duration"))
	payload = appendAMF0Number(payload, math.Float64bits(1.5))
	payload = appendAMF0PropertyName(payload, []byte("live"))
	payload = append(payload, byte(amf0.TypeBoolean), 1)
	payload = append(payload, 0, 0, byte(amf0.TypeObjectEnd))

	data, tag, detail := parseScriptTagDetail(t, payload)
	document := NewScriptTagDetail(uint64(len(data)), tag, detail)

	if document.SchemaVersion != 1 || document.DocumentType != TagDetailDocumentType {
		t.Fatalf("unexpected document identity: %+v", document)
	}
	if document.Detail.EventName == nil || *document.Detail.EventName != "onMetaData" {
		t.Fatalf("event name = %v", document.Detail.EventName)
	}
	if !document.Detail.Complete || document.Detail.BytesConsumed != uint64(len(payload)) {
		t.Fatalf("unexpected completion: %+v", document.Detail)
	}
	if len(document.Detail.Roots) != 2 || len(document.Detail.Nodes) != 4 || len(document.Detail.Properties) != 2 {
		t.Fatalf("unexpected flat arena sizes: roots=%v nodes=%d properties=%d", document.Detail.Roots, len(document.Detail.Nodes), len(document.Detail.Properties))
	}
	root := document.Detail.Nodes[1]
	if root.TypeName != "ecmaArray" || root.TerminatorSpan == nil || root.TerminatorSpan.ByteOffset != 69 || root.TerminatorSpan.BitLength != 24 {
		t.Fatalf("unexpected ECMA Array node: %+v", root)
	}
	if got := document.Detail.Properties[0].Name.Span; got.ByteOffset != 44 || got.BitLength != 64 {
		t.Fatalf("unexpected property-name span: %+v", got)
	}
	if got := document.Detail.Nodes[2].Number; got == nil || got.Bits != "3ff8000000000000" || got.Value == nil || *got.Value != 1.5 || got.Span.ByteOffset != 53 {
		t.Fatalf("unexpected number mapping: %+v", got)
	}

	var output bytes.Buffer
	if err := WriteTagDetailJSON(&output, document, true); err != nil {
		t.Fatalf("WriteTagDetailJSON: %v", err)
	}
	goldenPath := filepath.Join("..", "..", "testdata", "golden", "flv-amf0-detail-v1.json")
	expected, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read Golden: %v", err)
	}
	if !bytes.Equal(output.Bytes(), expected) {
		t.Fatalf("Golden mismatch\n--- got ---\n%s\n--- want ---\n%s", output.Bytes(), expected)
	}

	for iteration := 0; iteration < 3; iteration++ {
		var repeated bytes.Buffer
		if err := WriteTagDetailJSON(&repeated, document, true); err != nil {
			t.Fatalf("repeat WriteTagDetailJSON: %v", err)
		}
		if !bytes.Equal(repeated.Bytes(), output.Bytes()) {
			t.Fatalf("output changed on iteration %d", iteration)
		}
	}

	var compact bytes.Buffer
	if err := WriteTagDetailJSON(&compact, document, false); err != nil {
		t.Fatalf("compact WriteTagDetailJSON: %v", err)
	}
	var decoded TagDetailDocument
	if err := json.Unmarshal(compact.Bytes(), &decoded); err != nil {
		t.Fatalf("decode compact document: %v", err)
	}
	var reencoded bytes.Buffer
	if err := WriteTagDetailJSON(&reencoded, decoded, false); err != nil {
		t.Fatalf("re-encode compact document: %v", err)
	}
	if !bytes.Equal(compact.Bytes(), reencoded.Bytes()) {
		t.Fatalf("materialized document changed after JSON round trip\nfirst=%s\nsecond=%s", compact.Bytes(), reencoded.Bytes())
	}
}

func TestTagDetailIEEE754ClassesAndNegativeZero(t *testing.T) {
	payload := appendAMF0String(nil, []byte("event"))
	for _, bits := range []uint64{
		0x8000000000000000,
		0x7ff8000000000001,
		0x7ff0000000000000,
		0xfff0000000000000,
	} {
		payload = appendAMF0Number(payload, bits)
	}
	data, tag, detail := parseScriptTagDetail(t, payload)
	document := NewScriptTagDetail(uint64(len(data)), tag, detail)

	wantClasses := []string{"finite", "nan", "positiveInfinity", "negativeInfinity"}
	wantBits := []string{"8000000000000000", "7ff8000000000001", "7ff0000000000000", "fff0000000000000"}
	for index, wantClass := range wantClasses {
		number := document.Detail.Nodes[index+1].Number
		if number == nil || number.Class != wantClass || number.Bits != wantBits[index] {
			t.Fatalf("number %d = %+v, want class=%s bits=%s", index, number, wantClass, wantBits[index])
		}
		if index == 0 {
			if number.Value == nil || *number.Value != 0 || !math.Signbit(*number.Value) {
				t.Fatalf("negative zero was not preserved: %+v", number)
			}
		} else if number.Value != nil {
			t.Fatalf("non-finite value %d unexpectedly has JSON number: %+v", index, number)
		}
	}

	var output bytes.Buffer
	if err := WriteTagDetailJSON(&output, document, false); err != nil {
		t.Fatalf("WriteTagDetailJSON: %v", err)
	}
	if !bytes.Contains(output.Bytes(), []byte(`"value":-0`)) {
		t.Fatalf("encoded report did not preserve negative zero: %s", output.Bytes())
	}
	if !json.Valid(output.Bytes()) {
		t.Fatalf("encoded report is not valid JSON: %s", output.Bytes())
	}
}

func TestTagDetailInvalidUTF8UsesJSONReplacement(t *testing.T) {
	payload := appendAMF0String(nil, []byte{0xff})
	payload = append(payload, byte(amf0.TypeNull))
	data, tag, detail := parseScriptTagDetail(t, payload)
	document := NewScriptTagDetail(uint64(len(data)), tag, detail)

	if len(document.Diagnostics) != 1 || document.Diagnostics[0].Code != amf0.CodeUTF8Invalid {
		t.Fatalf("unexpected diagnostics: %+v", document.Diagnostics)
	}
	var output bytes.Buffer
	if err := WriteTagDetailJSON(&output, document, false); err != nil {
		t.Fatalf("WriteTagDetailJSON: %v", err)
	}
	if !bytes.Contains(output.Bytes(), []byte("\ufffd")) {
		t.Fatalf("invalid UTF-8 was not replaced by encoding/json: %s", output.Bytes())
	}

	var decoded struct {
		Detail struct {
			EventName string `json:"eventName"`
		} `json:"detail"`
	}
	if err := json.Unmarshal(output.Bytes(), &decoded); err != nil {
		t.Fatalf("decode output: %v", err)
	}
	if decoded.Detail.EventName != "\ufffd" {
		t.Fatalf("decoded replacement = %q", decoded.Detail.EventName)
	}
}

func TestTagDetailDiagnosticRecoveryOffset(t *testing.T) {
	payload := appendAMF0String(nil, []byte("event"))
	payload = append(payload, 0xff, 0xaa, 0xbb)
	data, tag, detail := parseScriptTagDetail(t, payload)
	document := NewScriptTagDetail(uint64(len(data)), tag, detail)

	if document.Detail.Complete || document.Detail.BytesConsumed != 9 {
		t.Fatalf("unexpected fatal detail state: %+v", document.Detail)
	}
	if len(document.Diagnostics) == 0 || document.Diagnostics[0].RecoveryOffset == nil {
		t.Fatalf("missing recovery offset: %+v", document.Diagnostics)
	}
	wantRecovery := document.Detail.Span.ByteOffset + uint64(len(payload))
	if *document.Diagnostics[0].RecoveryOffset != wantRecovery {
		t.Fatalf("recovery offset = %d, want %d", *document.Diagnostics[0].RecoveryOffset, wantRecovery)
	}
}

func TestTagDetailOmitsUndecodedScalarFields(t *testing.T) {
	tests := []struct {
		name    string
		encoded []byte
		absent  func(AMF0Node) bool
	}{
		{name: "number", encoded: []byte{byte(amf0.TypeNumber), 0, 0}, absent: func(node AMF0Node) bool { return node.Number == nil }},
		{name: "boolean", encoded: []byte{byte(amf0.TypeBoolean)}, absent: func(node AMF0Node) bool { return node.Boolean == nil }},
		{name: "string length", encoded: []byte{byte(amf0.TypeString), 0}, absent: func(node AMF0Node) bool { return node.String == nil }},
		{name: "string data", encoded: []byte{byte(amf0.TypeString), 0, 3, 'x'}, absent: func(node AMF0Node) bool { return node.String == nil }},
		{name: "reference", encoded: []byte{byte(amf0.TypeReference), 0}, absent: func(node AMF0Node) bool { return node.Reference == nil }},
		{name: "date milliseconds", encoded: []byte{byte(amf0.TypeDate), 0}, absent: func(node AMF0Node) bool { return node.Date == nil }},
		{name: "date timezone", encoded: append(appendAMF0Number(nil, 0)[1:], 0), absent: func(node AMF0Node) bool { return node.Date == nil }},
		{name: "typed class length", encoded: []byte{byte(amf0.TypeTypedObject), 0}, absent: func(node AMF0Node) bool { return node.ClassName == nil }},
	}
	// Date uses its own marker followed by the same eight-byte IEEE payload as
	// Number; assemble that case without obscuring the table's intent.
	tests[6].encoded = append([]byte{byte(amf0.TypeDate)}, tests[6].encoded...)

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			payload := appendAMF0String(nil, []byte("event"))
			payload = append(payload, test.encoded...)
			data, tag, detail := parseScriptTagDetail(t, payload)
			document := NewScriptTagDetail(uint64(len(data)), tag, detail)
			if document.Detail.Complete || len(document.Detail.Nodes) != 2 {
				t.Fatalf("unexpected malformed detail: %+v", document.Detail)
			}
			node := document.Detail.Nodes[1]
			if node.Complete || !test.absent(node) {
				t.Fatalf("undecoded field was materialized: %+v", node)
			}
			if node.MarkerSpan.ByteOffset <= document.Detail.Span.ByteOffset {
				t.Fatalf("test did not exercise a nonzero absolute field offset: %+v", node.MarkerSpan)
			}
		})
	}
}

func TestTagDetailTruncatedEventNameIsAbsent(t *testing.T) {
	payload := []byte{byte(amf0.TypeString), 0}
	data, tag, detail := parseScriptTagDetail(t, payload)
	document := NewScriptTagDetail(uint64(len(data)), tag, detail)
	if document.Detail.EventName != nil || len(document.Detail.Nodes) != 1 {
		t.Fatalf("truncated event name was exposed: %+v", document.Detail)
	}
	node := document.Detail.Nodes[0]
	if node.Complete || node.String != nil || node.MarkerSpan.ByteOffset != document.Detail.Span.ByteOffset {
		t.Fatalf("unexpected partial event-name node: %+v", node)
	}
	var output bytes.Buffer
	if err := WriteTagDetailJSON(&output, document, true); err != nil {
		t.Fatalf("WriteTagDetailJSON: %v", err)
	}
	goldenPath := filepath.Join("..", "..", "testdata", "golden", "flv-amf0-detail-malformed-v1.json")
	expected, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read malformed Golden: %v", err)
	}
	if !bytes.Equal(output.Bytes(), expected) {
		t.Fatalf("malformed Golden mismatch\n--- got ---\n%s\n--- want ---\n%s", output.Bytes(), expected)
	}
}

func TestWriteTagDetailJSONPropagatesWriterFailure(t *testing.T) {
	want := errors.New("detail output failed")
	writer := &detailFailingWriter{remaining: 4, err: want}
	if err := WriteTagDetailJSON(writer, TagDetailDocument{}, false); !errors.Is(err, want) {
		t.Fatalf("WriteTagDetailJSON error = %v, want wrapped %v", err, want)
	}
}

func parseScriptTagDetail(t testing.TB, payload []byte) ([]byte, flv.TagRecord, flv.ScriptData) {
	return parseScriptTagDetailWithOptions(t, payload, amf0.Options{})
}

func parseScriptTagDetailWithOptions(t testing.TB, payload []byte, options amf0.Options) ([]byte, flv.TagRecord, flv.ScriptData) {
	t.Helper()
	data := makeScriptFLV(payload)
	scan, err := flv.Scan(bytes.NewReader(data), uint64(len(data)), flv.Options{})
	if err != nil {
		t.Fatalf("flv.Scan: %v", err)
	}
	if !scan.Complete || len(scan.Tags) != 1 {
		t.Fatalf("unexpected scan: complete=%v tags=%d diagnostics=%+v", scan.Complete, len(scan.Tags), scan.Diagnostics)
	}
	tag := scan.Tags[0]
	detail, err := flv.ParseScriptData(bytes.NewReader(data), tag, options)
	if err != nil {
		t.Fatalf("flv.ParseScriptData: %v", err)
	}
	return data, tag, detail
}

func makeScriptFLV(payload []byte) []byte {
	data := []byte{'F', 'L', 'V', 1, 0, 0, 0, 0, 9, 0, 0, 0, 0, byte(flv.TagTypeScript)}
	data = appendUint24(data, uint32(len(payload)))
	data = append(data, 0, 0, 0, 0, 0, 0, 0)
	data = append(data, payload...)
	var previous [4]byte
	binary.BigEndian.PutUint32(previous[:], uint32(flv.TagHeaderSize)+uint32(len(payload)))
	return append(data, previous[:]...)
}

func appendAMF0String(destination, value []byte) []byte {
	destination = append(destination, byte(amf0.TypeString), byte(len(value)>>8), byte(len(value)))
	return append(destination, value...)
}

func appendAMF0PropertyName(destination, value []byte) []byte {
	destination = append(destination, byte(len(value)>>8), byte(len(value)))
	return append(destination, value...)
}

func appendAMF0Number(destination []byte, bits uint64) []byte {
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], bits)
	destination = append(destination, byte(amf0.TypeNumber))
	return append(destination, encoded[:]...)
}

func appendUint24(destination []byte, value uint32) []byte {
	return append(destination, byte(value>>16), byte(value>>8), byte(value))
}

type detailFailingWriter struct {
	remaining int
	err       error
}

func (w *detailFailingWriter) Write(value []byte) (int, error) {
	if w.remaining <= 0 {
		return 0, w.err
	}
	if len(value) > w.remaining {
		written := w.remaining
		w.remaining = 0
		return written, w.err
	}
	w.remaining -= len(value)
	return len(value), nil
}

var _ io.Writer = (*detailFailingWriter)(nil)

var benchmarkTagDetailDocument TagDetailDocument

func BenchmarkNewScriptTagDetailHardNodeBudget(b *testing.B) {
	payload := appendAMF0String(nil, []byte("event"))
	payload = append(payload, bytes.Repeat([]byte{byte(amf0.TypeNull)}, int(amf0.HardMaxNodes)-1)...)
	data, tag, detail := parseScriptTagDetailWithOptions(b, payload, amf0.Options{MaxNodes: amf0.HardMaxNodes})
	if !detail.Complete || len(detail.AMF.Nodes) != int(amf0.HardMaxNodes) {
		b.Fatalf("unexpected hard-budget fixture: complete=%v nodes=%d", detail.Complete, len(detail.AMF.Nodes))
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		benchmarkTagDetailDocument = NewScriptTagDetail(uint64(len(data)), tag, detail)
	}
}

func BenchmarkNewScriptTagDetailHardPropertyBudget(b *testing.B) {
	payload := appendAMF0String(nil, []byte("event"))
	payload = append(payload, byte(amf0.TypeObject))
	// The event-name and Object root consume two node slots; each empty-name
	// Null property consumes one more. Empty names are legal unless immediately
	// followed by ObjectEnd.
	propertyCount := int(amf0.HardMaxNodes) - 2
	for range propertyCount {
		payload = append(payload, 0, 0, byte(amf0.TypeNull))
	}
	payload = append(payload, 0, 0, byte(amf0.TypeObjectEnd))
	data, tag, detail := parseScriptTagDetailWithOptions(b, payload, amf0.Options{
		MaxNodes: amf0.HardMaxNodes, MaxContainerEntries: amf0.HardMaxContainerEntries,
	})
	if !detail.Complete || len(detail.AMF.Nodes) != int(amf0.HardMaxNodes) || len(detail.AMF.Properties) != propertyCount {
		b.Fatalf("unexpected hard-property fixture: complete=%v nodes=%d properties=%d", detail.Complete, len(detail.AMF.Nodes), len(detail.AMF.Properties))
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		benchmarkTagDetailDocument = NewScriptTagDetail(uint64(len(data)), tag, detail)
	}
}
