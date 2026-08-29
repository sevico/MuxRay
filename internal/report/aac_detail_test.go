package report

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/sevico/muxray/internal/aac"
	"github.com/sevico/muxray/internal/amf0"
	"github.com/sevico/muxray/internal/flv"
)

func TestNewTagDetailPreservesScriptDataEncoding(t *testing.T) {
	payload := appendAMF0String(nil, []byte("onMetaData"))
	payload = append(payload, byte(amf0.TypeNull))
	data, tag, parsed := parseScriptTagDetail(t, payload)

	direct := NewScriptTagDetail(uint64(len(data)), tag, parsed)
	generic, err := NewTagDetail(uint64(len(data)), tag, parsed)
	if err != nil {
		t.Fatalf("NewTagDetail: %v", err)
	}
	if _, ok := generic.(TagDetailDocument); !ok {
		t.Fatalf("generic document type = %T, want report.TagDetailDocument", generic)
	}

	for _, pretty := range []bool{false, true} {
		var directOutput, genericOutput bytes.Buffer
		if err := WriteTagDetailJSON(&directOutput, direct, pretty); err != nil {
			t.Fatalf("WriteTagDetailJSON: %v", err)
		}
		if err := WriteParsedTagDetailJSON(&genericOutput, generic, pretty); err != nil {
			t.Fatalf("WriteParsedTagDetailJSON: %v", err)
		}
		if !bytes.Equal(directOutput.Bytes(), genericOutput.Bytes()) {
			t.Fatalf("generic ScriptData output changed existing encoding (pretty=%v)\ndirect:  %s\ngeneric: %s", pretty, directOutput.Bytes(), genericOutput.Bytes())
		}
	}
}

func TestAACTagDetailSequenceHeaderAndDeterminism(t *testing.T) {
	data, tag, parsed := parseAACTagDetail(t, []byte{0xaf, 0x00, 0x12, 0x10})
	document := NewAACTagDetail(uint64(len(data)), tag, parsed)
	if document.SchemaVersion != 1 || document.DocumentType != AACTagDetailDocumentType {
		t.Fatalf("unexpected AAC envelope: %+v", document)
	}
	if !document.Detail.Complete || document.Detail.Kind != AACAudioKind || document.Detail.AudioTagHeader == nil || document.Detail.PacketType == nil || document.Detail.AudioSpecificConfig == nil {
		t.Fatalf("unexpected AAC detail: %+v", document.Detail)
	}
	config := document.Detail.AudioSpecificConfig
	if config.AudioObjectType == nil || config.AudioObjectType.Value == nil || *config.AudioObjectType.Value != 2 {
		t.Fatalf("unexpected object type: %+v", config.AudioObjectType)
	}
	if config.SamplingFrequency == nil || config.SamplingFrequency.Hertz == nil || *config.SamplingFrequency.Hertz != 44100 {
		t.Fatalf("unexpected sampling frequency: %+v", config.SamplingFrequency)
	}
	if config.ChannelConfiguration == nil || config.ChannelConfiguration.ChannelCount == nil || *config.ChannelConfiguration.ChannelCount != 2 {
		t.Fatalf("unexpected channel configuration: %+v", config.ChannelConfiguration)
	}
	if config.GASpecific == nil || !config.GASpecific.Complete || len(document.Diagnostics) != 0 {
		t.Fatalf("unexpected GA/diagnostic state: ga=%+v diagnostics=%+v", config.GASpecific, document.Diagnostics)
	}

	generic, err := NewTagDetail(uint64(len(data)), tag, parsed)
	if err != nil {
		t.Fatalf("NewTagDetail: %v", err)
	}
	if _, ok := generic.(AACTagDetailDocument); !ok {
		t.Fatalf("generic document type = %T, want report.AACTagDetailDocument", generic)
	}
	for _, pretty := range []bool{false, true} {
		var first, second bytes.Buffer
		if err := WriteAACTagDetailJSON(&first, document, pretty); err != nil {
			t.Fatalf("WriteAACTagDetailJSON: %v", err)
		}
		if err := WriteParsedTagDetailJSON(&second, generic, pretty); err != nil {
			t.Fatalf("WriteParsedTagDetailJSON: %v", err)
		}
		if !bytes.Equal(first.Bytes(), second.Bytes()) {
			t.Fatalf("generic AAC encoding differs (pretty=%v)\nspecial: %s\ngeneric: %s", pretty, first.Bytes(), second.Bytes())
		}
		var repeated bytes.Buffer
		if err := WriteAACTagDetailJSON(&repeated, document, pretty); err != nil {
			t.Fatalf("repeat WriteAACTagDetailJSON: %v", err)
		}
		if !bytes.Equal(first.Bytes(), repeated.Bytes()) {
			t.Fatalf("AAC encoding is not deterministic (pretty=%v)", pretty)
		}
	}
}

func TestAACTagDetailGoldens(t *testing.T) {
	for _, fixture := range []struct {
		name       string
		payload    []byte
		goldenName string
	}{
		{name: "sequence header", payload: []byte{0xaf, 0x00, 0x12, 0x10}, goldenName: "flv-aac-detail-v1.json"},
		{name: "raw", payload: []byte{0xaf, 0x01, 0xde, 0xad}, goldenName: "flv-aac-detail-raw-v1.json"},
		{name: "malformed", payload: []byte{0xaf, 0x00, 0x12}, goldenName: "flv-aac-detail-malformed-v1.json"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			data, tag, parsed := parseAACTagDetail(t, fixture.payload)
			document := NewAACTagDetail(uint64(len(data)), tag, parsed)
			var output bytes.Buffer
			if err := WriteAACTagDetailJSON(&output, document, true); err != nil {
				t.Fatalf("WriteAACTagDetailJSON: %v", err)
			}
			goldenPath := filepath.Join("..", "..", "testdata", "golden", fixture.goldenName)
			golden, err := os.ReadFile(goldenPath)
			if err != nil {
				t.Fatalf("ReadFile(%s): %v", goldenPath, err)
			}
			if !bytes.Equal(output.Bytes(), golden) {
				t.Fatalf("AAC detail differs from %s\nactual:\n%s", fixture.goldenName, output.Bytes())
			}
		})
	}
}

func TestAACTagDetailRawPacketHasOnlyRawSpan(t *testing.T) {
	data, tag, parsed := parseAACTagDetail(t, []byte{0xaf, 0x01, 0xde, 0xad})
	document := NewAACTagDetail(uint64(len(data)), tag, parsed)
	if !document.Detail.Complete || document.Detail.RawDataSpan == nil || document.Detail.AudioSpecificConfig != nil {
		t.Fatalf("unexpected raw AAC report: %+v", document.Detail)
	}
	if document.Detail.RawDataSpan.BitLength != 16 || document.Detail.DataSpan == nil || *document.Detail.RawDataSpan != *document.Detail.DataSpan {
		t.Fatalf("raw/data spans differ: raw=%+v data=%+v", document.Detail.RawDataSpan, document.Detail.DataSpan)
	}
}

func TestAACTagDetailMalformedASCMapsPartialSyntaxAndRecovery(t *testing.T) {
	data, tag, parsed := parseAACTagDetail(t, []byte{0xaf, 0x00, 0x12})
	document := NewAACTagDetail(uint64(len(data)), tag, parsed)
	if document.Detail.Complete || document.Detail.AudioSpecificConfig == nil || len(document.Diagnostics) == 0 {
		t.Fatalf("malformed ASC lost partial report: %+v", document)
	}
	config := document.Detail.AudioSpecificConfig
	if config.InitialAudioObjectType == nil || config.SamplingFrequency != nil || config.ChannelConfiguration != nil {
		t.Fatalf("unexpected partial syntax fields: %+v", config)
	}
	payloadEnd := tag.PayloadSpan().ByteOffset + uint64(tag.DataSize)
	for _, diagnostic := range document.Diagnostics {
		if diagnostic.Severity == amf0.SeverityError && (diagnostic.RecoveryOffset == nil || *diagnostic.RecoveryOffset != payloadEnd) {
			t.Fatalf("recovery offset = %v, want %d: %+v", diagnostic.RecoveryOffset, payloadEnd, diagnostic)
		}
	}
}

func TestAACTagDetailJSONRoundTrip(t *testing.T) {
	data, tag, parsed := parseAACTagDetail(t, []byte{0xaf, 0x00, 0x12, 0x10})
	document := NewAACTagDetail(uint64(len(data)), tag, parsed)
	var encoded bytes.Buffer
	if err := WriteAACTagDetailJSON(&encoded, document, false); err != nil {
		t.Fatalf("WriteAACTagDetailJSON: %v", err)
	}
	var decoded AACTagDetailDocument
	if err := json.Unmarshal(encoded.Bytes(), &decoded); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	var reencoded bytes.Buffer
	if err := WriteAACTagDetailJSON(&reencoded, decoded, false); err != nil {
		t.Fatalf("re-encode: %v", err)
	}
	if !bytes.Equal(encoded.Bytes(), reencoded.Bytes()) {
		t.Fatalf("AAC round trip changed bytes\nfirst:  %s\nsecond: %s", encoded.Bytes(), reencoded.Bytes())
	}
}

func TestWriteParsedTagDetailJSONPropagatesWriterFailure(t *testing.T) {
	data, tag, parsed := parseAACTagDetail(t, []byte{0xaf, 0x01})
	document, err := NewTagDetail(uint64(len(data)), tag, parsed)
	if err != nil {
		t.Fatalf("NewTagDetail: %v", err)
	}
	want := errors.New("AAC detail output failed")
	writer := &detailFailingWriter{remaining: 4, err: want}
	if err := WriteParsedTagDetailJSON(writer, document, false); !errors.Is(err, want) {
		t.Fatalf("WriteParsedTagDetailJSON error = %v, want %v", err, want)
	}
}

func parseAACTagDetail(t testing.TB, payload []byte) ([]byte, flv.TagRecord, flv.AACAudio) {
	t.Helper()
	data := makeAudioFLV(payload)
	scan, err := flv.Scan(bytes.NewReader(data), uint64(len(data)), flv.Options{})
	if err != nil {
		t.Fatalf("flv.Scan: %v", err)
	}
	if !scan.Complete || len(scan.Tags) != 1 {
		t.Fatalf("unexpected scan: complete=%v tags=%d diagnostics=%+v", scan.Complete, len(scan.Tags), scan.Diagnostics)
	}
	tag := scan.Tags[0]
	detail, err := flv.ParseAACAudio(bytes.NewReader(data), tag, aac.Options{})
	if err != nil {
		t.Fatalf("flv.ParseAACAudio: %v", err)
	}
	return data, tag, detail
}

func makeAudioFLV(payload []byte) []byte {
	data := []byte{'F', 'L', 'V', 1, 4, 0, 0, 0, 9, 0, 0, 0, 0, byte(flv.TagTypeAudio)}
	data = appendUint24(data, uint32(len(payload)))
	data = append(data, 0, 0, 0, 0, 0, 0, 0)
	data = append(data, payload...)
	var previous [4]byte
	binary.BigEndian.PutUint32(previous[:], uint32(flv.TagHeaderSize)+uint32(len(payload)))
	return append(data, previous[:]...)
}
