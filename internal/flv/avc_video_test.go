package flv

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/sevico/muxray/internal/avc"
	"github.com/sevico/muxray/internal/cursor"
)

func TestParseAVCVideoSequenceHeader(t *testing.T) {
	// AVCDecoderConfigurationRecord with zero SPS/PPS entries and a four-byte
	// NAL length field. The FLV composition time for a sequence header is zero.
	payload := []byte{
		0x17, 0x00, 0x00, 0x00, 0x00,
		0x01, 0x42, 0x00, 0x1e, 0xff, 0xe0, 0x00,
	}
	data, tag := scriptDataFixture(t, byte(TagTypeVideo), payload)
	detail, err := ParseAVCVideo(bytes.NewReader(data), tag, AVCVideoOptions{})
	if err != nil {
		t.Fatalf("ParseAVCVideo: %v", err)
	}
	if !detail.Complete || !detail.HeaderPresent || !detail.PacketTypePresent || !detail.CompositionTimePresent || !detail.ConfigPresent {
		t.Fatalf("unexpected sequence-header state: %+v", detail)
	}
	if detail.NALUnitsPresent || detail.EndOfSequencePresent {
		t.Fatalf("sequence header selected another payload kind: %+v", detail)
	}
	if detail.Header.Raw != 0x17 || detail.Header.FrameType != 1 || detail.Header.CodecID != CodecIDAVC {
		t.Fatalf("unexpected FLV video header: %+v", detail.Header)
	}
	if detail.PacketType != AVCPacketTypeSequenceHeader || detail.CompositionTime != 0 {
		t.Fatalf("packet type/composition time = %s/%d", detail.PacketType, detail.CompositionTime)
	}
	if detail.Config.LengthSize != 4 || detail.BitsConsumed != uint64(len(payload))*8 {
		t.Fatalf("length size/bits = %d/%d, want 4/%d", detail.Config.LengthSize, detail.BitsConsumed, len(payload)*8)
	}
	payloadOffset := tag.PayloadSpan().ByteOffset
	assertAVCSpan(t, detail.Header.Span, payloadOffset, 0, 8)
	assertAVCSpan(t, detail.Header.FrameTypeSpan, payloadOffset, 0, 4)
	assertAVCSpan(t, detail.Header.CodecIDSpan, payloadOffset, 4, 4)
	assertAVCSpan(t, detail.PacketTypeSpan, payloadOffset+1, 0, 8)
	assertAVCSpan(t, detail.CompositionTimeSpan, payloadOffset+2, 0, 24)
	assertAVCSpan(t, detail.DataSpan, payloadOffset+5, 0, uint32(len(payload)-5)*8)
	if len(detail.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %+v", detail.Diagnostics)
	}
}

func TestParseAVCVideoNALUUsesExplicitLengthSizeAndSignedCompositionTime(t *testing.T) {
	payload := []byte{
		0x27, 0x01, 0xff, 0xff, 0xfe,
		0x00, 0x00, 0x00, 0x02, 0x65, 0xaa,
		0x00, 0x00, 0x00, 0x01, 0x09,
	}
	data, tag := scriptDataFixture(t, byte(TagTypeVideo), payload)
	detail, err := ParseAVCVideo(bytes.NewReader(data), tag, AVCVideoOptions{NALLengthSize: 4})
	if err != nil {
		t.Fatalf("ParseAVCVideo: %v", err)
	}
	if !detail.Complete || !detail.NALUnitsPresent || detail.ConfigPresent || detail.EndOfSequencePresent {
		t.Fatalf("unexpected NALU state: %+v", detail)
	}
	if detail.PacketType != AVCPacketTypeNALU || detail.CompositionTime != -2 {
		t.Fatalf("packet type/composition time = %s/%d, want nalu/-2", detail.PacketType, detail.CompositionTime)
	}
	if detail.BitsConsumed != uint64(len(payload))*8 || len(detail.NALUnits.NALUnits) != 2 {
		t.Fatalf("NAL sequence framing = %+v bits=%d", detail.NALUnits, detail.BitsConsumed)
	}
	if detail.NALUnits.NALUnits[0].Span.BitLength != 16 || detail.NALUnits.NALUnits[1].Span.BitLength != 8 {
		t.Fatalf("unexpected NAL payload spans: %+v", detail.NALUnits.NALUnits)
	}
}

func TestSignedInt24Boundaries(t *testing.T) {
	for _, test := range []struct {
		raw  uint32
		want int32
	}{
		{raw: 0, want: 0},
		{raw: 0x007f_ffff, want: 8_388_607},
		{raw: 0x0080_0000, want: -8_388_608},
		{raw: 0x00ff_ffff, want: -1},
		{raw: 0xffff_fffe, want: -2},
	} {
		if got := signedInt24(test.raw); got != test.want {
			t.Fatalf("signedInt24(%#x) = %d, want %d", test.raw, got, test.want)
		}
	}
}

func TestParseAVCVideoNALUWithoutLengthSizeIsPartial(t *testing.T) {
	payload := []byte{0x27, 0x01, 0x00, 0x00, 0x02, 0x00, 0x01, 0x09}
	data, tag := scriptDataFixture(t, byte(TagTypeVideo), payload)
	detail, err := ParseAVCVideo(bytes.NewReader(data), tag, AVCVideoOptions{})
	if err != nil {
		t.Fatalf("ParseAVCVideo: %v", err)
	}
	if detail.Complete || detail.NALUnitsPresent || detail.DataSpan.BitLength != 24 {
		t.Fatalf("unresolved NAL packet state: %+v", detail)
	}
	diagnostic := findAVCDiagnostic(detail.Diagnostics, CodeAVCNALLengthSizeUnavailable)
	payloadEnd := tag.PayloadSpan().ByteOffset + uint64(tag.DataSize)
	if diagnostic == nil || diagnostic.Severity != avc.SeverityError || diagnostic.RecoveryOffset == nil || *diagnostic.RecoveryOffset != payloadEnd {
		t.Fatalf("missing stable length-size diagnostic/recovery: %+v", detail.Diagnostics)
	}
}

func TestParseAVCVideoEndOfSequence(t *testing.T) {
	data, tag := scriptDataFixture(t, byte(TagTypeVideo), []byte{0x17, 0x02, 0x00, 0x00, 0x00})
	detail, err := ParseAVCVideo(bytes.NewReader(data), tag, AVCVideoOptions{})
	if err != nil {
		t.Fatalf("ParseAVCVideo: %v", err)
	}
	if !detail.Complete || !detail.EndOfSequencePresent || detail.ConfigPresent || detail.NALUnitsPresent || !detail.DataSpan.Empty() {
		t.Fatalf("unexpected end-of-sequence state: %+v", detail)
	}
	if detail.PacketType != AVCPacketTypeEndOfSequence || detail.BitsConsumed != 40 || len(detail.Diagnostics) != 0 {
		t.Fatalf("unexpected end-of-sequence framing: %+v", detail)
	}
}

func TestParseAVCVideoWarnsForNonZeroControlCompositionTime(t *testing.T) {
	data, tag := scriptDataFixture(t, byte(TagTypeVideo), []byte{0x17, 0x02, 0xff, 0xff, 0xff})
	detail, err := ParseAVCVideo(bytes.NewReader(data), tag, AVCVideoOptions{})
	if err != nil {
		t.Fatalf("ParseAVCVideo: %v", err)
	}
	if !detail.Complete || detail.CompositionTime != -1 {
		t.Fatalf("nonzero control CTS should remain a complete warning: %+v", detail)
	}
	diagnostic := findAVCDiagnostic(detail.Diagnostics, CodeAVCCompositionTimeNonZero)
	if diagnostic == nil || diagnostic.Severity != avc.SeverityWarning || diagnostic.RecoveryOffset != nil || diagnostic.Actual == nil || *diagnostic.Actual != 0x00ff_ffff {
		t.Fatalf("unexpected nonzero CTS diagnostic: %+v", detail.Diagnostics)
	}
}

func TestParseAVCVideoWarnsForReservedFrameType(t *testing.T) {
	data, tag := scriptDataFixture(t, byte(TagTypeVideo), []byte{0x07, 0x02, 0, 0, 0})
	detail, err := ParseAVCVideo(bytes.NewReader(data), tag, AVCVideoOptions{})
	if err != nil {
		t.Fatalf("ParseAVCVideo: %v", err)
	}
	if !detail.Complete || findAVCDiagnostic(detail.Diagnostics, CodeAVCFrameTypeReserved) == nil {
		t.Fatalf("reserved frame type should warn without hiding EOS framing: %+v", detail)
	}
}

func TestParseAVCVideoWarnsForH263OnlyFrameType(t *testing.T) {
	data, tag := scriptDataFixture(t, byte(TagTypeVideo), []byte{0x37, 0x02, 0, 0, 0})
	detail, err := ParseAVCVideo(bytes.NewReader(data), tag, AVCVideoOptions{})
	if err != nil {
		t.Fatalf("ParseAVCVideo: %v", err)
	}
	diagnostic := findAVCDiagnostic(detail.Diagnostics, CodeAVCFrameTypeUnexpected)
	if !detail.Complete || diagnostic == nil || diagnostic.Severity != avc.SeverityWarning {
		t.Fatalf("H.263-only frame type should warn on AVC: %+v", detail)
	}
}

func TestParseAVCVideoDiagnosticLimitMarkerHasTagRecovery(t *testing.T) {
	// FrameType 3, non-zero sequence-header CTS, and non-canonical avcC
	// reserved bits produce three warnings. With a budget of two, the public
	// truncation marker becomes an error and must still recover at Tag end.
	payload := []byte{
		0x37, 0x00, 0x00, 0x00, 0x01,
		0x01, 0x42, 0x00, 0x1e, 0x03, 0xe0, 0x00,
	}
	data, tag := scriptDataFixture(t, byte(TagTypeVideo), payload)
	detail, err := ParseAVCVideo(bytes.NewReader(data), tag, AVCVideoOptions{
		AVC: avc.Options{MaxDiagnostics: 2},
	})
	if err != nil {
		t.Fatalf("ParseAVCVideo: %v", err)
	}
	marker := findAVCDiagnostic(detail.Diagnostics, avc.CodeDiagnosticLimitReached)
	payloadEnd := tag.PayloadSpan().ByteOffset + uint64(tag.DataSize)
	if detail.Complete || len(detail.Diagnostics) != 2 || marker == nil || marker.Severity != avc.SeverityError ||
		marker.RecoveryOffset == nil || *marker.RecoveryOffset != payloadEnd {
		t.Fatalf("diagnostic marker/recovery = %+v, want error recovery %d", detail.Diagnostics, payloadEnd)
	}
}

func TestParseAVCVideoFramingDiagnostics(t *testing.T) {
	tests := []struct {
		name     string
		payload  []byte
		wantCode string
	}{
		{name: "empty video body", payload: nil, wantCode: CodeAVCVideoHeaderTruncated},
		{name: "packet type missing", payload: []byte{0x17}, wantCode: CodeAVCPacketTypeTruncated},
		{name: "composition time missing", payload: []byte{0x17, 0x01}, wantCode: CodeAVCCompositionTimeTruncated},
		{name: "composition time short", payload: []byte{0x17, 0x01, 0x00, 0x00}, wantCode: CodeAVCCompositionTimeTruncated},
		{name: "packet type unknown", payload: []byte{0x17, 0x03, 0x00, 0x00, 0x00}, wantCode: CodeAVCPacketTypeUnknown},
		{name: "end of sequence has payload", payload: []byte{0x17, 0x02, 0x00, 0x00, 0x00, 0xff}, wantCode: CodeAVCEndOfSequencePayloadUnexpected},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			data, tag := scriptDataFixture(t, byte(TagTypeVideo), test.payload)
			detail, err := ParseAVCVideo(bytes.NewReader(data), tag, AVCVideoOptions{})
			if err != nil {
				t.Fatalf("ParseAVCVideo: %v", err)
			}
			if detail.Complete {
				t.Fatalf("malformed framing unexpectedly complete: %+v", detail)
			}
			diagnostic := findAVCDiagnostic(detail.Diagnostics, test.wantCode)
			payloadEnd := tag.PayloadSpan().ByteOffset + uint64(tag.DataSize)
			if diagnostic == nil || diagnostic.Severity != avc.SeverityError || diagnostic.RecoveryOffset == nil || *diagnostic.RecoveryOffset != payloadEnd {
				t.Fatalf("diagnostic/recovery = %+v, want %s -> %d", detail.Diagnostics, test.wantCode, payloadEnd)
			}
		})
	}
}

func TestParseAVCVideoRejectsInvalidSelections(t *testing.T) {
	validData, validTag := scriptDataFixture(t, byte(TagTypeVideo), []byte{0x17, 0x02, 0, 0, 0})
	audioData, audioTag := scriptDataFixture(t, byte(TagTypeAudio), []byte{0xaf, 0x01})
	filteredData, filteredTag := scriptDataFixture(t, 0x20|byte(TagTypeVideo), []byte{0x17, 0x02, 0, 0, 0})
	otherCodecData, otherCodecTag := scriptDataFixture(t, byte(TagTypeVideo), []byte{0x12, 0x00, 0, 0, 0})

	full := makeFLV(t, 0, nil, fixtureTag{rawTypeFlags: byte(TagTypeVideo), payload: []byte{0x17, 0x02, 0, 0, 0}})
	truncatedData := full[:13+TagHeaderSize+1]
	truncatedScan, err := Scan(bytes.NewReader(truncatedData), uint64(len(truncatedData)), Options{})
	if err != nil {
		t.Fatalf("Scan truncated fixture: %v", err)
	}

	tests := []struct {
		name    string
		source  io.ReaderAt
		tag     TagRecord
		options AVCVideoOptions
		wantErr error
	}{
		{name: "nil source", source: nil, tag: validTag, wantErr: ErrAVCVideoNilSource},
		{name: "non video tag", source: bytes.NewReader(audioData), tag: audioTag, wantErr: ErrAVCVideoWrongTagType},
		{name: "filtered tag", source: bytes.NewReader(filteredData), tag: filteredTag, wantErr: ErrAVCVideoFiltered},
		{name: "incomplete payload", source: bytes.NewReader(truncatedData), tag: truncatedScan.Tags[0], wantErr: ErrAVCVideoPayloadIncomplete},
		{name: "non AVC video", source: bytes.NewReader(otherCodecData), tag: otherCodecTag, wantErr: ErrAVCVideoWrongCodecID},
		{name: "command frame", source: bytes.NewReader(dataForTag(t, validTag, []byte{0x57, 0x00, 0, 0, 0})), tag: validTag, wantErr: ErrAVCVideoCommandFrame},
		{name: "invalid NAL length size", source: bytes.NewReader(validData), tag: validTag, options: AVCVideoOptions{NALLengthSize: 3}, wantErr: ErrAVCNALLengthSizeInvalid},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ParseAVCVideo(test.source, test.tag, test.options)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("error = %v, want errors.Is(_, %v)", err, test.wantErr)
			}
		})
	}
}

func TestParseAVCVideoReaderAtErrorIsNotSyntaxDiagnostic(t *testing.T) {
	_, tag := scriptDataFixture(t, byte(TagTypeVideo), []byte{0x17, 0x00, 0, 0, 0, 1, 0x42, 0, 0x1e, 0xff, 0xe0, 0})
	readErr := errors.New("AVC fixture read failed")
	detail, err := ParseAVCVideo(scriptFailingReaderAt{err: readErr}, tag, AVCVideoOptions{})
	if !errors.Is(err, readErr) {
		t.Fatalf("error = %v, want wrapped reader error", err)
	}
	if detail.Complete || len(detail.Diagnostics) != 0 {
		t.Fatalf("I/O error was converted to syntax detail: %+v", detail)
	}
}

func TestMalformedAVCNALPacketDoesNotAffectFollowingTag(t *testing.T) {
	data := makeFLV(t, 0, nil,
		fixtureTag{rawTypeFlags: byte(TagTypeVideo), payload: []byte{0x27, 0x01, 0, 0, 0, 0, 0, 0, 5, 0x65}},
		fixtureTag{rawTypeFlags: byte(TagTypeScript), payload: []byte{byte(5), byte(5)}},
	)
	scan, err := Scan(bytes.NewReader(data), uint64(len(data)), Options{})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if !scan.Complete || len(scan.Tags) != 2 || scan.Tags[1].ID != 1 {
		t.Fatalf("following tag was not indexed: %+v", scan)
	}
	detail, err := ParseAVCVideo(bytes.NewReader(data), scan.Tags[0], AVCVideoOptions{NALLengthSize: 4})
	if err != nil {
		t.Fatalf("ParseAVCVideo: %v", err)
	}
	if detail.Complete || len(detail.Diagnostics) == 0 {
		t.Fatalf("truncated NAL payload unexpectedly complete: %+v", detail)
	}
	payloadEnd := scan.Tags[0].PayloadSpan().ByteOffset + uint64(scan.Tags[0].DataSize)
	for _, diagnostic := range detail.Diagnostics {
		if diagnostic.Severity == avc.SeverityError && (diagnostic.RecoveryOffset == nil || *diagnostic.RecoveryOffset != payloadEnd) {
			t.Fatalf("fatal recovery offset = %v, want %d", diagnostic.RecoveryOffset, payloadEnd)
		}
	}
	if !scan.Complete || scan.Tags[1].ID != 1 {
		t.Fatalf("detail parsing mutated scan result: %+v", scan)
	}
}

func dataForTag(t testing.TB, tag TagRecord, payload []byte) []byte {
	t.Helper()
	data := make([]byte, int(tag.PayloadSpan().ByteOffset)+len(payload))
	copy(data[tag.PayloadSpan().ByteOffset:], payload)
	return data
}

func assertAVCSpan(t testing.TB, got cursor.Span, byteOffset uint64, bitOffset uint8, bitLength uint32) {
	t.Helper()
	want := cursor.Span{ByteOffset: byteOffset, BitOffset: bitOffset, BitLength: bitLength}
	if got != want {
		t.Fatalf("span = %+v, want %+v", got, want)
	}
}

func findAVCDiagnostic(items []avc.Diagnostic, code string) *avc.Diagnostic {
	for index := range items {
		if items[index].Code == code {
			return &items[index]
		}
	}
	return nil
}
