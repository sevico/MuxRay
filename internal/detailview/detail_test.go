package detailview

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"math"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/sevico/muxray/internal/flv"
)

func fixture(t *testing.T, kind uint8, payload []byte) ([]byte, flv.TagRecord) {
	t.Helper()
	data := make([]byte, 13+11+len(payload)+4)
	copy(data, []byte{'F', 'L', 'V', 1, 5, 0, 0, 0, 9})
	data[13] = kind
	data[14], data[15], data[16] = byte(len(payload)>>16), byte(len(payload)>>8), byte(len(payload))
	copy(data[24:], payload)
	binary.BigEndian.PutUint32(data[len(data)-4:], uint32(11+len(payload)))
	scan, err := flv.Scan(bytes.NewReader(data), uint64(len(data)), flv.Options{})
	if err != nil || scan.Index().Len() != 1 {
		t.Fatalf("fixture scan: %+v %v", scan, err)
	}
	return data, scan.Tags[0]
}

func short(s string) []byte {
	p := []byte{2, byte(len(s) >> 8), byte(len(s))}
	return append(p, []byte(s)...)
}
func named(name string, value []byte) []byte {
	p := []byte{byte(len(name) >> 8), byte(len(name))}
	p = append(p, []byte(name)...)
	return append(p, value...)
}
func metadata(value []byte) []byte { return append(short("onMetaData"), value...) }
func number(bits uint64) []byte {
	p := make([]byte, 9)
	binary.BigEndian.PutUint64(p[1:], bits)
	return p
}
func parseFixture(t *testing.T, kind uint8, payload []byte) *Detail {
	t.Helper()
	data, tag := fixture(t, kind, payload)
	detail, err := Parse(context.Background(), bytes.NewReader(data), tag)
	if err != nil {
		t.Fatal(err)
	}
	assertBounded(t, detail)
	for _, f := range detail.Fields {
		if f.Span == nil {
			continue
		}
		offset, err := strconv.ParseUint(f.Span.ByteOffset, 10, 64)
		if err != nil || offset < 24 || offset+(uint64(f.Span.BitOffset)+uint64(f.Span.BitLength)+7)/8 > 24+uint64(len(payload)) {
			t.Fatalf("field span outside payload: %+v", f)
		}
	}
	return detail
}
func find(t *testing.T, d *Detail, name string) Field {
	t.Helper()
	for _, f := range d.Fields {
		if f.Name == name {
			return f
		}
	}
	t.Fatalf("missing field %s: %+v", name, d.Fields)
	return Field{}
}
func hasDiagnostic(d *Detail, code string) bool {
	for _, v := range d.Diagnostics {
		if v.Code == code {
			return true
		}
	}
	return false
}
func assertBounded(t *testing.T, d *Detail) {
	t.Helper()
	encoded, err := json.Marshal(d)
	if err != nil || len(encoded) > MaxJSONBytes || len(d.Fields) > MaxFields || len(d.Diagnostics) > MaxDiagnostics {
		t.Fatalf("unbounded DTO: bytes=%d fields=%d diagnostics=%d err=%v", len(encoded), len(d.Fields), len(d.Diagnostics), err)
	}
	seen := map[string]bool{}
	for _, f := range d.Fields {
		if seen[f.ID] || (f.ParentID != "" && !seen[f.ParentID]) {
			t.Fatalf("invalid field tree: %+v", f)
		}
		seen[f.ID] = true
		for _, s := range []string{f.Name, f.Type, f.Value} {
			if len(s) > MaxPreviewBytes || !utf8.ValidString(s) {
				t.Fatalf("invalid preview: %q", s)
			}
		}
	}
}

func TestAMFObjectSpansAndValues(t *testing.T) {
	obj := []byte{3}
	obj = append(obj, named("width", number(math.Float64bits(1920)))...)
	obj = append(obj, named("label", short("视频"))...)
	obj = append(obj, 0, 0, 9)
	d := parseFixture(t, uint8(flv.TagTypeScript), metadata(obj))
	if d.Kind != "amf0ScriptData" || !d.Complete || d.Truncated || len(d.Diagnostics) != 0 {
		t.Fatalf("detail: %+v", d)
	}
	width := find(t, d, "width")
	event := find(t, d, "eventValue")
	if width.ParentID != event.ID || width.Type != "number" || width.Span.ByteOffset != "45" || width.Span.BitLength != 72 {
		t.Fatalf("width: %+v event: %+v", width, event)
	}
	var property, scalar bool
	for _, f := range d.Fields {
		if f.ParentID == width.ID && f.Name == "propertyName" {
			property = f.Value == "width" && f.Span.ByteOffset == "40" && f.Span.BitLength == 40
		}
		if f.ParentID == width.ID && f.Name == "value" {
			scalar = strings.HasPrefix(f.Value, "1920 (IEEE-754") && f.Span.ByteOffset == "46" && f.Span.BitLength == 64
		}
	}
	if !property || !scalar {
		t.Fatalf("lost property/value spans: %+v", d.Fields)
	}
}

func TestMalformedAMFPreservesOnlyReadFields(t *testing.T) {
	d := parseFixture(t, uint8(flv.TagTypeScript), metadata([]byte{0, 1, 2}))
	if d.Complete || !d.Truncated || !hasDiagnostic(d, "AMF0_NUMBER_TRUNCATED") {
		t.Fatalf("partial: %+v", d)
	}
	event := find(t, d, "eventValue")
	for _, f := range d.Fields {
		if f.ParentID == event.ID && f.Name == "value" {
			t.Fatalf("invented truncated scalar: %+v", f)
		}
	}
	bad := parseFixture(t, uint8(flv.TagTypeScript), metadata([]byte{0xff}))
	if bad.Complete || !hasDiagnostic(bad, "AMF0_MARKER_UNKNOWN") {
		t.Fatalf("unknown marker: %+v", bad)
	}
	// A uint32 byte length must not wrap during comparison with a bit span.
	oversize := parseFixture(t, uint8(flv.TagTypeScript), metadata([]byte{12, 0x20, 0, 0, 1, 'x'}))
	event = find(t, oversize, "eventValue")
	for _, f := range oversize.Fields {
		if f.ParentID == event.ID && f.Name == "value" {
			t.Fatalf("overflow fabricated complete string: %+v", f)
		}
	}
}

func TestAMFSpecialFloatsAndReferences(t *testing.T) {
	values := []uint64{0x7ff8000000000042, math.Float64bits(math.Inf(1)), math.Float64bits(math.Inf(-1)), 0x8000000000000000}
	for _, bits := range values {
		d := parseFixture(t, uint8(flv.TagTypeScript), metadata(number(bits)))
		event := find(t, d, "eventValue")
		found := false
		for _, f := range d.Fields {
			if f.ParentID == event.ID && f.Name == "value" {
				found = f.Value == floatValue(bits)
			}
		}
		if !found || !d.Complete {
			t.Fatalf("special float %x: %+v", bits, d)
		}
	}
	p := metadata([]byte{3, 0, 0, 9})
	p = append(p, 7, 0, 0)
	d := parseFixture(t, uint8(flv.TagTypeScript), p)
	if !strings.Contains(find(t, d, "referenceIndex").Value, "valid=true") {
		t.Fatalf("reference: %+v", d)
	}
}

func TestAMFPreviewAndParserBudgets(t *testing.T) {
	t.Run("UTF8 preview", func(t *testing.T) {
		d := parseFixture(t, uint8(flv.TagTypeScript), metadata(short(strings.Repeat("界", 120))))
		if !d.Complete || !d.Truncated {
			t.Fatalf("preview flags: %+v", d)
		}
		event := find(t, d, "eventValue")
		found := false
		for _, f := range d.Fields {
			if f.ParentID == event.ID && f.Name == "value" {
				found = strings.HasSuffix(f.Value, "…") && len(f.Value) <= MaxPreviewBytes && utf8.ValidString(f.Value) && f.Span.BitLength == 360*8
			}
		}
		if !found {
			t.Fatalf("preview lost actual span: %+v", d.Fields)
		}
	})
	t.Run("invalid UTF8", func(t *testing.T) {
		d := parseFixture(t, uint8(flv.TagTypeScript), metadata(short(string([]byte{0xff, 0xff, 'x'}))))
		if !d.Truncated || !hasDiagnostic(d, "AMF0_UTF8_INVALID") {
			t.Fatalf("invalid UTF8: %+v", d)
		}
	})
	t.Run("string budget", func(t *testing.T) {
		p := []byte{12, 0, 1, 0, 1}
		p = append(p, bytes.Repeat([]byte{'x'}, MaxAMFStringBytes+1)...)
		d := parseFixture(t, uint8(flv.TagTypeScript), metadata(p))
		if !d.Truncated || !hasDiagnostic(d, "AMF0_STRING_LIMIT_EXCEEDED") {
			t.Fatalf("string budget: %+v", d)
		}
	})
	t.Run("node budget", func(t *testing.T) {
		d := parseFixture(t, uint8(flv.TagTypeScript), metadata(bytes.Repeat([]byte{5}, MaxAMFNodes+1)))
		if !d.Truncated || d.Complete || !hasDiagnostic(d, "AMF0_NODE_LIMIT_EXCEEDED") {
			t.Fatalf("node budget: complete=%t truncated=%t diagnostics=%+v", d.Complete, d.Truncated, d.Diagnostics)
		}
	})
	t.Run("projection budget", func(t *testing.T) {
		d := parseFixture(t, uint8(flv.TagTypeScript), metadata(bytes.Repeat([]byte{5}, 300)))
		if !d.Complete || !d.Truncated || len(d.Fields) != MaxFields {
			t.Fatalf("projection budget: complete=%t truncated=%t count=%d", d.Complete, d.Truncated, len(d.Fields))
		}
	})
}

func TestExactJSONBudgetAccountsForEscaping(t *testing.T) {
	b := builder{detail: Detail{Kind: "amf0ScriptData", Complete: true, Fields: []Field{}, Diagnostics: []Diagnostic{}}}
	s := strings.Repeat("\x00", MaxPreviewBytes)
	for i := 0; i < MaxDiagnostics; i++ {
		b.diagnostic(s, s, math.MaxUint64, 7, s, nil, nil)
	}
	for i := 0; i < MaxFields; i++ {
		b.add("", s, s, s, &Span{ByteOffset: decimal(math.MaxUint64), BitOffset: 7, BitLength: math.MaxUint32})
	}
	assertBounded(t, &b.detail)
	encoded, _ := json.Marshal(b.detail)
	if !b.detail.Truncated || len(encoded) < MaxJSONBytes-10000 {
		t.Fatalf("wire budget not exercised: %d", len(encoded))
	}
}

func TestAACConfigAndRaw(t *testing.T) {
	d := parseFixture(t, uint8(flv.TagTypeAudio), []byte{0xaf, 0, 0x12, 0x10})
	if d.Kind != "aacAudio" || !d.Complete || d.Truncated {
		t.Fatalf("ASC: %+v", d)
	}
	format := find(t, d, "soundFormat")
	rate := find(t, d, "samplingFrequencyIndex")
	channel := find(t, d, "channelConfiguration")
	if format.Span.ByteOffset != "24" || format.Span.BitLength != 4 || rate.Span.ByteOffset != "26" || rate.Span.BitOffset != 5 || rate.Span.BitLength != 4 || channel.Span.ByteOffset != "27" || channel.Span.BitOffset != 1 || channel.Span.BitLength != 4 {
		t.Fatalf("ASC spans: %+v %+v %+v", format, rate, channel)
	}
	if !strings.Contains(find(t, d, "initialAudioObjectType").Value, "aacLC") || !strings.Contains(find(t, d, "samplingFrequency").Value, "44100 Hz") || !strings.Contains(channel.Value, "stereo") {
		t.Fatalf("ASC labels: %+v", d.Fields)
	}
	raw := parseFixture(t, uint8(flv.TagTypeAudio), []byte{0xaf, 1, 0xde, 0xad})
	f := find(t, raw, "rawAAC")
	if !raw.Complete || f.Span.ByteOffset != "26" || f.Span.BitLength != 16 || !strings.Contains(f.Value, "decoding is not supported") {
		t.Fatalf("raw: %+v", raw)
	}
	data, tag := fixture(t, 8, []byte{0xaf, 1, 0xde, 0xad})
	// The opaque raw data is deliberately unavailable to this ReaderAt. Only
	// the two-byte FLV framing is required, with no hidden access-unit decoding.
	if detail, err := Parse(context.Background(), bytes.NewReader(data[:26]), tag); err != nil || !detail.Complete {
		t.Fatalf("raw payload was read: %+v %v", detail, err)
	}
}

func TestAACMalformedIsPartial(t *testing.T) {
	for _, payload := range [][]byte{{0xaf}, {0xaf, 0, 0x12}, {0xaf, 2}} {
		d := parseFixture(t, uint8(flv.TagTypeAudio), payload)
		if d.Complete || len(d.Diagnostics) == 0 {
			t.Fatalf("missing partial diagnostics: %+v", d)
		}
		if len(payload) < 3 && payload[len(payload)-1] != 2 && !d.Truncated {
			t.Fatalf("missing truncation: %+v", d)
		}
	}
}

func packed(values ...uint64) []byte {
	var out []byte
	bits := 0
	for i := 0; i < len(values); i += 2 {
		for n := int(values[i+1]) - 1; n >= 0; n-- {
			if bits%8 == 0 {
				out = append(out, 0)
			}
			out[len(out)-1] |= byte((values[i]>>n)&1) << (7 - bits%8)
			bits++
		}
	}
	return out
}

func TestAACExtensionsAndConditionalBits(t *testing.T) {
	for _, initial := range []uint64{5, 29} {
		asc := packed(initial, 5, 3, 4, 2, 4, 6, 4, 2, 5, 0, 3)
		d := parseFixture(t, 8, append([]byte{0xaf, 0}, asc...))
		if !d.Complete || !strings.Contains(find(t, d, "coreAudioObjectType").Value, "aacLC") || !strings.Contains(find(t, d, "extensionSamplingFrequency").Value, "24000 Hz") {
			t.Fatalf("extension: %+v", d)
		}
		field := find(t, d, "coreAudioObjectType")
		if field.Span.ByteOffset != "28" || field.Span.BitOffset != 1 || field.Span.BitLength != 5 {
			t.Fatalf("core AOT span: %+v", field)
		}
	}
	asc := packed(20, 5, 4, 4, 2, 4, 1, 1, 1, 1, 0x123, 14, 1, 1, 5, 3, 1, 1, 0, 1, 1, 1, 0, 1, 1, 2)
	d := parseFixture(t, 8, append([]byte{0xaf, 0}, asc...))
	if !d.Complete || find(t, d, "coreCoderDelay").Value != "291" || find(t, d, "layerNr").Value != "5" || find(t, d, "epConfig").Value != "1" {
		t.Fatalf("conditional config: %+v", d)
	}
	if find(t, d, "coreCoderDelay").Span.BitLength != 14 || find(t, d, "coreCoderDelay").Span.BitOffset != 7 {
		t.Fatal("conditional bit span lost")
	}
	escaped := parseFixture(t, 8, append([]byte{0xaf, 0}, packed(31, 5, 10, 6, 4, 4, 2, 4)...))
	if escaped.Complete || !hasDiagnostic(escaped, "AAC_AUDIO_OBJECT_TYPE_UNSUPPORTED") || find(t, escaped, "escape").Value != "10" {
		t.Fatalf("escaped AOT: %+v", escaped)
	}
}

func TestAMFTypedObjectDateAndDiagnosticBudget(t *testing.T) {
	date := append([]byte{11}, number(math.Float64bits(1000))[1:]...)
	date = append(date, 0, 0)
	object := []byte{16, 0, 4, 'D', 'a', 't', 'a'}
	object = append(object, named("created", date)...)
	object = append(object, 0, 0, 9)
	d := parseFixture(t, 18, metadata(object))
	if !d.Complete || find(t, d, "className").Value != "Data" || find(t, d, "timezoneMinutes").Value != "0" || !strings.Contains(find(t, d, "milliseconds").Value, "1000 (IEEE-754") {
		t.Fatalf("typed date: %+v", d)
	}
	p := metadata(nil)
	for i := 0; i < 100; i++ {
		p = append(p, 1, 2)
	}
	p = append(p, 0xff)
	limited := parseFixture(t, 18, p)
	if !limited.Truncated || limited.Complete || len(limited.Diagnostics) != MaxDiagnostics || !hasDiagnostic(limited, "AMF0_DIAGNOSTIC_LIMIT_REACHED") || !hasDiagnostic(limited, "AMF0_MARKER_UNKNOWN") {
		t.Fatalf("diagnostic budget: %+v", limited.Diagnostics)
	}
}

type shiftedReader struct {
	base int64
	data []byte
}

func (s shiftedReader) ReadAt(p []byte, off int64) (int, error) {
	return bytes.NewReader(s.data).ReadAt(p, off-s.base)
}

func TestLargeOffsetsAndIDsStayDecimal(t *testing.T) {
	data, tag := fixture(t, uint8(flv.TagTypeScript), metadata([]byte{5}))
	const delta uint64 = 1<<53 + 123
	tag.Offset += delta
	tag.ID = math.MaxUint64
	d, err := Parse(context.Background(), shiftedReader{base: int64(delta), data: data}, tag)
	if err != nil {
		t.Fatal(err)
	}
	if find(t, d, "tagId").Value != "18446744073709551615" || find(t, d, "eventName").Span.ByteOffset != strconv.FormatUint(delta+24, 10) {
		t.Fatalf("rounded identity: %+v", d)
	}
	assertBounded(t, d)
}

type trackingReader struct {
	source  io.ReaderAt
	reads   int
	cancel  context.CancelFunc
	failure error
}

func (r *trackingReader) ReadAt(p []byte, off int64) (int, error) {
	r.reads++
	if r.cancel != nil {
		r.cancel()
	}
	if r.failure != nil {
		return 0, r.failure
	}
	return r.source.ReadAt(p, off)
}

func TestUnsupportedSelectionAndSourceFailures(t *testing.T) {
	for _, item := range []struct {
		kind    uint8
		payload []byte
		want    error
	}{{9, []byte{0x17, 0}, ErrUnsupported}, {8, nil, ErrUnsupported}, {8, []byte{0x2f, 1}, ErrUnsupported}, {18 | 0x20, []byte{5}, ErrFiltered}} {
		data, tag := fixture(t, item.kind, item.payload)
		reader := &trackingReader{source: bytes.NewReader(data)}
		if _, err := Parse(context.Background(), reader, tag); !errors.Is(err, item.want) {
			t.Fatalf("selection error: %v want %v", err, item.want)
		}
		if item.kind != 8 && reader.reads != 0 {
			t.Fatal("rejected tag read source")
		}
	}
	data, tag := fixture(t, 18, metadata([]byte{5}))
	truncated := tag
	truncated.AvailableDataSize--
	if _, err := Parse(context.Background(), bytes.NewReader(data), truncated); !errors.Is(err, ErrTruncated) {
		t.Fatalf("truncated: %v", err)
	}
	if _, err := Parse(context.Background(), nil, tag); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("nil source: %v", err)
	}
	bad := tag
	bad.Offset = math.MaxUint64
	if _, err := Parse(context.Background(), bytes.NewReader(data), bad); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("overflow: %v", err)
	}
	sentinel := errors.New("source failed")
	if _, err := Parse(context.Background(), &trackingReader{failure: sentinel}, tag); !errors.Is(err, sentinel) {
		t.Fatalf("source failure: %v", err)
	}
}

func TestCancellationBeforeAndDuringRead(t *testing.T) {
	data, tag := fixture(t, 18, metadata([]byte{5}))
	for _, before := range []bool{true, false} {
		ctx, cancel := context.WithCancel(context.Background())
		reader := &trackingReader{source: bytes.NewReader(data), cancel: cancel}
		if before {
			cancel()
		}
		if _, err := Parse(ctx, reader, tag); !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation: %v", err)
		}
		if before && reader.reads != 0 {
			t.Fatal("read after initial cancellation")
		}
		cancel()
	}
}
