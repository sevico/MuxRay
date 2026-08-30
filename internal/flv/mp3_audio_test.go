package flv

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/sevico/muxray/internal/mp3"
)

func TestParseMP3AudioFirstFrameHeader(t *testing.T) {
	payload := []byte{0x2f, 0xff, 0xfb, 0x90, 0x64, 0xde, 0xad}
	data, tag := scriptDataFixture(t, byte(TagTypeAudio), payload)
	detail, err := ParseMP3Audio(bytes.NewReader(data), tag, mp3.Options{})
	if err != nil {
		t.Fatalf("ParseMP3Audio: %v", err)
	}
	if !detail.Complete || !detail.HeaderPresent || !detail.FrameHeader.HeaderPresent || detail.BitsConsumed != 40 || len(detail.Diagnostics) != 0 {
		t.Fatalf("unexpected MP3 detail: %+v", detail)
	}
	if detail.Header.Raw != 0x2f || detail.Header.SoundFormat != SoundFormatMP3 ||
		detail.FrameHeader.Header.Version != mp3.VersionMPEG1 || detail.FrameHeader.Header.Layer != mp3.LayerIII ||
		detail.FrameHeader.Header.BitrateKbps != 128 || detail.FrameHeader.Header.SamplingFrequencyHz != 44100 ||
		detail.FrameHeader.Header.FrameLengthBytes != 417 {
		t.Fatalf("unexpected MP3 headers: flv=%+v mpeg=%+v", detail.Header, detail.FrameHeader.Header)
	}
	payloadOffset := tag.PayloadSpan().ByteOffset
	assertAACSpan(t, detail.Header.Span, payloadOffset, 0, 8)
	assertAACSpan(t, detail.DataSpan, payloadOffset+1, 0, uint32((len(payload)-1)*8))
	assertAACSpan(t, detail.FrameHeader.Header.Span, payloadOffset+1, 0, 32)
	assertAACSpan(t, detail.FrameHeader.Header.VersionSpan, payloadOffset+2, 3, 2)
}

func TestParseMP3AudioFreeFormatIsComplete(t *testing.T) {
	data, tag := scriptDataFixture(t, byte(TagTypeAudio), []byte{0x2f, 0xff, 0xfb, 0x00, 0x64})
	detail, err := ParseMP3Audio(bytes.NewReader(data), tag, mp3.Options{})
	if err != nil {
		t.Fatalf("ParseMP3Audio: %v", err)
	}
	if !detail.Complete || detail.FrameHeader.Header.BitrateMode != mp3.BitrateModeFree ||
		detail.FrameHeader.Header.BitratePresent || detail.FrameHeader.Header.FrameLengthPresent ||
		findMP3AudioDiagnostic(detail.Diagnostics, mp3.CodeFreeFormatUnresolved) == nil {
		t.Fatalf("unexpected free-format detail: %+v", detail)
	}
}

func TestParseMP3AudioSyntaxDiagnosticsRecoverAtTagEnd(t *testing.T) {
	tests := []struct {
		name    string
		payload []byte
		code    string
	}{
		{name: "empty SoundData", payload: []byte{0x2f}, code: mp3.CodeFrameHeaderTruncated},
		{name: "short header", payload: []byte{0x2f, 0xff, 0xfb, 0x90}, code: mp3.CodeFrameHeaderTruncated},
		{name: "invalid sync", payload: []byte{0x2f, 0xfe, 0xfb, 0x90, 0x64}, code: mp3.CodeSyncInvalid},
		{name: "non-Layer-III", payload: []byte{0x2f, 0xff, 0xff, 0x96, 0x00}, code: mp3.CodeLayerNotIII},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			data, tag := scriptDataFixture(t, byte(TagTypeAudio), test.payload)
			detail, err := ParseMP3Audio(bytes.NewReader(data), tag, mp3.Options{})
			if err != nil {
				t.Fatalf("ParseMP3Audio: %v", err)
			}
			diagnostic := findMP3AudioDiagnostic(detail.Diagnostics, test.code)
			payloadEnd := tag.PayloadSpan().ByteOffset + uint64(tag.DataSize)
			if detail.Complete || diagnostic == nil || diagnostic.Severity != mp3.SeverityError ||
				diagnostic.RecoveryOffset == nil || *diagnostic.RecoveryOffset != payloadEnd {
				t.Fatalf("unexpected malformed detail: %+v", detail)
			}
		})
	}
}

func TestParseMP3AudioEmptyFLVPayload(t *testing.T) {
	data, tag := scriptDataFixture(t, byte(TagTypeAudio), nil)
	detail, err := ParseMP3Audio(bytes.NewReader(data), tag, mp3.Options{})
	if err != nil {
		t.Fatalf("ParseMP3Audio: %v", err)
	}
	diagnostic := findMP3AudioDiagnostic(detail.Diagnostics, CodeMP3AudioHeaderTruncated)
	if detail.Complete || detail.HeaderPresent || detail.BitsConsumed != 0 || diagnostic == nil ||
		diagnostic.RecoveryOffset == nil || *diagnostic.RecoveryOffset != tag.PayloadSpan().ByteOffset {
		t.Fatalf("unexpected empty-payload detail: %+v", detail)
	}
}

func TestParseMP3AudioRejectsInvalidSelections(t *testing.T) {
	_, validTag := scriptDataFixture(t, byte(TagTypeAudio), []byte{0x2f, 0xff, 0xfb, 0x90, 0x64})
	scriptData, scriptTag := scriptDataFixture(t, byte(TagTypeScript), []byte{0, 0})
	filteredData, filteredTag := scriptDataFixture(t, 0x20|byte(TagTypeAudio), []byte{0x2f, 0xff, 0xfb, 0x90, 0x64})
	aacData, aacTag := scriptDataFixture(t, byte(TagTypeAudio), []byte{0xaf, 0x01})

	full := makeFLV(t, 0, nil, fixtureTag{rawTypeFlags: byte(TagTypeAudio), payload: []byte{0x2f, 0xff, 0xfb, 0x90, 0x64}})
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
		{name: "nil source", source: nil, tag: validTag, wantErr: ErrMP3AudioNilSource},
		{name: "non audio tag", source: bytes.NewReader(scriptData), tag: scriptTag, wantErr: ErrMP3AudioWrongTagType},
		{name: "filtered tag", source: bytes.NewReader(filteredData), tag: filteredTag, wantErr: ErrMP3AudioFiltered},
		{name: "incomplete payload", source: bytes.NewReader(truncatedData), tag: truncatedScan.Tags[0], wantErr: ErrMP3AudioPayloadIncomplete},
		{name: "non MP3 audio", source: bytes.NewReader(aacData), tag: aacTag, wantErr: ErrMP3AudioWrongSoundFormat},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, parseErr := ParseMP3Audio(test.source, test.tag, mp3.Options{})
			if !errors.Is(parseErr, test.wantErr) {
				t.Fatalf("error = %v, want errors.Is(_, %v)", parseErr, test.wantErr)
			}
		})
	}
}

func TestParseMP3AudioReaderAtErrorIsNotSyntaxDiagnostic(t *testing.T) {
	_, tag := scriptDataFixture(t, byte(TagTypeAudio), []byte{0x2f, 0xff, 0xfb, 0x90, 0x64})
	readErr := errors.New("MP3 fixture read failed")
	detail, err := ParseMP3Audio(scriptFailingReaderAt{err: readErr}, tag, mp3.Options{})
	if !errors.Is(err, readErr) || detail.Complete || len(detail.Diagnostics) != 0 {
		t.Fatalf("I/O error converted to syntax detail: detail=%+v err=%v", detail, err)
	}
}

func TestMalformedMP3DoesNotAffectFollowingTag(t *testing.T) {
	data := makeFLV(t, 0, nil,
		fixtureTag{rawTypeFlags: byte(TagTypeAudio), payload: []byte{0x2f, 0xfe, 0xfb, 0x90, 0x64}},
		fixtureTag{rawTypeFlags: byte(TagTypeScript), payload: []byte{byte(5), byte(5)}},
	)
	scan, err := Scan(bytes.NewReader(data), uint64(len(data)), Options{})
	if err != nil || !scan.Complete || len(scan.Tags) != 2 {
		t.Fatalf("Scan: result=%+v err=%v", scan, err)
	}
	detail, err := ParseMP3Audio(bytes.NewReader(data), scan.Tags[0], mp3.Options{})
	if err != nil || detail.Complete || scan.Tags[1].ID != 1 {
		t.Fatalf("detail crossed tag boundary: detail=%+v scan=%+v err=%v", detail, scan, err)
	}
}

func TestParseMP3AudioDoesNotReadFrameBody(t *testing.T) {
	payload := append([]byte{0x2f, 0xff, 0xfb, 0x90, 0x64}, bytes.Repeat([]byte{0xaa}, 4096)...)
	data, tag := scriptDataFixture(t, byte(TagTypeAudio), payload)
	limit := int64(tag.PayloadSpan().ByteOffset + 5)
	reader := &boundedMP3AudioReaderAt{data: data, maxExclusive: limit}
	detail, err := ParseMP3Audio(reader, tag, mp3.Options{})
	if err != nil || !detail.Complete || detail.BitsConsumed != 40 {
		t.Fatalf("bounded FLV MP3 parse failed: detail=%+v err=%v", detail, err)
	}
	if reader.maxReadEnd != limit {
		t.Fatalf("maximum read end = %d, want %d", reader.maxReadEnd, limit)
	}
}

func findMP3AudioDiagnostic(items []mp3.Diagnostic, code string) *mp3.Diagnostic {
	for index := range items {
		if items[index].Code == code {
			return &items[index]
		}
	}
	return nil
}

type boundedMP3AudioReaderAt struct {
	data         []byte
	maxExclusive int64
	maxReadEnd   int64
}

func (reader *boundedMP3AudioReaderAt) ReadAt(output []byte, offset int64) (int, error) {
	end := offset + int64(len(output))
	if end > reader.maxReadEnd {
		reader.maxReadEnd = end
	}
	if offset < 0 || end > reader.maxExclusive {
		return 0, errors.New("read crossed MP3 framing prefix")
	}
	return copy(output, reader.data[offset:]), nil
}
