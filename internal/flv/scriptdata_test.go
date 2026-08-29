package flv

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/sevico/muxray/internal/amf0"
	"github.com/sevico/muxray/internal/cursor"
)

func TestParseScriptDataOnMetaData(t *testing.T) {
	payload := append([]byte{byte(amf0.TypeString), 0, 10}, []byte("onMetaData")...)
	payload = append(payload, byte(amf0.TypeNull))
	data, tag := scriptDataFixture(t, byte(TagTypeScript), payload)

	detail, err := ParseScriptData(bytes.NewReader(data), tag, amf0.Options{})
	if err != nil {
		t.Fatalf("ParseScriptData: %v", err)
	}
	if !detail.Complete || len(detail.Diagnostics) != 0 {
		t.Fatalf("unexpected detail state: complete=%v diagnostics=%+v", detail.Complete, detail.Diagnostics)
	}
	if detail.TagID != tag.ID || detail.Span != tag.PayloadSpan() || detail.AMF.Span != tag.PayloadSpan() {
		t.Fatalf("detail identity/span mismatch: detail=%+v tag=%+v", detail, tag)
	}
	if detail.EventName != "onMetaData" || detail.EventNameNodeID != 0 || detail.ValueNodeID != 1 {
		t.Fatalf("unexpected script roots: name=%q nameID=%d valueID=%d", detail.EventName, detail.EventNameNodeID, detail.ValueNodeID)
	}
	if len(detail.AMF.Nodes) != 2 {
		t.Fatalf("got %d AMF nodes, want 2", len(detail.AMF.Nodes))
	}

	payloadOffset := tag.Offset + TagHeaderSize
	wantNameSpan := cursor.Span{ByteOffset: payloadOffset, BitLength: 13 * 8}
	if got := detail.AMF.Nodes[0].Span; got != wantNameSpan {
		t.Fatalf("event name span = %+v, want %+v", got, wantNameSpan)
	}
	wantStringSpan := cursor.Span{ByteOffset: payloadOffset + 3, BitLength: 10 * 8}
	if got := detail.AMF.Nodes[0].StringSpan; got != wantStringSpan {
		t.Fatalf("event name string span = %+v, want %+v", got, wantStringSpan)
	}
	wantValueSpan := cursor.Span{ByteOffset: payloadOffset + 13, BitLength: 8}
	if got := detail.AMF.Nodes[1].Span; got != wantValueSpan {
		t.Fatalf("value span = %+v, want %+v", got, wantValueSpan)
	}
}

func TestParseScriptDataDoesNotInventTruncatedEventName(t *testing.T) {
	payload := []byte{byte(amf0.TypeString), 0}
	data, tag := scriptDataFixture(t, byte(TagTypeScript), payload)
	detail, err := ParseScriptData(bytes.NewReader(data), tag, amf0.Options{})
	if err != nil {
		t.Fatalf("ParseScriptData: %v", err)
	}
	if detail.EventName != "" || detail.EventNameNodeID == amf0.NoNode || detail.AMF.Nodes[0].Complete {
		t.Fatalf("unexpected truncated event-name detail: %+v", detail)
	}
}

func TestParseScriptDataDiagnosticLimitPreservesParserFatal(t *testing.T) {
	payload := []byte{byte(amf0.TypeBoolean), 2, 0xff}
	data, tag := scriptDataFixture(t, byte(TagTypeScript), payload)
	detail, err := ParseScriptData(bytes.NewReader(data), tag, amf0.Options{MaxDiagnostics: 2})
	if err != nil {
		t.Fatalf("ParseScriptData: %v", err)
	}
	if detail.Complete || len(detail.Diagnostics) != 2 {
		t.Fatalf("unexpected limited diagnostics: %+v", detail.Diagnostics)
	}
	fatal := detail.Diagnostics[0]
	marker := detail.Diagnostics[1]
	wantRecovery := tag.PayloadSpan().ByteOffset + uint64(tag.DataSize)
	if fatal.Code != amf0.CodeMarkerUnknown || fatal.RecoveryOffset == nil || *fatal.RecoveryOffset != wantRecovery {
		t.Fatalf("parser fatal was not retained: %+v", fatal)
	}
	if marker.Code != amf0.CodeDiagnosticLimitReached || marker.RecoveryOffset == nil || *marker.RecoveryOffset != wantRecovery {
		t.Fatalf("diagnostic-limit marker lost recovery: %+v", marker)
	}
}

func TestParseScriptDataSemanticDiagnostics(t *testing.T) {
	stringRoot := append([]byte{byte(amf0.TypeString), 0, 3}, []byte("evt")...)
	numberThenNull := append([]byte{byte(amf0.TypeNumber)}, make([]byte, 8)...)
	numberThenNull = append(numberThenNull, byte(amf0.TypeNull))

	tests := []struct {
		name          string
		payload       []byte
		wantCodes     []string
		wantComplete  bool
		wantNameID    amf0.NodeID
		wantValueID   amf0.NodeID
		wantEventName string
	}{
		{
			name:         "both roots missing",
			payload:      nil,
			wantCodes:    []string{CodeScriptNameMissing, CodeScriptValueMissing},
			wantComplete: false,
			wantNameID:   amf0.NoNode,
			wantValueID:  amf0.NoNode,
		},
		{
			name:          "value missing",
			payload:       stringRoot,
			wantCodes:     []string{CodeScriptValueMissing},
			wantComplete:  false,
			wantNameID:    0,
			wantValueID:   amf0.NoNode,
			wantEventName: "evt",
		},
		{
			name:         "name is not string",
			payload:      numberThenNull,
			wantCodes:    []string{CodeScriptNameNotString},
			wantComplete: true,
			wantNameID:   0,
			wantValueID:  1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			data, tag := scriptDataFixture(t, byte(TagTypeScript), test.payload)
			detail, err := ParseScriptData(bytes.NewReader(data), tag, amf0.Options{})
			if err != nil {
				t.Fatalf("ParseScriptData: %v", err)
			}
			if detail.Complete != test.wantComplete {
				t.Fatalf("complete = %v, want %v; diagnostics=%+v", detail.Complete, test.wantComplete, detail.Diagnostics)
			}
			if detail.EventNameNodeID != test.wantNameID || detail.ValueNodeID != test.wantValueID || detail.EventName != test.wantEventName {
				t.Fatalf("unexpected roots/name: %+v", detail)
			}
			if got := scriptDiagnosticCodes(detail.Diagnostics); !equalStrings(got, test.wantCodes) {
				t.Fatalf("diagnostic codes = %v, want %v", got, test.wantCodes)
			}

			payloadEnd := tag.PayloadSpan().ByteOffset + uint64(tag.DataSize)
			for _, diagnostic := range detail.Diagnostics {
				switch diagnostic.Code {
				case CodeScriptNameMissing, CodeScriptValueMissing:
					if diagnostic.Severity != amf0.SeverityError || diagnostic.Offset != payloadEnd || diagnostic.RecoveryOffset == nil || *diagnostic.RecoveryOffset != payloadEnd {
						t.Fatalf("missing-root diagnostic does not recover at payload end %d: %+v", payloadEnd, diagnostic)
					}
				case CodeScriptNameNotString:
					if diagnostic.Severity != amf0.SeverityWarning || diagnostic.Offset != tag.PayloadSpan().ByteOffset {
						t.Fatalf("unexpected name-type diagnostic: %+v", diagnostic)
					}
				}
			}
		})
	}
}

func TestParseScriptDataRejectsInvalidInputs(t *testing.T) {
	_, validTag := scriptDataFixture(t, byte(TagTypeScript), []byte{byte(amf0.TypeNull), byte(amf0.TypeNull)})
	audioData, audioTag := scriptDataFixture(t, byte(TagTypeAudio), []byte{0})
	filteredData, filteredTag := scriptDataFixture(t, 0x20|byte(TagTypeScript), []byte{byte(amf0.TypeNull), byte(amf0.TypeNull)})

	full := makeFLV(t, 0, nil, fixtureTag{rawTypeFlags: byte(TagTypeScript), payload: []byte{1, 2}})
	truncatedData := full[:13+TagHeaderSize+1]
	truncatedScan, err := Scan(bytes.NewReader(truncatedData), uint64(len(truncatedData)), Options{})
	if err != nil {
		t.Fatalf("Scan truncated fixture: %v", err)
	}
	if len(truncatedScan.Tags) != 1 || truncatedScan.Tags[0].PayloadComplete() {
		t.Fatalf("fixture did not produce one incomplete tag: %+v", truncatedScan.Tags)
	}

	tests := []struct {
		name    string
		source  io.ReaderAt
		tag     TagRecord
		wantErr error
	}{
		{name: "nil source", source: nil, tag: validTag, wantErr: ErrScriptDataNilSource},
		{name: "non script tag", source: bytes.NewReader(audioData), tag: audioTag, wantErr: ErrScriptDataWrongTagType},
		{name: "filtered tag", source: bytes.NewReader(filteredData), tag: filteredTag, wantErr: ErrScriptDataFiltered},
		{name: "incomplete payload", source: bytes.NewReader(truncatedData), tag: truncatedScan.Tags[0], wantErr: ErrScriptDataPayloadIncomplete},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ParseScriptData(test.source, test.tag, amf0.Options{})
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("error = %v, want errors.Is(_, %v)", err, test.wantErr)
			}
		})
	}
}

func TestParseScriptDataReaderAtError(t *testing.T) {
	_, tag := scriptDataFixture(t, byte(TagTypeScript), []byte{byte(amf0.TypeNull), byte(amf0.TypeNull)})
	readErr := errors.New("script fixture read failed")
	detail, err := ParseScriptData(scriptFailingReaderAt{err: readErr}, tag, amf0.Options{})
	if !errors.Is(err, readErr) {
		t.Fatalf("error = %v, want wrapped reader error", err)
	}
	if detail.Complete || detail.TagID != tag.ID || detail.Span != tag.PayloadSpan() {
		t.Fatalf("partial detail lost identity or became complete: %+v", detail)
	}
	if detail.AMF.Complete {
		t.Fatalf("partial AMF result unexpectedly complete: %+v", detail.AMF)
	}
}

func TestMalformedScriptDataDoesNotAffectContainerScan(t *testing.T) {
	data := makeFLV(t, 0, nil,
		fixtureTag{rawTypeFlags: byte(TagTypeScript), payload: []byte{0xff}},
		fixtureTag{rawTypeFlags: byte(TagTypeAudio), payload: []byte{0xaf, 0x01}},
	)
	scan, err := Scan(bytes.NewReader(data), uint64(len(data)), Options{})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if !scan.Complete || len(scan.Tags) != 2 || scan.Tags[1].Type() != TagTypeAudio {
		t.Fatalf("container did not index the following tag: complete=%v tags=%+v diagnostics=%+v", scan.Complete, scan.Tags, scan.Diagnostics)
	}

	detail, err := ParseScriptData(bytes.NewReader(data), scan.Tags[0], amf0.Options{})
	if err != nil {
		t.Fatalf("ParseScriptData: %v", err)
	}
	if detail.Complete || !hasScriptDiagnostic(detail.Diagnostics, amf0.CodeMarkerUnknown) {
		t.Fatalf("malformed AMF detail was not isolated: %+v", detail)
	}
	payloadEnd := scan.Tags[0].PayloadSpan().ByteOffset + uint64(scan.Tags[0].DataSize)
	for _, diagnostic := range detail.Diagnostics {
		if diagnostic.Code == amf0.CodeMarkerUnknown && (diagnostic.RecoveryOffset == nil || *diagnostic.RecoveryOffset != payloadEnd) {
			t.Fatalf("AMF recovery offset = %v, want %d", diagnostic.RecoveryOffset, payloadEnd)
		}
	}
	if !scan.Complete || len(scan.Tags) != 2 || scan.Tags[1].ID != 1 {
		t.Fatalf("detail parsing changed the container result: %+v", scan)
	}
}

func TestParseScriptDataSemanticDiagnosticsRespectLimit(t *testing.T) {
	data, tag := scriptDataFixture(t, byte(TagTypeScript), []byte{0xff})
	detail, err := ParseScriptData(bytes.NewReader(data), tag, amf0.Options{MaxDiagnostics: 2})
	if err != nil {
		t.Fatalf("ParseScriptData: %v", err)
	}
	if len(detail.Diagnostics) != 2 {
		t.Fatalf("diagnostic count = %d, want hard limit 2: %+v", len(detail.Diagnostics), detail.Diagnostics)
	}
	if detail.Diagnostics[0].Code != amf0.CodeMarkerUnknown || detail.Diagnostics[1].Code != amf0.CodeDiagnosticLimitReached {
		t.Fatalf("unexpected bounded diagnostics: %+v", detail.Diagnostics)
	}
}

type scriptFailingReaderAt struct{ err error }

func (r scriptFailingReaderAt) ReadAt([]byte, int64) (int, error) { return 0, r.err }

func scriptDataFixture(t testing.TB, rawTypeFlags byte, payload []byte) ([]byte, TagRecord) {
	t.Helper()
	data := makeFLV(t, 0, nil, fixtureTag{rawTypeFlags: rawTypeFlags, payload: payload})
	scan, err := Scan(bytes.NewReader(data), uint64(len(data)), Options{})
	if err != nil {
		t.Fatalf("Scan fixture: %v", err)
	}
	if len(scan.Tags) != 1 {
		t.Fatalf("fixture has %d tags, want 1", len(scan.Tags))
	}
	return data, scan.Tags[0]
}

func scriptDiagnosticCodes(diagnostics []amf0.Diagnostic) []string {
	codes := make([]string, len(diagnostics))
	for index, diagnostic := range diagnostics {
		codes[index] = diagnostic.Code
	}
	return codes
}

func hasScriptDiagnostic(diagnostics []amf0.Diagnostic, code string) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == code {
			return true
		}
	}
	return false
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
