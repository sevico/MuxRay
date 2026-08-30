package mp3

import (
	"bytes"
	"errors"
	"io"
	"math"
	"reflect"
	"testing"

	"github.com/sevico/muxray/internal/cursor"
)

func TestParseFrameHeaderDerivations(t *testing.T) {
	tests := []struct {
		name        string
		header      []byte
		version     Version
		bitrateKbps uint16
		frequencyHz uint32
		channels    uint8
		samples     uint16
		frameLength uint32
		padding     bool
	}{
		{name: "MPEG-1", header: []byte{0xff, 0xfb, 0x90, 0x64}, version: VersionMPEG1, bitrateKbps: 128, frequencyHz: 44100, channels: 2, samples: 1152, frameLength: 417},
		{name: "MPEG-1 padded", header: []byte{0xff, 0xfb, 0x92, 0x64}, version: VersionMPEG1, bitrateKbps: 128, frequencyHz: 44100, channels: 2, samples: 1152, frameLength: 418, padding: true},
		{name: "MPEG-2 mono", header: []byte{0xff, 0xf3, 0x80, 0xc4}, version: VersionMPEG2, bitrateKbps: 64, frequencyHz: 22050, channels: 1, samples: 576, frameLength: 208},
		{name: "MPEG-2.5 8 kHz", header: []byte{0xff, 0xe3, 0x48, 0xc4}, version: VersionMPEG25, bitrateKbps: 32, frequencyHz: 8000, channels: 1, samples: 576, frameLength: 288},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			data := append(bytes.Repeat([]byte{0xaa}, 7), test.header...)
			data = append(data, 0xde, 0xad)
			span := cursor.Span{ByteOffset: 7, BitLength: uint32((len(data) - 7) * 8)}
			result, err := ParseFrameHeader(bytes.NewReader(data), span, Options{})
			if err != nil {
				t.Fatalf("ParseFrameHeader: %v", err)
			}
			if !result.Complete || !result.HeaderPresent || result.BitsConsumed != 32 || len(result.Diagnostics) != 0 {
				t.Fatalf("unexpected parse state: %+v", result)
			}
			header := result.Header
			if header.Version != test.version || header.Layer != LayerIII || header.BitrateKbps != test.bitrateKbps ||
				header.SamplingFrequencyHz != test.frequencyHz || header.Channels != test.channels ||
				header.SamplesPerFrame != test.samples || header.FrameLengthBytes != test.frameLength || header.Padding != test.padding {
				t.Fatalf("unexpected derived header: %+v", header)
			}
			if !header.BitratePresent || !header.SamplingFrequencyPresent || !header.SamplesPerFramePresent || !header.FrameLengthPresent || header.BitrateMode != BitrateModeIndexed {
				t.Fatalf("missing derived values: %+v", header)
			}
		})
	}
}

func TestParseFrameHeaderAbsoluteSpans(t *testing.T) {
	data := append(bytes.Repeat([]byte{0}, 7), 0xff, 0xfb, 0x90, 0x64)
	result, err := ParseFrameHeader(bytes.NewReader(data), cursor.Span{ByteOffset: 7, BitLength: 32}, Options{})
	if err != nil {
		t.Fatalf("ParseFrameHeader: %v", err)
	}
	header := result.Header
	assertMP3Span(t, header.Span, 7, 0, 32)
	assertMP3Span(t, header.SyncWordSpan, 7, 0, 11)
	assertMP3Span(t, header.VersionSpan, 8, 3, 2)
	assertMP3Span(t, header.LayerSpan, 8, 5, 2)
	assertMP3Span(t, header.ProtectionBitSpan, 8, 7, 1)
	assertMP3Span(t, header.BitrateIndexSpan, 9, 0, 4)
	assertMP3Span(t, header.SamplingFrequencyIndexSpan, 9, 4, 2)
	assertMP3Span(t, header.PaddingSpan, 9, 6, 1)
	assertMP3Span(t, header.PrivateSpan, 9, 7, 1)
	assertMP3Span(t, header.ChannelModeSpan, 10, 0, 2)
	assertMP3Span(t, header.ModeExtensionSpan, 10, 2, 2)
	assertMP3Span(t, header.CopyrightSpan, 10, 4, 1)
	assertMP3Span(t, header.OriginalSpan, 10, 5, 1)
	assertMP3Span(t, header.EmphasisSpan, 10, 6, 2)
}

func TestParseFrameHeaderFreeFormatIsCompleteButUnresolved(t *testing.T) {
	result, err := ParseFrameHeader(bytes.NewReader([]byte{0xff, 0xfb, 0x00, 0x64}), cursor.Span{BitLength: 32}, Options{})
	if err != nil {
		t.Fatalf("ParseFrameHeader: %v", err)
	}
	if !result.Complete || result.HasErrors() || result.Header.BitrateMode != BitrateModeFree ||
		result.Header.BitratePresent || result.Header.FrameLengthPresent {
		t.Fatalf("unexpected free-format state: %+v", result)
	}
	diagnostic := findMP3Diagnostic(result.Diagnostics, CodeFreeFormatUnresolved)
	if diagnostic == nil || diagnostic.Severity != SeverityWarning || diagnostic.RecoveryOffset != nil {
		t.Fatalf("missing free-format warning: %+v", result.Diagnostics)
	}
}

func TestParseFrameHeaderSyntaxDiagnostics(t *testing.T) {
	tests := []struct {
		name   string
		header []byte
		code   string
	}{
		{name: "sync", header: []byte{0xfe, 0xfb, 0x90, 0x64}, code: CodeSyncInvalid},
		{name: "reserved version", header: []byte{0xff, 0xeb, 0x90, 0x64}, code: CodeVersionReserved},
		{name: "reserved layer", header: []byte{0xff, 0xf9, 0x90, 0x64}, code: CodeLayerReserved},
		{name: "Layer I is not MP3", header: []byte{0xff, 0xff, 0x96, 0x00}, code: CodeLayerNotIII},
		{name: "bad bitrate", header: []byte{0xff, 0xfb, 0xf0, 0x64}, code: CodeBitrateIndexInvalid},
		{name: "reserved frequency", header: []byte{0xff, 0xfb, 0x9c, 0x64}, code: CodeSamplingFrequencyReserved},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			data := append([]byte{0, 0, 0}, test.header...)
			result, err := ParseFrameHeader(bytes.NewReader(data), cursor.Span{ByteOffset: 3, BitLength: 32}, Options{})
			if err != nil {
				t.Fatalf("ParseFrameHeader: %v", err)
			}
			diagnostic := findMP3Diagnostic(result.Diagnostics, test.code)
			if result.Complete || diagnostic == nil || diagnostic.Severity != SeverityError || diagnostic.RecoveryOffset == nil || *diagnostic.RecoveryOffset != 7 {
				t.Fatalf("unexpected malformed result: %+v", result)
			}
		})
	}
}

func TestParseFrameHeaderReservedEmphasisWarns(t *testing.T) {
	result, err := ParseFrameHeader(bytes.NewReader([]byte{0xff, 0xfb, 0x90, 0x02}), cursor.Span{BitLength: 32}, Options{})
	if err != nil {
		t.Fatalf("ParseFrameHeader: %v", err)
	}
	diagnostic := findMP3Diagnostic(result.Diagnostics, CodeEmphasisReserved)
	if !result.Complete || diagnostic == nil || diagnostic.Severity != SeverityWarning || diagnostic.RecoveryOffset != nil {
		t.Fatalf("unexpected reserved-emphasis state: %+v", result)
	}
}

func TestParseFrameHeaderTruncated(t *testing.T) {
	for length := 0; length < 4; length++ {
		t.Run(string(rune('0'+length))+" bytes", func(t *testing.T) {
			prefix := bytes.Repeat([]byte{0xaa}, 5)
			data := append(prefix, []byte{0xff, 0xfb, 0x90}[:length]...)
			result, err := ParseFrameHeader(bytes.NewReader(data), cursor.Span{ByteOffset: 5, BitLength: uint32(length * 8)}, Options{})
			if err != nil {
				t.Fatalf("ParseFrameHeader: %v", err)
			}
			diagnostic := findMP3Diagnostic(result.Diagnostics, CodeFrameHeaderTruncated)
			if result.Complete || result.HeaderPresent || result.BitsConsumed != 0 || diagnostic == nil ||
				diagnostic.Expected == nil || *diagnostic.Expected != 4 || diagnostic.Actual == nil || *diagnostic.Actual != uint64(length) ||
				diagnostic.RecoveryOffset == nil || *diagnostic.RecoveryOffset != uint64(5+length) {
				t.Fatalf("unexpected truncation result: %+v", result)
			}
		})
	}
}

func TestParseFrameHeaderDiagnosticLimitPreservesFatal(t *testing.T) {
	result, err := ParseFrameHeader(bytes.NewReader([]byte{0x00, 0xe9, 0xfc, 0x02}), cursor.Span{BitLength: 32}, Options{MaxDiagnostics: 2})
	if err != nil {
		t.Fatalf("ParseFrameHeader: %v", err)
	}
	if result.Complete || len(result.Diagnostics) != 2 || result.Diagnostics[0].Severity != SeverityError || result.Diagnostics[1].Code != CodeDiagnosticLimitReached {
		t.Fatalf("fatal/limit marker not retained: %+v", result.Diagnostics)
	}
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.RecoveryOffset == nil || *diagnostic.RecoveryOffset != 4 {
			t.Fatalf("diagnostic lost recovery offset: %+v", diagnostic)
		}
	}
}

func TestParseFrameHeaderArgumentAndSourceErrors(t *testing.T) {
	span := cursor.Span{BitLength: 32}
	if _, err := ParseFrameHeader(nil, span, Options{}); err == nil {
		t.Fatal("nil source accepted")
	}
	if _, err := ParseFrameHeader(bytes.NewReader(make([]byte, 4)), cursor.Span{BitOffset: 1, BitLength: 31}, Options{}); err == nil {
		t.Fatal("unaligned span accepted")
	}
	tooLarge := cursor.Span{BitLength: uint32((HardMaxPayloadBytes + 1) * 8)}
	if _, err := ParseFrameHeader(bytes.NewReader(nil), tooLarge, Options{}); err == nil {
		t.Fatal("oversized span accepted")
	}
	readErr := errors.New("MP3 fixture read failed")
	result, err := ParseFrameHeader(failingMP3ReaderAt{err: readErr}, span, Options{})
	if !errors.Is(err, readErr) || result.Complete || len(result.Diagnostics) != 0 {
		t.Fatalf("source error converted to syntax result: result=%+v err=%v", result, err)
	}
	result, err = ParseFrameHeader(fullReadEOFMP3ReaderAt{data: []byte{0xff, 0xfb, 0x90, 0x64}}, span, Options{})
	if err != nil || !result.Complete {
		t.Fatalf("full read accompanied by EOF rejected: result=%+v err=%v", result, err)
	}
}

func TestParseFrameHeaderReaderAtOffsetBoundary(t *testing.T) {
	reader := &offsetMP3ReaderAt{data: [4]byte{0xff, 0xfb, 0x90, 0x64}}
	start := uint64(math.MaxInt64 - 3)
	result, err := ParseFrameHeader(reader, cursor.Span{ByteOffset: start, BitLength: 32}, Options{})
	if err != nil || !result.Complete || reader.offset != int64(start) {
		t.Fatalf("last representable header failed: result=%+v offset=%d err=%v", result, reader.offset, err)
	}

	reader.called = false
	_, err = ParseFrameHeader(reader, cursor.Span{ByteOffset: start + 1, BitLength: 32}, Options{})
	var offsetErr *cursor.OffsetError
	if !errors.As(err, &offsetErr) || reader.called {
		t.Fatalf("unrepresentable ReaderAt range: err=%v called=%v", err, reader.called)
	}
}

func TestParseFrameHeaderOptionsAndDeterminism(t *testing.T) {
	if _, err := (Options{MaxDiagnostics: 1}).Normalized(); err == nil {
		t.Fatal("too-small diagnostic budget accepted")
	}
	if _, err := (Options{MaxDiagnostics: HardMaxDiagnostics + 1}).Normalized(); err == nil {
		t.Fatal("oversized diagnostic budget accepted")
	}
	data := []byte{0xff, 0xfb, 0x90, 0x64, 0xde, 0xad}
	first, err := ParseFrameHeader(bytes.NewReader(data), cursor.Span{BitLength: uint32(len(data) * 8)}, Options{})
	if err != nil {
		t.Fatalf("ParseFrameHeader: %v", err)
	}
	for i := 0; i < 100; i++ {
		next, parseErr := ParseFrameHeader(bytes.NewReader(data), cursor.Span{BitLength: uint32(len(data) * 8)}, Options{})
		if parseErr != nil || !reflect.DeepEqual(first, next) {
			t.Fatalf("nondeterministic parse %d: next=%+v err=%v", i, next, parseErr)
		}
	}
}

func TestParseFrameHeaderDoesNotReadPastFixedHeader(t *testing.T) {
	data := append([]byte{0xff, 0xfb, 0x90, 0x64}, bytes.Repeat([]byte{0xaa}, 4096)...)
	reader := &boundedMP3ReaderAt{data: data, maxExclusive: 4}
	result, err := ParseFrameHeader(reader, cursor.Span{BitLength: uint32(len(data) * 8)}, Options{})
	if err != nil || !result.Complete {
		t.Fatalf("bounded parse failed: result=%+v err=%v", result, err)
	}
	if reader.readCalls != 1 || reader.bytesRequested != 4 {
		t.Fatalf("read calls/bytes = %d/%d, want 1/4", reader.readCalls, reader.bytesRequested)
	}
}

func assertMP3Span(t testing.TB, got cursor.Span, byteOffset uint64, bitOffset uint8, bitLength uint32) {
	t.Helper()
	want := cursor.Span{ByteOffset: byteOffset, BitOffset: bitOffset, BitLength: bitLength}
	if got != want {
		t.Fatalf("span = %+v, want %+v", got, want)
	}
}

func findMP3Diagnostic(items []Diagnostic, code string) *Diagnostic {
	for index := range items {
		if items[index].Code == code {
			return &items[index]
		}
	}
	return nil
}

type failingMP3ReaderAt struct{ err error }

func (reader failingMP3ReaderAt) ReadAt([]byte, int64) (int, error) { return 0, reader.err }

type fullReadEOFMP3ReaderAt struct{ data []byte }

func (reader fullReadEOFMP3ReaderAt) ReadAt(output []byte, offset int64) (int, error) {
	if offset < 0 || offset >= int64(len(reader.data)) {
		return 0, io.EOF
	}
	return copy(output, reader.data[offset:]), io.EOF
}

type boundedMP3ReaderAt struct {
	data           []byte
	maxExclusive   int64
	readCalls      int
	bytesRequested int
}

func (reader *boundedMP3ReaderAt) ReadAt(output []byte, offset int64) (int, error) {
	reader.readCalls++
	reader.bytesRequested += len(output)
	if offset < 0 || offset+int64(len(output)) > reader.maxExclusive {
		return 0, errors.New("read crossed fixed MP3 header")
	}
	return copy(output, reader.data[offset:]), nil
}

type offsetMP3ReaderAt struct {
	data   [4]byte
	offset int64
	called bool
}

func (reader *offsetMP3ReaderAt) ReadAt(output []byte, offset int64) (int, error) {
	reader.called = true
	reader.offset = offset
	return copy(output, reader.data[:]), nil
}
