package flv

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"testing"
)

type fixtureTag struct {
	rawTypeFlags byte
	payload      []byte
	timestamp    uint32
	streamID     uint32
	previousSize *uint32
}

func makeFLV(t testing.TB, typeFlags byte, extra []byte, tags ...fixtureTag) []byte {
	t.Helper()
	var output bytes.Buffer
	output.WriteString("FLV")
	output.WriteByte(1)
	output.WriteByte(typeFlags)
	writeUint32(t, &output, uint32(HeaderSize)+uint32(len(extra)))
	output.Write(extra)
	writeUint32(t, &output, 0)

	for _, tag := range tags {
		if len(tag.payload) > 0xffffff {
			t.Fatal("test payload exceeds FLV DataSize")
		}
		output.WriteByte(tag.rawTypeFlags)
		writeUint24(&output, uint32(len(tag.payload)))
		writeUint24(&output, tag.timestamp&0xffffff)
		output.WriteByte(byte(tag.timestamp >> 24))
		writeUint24(&output, tag.streamID)
		output.Write(tag.payload)
		previousSize := uint32(TagHeaderSize) + uint32(len(tag.payload))
		if tag.previousSize != nil {
			previousSize = *tag.previousSize
		}
		writeUint32(t, &output, previousSize)
	}
	return output.Bytes()
}

func writeUint24(output *bytes.Buffer, value uint32) {
	output.WriteByte(byte(value >> 16))
	output.WriteByte(byte(value >> 8))
	output.WriteByte(byte(value))
}

func writeUint32(t testing.TB, output *bytes.Buffer, value uint32) {
	t.Helper()
	if err := binary.Write(output, binary.BigEndian, value); err != nil {
		t.Fatalf("binary.Write: %v", err)
	}
}

func TestScanValidFLV(t *testing.T) {
	data := makeFLV(t, 0x05, []byte{0xaa, 0xbb},
		fixtureTag{rawTypeFlags: byte(TagTypeScript), payload: []byte{1, 2, 3}, timestamp: 0x01000002},
		fixtureTag{rawTypeFlags: byte(TagTypeAudio), payload: []byte{0xaf, 0x00}, timestamp: 23},
	)

	result, err := Scan(bytes.NewReader(data), uint64(len(data)), Options{})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if !result.Complete || result.HasErrors() {
		t.Fatalf("expected complete valid scan, got complete=%v diagnostics=%+v", result.Complete, result.Diagnostics)
	}
	if !result.HeaderPresent || !result.Header.HasAudio || !result.Header.HasVideo {
		t.Fatalf("unexpected header: %+v", result.Header)
	}
	if result.Header.DataOffset != 11 || result.Header.ExtraDataSize != 2 {
		t.Fatalf("unexpected data offset fields: %+v", result.Header)
	}
	if len(result.Tags) != 2 {
		t.Fatalf("got %d tags, want 2", len(result.Tags))
	}

	first := result.Tags[0]
	if first.ID != 0 || first.Offset != 15 || first.Type() != TagTypeScript {
		t.Fatalf("unexpected first tag identity: %+v", first)
	}
	if first.DTS != 0x01000002 || first.DataSize != 3 || first.AvailableDataSize != 3 {
		t.Fatalf("unexpected first tag values: %+v", first)
	}
	if !first.PayloadComplete() || !first.PreviousTagSizePresent() || !first.PreviousTagSizeValid() {
		t.Fatalf("unexpected first tag completion: %+v", first)
	}
	headerSpan := first.HeaderSpan()
	if headerSpan.ByteOffset != first.Offset || headerSpan.BitLength != 88 {
		t.Fatalf("unexpected first header span: %+v", headerSpan)
	}
	payloadSpan := first.PayloadSpan()
	if payloadSpan.ByteOffset != first.Offset+TagHeaderSize || payloadSpan.BitLength != 24 {
		t.Fatalf("unexpected first payload span: %+v", payloadSpan)
	}
	if result.BytesScanned != uint64(len(data)) {
		t.Fatalf("bytes scanned = %d, want %d", result.BytesScanned, len(data))
	}
}

func TestScanStructuralFailures(t *testing.T) {
	validHeader := []byte{'F', 'L', 'V', 1, 0, 0, 0, 0, 9}
	validEmpty := append(append([]byte{}, validHeader...), 0, 0, 0, 0)
	invalidOffset := append([]byte{}, validHeader...)
	invalidOffset[8] = 8
	outOfBoundsOffset := append([]byte{}, validHeader...)
	outOfBoundsOffset[8] = 20

	tests := []struct {
		name string
		data []byte
		code string
	}{
		{name: "short header", data: []byte("FLV"), code: CodeHeaderTruncated},
		{name: "invalid signature", data: append([]byte("BAD"), validHeader[3:]...), code: CodeSignatureInvalid},
		{name: "data offset inside header", data: invalidOffset, code: CodeDataOffsetInvalid},
		{name: "data offset beyond source", data: outOfBoundsOffset, code: CodeDataOffsetOutOfBounds},
		{name: "missing previous tag size zero", data: validHeader, code: CodePreviousTagSize0Truncated},
		{name: "truncated tag header", data: append(validEmpty, 0x12, 0), code: CodeTagHeaderTruncated},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := Scan(bytes.NewReader(test.data), uint64(len(test.data)), Options{})
			if err != nil {
				t.Fatalf("Scan: %v", err)
			}
			if result.Complete {
				t.Fatal("invalid input reported a complete scan")
			}
			assertDiagnostic(t, result, test.code)
		})
	}
}

func TestScanTruncatedPayloadAndPreviousTagSize(t *testing.T) {
	full := makeFLV(t, 0, nil, fixtureTag{rawTypeFlags: byte(TagTypeScript), payload: []byte{1, 2, 3}})

	t.Run("payload", func(t *testing.T) {
		data := append([]byte{}, full[:len(full)-6]...)
		result, err := Scan(bytes.NewReader(data), uint64(len(data)), Options{})
		if err != nil {
			t.Fatalf("Scan: %v", err)
		}
		assertDiagnostic(t, result, CodeTagBodyTruncated)
		if len(result.Tags) != 1 || result.Tags[0].PayloadComplete() {
			t.Fatalf("unexpected partial tag: %+v", result.Tags)
		}
	})

	t.Run("previous tag size", func(t *testing.T) {
		data := append([]byte{}, full[:len(full)-2]...)
		result, err := Scan(bytes.NewReader(data), uint64(len(data)), Options{})
		if err != nil {
			t.Fatalf("Scan: %v", err)
		}
		assertDiagnostic(t, result, CodePreviousTagSizeTruncated)
		if len(result.Tags) != 1 || !result.Tags[0].PayloadComplete() || result.Tags[0].PreviousTagSizePresent() {
			t.Fatalf("unexpected tag state: %+v", result.Tags)
		}
	})
}

func TestScanWarningsAndContinuedIndexing(t *testing.T) {
	wrongPreviousSize := uint32(99)
	data := makeFLV(t, 0xf8, nil,
		fixtureTag{
			rawTypeFlags: 0xe7,
			payload:      []byte{1},
			streamID:     1,
			previousSize: &wrongPreviousSize,
		},
		fixtureTag{rawTypeFlags: byte(TagTypeVideo), payload: []byte{2}},
	)
	data[3] = 2
	data[9] = 1

	result, err := Scan(bytes.NewReader(data), uint64(len(data)), Options{})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if !result.Complete || len(result.Tags) != 2 {
		t.Fatalf("scanner did not continue after warnings: %+v", result)
	}
	for _, code := range []string{
		CodeVersionUnsupported,
		CodeTypeFlagsReserved,
		CodePreviousTagSize0NonZero,
		CodeTagReservedBitsNonZero,
		CodeTagFilterUnsupported,
		CodeTagTypeUnknown,
		CodeStreamIDNonZero,
		CodePreviousTagSizeMismatch,
	} {
		assertDiagnostic(t, result, code)
	}
}

func TestScanLimitsDiagnostics(t *testing.T) {
	data := makeFLV(t, 0, nil,
		fixtureTag{rawTypeFlags: 1},
		fixtureTag{rawTypeFlags: 2},
		fixtureTag{rawTypeFlags: 3},
	)

	t.Run("tag limit", func(t *testing.T) {
		result, err := Scan(bytes.NewReader(data), uint64(len(data)), Options{MaxTags: 1})
		if err != nil {
			t.Fatalf("Scan: %v", err)
		}
		assertDiagnostic(t, result, CodeTagLimitExceeded)
		if result.Complete || len(result.Tags) != 1 {
			t.Fatalf("unexpected limited result: %+v", result)
		}
	})

	t.Run("diagnostic limit", func(t *testing.T) {
		result, err := Scan(bytes.NewReader(data), uint64(len(data)), Options{MaxDiagnostics: 2})
		if err != nil {
			t.Fatalf("Scan: %v", err)
		}
		if len(result.Diagnostics) != 2 || result.Diagnostics[0].Code != CodeTagTypeUnknown || result.Diagnostics[1].Code != CodeDiagnosticLimitReached {
			t.Fatalf("unexpected diagnostics: %+v", result.Diagnostics)
		}
	})

}

func TestScanReadErrorAndNilSource(t *testing.T) {
	if _, err := Scan(nil, 0, Options{}); err == nil {
		t.Fatal("Scan(nil) succeeded")
	}
	reader := errorReaderAt{err: errors.New("storage failed")}
	if _, err := Scan(reader, HeaderSize, Options{}); !errors.Is(err, reader.err) {
		t.Fatalf("Scan error = %v, want wrapped %v", err, reader.err)
	}
}

func TestScanRejectsUnsafeResourceLimits(t *testing.T) {
	data := []byte{'F', 'L', 'V', 1, 0, 0, 0, 0, 9, 0, 0, 0, 0}
	for _, options := range []Options{
		{MaxTags: HardMaxTags + 1},
		{MaxDiagnostics: MinDiagnostics - 1},
		{MaxDiagnostics: HardMaxDiagnostics + 1},
	} {
		if _, err := Scan(bytes.NewReader(data), uint64(len(data)), options); err == nil {
			t.Fatalf("Scan accepted unsafe options: %+v", options)
		}
	}
}

func TestInitialTagCapacityIsBoundedAndOverflowSafe(t *testing.T) {
	tests := []struct {
		remaining uint64
		maxTags   uint64
		want      int
	}{
		{remaining: 0, maxTags: 10, want: 0},
		{remaining: 1, maxTags: 10, want: 1},
		{remaining: 15, maxTags: 10, want: 1},
		{remaining: 16, maxTags: 10, want: 2},
		{remaining: ^uint64(0), maxTags: 10, want: 10},
	}
	for _, test := range tests {
		if got := initialTagCapacity(test.remaining, test.maxTags); got != test.want {
			t.Errorf("initialTagCapacity(%d, %d) = %d, want %d", test.remaining, test.maxTags, got, test.want)
		}
	}
}

type errorReaderAt struct{ err error }

func (r errorReaderAt) ReadAt([]byte, int64) (int, error) { return 0, r.err }

func assertDiagnostic(t *testing.T, result ScanResult, code string) {
	t.Helper()
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Code == code {
			return
		}
	}
	t.Fatalf("missing diagnostic %s in %+v", code, result.Diagnostics)
}

func FuzzFLVHeader(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte("FLV"))
	f.Add([]byte{'F', 'L', 'V', 1, 5, 0, 0, 0, 9, 0, 0, 0, 0})
	f.Fuzz(func(t *testing.T, data []byte) {
		result, _ := Scan(bytes.NewReader(data), uint64(len(data)), Options{MaxTags: 128, MaxDiagnostics: 32})
		if result.BytesScanned > uint64(len(data)) {
			t.Fatalf("scanner passed source boundary: %d > %d", result.BytesScanned, len(data))
		}
	})
}

func FuzzTag(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{0x12, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0})
	f.Add([]byte{0x09, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0xaa, 0, 0, 0, 12})
	f.Fuzz(func(t *testing.T, tagBytes []byte) {
		prefix := []byte{'F', 'L', 'V', 1, 0, 0, 0, 0, 9, 0, 0, 0, 0}
		data := make([]byte, 0, len(prefix)+len(tagBytes))
		data = append(data, prefix...)
		data = append(data, tagBytes...)
		result, _ := Scan(bytes.NewReader(data), uint64(len(data)), Options{MaxTags: 128, MaxDiagnostics: 32})
		if result.BytesScanned > uint64(len(data)) {
			t.Fatalf("scanner passed source boundary: %d > %d", result.BytesScanned, len(data))
		}
	})
}

func BenchmarkScanTags(b *testing.B) {
	payload := make([]byte, 64)
	tags := make([]fixtureTag, 10_000)
	for index := range tags {
		tags[index] = fixtureTag{rawTypeFlags: byte(TagTypeVideo), payload: payload, timestamp: uint32(index * 40)}
	}
	data := makeFLV(b, 1, nil, tags...)
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := Scan(bytes.NewReader(data), uint64(len(data)), Options{}); err != nil && !errors.Is(err, io.EOF) {
			b.Fatal(err)
		}
	}
}

var benchmarkScanResult ScanResult

func BenchmarkScanOneMillionEmptyTags(b *testing.B) {
	const tagCount = 1_000_000
	data := make([]byte, 13+tagCount*15)
	copy(data, []byte{'F', 'L', 'V', 1, 0, 0, 0, 0, 9})
	for offset := 13; offset < len(data); offset += 15 {
		data[offset] = byte(TagTypeScript)
		binary.BigEndian.PutUint32(data[offset+11:offset+15], uint32(TagHeaderSize))
	}

	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		result, err := Scan(bytes.NewReader(data), uint64(len(data)), Options{})
		if err != nil {
			b.Fatal(err)
		}
		if !result.Complete || len(result.Tags) != tagCount {
			b.Fatalf("unexpected scan: complete=%v tags=%d", result.Complete, len(result.Tags))
		}
		benchmarkScanResult = result
	}
}
