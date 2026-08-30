package report

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/sevico/muxray/internal/amf0"
	"github.com/sevico/muxray/internal/flv"
	"github.com/sevico/muxray/internal/mp3"
)

func TestMP3TagDetailIndexedAndDeterminism(t *testing.T) {
	data, tag, parsed := parseMP3TagDetail(t, []byte{0x2f, 0xff, 0xfb, 0x90, 0x64, 0xde, 0xad})
	document := NewMP3TagDetail(uint64(len(data)), tag, parsed)
	if document.SchemaVersion != MP3TagDetailSchemaVersion || document.DocumentType != MP3TagDetailDocumentType {
		t.Fatalf("unexpected MP3 envelope: %+v", document)
	}
	if !document.Detail.Complete || document.Detail.Kind != MP3AudioKind || document.Detail.BitsConsumed != 40 || len(document.Diagnostics) != 0 {
		t.Fatalf("unexpected MP3 detail state: %+v", document)
	}

	payloadOffset := tag.PayloadSpan().ByteOffset
	assertReportSpan(t, document.Detail.Span, payloadOffset, 0, 56)
	header := document.Detail.AudioTagHeader
	if header == nil || header.Raw != 0x2f || header.SoundFormat.Value != flv.SoundFormatMP3 || header.SoundFormat.Name != "MP3" ||
		header.SoundRate.Name != "44kHz" || header.SoundSize.Name != "16-bit" || header.SoundType.Name != "stereo" {
		t.Fatalf("unexpected FLV MP3 header: %+v", header)
	}
	assertReportSpan(t, header.Span, payloadOffset, 0, 8)
	assertReportSpan(t, header.SoundFormat.Span, payloadOffset, 0, 4)
	assertReportSpan(t, header.SoundRate.Span, payloadOffset, 4, 2)
	assertReportSpan(t, header.SoundSize.Span, payloadOffset, 6, 1)
	assertReportSpan(t, header.SoundType.Span, payloadOffset, 7, 1)
	if document.Detail.AudioDataSpan == nil {
		t.Fatal("MP3 SoundData span was omitted")
	}
	assertReportSpan(t, *document.Detail.AudioDataSpan, payloadOffset+1, 0, 48)

	frame := document.Detail.FirstFrameHeader
	if frame == nil || !frame.Complete || frame.Raw != 0xfffb9064 || frame.SyncWord.Value != 0x7ff ||
		frame.Version.Value != uint8(mp3.VersionMPEG1) || frame.Version.Name != "mpeg1" ||
		frame.Layer.Value != uint8(mp3.LayerIII) || frame.Layer.Name != "layerIII" ||
		frame.ProtectionBit.Value != 1 || frame.ProtectionBit.Name != "none" || frame.CRCProtected ||
		frame.BitrateIndex.Value != 9 || frame.BitrateMode != string(mp3.BitrateModeIndexed) || frame.BitrateKbps == nil || *frame.BitrateKbps != 128 ||
		frame.SamplingFrequencyIndex.Value != 0 || frame.SamplingFrequencyHz == nil || *frame.SamplingFrequencyHz != 44100 ||
		frame.Padding.Value || frame.Private.Value || frame.ChannelMode.Name != "jointStereo" || frame.ModeExtension.Value != 2 || frame.Channels != 2 ||
		frame.Copyright.Value || !frame.Original.Value || frame.Emphasis.Name != "none" ||
		frame.SamplesPerFrame == nil || *frame.SamplesPerFrame != 1152 || frame.FrameLengthBytes == nil || *frame.FrameLengthBytes != 417 {
		t.Fatalf("unexpected MPEG Audio frame header: %+v", frame)
	}
	assertReportSpan(t, frame.Span, payloadOffset+1, 0, 32)
	assertReportSpan(t, frame.SyncWord.Span, payloadOffset+1, 0, 11)
	assertReportSpan(t, frame.Version.Span, payloadOffset+2, 3, 2)
	assertReportSpan(t, frame.Layer.Span, payloadOffset+2, 5, 2)
	assertReportSpan(t, frame.BitrateIndex.Span, payloadOffset+3, 0, 4)
	assertReportSpan(t, frame.SamplingFrequencyIndex.Span, payloadOffset+3, 4, 2)
	assertReportSpan(t, frame.ChannelMode.Span, payloadOffset+4, 0, 2)
	assertReportSpan(t, frame.Emphasis.Span, payloadOffset+4, 6, 2)
	if document.Detail.UnparsedDataSpan == nil {
		t.Fatal("unparsed MP3 payload span was omitted")
	}
	assertReportSpan(t, *document.Detail.UnparsedDataSpan, payloadOffset+5, 0, 16)

	generic, err := NewTagDetail(uint64(len(data)), tag, parsed)
	if err != nil {
		t.Fatalf("NewTagDetail: %v", err)
	}
	if _, ok := generic.(MP3TagDetailDocument); !ok {
		t.Fatalf("generic document type = %T, want report.MP3TagDetailDocument", generic)
	}
	for _, pretty := range []bool{false, true} {
		var direct, routed bytes.Buffer
		if err := WriteMP3TagDetailJSON(&direct, document, pretty); err != nil {
			t.Fatalf("WriteMP3TagDetailJSON: %v", err)
		}
		if err := WriteParsedTagDetailJSON(&routed, generic, pretty); err != nil {
			t.Fatalf("WriteParsedTagDetailJSON: %v", err)
		}
		if !bytes.Equal(direct.Bytes(), routed.Bytes()) {
			t.Fatalf("generic MP3 encoding differs (pretty=%v)\ndirect:  %s\ngeneric: %s", pretty, direct.Bytes(), routed.Bytes())
		}
		var repeated bytes.Buffer
		if err := WriteMP3TagDetailJSON(&repeated, document, pretty); err != nil {
			t.Fatalf("repeat WriteMP3TagDetailJSON: %v", err)
		}
		if !bytes.Equal(direct.Bytes(), repeated.Bytes()) {
			t.Fatalf("MP3 encoding is not deterministic (pretty=%v)", pretty)
		}
	}
}

func TestMP3TagDetailGoldens(t *testing.T) {
	for _, fixture := range []struct {
		name       string
		payload    []byte
		goldenName string
	}{
		{name: "indexed", payload: []byte{0x2f, 0xff, 0xfb, 0x90, 0x64, 0xde, 0xad}, goldenName: "flv-mp3-detail-v1.json"},
		{name: "free format", payload: []byte{0x2f, 0xff, 0xfb, 0x00, 0x64, 0xde}, goldenName: "flv-mp3-detail-free-v1.json"},
		{name: "malformed", payload: []byte{0x2f, 0xfe, 0xfb, 0x90, 0x64, 0xde}, goldenName: "flv-mp3-detail-malformed-v1.json"},
		{name: "truncated", payload: []byte{0x2f, 0xff, 0xfb, 0x90}, goldenName: "flv-mp3-detail-truncated-v1.json"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			data, tag, parsed := parseMP3TagDetail(t, fixture.payload)
			document := NewMP3TagDetail(uint64(len(data)), tag, parsed)
			var output bytes.Buffer
			if err := WriteMP3TagDetailJSON(&output, document, true); err != nil {
				t.Fatalf("WriteMP3TagDetailJSON: %v", err)
			}
			goldenPath := filepath.Join("..", "..", "testdata", "golden", fixture.goldenName)
			if os.Getenv("MUXRAY_UPDATE_GOLDEN") == "1" {
				if err := os.WriteFile(goldenPath, output.Bytes(), 0o600); err != nil {
					t.Fatalf("update golden %s: %v", goldenPath, err)
				}
			}
			golden, err := os.ReadFile(goldenPath)
			if err != nil {
				t.Fatalf("ReadFile(%s): %v\nactual:\n%s", goldenPath, err, output.Bytes())
			}
			if !bytes.Equal(output.Bytes(), golden) {
				t.Fatalf("MP3 detail differs from %s\nactual:\n%s", fixture.goldenName, output.Bytes())
			}
		})
	}
}

func TestMP3TagDetailFreeFormatOmitsUnresolvedDerivations(t *testing.T) {
	data, tag, parsed := parseMP3TagDetail(t, []byte{0x2f, 0xff, 0xfb, 0x00, 0x64})
	document := NewMP3TagDetail(uint64(len(data)), tag, parsed)
	frame := document.Detail.FirstFrameHeader
	diagnostic := findMP3ReportDiagnostic(document.Diagnostics, mp3.CodeFreeFormatUnresolved)
	if !document.Detail.Complete || frame == nil || !frame.Complete || frame.BitrateMode != string(mp3.BitrateModeFree) ||
		frame.BitrateKbps != nil || frame.FrameLengthBytes != nil || diagnostic == nil || diagnostic.Severity != amf0.SeverityWarning ||
		diagnostic.RecoveryOffset != nil {
		t.Fatalf("unexpected free-format report: %+v", document)
	}
}

func TestMP3TagDetailMalformedAndTruncatedRecovery(t *testing.T) {
	for _, fixture := range []struct {
		name              string
		payload           []byte
		code              string
		wantFrameHeader   bool
		wantBitsConsumed  uint64
		wantSoundDataBits uint32
	}{
		{name: "malformed", payload: []byte{0x2f, 0xfe, 0xfb, 0x90, 0x64}, code: mp3.CodeSyncInvalid, wantFrameHeader: true, wantBitsConsumed: 40, wantSoundDataBits: 32},
		{name: "truncated", payload: []byte{0x2f, 0xff, 0xfb, 0x90}, code: mp3.CodeFrameHeaderTruncated, wantBitsConsumed: 8, wantSoundDataBits: 24},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			data, tag, parsed := parseMP3TagDetail(t, fixture.payload)
			document := NewMP3TagDetail(uint64(len(data)), tag, parsed)
			diagnostic := findMP3ReportDiagnostic(document.Diagnostics, fixture.code)
			payloadEnd := tag.PayloadSpan().ByteOffset + uint64(tag.DataSize)
			if document.Detail.Complete || (document.Detail.FirstFrameHeader != nil) != fixture.wantFrameHeader ||
				document.Detail.BitsConsumed != fixture.wantBitsConsumed || document.Detail.AudioDataSpan == nil ||
				document.Detail.AudioDataSpan.BitLength != fixture.wantSoundDataBits || diagnostic == nil || diagnostic.Severity != amf0.SeverityError ||
				diagnostic.RecoveryOffset == nil || *diagnostic.RecoveryOffset != payloadEnd {
				t.Fatalf("malformed MP3 report lost fields or recovery: %+v", document)
			}
		})
	}
}

func TestMP3TagDetailJSONRoundTrip(t *testing.T) {
	data, tag, parsed := parseMP3TagDetail(t, []byte{0x2f, 0xff, 0xfb, 0x90, 0x64, 0xde})
	document := NewMP3TagDetail(uint64(len(data)), tag, parsed)
	var encoded bytes.Buffer
	if err := WriteMP3TagDetailJSON(&encoded, document, false); err != nil {
		t.Fatalf("WriteMP3TagDetailJSON: %v", err)
	}
	var decoded MP3TagDetailDocument
	if err := json.Unmarshal(encoded.Bytes(), &decoded); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	var reencoded bytes.Buffer
	if err := WriteMP3TagDetailJSON(&reencoded, decoded, false); err != nil {
		t.Fatalf("re-encode: %v", err)
	}
	if !bytes.Equal(encoded.Bytes(), reencoded.Bytes()) {
		t.Fatalf("MP3 round trip changed bytes\nfirst:  %s\nsecond: %s", encoded.Bytes(), reencoded.Bytes())
	}
}

func TestMP3DiagnosticConstantsMatchSchemaEnum(t *testing.T) {
	schemaPath := filepath.Join("..", "..", "docs", "schemas", "flv-mp3-tag-detail-v1.schema.json")
	data, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", schemaPath, err)
	}
	var schema map[string]any
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatalf("json.Unmarshal schema: %v", err)
	}
	definitions := schema["$defs"].(map[string]any)
	diagnostic := definitions["diagnostic"].(map[string]any)
	properties := diagnostic["properties"].(map[string]any)
	code := properties["code"].(map[string]any)
	values := code["enum"].([]any)

	want := []string{
		flv.CodeMP3AudioHeaderTruncated,
		mp3.CodeFrameHeaderTruncated,
		mp3.CodeSyncInvalid,
		mp3.CodeVersionReserved,
		mp3.CodeLayerReserved,
		mp3.CodeLayerNotIII,
		mp3.CodeBitrateIndexInvalid,
		mp3.CodeSamplingFrequencyReserved,
		mp3.CodeFreeFormatUnresolved,
		mp3.CodeEmphasisReserved,
		mp3.CodeDiagnosticLimitReached,
	}
	got := make(map[string]bool, len(values))
	for _, value := range values {
		got[value.(string)] = true
	}
	if len(got) != len(want) {
		t.Fatalf("schema diagnostic count = %d, Go constant count = %d", len(got), len(want))
	}
	for _, value := range want {
		if !got[value] {
			t.Errorf("schema diagnostic enum lacks %q", value)
		}
	}
}

func TestWriteMP3TagDetailJSONPropagatesWriterFailure(t *testing.T) {
	data, tag, parsed := parseMP3TagDetail(t, []byte{0x2f, 0xff, 0xfb, 0x90, 0x64})
	direct := NewMP3TagDetail(uint64(len(data)), tag, parsed)
	generic, err := NewTagDetail(uint64(len(data)), tag, parsed)
	if err != nil {
		t.Fatalf("NewTagDetail: %v", err)
	}
	want := errors.New("MP3 detail output failed")
	for _, test := range []struct {
		name  string
		write func(*detailFailingWriter) error
	}{
		{name: "direct", write: func(writer *detailFailingWriter) error { return WriteMP3TagDetailJSON(writer, direct, false) }},
		{name: "generic", write: func(writer *detailFailingWriter) error { return WriteParsedTagDetailJSON(writer, generic, false) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			writer := &detailFailingWriter{remaining: 4, err: want}
			if err := test.write(writer); !errors.Is(err, want) {
				t.Fatalf("writer error = %v, want %v", err, want)
			}
		})
	}
}

func parseMP3TagDetail(t testing.TB, payload []byte) ([]byte, flv.TagRecord, flv.MP3Audio) {
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
	detail, err := flv.ParseMP3Audio(bytes.NewReader(data), tag, mp3.Options{})
	if err != nil {
		t.Fatalf("flv.ParseMP3Audio: %v", err)
	}
	return data, tag, detail
}

func findMP3ReportDiagnostic(items []TagDetailDiagnostic, code string) *TagDetailDiagnostic {
	for index := range items {
		if items[index].Code == code {
			return &items[index]
		}
	}
	return nil
}

func assertReportSpan(t testing.TB, got Span, byteOffset uint64, bitOffset uint8, bitLength uint32) {
	t.Helper()
	want := Span{ByteOffset: byteOffset, BitOffset: bitOffset, BitLength: bitLength}
	if got != want {
		t.Fatalf("span = %+v, want %+v", got, want)
	}
}
