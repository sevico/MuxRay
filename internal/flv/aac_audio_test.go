package flv

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/sevico/muxray/internal/aac"
	"github.com/sevico/muxray/internal/cursor"
)

func TestParseAACAudioSequenceHeader(t *testing.T) {
	data, tag := scriptDataFixture(t, byte(TagTypeAudio), []byte{0xaf, 0x00, 0x12, 0x10})
	detail, err := ParseAACAudio(bytes.NewReader(data), tag, aac.Options{})
	if err != nil {
		t.Fatalf("ParseAACAudio: %v", err)
	}
	if !detail.Complete || !detail.HeaderPresent || !detail.PacketTypePresent || !detail.ConfigPresent || detail.RawDataPresent {
		t.Fatalf("unexpected sequence-header state: %+v", detail)
	}
	if detail.Header.Raw != 0xaf || detail.Header.SoundFormat != SoundFormatAAC || detail.Header.SoundRate != 3 || detail.Header.SoundSize != 1 || detail.Header.SoundType != 1 {
		t.Fatalf("unexpected FLV audio header: %+v", detail.Header)
	}
	if detail.PacketType != AACPacketTypeSequenceHeader || detail.BitsConsumed != 32 {
		t.Fatalf("packet type/bits = %s/%d, want sequenceHeader/32", detail.PacketType, detail.BitsConsumed)
	}
	payloadOffset := tag.PayloadSpan().ByteOffset
	assertAACSpan(t, detail.Header.Span, payloadOffset, 0, 8)
	assertAACSpan(t, detail.Header.SoundFormatSpan, payloadOffset, 0, 4)
	assertAACSpan(t, detail.Header.SoundRateSpan, payloadOffset, 4, 2)
	assertAACSpan(t, detail.Header.SoundSizeSpan, payloadOffset, 6, 1)
	assertAACSpan(t, detail.Header.SoundTypeSpan, payloadOffset, 7, 1)
	assertAACSpan(t, detail.PacketTypeSpan, payloadOffset+1, 0, 8)
	assertAACSpan(t, detail.DataSpan, payloadOffset+2, 0, 16)
	if detail.Config.AudioObjectType.Value != 2 || detail.Config.SamplingFrequency.Hertz != 44100 || detail.Config.ChannelConfiguration.Value != 2 {
		t.Fatalf("unexpected AudioSpecificConfig: %+v", detail.Config)
	}
	if len(detail.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %+v", detail.Diagnostics)
	}
}

func TestParseAACAudioRawPacketIsNotParsedAsASC(t *testing.T) {
	data, tag := scriptDataFixture(t, byte(TagTypeAudio), []byte{0xaf, 0x01, 0xde, 0xad})
	detail, err := ParseAACAudio(bytes.NewReader(data), tag, aac.Options{})
	if err != nil {
		t.Fatalf("ParseAACAudio: %v", err)
	}
	if !detail.Complete || !detail.RawDataPresent || detail.ConfigPresent || detail.PacketType != AACPacketTypeRaw {
		t.Fatalf("unexpected raw AAC state: %+v", detail)
	}
	if detail.BitsConsumed != 16 || detail.DataSpan.BitLength != 16 || len(detail.Config.Diagnostics) != 0 {
		t.Fatalf("raw data was not represented only by span: %+v", detail)
	}
}

func TestParseAACAudioFramingDiagnostics(t *testing.T) {
	tests := []struct {
		name         string
		payload      []byte
		wantCode     string
		wantComplete bool
	}{
		{name: "empty audio body", payload: nil, wantCode: CodeAACAudioHeaderTruncated},
		{name: "packet type missing", payload: []byte{0xaf}, wantCode: CodeAACPacketTypeTruncated},
		{name: "packet type unknown", payload: []byte{0xaf, 0x02}, wantCode: CodeAACPacketTypeUnknown},
		{name: "sequence config missing", payload: []byte{0xaf, 0x00}, wantCode: aac.CodeAudioObjectTypeTruncated},
		{name: "noncanonical header warning", payload: []byte{0xa8, 0x01}, wantCode: CodeAACSoundRateUnexpected, wantComplete: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			data, tag := scriptDataFixture(t, byte(TagTypeAudio), test.payload)
			detail, err := ParseAACAudio(bytes.NewReader(data), tag, aac.Options{})
			if err != nil {
				t.Fatalf("ParseAACAudio: %v", err)
			}
			if detail.Complete != test.wantComplete || !hasAACDiagnostic(detail.Diagnostics, test.wantCode) {
				t.Fatalf("complete=%v diagnostics=%+v, want complete=%v code=%s", detail.Complete, detail.Diagnostics, test.wantComplete, test.wantCode)
			}
			payloadEnd := tag.PayloadSpan().ByteOffset + uint64(tag.DataSize)
			if !test.wantComplete {
				fatal := findAACDiagnostic(detail.Diagnostics, test.wantCode)
				if fatal == nil || fatal.Severity != aac.SeverityError || fatal.RecoveryOffset == nil || *fatal.RecoveryOffset != payloadEnd {
					t.Fatalf("fatal recovery = %+v, want payload end %d", fatal, payloadEnd)
				}
			}
		})
	}
}

func TestParseAACAudioReportsBothNoncanonicalHeaderFields(t *testing.T) {
	data, tag := scriptDataFixture(t, byte(TagTypeAudio), []byte{0xa8, 0x01})
	detail, err := ParseAACAudio(bytes.NewReader(data), tag, aac.Options{})
	if err != nil {
		t.Fatalf("ParseAACAudio: %v", err)
	}
	if !detail.Complete || !hasAACDiagnostic(detail.Diagnostics, CodeAACSoundRateUnexpected) || !hasAACDiagnostic(detail.Diagnostics, CodeAACSoundTypeUnexpected) {
		t.Fatalf("unexpected warning state: %+v", detail)
	}
	for _, diagnostic := range detail.Diagnostics {
		if diagnostic.Severity != aac.SeverityWarning || diagnostic.RecoveryOffset != nil {
			t.Fatalf("noncanonical header should only warn: %+v", diagnostic)
		}
	}
}

func TestParseAACAudioDiagnosticLimitPreservesFatalRecovery(t *testing.T) {
	data, tag := scriptDataFixture(t, byte(TagTypeAudio), []byte{0xa8, 0x00, 0x12})
	detail, err := ParseAACAudio(bytes.NewReader(data), tag, aac.Options{MaxDiagnostics: 2})
	if err != nil {
		t.Fatalf("ParseAACAudio: %v", err)
	}
	if detail.Complete || len(detail.Diagnostics) != 2 {
		t.Fatalf("unexpected bounded diagnostic state: %+v", detail)
	}
	if detail.Diagnostics[0].Code != aac.CodeSamplingFrequencyIndexTruncated || detail.Diagnostics[1].Code != aac.CodeDiagnosticLimitReached {
		t.Fatalf("fatal cause/marker were not preserved: %+v", detail.Diagnostics)
	}
	payloadEnd := tag.PayloadSpan().ByteOffset + uint64(tag.DataSize)
	for _, diagnostic := range detail.Diagnostics {
		if diagnostic.RecoveryOffset == nil || *diagnostic.RecoveryOffset != payloadEnd {
			t.Fatalf("bounded diagnostic lost recovery %d: %+v", payloadEnd, diagnostic)
		}
	}
}

func TestParseAACAudioRejectsInvalidSelections(t *testing.T) {
	_, validTag := scriptDataFixture(t, byte(TagTypeAudio), []byte{0xaf, 0x01})
	scriptData, scriptTag := scriptDataFixture(t, byte(TagTypeScript), []byte{0, 0})
	filteredData, filteredTag := scriptDataFixture(t, 0x20|byte(TagTypeAudio), []byte{0xaf, 0x01})
	mp3Data, mp3Tag := scriptDataFixture(t, byte(TagTypeAudio), []byte{0x2f, 0x01})

	full := makeFLV(t, 0, nil, fixtureTag{rawTypeFlags: byte(TagTypeAudio), payload: []byte{0xaf, 0x01}})
	truncatedData := full[:13+TagHeaderSize+1]
	truncatedScan, err := Scan(bytes.NewReader(truncatedData), uint64(len(truncatedData)), Options{})
	if err != nil {
		t.Fatalf("Scan truncated fixture: %v", err)
	}

	tests := []struct {
		name    string
		source  io.ReaderAt
		tag     TagRecord
		wantErr error
	}{
		{name: "nil source", source: nil, tag: validTag, wantErr: ErrAACAudioNilSource},
		{name: "non audio tag", source: bytes.NewReader(scriptData), tag: scriptTag, wantErr: ErrAACAudioWrongTagType},
		{name: "filtered tag", source: bytes.NewReader(filteredData), tag: filteredTag, wantErr: ErrAACAudioFiltered},
		{name: "incomplete payload", source: bytes.NewReader(truncatedData), tag: truncatedScan.Tags[0], wantErr: ErrAACAudioPayloadIncomplete},
		{name: "non AAC audio", source: bytes.NewReader(mp3Data), tag: mp3Tag, wantErr: ErrAACAudioWrongSoundFormat},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ParseAACAudio(test.source, test.tag, aac.Options{})
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("error = %v, want errors.Is(_, %v)", err, test.wantErr)
			}
		})
	}
}

func TestParseAACAudioReaderAtErrorIsNotSyntaxDiagnostic(t *testing.T) {
	_, tag := scriptDataFixture(t, byte(TagTypeAudio), []byte{0xaf, 0x00, 0x12, 0x10})
	readErr := errors.New("AAC fixture read failed")
	detail, err := ParseAACAudio(scriptFailingReaderAt{err: readErr}, tag, aac.Options{})
	if !errors.Is(err, readErr) {
		t.Fatalf("error = %v, want wrapped reader error", err)
	}
	if detail.Complete || len(detail.Diagnostics) != 0 {
		t.Fatalf("I/O error was converted to syntax detail: %+v", detail)
	}
}

func TestMalformedAACDoesNotAffectFollowingTag(t *testing.T) {
	data := makeFLV(t, 0, nil,
		fixtureTag{rawTypeFlags: byte(TagTypeAudio), payload: []byte{0xaf, 0x00, 0x12}},
		fixtureTag{rawTypeFlags: byte(TagTypeScript), payload: []byte{byte(5), byte(5)}},
	)
	scan, err := Scan(bytes.NewReader(data), uint64(len(data)), Options{})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if !scan.Complete || len(scan.Tags) != 2 || scan.Tags[1].ID != 1 {
		t.Fatalf("following tag was not indexed: %+v", scan)
	}
	detail, err := ParseAACAudio(bytes.NewReader(data), scan.Tags[0], aac.Options{})
	if err != nil {
		t.Fatalf("ParseAACAudio: %v", err)
	}
	if detail.Complete || len(detail.Diagnostics) == 0 {
		t.Fatalf("malformed ASC unexpectedly complete: %+v", detail)
	}
	payloadEnd := scan.Tags[0].PayloadSpan().ByteOffset + uint64(scan.Tags[0].DataSize)
	for _, diagnostic := range detail.Diagnostics {
		if diagnostic.Severity == aac.SeverityError && (diagnostic.RecoveryOffset == nil || *diagnostic.RecoveryOffset != payloadEnd) {
			t.Fatalf("fatal recovery offset = %v, want %d", diagnostic.RecoveryOffset, payloadEnd)
		}
	}
	if !scan.Complete || scan.Tags[1].ID != 1 {
		t.Fatalf("detail parsing mutated scan result: %+v", scan)
	}
}

func assertAACSpan(t testing.TB, got cursor.Span, byteOffset uint64, bitOffset uint8, bitLength uint32) {
	t.Helper()
	want := cursor.Span{ByteOffset: byteOffset, BitOffset: bitOffset, BitLength: bitLength}
	if got != want {
		t.Fatalf("span = %+v, want %+v", got, want)
	}
}

func hasAACDiagnostic(items []aac.Diagnostic, code string) bool {
	return findAACDiagnostic(items, code) != nil
}

func findAACDiagnostic(items []aac.Diagnostic, code string) *aac.Diagnostic {
	for index := range items {
		if items[index].Code == code {
			return &items[index]
		}
	}
	return nil
}
