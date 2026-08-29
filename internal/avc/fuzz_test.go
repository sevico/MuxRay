package avc

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/sevico/muxray/internal/cursor"
)

func FuzzDecoderConfigurationRecord(f *testing.F) {
	f.Add([]byte{})
	f.Add(mustDecodeHex(highConfigurationHex))
	f.Add(mustDecodeHex(baselineConfigurationHex))
	f.Add(mustDecodeHex("0142001effe000"))
	f.Add(mustDecodeHex("0264000cffe000"))
	f.Add(mustDecodeHex("0164000cffe1000467000003"))

	options := Options{
		MaxDiagnostics: 16, MaxParameterSets: 32,
		MaxNALUnits: 512, MaxParameterSetBytes: 4 << 10,
	}
	normalized, err := options.Normalized()
	if err != nil {
		f.Fatal(err)
	}
	f.Fuzz(func(t *testing.T, payload []byte) {
		if uint64(len(payload)) > HardMaxPayloadBytes {
			return
		}
		const base = 7
		source := make([]byte, base+len(payload)+1)
		copy(source[base:], payload)
		span := cursor.Span{ByteOffset: base, BitLength: uint32(len(payload) * 8)}
		result, err := ParseDecoderConfigurationRecord(bytes.NewReader(source), span, options)
		if err != nil {
			t.Fatalf("ParseDecoderConfigurationRecord: %v", err)
		}
		assertConfigurationInvariants(t, result, normalized, base, len(payload))

		repeated, err := ParseDecoderConfigurationRecord(bytes.NewReader(source), span, options)
		if err != nil {
			t.Fatalf("repeat ParseDecoderConfigurationRecord: %v", err)
		}
		if !reflect.DeepEqual(result, repeated) {
			t.Fatalf("non-deterministic parse\nfirst=%+v\nsecond=%+v", result, repeated)
		}
	})
}

func FuzzLengthPrefixedNALUnits(f *testing.F) {
	f.Add(byte(2), []byte{})
	f.Add(byte(2), mustDecodeHex("0000000265aa0000000109"))
	f.Add(byte(0), mustDecodeHex("0167"))
	f.Add(byte(1), mustDecodeHex("00026700"))
	f.Add(byte(2), mustDecodeHex("0000000467000000"))
	f.Add(byte(2), mustDecodeHex("000000056700000300"))

	options := Options{
		MaxDiagnostics: 16, MaxParameterSets: 32,
		MaxNALUnits: 512, MaxParameterSetBytes: 4 << 10,
	}
	normalized, err := options.Normalized()
	if err != nil {
		f.Fatal(err)
	}
	lengthSizes := [...]uint8{1, 2, 4}
	f.Fuzz(func(t *testing.T, selector byte, payload []byte) {
		if uint64(len(payload)) > HardMaxPayloadBytes {
			return
		}
		lengthSize := lengthSizes[int(selector)%len(lengthSizes)]
		const base = 9
		source := make([]byte, base+len(payload)+1)
		copy(source[base:], payload)
		span := cursor.Span{ByteOffset: base, BitLength: uint32(len(payload) * 8)}
		result, err := ParseLengthPrefixedNALUnits(bytes.NewReader(source), span, lengthSize, options)
		if err != nil {
			t.Fatalf("ParseLengthPrefixedNALUnits: %v", err)
		}
		assertNALSequenceInvariants(t, result, normalized, base, len(payload))

		repeated, err := ParseLengthPrefixedNALUnits(bytes.NewReader(source), span, lengthSize, options)
		if err != nil {
			t.Fatalf("repeat ParseLengthPrefixedNALUnits: %v", err)
		}
		if !reflect.DeepEqual(result, repeated) {
			t.Fatalf("non-deterministic parse\nfirst=%+v\nsecond=%+v", result, repeated)
		}
	})
}

func assertConfigurationInvariants(t *testing.T, result ConfigurationRecord, options Options, base, length int) {
	t.Helper()
	assertAccounting(t, result.BytesConsumed, result.SyntaxSpan, result.TrailingSpan, base, length)
	if uint32(len(result.Diagnostics)) > options.MaxDiagnostics {
		t.Fatalf("diagnostics %d exceed limit %d", len(result.Diagnostics), options.MaxDiagnostics)
	}
	if uint32(len(result.SequenceParameterSets)+len(result.PictureParameterSets)+len(result.SequenceParameterSetExts)) > options.MaxParameterSets {
		t.Fatalf("parameter sets exceed limit: %+v", result)
	}
	if result.Complete && (!result.FramingComplete || hasError(result.Diagnostics)) {
		t.Fatalf("complete result has framing/error conflict: %+v", result)
	}
	assertDiagnosticBounds(t, result.Diagnostics, base, length)
	assertSpansByReflection(t, reflect.ValueOf(result), base, length)
	for _, item := range append(append(append([]ParameterSet{}, result.SequenceParameterSets...), result.PictureParameterSets...), result.SequenceParameterSetExts...) {
		assertParameterSetMapping(t, item)
	}
}

func assertNALSequenceInvariants(t *testing.T, result NALUnitSequence, options Options, base, length int) {
	t.Helper()
	assertAccounting(t, result.BytesConsumed, result.SyntaxSpan, result.TrailingSpan, base, length)
	if uint32(len(result.Diagnostics)) > options.MaxDiagnostics || uint32(len(result.NALUnits)) > options.MaxNALUnits {
		t.Fatalf("resource limit exceeded: NALs=%d diagnostics=%d", len(result.NALUnits), len(result.Diagnostics))
	}
	if result.Complete && (!result.FramingComplete || hasError(result.Diagnostics)) {
		t.Fatalf("complete result has framing/error conflict: %+v", result)
	}
	assertDiagnosticBounds(t, result.Diagnostics, base, length)
	assertSpansByReflection(t, reflect.ValueOf(result), base, length)
	for _, unit := range result.NALUnits {
		item := ParameterSet{
			Span: unit.Span, SequenceParameterSet: unit.SequenceParameterSet,
			PictureParameterSet: unit.PictureParameterSet,
		}
		assertParameterSetMapping(t, item)
	}
}

func assertAccounting(t *testing.T, consumed uint64, syntax, trailing cursor.Span, base, length int) {
	t.Helper()
	if consumed > uint64(length) || syntax.ByteOffset != uint64(base) || syntax.BitOffset != 0 ||
		uint64(syntax.BitLength) != consumed*8 || trailing.ByteOffset != uint64(base)+consumed || trailing.BitOffset != 0 ||
		uint64(trailing.BitLength) != (uint64(length)-consumed)*8 {
		t.Fatalf("invalid accounting: consumed=%d syntax=%+v trailing=%+v base=%d length=%d", consumed, syntax, trailing, base, length)
	}
}

func assertDiagnosticBounds(t *testing.T, diagnostics []Diagnostic, base, length int) {
	t.Helper()
	end := uint64(base + length)
	for _, diagnostic := range diagnostics {
		if diagnostic.Offset < uint64(base) || diagnostic.Offset > end || diagnostic.BitOffset > 7 ||
			(diagnostic.Offset == end && diagnostic.BitOffset != 0) {
			t.Fatalf("diagnostic outside payload: %+v", diagnostic)
		}
		if diagnostic.RecoveryOffset != nil && (*diagnostic.RecoveryOffset < uint64(base) || *diagnostic.RecoveryOffset > end) {
			t.Fatalf("recovery outside payload: %+v", diagnostic)
		}
	}
}

var cursorSpanType = reflect.TypeOf(cursor.Span{})

func assertSpansByReflection(t *testing.T, value reflect.Value, base, length int) {
	t.Helper()
	if !value.IsValid() {
		return
	}
	if value.Type() == cursorSpanType {
		span := value.Interface().(cursor.Span)
		if span == (cursor.Span{}) && base != 0 {
			return
		}
		if err := span.Validate(); err != nil {
			t.Fatalf("invalid span %+v: %v", span, err)
		}
		if span.BitLength == 0 {
			if span.ByteOffset < uint64(base) || span.ByteOffset > uint64(base+length) {
				t.Fatalf("empty span outside payload: %+v", span)
			}
			return
		}
		if span.ByteOffset < uint64(base) {
			t.Fatalf("span starts before payload: %+v", span)
		}
		start := (span.ByteOffset-uint64(base))*8 + uint64(span.BitOffset)
		if start+uint64(span.BitLength) > uint64(length*8) {
			t.Fatalf("span outside payload: %+v base=%d length=%d", span, base, length)
		}
		return
	}
	switch value.Kind() {
	case reflect.Pointer, reflect.Interface:
		if !value.IsNil() {
			assertSpansByReflection(t, value.Elem(), base, length)
		}
	case reflect.Struct:
		for index := 0; index < value.NumField(); index++ {
			assertSpansByReflection(t, value.Field(index), base, length)
		}
	case reflect.Slice, reflect.Array:
		for index := 0; index < value.Len(); index++ {
			assertSpansByReflection(t, value.Index(index), base, length)
		}
	}
}

func assertParameterSetMapping(t *testing.T, item ParameterSet) {
	t.Helper()
	var view *RBSPView
	if item.SequenceParameterSet != nil {
		view = &item.SequenceParameterSet.RBSP
	}
	if item.PictureParameterSet != nil {
		view = &item.PictureParameterSet.RBSP
	}
	if view == nil {
		return
	}
	var bytes uint32
	for _, segment := range view.SourceSegments {
		bytes += segment.RBSPByteLength
		if segment.SourceSpan.BitOffset != 0 || segment.SourceSpan.BitLength != segment.RBSPByteLength*8 {
			t.Fatalf("invalid source segment: %+v", segment)
		}
	}
	if bytes != view.ByteLength {
		t.Fatalf("mapped bytes=%d, want %d: %+v", bytes, view.ByteLength, *view)
	}
}

func mustDecodeHex(encoded string) []byte {
	data := make([]byte, len(encoded)/2)
	for index := range data {
		var high, low byte
		for part, destination := range []*byte{&high, &low} {
			value := encoded[index*2+part]
			switch {
			case value >= '0' && value <= '9':
				*destination = value - '0'
			case value >= 'a' && value <= 'f':
				*destination = value - 'a' + 10
			case value >= 'A' && value <= 'F':
				*destination = value - 'A' + 10
			default:
				panic("invalid hex seed")
			}
		}
		data[index] = high<<4 | low
	}
	return data
}
