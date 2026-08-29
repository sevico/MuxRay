package report

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/sevico/muxray/internal/avc"
	"github.com/sevico/muxray/internal/flv"
)

const avcCHigh320x180Hex = "0164000cffe1001a6764000cacd941419f9f011000000300100000030320f142996001000668ebe3cb22c0fdf8f800"

func TestAVCTagDetailSequenceHeaderAndDeterminism(t *testing.T) {
	payload := append([]byte{0x17, 0x00, 0x00, 0x00, 0x00}, mustDecodeHex(t, avcCHigh320x180Hex)...)
	data, tag, parsed := parseAVCTagDetail(t, payload, 0)
	document := NewAVCTagDetail(uint64(len(data)), tag, parsed)
	if document.SchemaVersion != 1 || document.DocumentType != AVCTagDetailDocumentType {
		t.Fatalf("unexpected AVC envelope: %+v", document)
	}
	if !document.Detail.Complete || document.Detail.Kind != AVCVideoKind || document.Detail.VideoTagHeader == nil || document.Detail.PacketType == nil || document.Detail.AVCDecoderConfigurationRecord == nil {
		t.Fatalf("unexpected AVC detail: %+v", document.Detail)
	}
	config := document.Detail.AVCDecoderConfigurationRecord
	if config.LengthSize == nil || *config.LengthSize != 4 || len(config.SequenceParameterSets) != 1 || len(config.PictureParameterSets) != 1 {
		t.Fatalf("unexpected avcC summary: %+v", config)
	}
	sps := config.SequenceParameterSets[0].SequenceParameterSet
	if sps == nil || sps.ProfileIDC == nil || sps.ProfileIDC.Value != 100 || sps.DisplayWidth == nil || *sps.DisplayWidth != 320 || sps.DisplayHeight == nil || *sps.DisplayHeight != 180 {
		t.Fatalf("unexpected SPS summary: %+v", sps)
	}
	if len(sps.RBSP.EmulationPreventionBytes) != 2 || len(document.Diagnostics) != 0 {
		t.Fatalf("unexpected RBSP mapping/diagnostics: rbsp=%+v diagnostics=%+v", sps.RBSP, document.Diagnostics)
	}

	generic, err := NewTagDetail(uint64(len(data)), tag, parsed)
	if err != nil {
		t.Fatalf("NewTagDetail: %v", err)
	}
	if _, ok := generic.(AVCTagDetailDocument); !ok {
		t.Fatalf("generic document type = %T, want report.AVCTagDetailDocument", generic)
	}
	for _, pretty := range []bool{false, true} {
		var direct, routed, repeated bytes.Buffer
		if err := WriteAVCTagDetailJSON(&direct, document, pretty); err != nil {
			t.Fatalf("WriteAVCTagDetailJSON: %v", err)
		}
		if err := WriteParsedTagDetailJSON(&routed, generic, pretty); err != nil {
			t.Fatalf("WriteParsedTagDetailJSON: %v", err)
		}
		if err := WriteAVCTagDetailJSON(&repeated, document, pretty); err != nil {
			t.Fatalf("repeat WriteAVCTagDetailJSON: %v", err)
		}
		if !bytes.Equal(direct.Bytes(), routed.Bytes()) || !bytes.Equal(direct.Bytes(), repeated.Bytes()) {
			t.Fatalf("AVC output is not deterministic or generic-compatible (pretty=%v)", pretty)
		}
	}
}

func TestAVCTagDetailGoldens(t *testing.T) {
	sequenceHeader := append([]byte{0x17, 0x00, 0x00, 0x00, 0x00}, mustDecodeHex(t, avcCHigh320x180Hex)...)
	for _, fixture := range []struct {
		name          string
		payload       []byte
		nalLengthSize uint8
		goldenName    string
	}{
		{name: "sequence header", payload: sequenceHeader, goldenName: "flv-avc-detail-v1.json"},
		{name: "NALU", payload: []byte{0x27, 0x01, 0xff, 0xff, 0xfe, 0, 0, 0, 2, 0x65, 0x88}, nalLengthSize: 4, goldenName: "flv-avc-detail-nalu-v1.json"},
		{name: "malformed avcC", payload: []byte{0x17, 0x00, 0, 0, 0, 1, 0x64}, goldenName: "flv-avc-detail-malformed-v1.json"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			data, tag, parsed := parseAVCTagDetail(t, fixture.payload, fixture.nalLengthSize)
			document := NewAVCTagDetail(uint64(len(data)), tag, parsed)
			var output bytes.Buffer
			if err := WriteAVCTagDetailJSON(&output, document, true); err != nil {
				t.Fatalf("WriteAVCTagDetailJSON: %v", err)
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
				t.Fatalf("AVC detail differs from %s\nactual:\n%s", fixture.goldenName, output.Bytes())
			}
		})
	}
}

func TestAVCTagDetailNALUUsesSignedCompositionTime(t *testing.T) {
	data, tag, parsed := parseAVCTagDetail(t, []byte{0x27, 0x01, 0xff, 0xff, 0xfe, 0, 0, 0, 2, 0x65, 0x88}, 4)
	document := NewAVCTagDetail(uint64(len(data)), tag, parsed)
	if !document.Detail.Complete || document.Detail.CompositionTime == nil || document.Detail.CompositionTime.Raw != 0x00ff_fffe || document.Detail.CompositionTime.Value != -2 {
		t.Fatalf("unexpected signed composition time: %+v", document.Detail.CompositionTime)
	}
	sequence := document.Detail.NALUnitSequence
	if sequence == nil || sequence.LengthSize != 4 || len(sequence.NALUnits) != 1 || sequence.NALUnits[0].Header == nil || sequence.NALUnits[0].Header.NALUnitType.Value != 5 {
		t.Fatalf("unexpected NAL sequence: %+v", sequence)
	}
}

func TestAVCTagDetailMalformedMapsPartialSyntaxAndRecovery(t *testing.T) {
	data, tag, parsed := parseAVCTagDetail(t, []byte{0x17, 0x00, 0, 0, 0, 1, 0x64}, 0)
	document := NewAVCTagDetail(uint64(len(data)), tag, parsed)
	config := document.Detail.AVCDecoderConfigurationRecord
	if document.Detail.Complete || config == nil || config.ConfigurationVersion == nil || config.AVCProfileIndication == nil || config.ProfileCompatibility != nil || len(document.Diagnostics) == 0 {
		t.Fatalf("malformed avcC lost partial syntax: %+v", document)
	}
	payloadEnd := tag.PayloadSpan().ByteOffset + uint64(tag.DataSize)
	for _, diagnostic := range document.Diagnostics {
		if diagnostic.Severity == "error" && (diagnostic.RecoveryOffset == nil || *diagnostic.RecoveryOffset != payloadEnd) {
			t.Fatalf("recovery offset = %v, want %d: %+v", diagnostic.RecoveryOffset, payloadEnd, diagnostic)
		}
	}
}

func TestAVCTagDetailOmitsTruncatedSPSScalar(t *testing.T) {
	avcC := mustDecodeHex(t, "0142001effe100056742001e0000")
	payload := append([]byte{0x17, 0x00, 0, 0, 0}, avcC...)
	data, tag, parsed := parseAVCTagDetail(t, payload, 0)
	document := NewAVCTagDetail(uint64(len(data)), tag, parsed)
	config := document.Detail.AVCDecoderConfigurationRecord
	if document.Detail.Complete || config == nil || len(config.SequenceParameterSets) != 1 {
		t.Fatalf("unexpected truncated SPS document: %+v", document)
	}
	sps := config.SequenceParameterSets[0].SequenceParameterSet
	if sps == nil || sps.ProfileIDC == nil || sps.SequenceParameterSetID != nil {
		t.Fatalf("truncated seq_parameter_set_id was exposed: %+v", sps)
	}
}

func TestMapAVCSPSOmitsUnavailableDisplayDimensions(t *testing.T) {
	mapped := mapAVCSPS(avc.SequenceParameterSet{
		ChromaArrayType: 1,
		CropUnitX:       2,
		CropUnitY:       2,
		CodedWidth:      16,
		CodedHeight:     16,
	})
	if mapped.CodedWidth == nil || mapped.CodedHeight == nil ||
		mapped.DisplayWidth != nil || mapped.DisplayHeight != nil {
		t.Fatalf("unavailable display dimensions were exposed: %+v", mapped)
	}
}

func TestAVCTagDetailJSONRoundTrip(t *testing.T) {
	payload := append([]byte{0x17, 0x00, 0, 0, 0}, mustDecodeHex(t, avcCHigh320x180Hex)...)
	data, tag, parsed := parseAVCTagDetail(t, payload, 0)
	document := NewAVCTagDetail(uint64(len(data)), tag, parsed)
	var encoded bytes.Buffer
	if err := WriteAVCTagDetailJSON(&encoded, document, false); err != nil {
		t.Fatalf("WriteAVCTagDetailJSON: %v", err)
	}
	var decoded AVCTagDetailDocument
	if err := json.Unmarshal(encoded.Bytes(), &decoded); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	var reencoded bytes.Buffer
	if err := WriteAVCTagDetailJSON(&reencoded, decoded, false); err != nil {
		t.Fatalf("re-encode: %v", err)
	}
	if !bytes.Equal(encoded.Bytes(), reencoded.Bytes()) {
		t.Fatalf("AVC round trip changed bytes")
	}
}

func TestAVCDiagnosticConstantsMatchSchemaEnum(t *testing.T) {
	schemaPath := filepath.Join("..", "..", "docs", "schemas", "flv-avc-tag-detail-v1.schema.json")
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
		flv.CodeAVCVideoHeaderTruncated, flv.CodeAVCFrameTypeReserved,
		flv.CodeAVCFrameTypeUnexpected, flv.CodeAVCPacketTypeTruncated,
		flv.CodeAVCCompositionTimeTruncated, flv.CodeAVCCompositionTimeNonZero,
		flv.CodeAVCPacketTypeUnknown, flv.CodeAVCNALLengthSizeUnavailable,
		flv.CodeAVCEndOfSequencePayloadUnexpected,
		avc.CodeRecordHeaderTruncated, avc.CodeConfigurationVersionUnexpected,
		avc.CodeReservedBitsUnexpected, avc.CodeLengthSizeReserved,
		avc.CodeConfigurationBitDepthInvalid, avc.CodeParameterSetCountTruncated,
		avc.CodeParameterSetLengthTruncated, avc.CodeParameterSetDataTruncated,
		avc.CodeParameterSetLimitReached, avc.CodeParameterSetTooLarge,
		avc.CodeParameterSetNALTypeUnexpected, avc.CodeHighProfileExtensionTruncated,
		avc.CodeNALLengthTruncated,
		avc.CodeNALPayloadTruncated, avc.CodeNALUnitLimitReached,
		avc.CodeNALUnitZeroLength, avc.CodeNALHeaderTruncated,
		avc.CodeForbiddenZeroBitSet, avc.CodeEmulationPreventionInvalid,
		avc.CodeRBSPMappingLimitReached,
		avc.CodeRBSPSyntaxTruncated, avc.CodeExpGolombOverflow,
		avc.CodeSPSIDOutOfRange, avc.CodePPSIDOutOfRange,
		avc.CodePPSReferencedSPSOutOfRange, avc.CodeSPSReservedBitsUnexpected,
		avc.CodeSPSChromaFormatInvalid, avc.CodeSPSBitDepthInvalid,
		avc.CodeSPSLog2MaxFrameNumInvalid, avc.CodeSPSLog2MaxPicOrderInvalid,
		avc.CodeSPSPicOrderCntTypeInvalid, avc.CodeSPSReferenceCycleTooLarge,
		avc.CodeSPSCropInvalid,
		avc.CodeDiagnosticLimitReached,
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

func parseAVCTagDetail(t testing.TB, payload []byte, nalLengthSize uint8) ([]byte, flv.TagRecord, flv.AVCVideo) {
	t.Helper()
	data := makeVideoFLV(payload)
	scan, err := flv.Scan(bytes.NewReader(data), uint64(len(data)), flv.Options{})
	if err != nil {
		t.Fatalf("flv.Scan: %v", err)
	}
	if !scan.Complete || len(scan.Tags) != 1 {
		t.Fatalf("unexpected scan: complete=%v tags=%d diagnostics=%+v", scan.Complete, len(scan.Tags), scan.Diagnostics)
	}
	tag := scan.Tags[0]
	detail, err := flv.ParseAVCVideo(bytes.NewReader(data), tag, flv.AVCVideoOptions{AVC: avc.Options{}, NALLengthSize: nalLengthSize})
	if err != nil {
		t.Fatalf("flv.ParseAVCVideo: %v", err)
	}
	return data, tag, detail
}

func makeVideoFLV(payload []byte) []byte {
	data := []byte{'F', 'L', 'V', 1, 1, 0, 0, 0, 9, 0, 0, 0, 0, byte(flv.TagTypeVideo)}
	data = appendUint24(data, uint32(len(payload)))
	data = append(data, 0, 0, 0, 0, 0, 0, 0)
	data = append(data, payload...)
	var previous [4]byte
	binary.BigEndian.PutUint32(previous[:], uint32(flv.TagHeaderSize)+uint32(len(payload)))
	return append(data, previous[:]...)
}

func mustDecodeHex(t testing.TB, value string) []byte {
	t.Helper()
	decoded, err := hex.DecodeString(value)
	if err != nil {
		t.Fatalf("hex.DecodeString: %v", err)
	}
	return decoded
}
