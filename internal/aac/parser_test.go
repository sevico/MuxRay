package aac

import (
	"bytes"
	"errors"
	"io"
	"math"
	"reflect"
	"testing"

	"github.com/sevico/muxray/internal/cursor"
)

func TestParseAACLCAndAbsoluteBitSpans(t *testing.T) {
	const base = 7
	result := parseAt(t, base, []byte{0x12, 0x10}, Options{})
	if !result.Complete || result.HasErrors() {
		t.Fatalf("unexpected result: complete=%v diagnostics=%+v", result.Complete, result.Diagnostics)
	}
	if result.BitsConsumed != 16 || result.TrailingBits != 0 {
		t.Fatalf("unexpected bit counters: consumed=%d trailing=%d", result.BitsConsumed, result.TrailingBits)
	}
	if result.InitialAudioObjectType.Raw != 2 || result.InitialAudioObjectType.Escaped || !result.InitialAudioObjectType.Complete ||
		result.AudioObjectType.Value != AudioObjectTypeAACLC || result.AudioObjectType.Name != "aacLC" {
		t.Fatalf("unexpected object type: initial=%+v effective=%+v", result.InitialAudioObjectType, result.AudioObjectType)
	}
	if result.SamplingFrequency.Index != 4 || result.SamplingFrequency.Hertz != 44_100 || result.SamplingFrequency.Explicit || !result.SamplingFrequency.Complete {
		t.Fatalf("unexpected frequency: %+v", result.SamplingFrequency)
	}
	if result.ChannelConfiguration.Value != 2 || result.ChannelConfiguration.Name != "stereo" || !result.ChannelConfiguration.Complete {
		t.Fatalf("unexpected channel configuration: %+v", result.ChannelConfiguration)
	}
	if !result.GASpecificPresent || !result.GASpecific.Complete ||
		result.GASpecific.FrameLengthFlag || result.GASpecific.DependsOnCoreCoder || result.GASpecific.ExtensionFlag {
		t.Fatalf("unexpected GA config: %+v", result.GASpecific)
	}

	assertSpan(t, result.InitialAudioObjectType.RawSpan, base, 0, 5)
	assertSpan(t, result.InitialAudioObjectType.Span, base, 0, 5)
	assertSpan(t, result.SamplingFrequency.IndexSpan, base, 5, 4)
	assertSpan(t, result.ChannelConfiguration.Span, base+1, 1, 4)
	assertSpan(t, result.GASpecific.FrameLengthFlagSpan, base+1, 5, 1)
	assertSpan(t, result.GASpecific.DependsOnCoreCoderSpan, base+1, 6, 1)
	assertSpan(t, result.GASpecific.ExtensionFlagSpan, base+1, 7, 1)
	assertSpan(t, result.GASpecific.Span, base+1, 5, 3)
	assertSpan(t, result.SyntaxSpan, base, 0, 16)
	assertAllResultSpans(t, result, base, 2)
}

func TestParseExplicitSamplingFrequency(t *testing.T) {
	var bits bitBuilder
	bits.write(uint64(AudioObjectTypeAACLC), 5)
	bits.write(15, 4)
	bits.write(12_345, 24)
	bits.write(1, 4)
	bits.write(1, 1)
	bits.write(0, 1)
	bits.write(0, 1)

	result := parseAt(t, 3, bits.bytes(), Options{})
	if !result.Complete || result.HasErrors() {
		t.Fatalf("unexpected result: %+v", result)
	}
	frequency := result.SamplingFrequency
	if frequency.Index != 15 || !frequency.Explicit || frequency.ExplicitValue != 12_345 || frequency.Hertz != 12_345 {
		t.Fatalf("unexpected frequency: %+v", frequency)
	}
	assertSpan(t, frequency.ExplicitSpan, 4, 1, 24)
	if !result.GASpecific.FrameLengthFlag {
		t.Fatal("frameLengthFlag was not preserved")
	}
}

func TestParseExplicitSBRAndPS(t *testing.T) {
	for _, test := range []struct {
		name    string
		initial AudioObjectType
		wantPS  bool
	}{
		{name: "SBR", initial: AudioObjectTypeSBR},
		{name: "PS", initial: AudioObjectTypePS, wantPS: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			var bits bitBuilder
			bits.write(uint64(test.initial), 5)
			bits.write(3, 4) // 48 kHz outer frequency.
			bits.write(2, 4)
			bits.write(6, 4) // 24 kHz extension frequency.
			bits.write(uint64(AudioObjectTypeAACLC), 5)
			bits.write(0, 3) // GA flags.

			result := parseAt(t, 5, bits.bytes(), Options{})
			if !result.Complete || result.HasErrors() {
				t.Fatalf("unexpected result: %+v", result)
			}
			if !result.ExplicitSBR || result.ExplicitPS != test.wantPS || result.ExtensionAudioObjectType != AudioObjectTypeSBR {
				t.Fatalf("unexpected extension flags: %+v", result)
			}
			if result.InitialAudioObjectType.Value != test.initial || result.AudioObjectType.Value != AudioObjectTypeAACLC {
				t.Fatalf("object type rewrite was not preserved: initial=%+v effective=%+v", result.InitialAudioObjectType, result.AudioObjectType)
			}
			if result.SamplingFrequency.Hertz != 48_000 || result.ExtensionSamplingFrequency.Hertz != 24_000 {
				t.Fatalf("unexpected frequencies: outer=%+v extension=%+v", result.SamplingFrequency, result.ExtensionSamplingFrequency)
			}
			if result.BitsConsumed != 25 || result.TrailingBits != 7 {
				t.Fatalf("unexpected bit counters: consumed=%d trailing=%d", result.BitsConsumed, result.TrailingBits)
			}
			assertSpan(t, result.ExtensionSamplingFrequency.IndexSpan, 6, 5, 4)
			assertSpan(t, result.AudioObjectType.Span, 7, 1, 5)
			assertAllResultSpans(t, result, 5, len(bits.bytes()))
		})
	}
}

func TestGetAudioObjectTypeEscapeIsPreserved(t *testing.T) {
	var bits bitBuilder
	bits.write(31, 5)
	bits.write(10, 6) // 32 + 10 = USAC (42).
	bits.write(4, 4)
	bits.write(2, 4)

	result := parseAt(t, 9, bits.bytes(), Options{})
	if result.Complete || len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != CodeAudioObjectTypeUnsupported {
		t.Fatalf("unexpected result: %+v", result)
	}
	field := result.InitialAudioObjectType
	if field.Raw != 31 || !field.Escaped || field.Escape != 10 || field.Value != AudioObjectTypeUSAC || field.Name != "usac" {
		t.Fatalf("unexpected escaped object type: %+v", field)
	}
	assertSpan(t, field.RawSpan, 9, 0, 5)
	assertSpan(t, field.EscapeSpan, 9, 5, 6)
	assertSpan(t, field.Span, 9, 0, 11)
	assertFatalRecovery(t, result.Diagnostics[0], 9+len(bits.bytes()))
}

func TestParseGASpecificConditionalFields(t *testing.T) {
	t.Run("ER AAC scalable", func(t *testing.T) {
		var bits bitBuilder
		writeHeader(&bits, AudioObjectTypeERAACScalable, 4, 2)
		bits.write(1, 1)      // frameLengthFlag
		bits.write(1, 1)      // dependsOnCoreCoder
		bits.write(0x123, 14) // coreCoderDelay
		bits.write(1, 1)      // extensionFlag
		bits.write(5, 3)      // layerNr
		bits.write(1, 1)      // section resilience
		bits.write(0, 1)      // scalefactor resilience
		bits.write(1, 1)      // spectral resilience
		bits.write(0, 1)      // extensionFlag3
		bits.write(1, 2)      // epConfig

		result := parseAt(t, 0, bits.bytes(), Options{})
		if !result.Complete || result.HasErrors() {
			t.Fatalf("unexpected result: %+v", result)
		}
		ga := result.GASpecific
		if !ga.FrameLengthFlag || !ga.DependsOnCoreCoder || !ga.CoreCoderDelayPresent || ga.CoreCoderDelay != 0x123 ||
			!ga.ExtensionFlag || !ga.LayerNrPresent || ga.LayerNr != 5 || !ga.ResilienceFlagsPresent ||
			!ga.SectionDataResilienceFlag || ga.ScalefactorDataResilienceFlag || !ga.SpectralDataResilienceFlag ||
			!ga.ExtensionFlag3Present || ga.ExtensionFlag3 {
			t.Fatalf("unexpected GA fields: %+v", ga)
		}
		if !result.EPConfigPresent || result.EPConfig != 1 {
			t.Fatalf("unexpected epConfig: %+v", result)
		}
		assertSpan(t, ga.CoreCoderDelaySpan, 1, 7, 14)
		assertSpan(t, ga.LayerNrSpan, 3, 6, 3)
		assertAllResultSpans(t, result, 0, len(bits.bytes()))
	})

	t.Run("ER BSAC", func(t *testing.T) {
		var bits bitBuilder
		writeHeader(&bits, AudioObjectTypeERBSAC, 3, 2)
		bits.write(0, 1)
		bits.write(0, 1)
		bits.write(1, 1)
		bits.write(17, 5)
		bits.write(0x456, 11)
		bits.write(0, 1)
		bits.write(0, 2)

		result := parseAt(t, 2, bits.bytes(), Options{})
		if !result.Complete || result.HasErrors() {
			t.Fatalf("unexpected result: %+v", result)
		}
		ga := result.GASpecific
		if !ga.NumOfSubFramePresent || ga.NumOfSubFrame != 17 || ga.LayerLength != 0x456 || !ga.ExtensionFlag3Present {
			t.Fatalf("unexpected BSAC fields: %+v", ga)
		}
	})
}

func TestExtensionFlag3FailsInsteadOfSilentlyCompleting(t *testing.T) {
	var bits bitBuilder
	writeHeader(&bits, AudioObjectTypeAACLC, 4, 2)
	bits.write(0, 1) // frameLengthFlag
	bits.write(0, 1) // dependsOnCoreCoder
	bits.write(1, 1) // extensionFlag
	bits.write(1, 1) // extensionFlag3: additional syntax is not implemented

	result := parseAt(t, 11, bits.bytes(), Options{})
	if result.Complete || result.GASpecific.Complete || len(result.Diagnostics) != 1 {
		t.Fatalf("extensionFlag3=1 was silently accepted: %+v", result)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != CodeExtensionFlag3Unsupported || diagnostic.Offset != 13 || diagnostic.BitOffset != 0 || diagnostic.Actual == nil || *diagnostic.Actual != 1 {
		t.Fatalf("unexpected extensionFlag3 diagnostic: %+v", diagnostic)
	}
	assertFatalRecovery(t, diagnostic, 11+len(bits.bytes()))
}

func TestEverySupportedGAObjectTypeHasDeterministicMinimumSyntax(t *testing.T) {
	types := []AudioObjectType{
		AudioObjectTypeAACMain, AudioObjectTypeAACLC, AudioObjectTypeAACSSR,
		AudioObjectTypeAACLTP, AudioObjectTypeAACScalable, AudioObjectTypeTwinVQ,
		AudioObjectTypeERAACLC, AudioObjectTypeERAACLTP, AudioObjectTypeERAACScalable,
		AudioObjectTypeERTwinVQ, AudioObjectTypeERBSAC, AudioObjectTypeERAACLD,
	}
	for _, objectType := range types {
		t.Run(objectType.String(), func(t *testing.T) {
			var bits bitBuilder
			writeHeader(&bits, objectType, 4, 2)
			bits.write(0, 3) // frameLengthFlag, dependsOnCoreCoder, extensionFlag.
			if objectType == AudioObjectTypeAACScalable || objectType == AudioObjectTypeERAACScalable {
				bits.write(0, 3)
			}
			if errorResilientAudioObjectType(objectType) {
				bits.write(0, 2)
			}
			result := parseAt(t, 0, bits.bytes(), Options{})
			if !result.Complete || result.HasErrors() {
				t.Fatalf("unexpected result: %+v", result)
			}
		})
	}
}

func TestExplicitSBRExtensionChannelConfiguration(t *testing.T) {
	var bits bitBuilder
	bits.write(uint64(AudioObjectTypeSBR), 5)
	bits.write(3, 4)
	bits.write(2, 4)
	bits.write(6, 4)
	bits.write(uint64(AudioObjectTypeERBSAC), 5)
	bits.write(6, 4)
	bits.write(0, 3)
	bits.write(0, 2)

	result := parseAt(t, 0, bits.bytes(), Options{})
	if !result.Complete || result.HasErrors() {
		t.Fatalf("unexpected result: %+v", result)
	}
	if !result.ExtensionChannelConfigurationPresent || result.ExtensionChannelConfiguration.Value != 6 || result.ExtensionChannelConfiguration.Name != "5.1" {
		t.Fatalf("unexpected extension channel configuration: %+v", result.ExtensionChannelConfiguration)
	}
}

func TestReservedAndUnsupportedValuesFailWithoutGuessing(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		code string
	}{
		{name: "null object type", data: packedFields(field{0, 5}), code: CodeAudioObjectTypeReserved},
		{name: "reserved object type", data: packedFields(field{10, 5}), code: CodeAudioObjectTypeReserved},
		{name: "unsupported CELP", data: packedFields(field{8, 5}), code: CodeAudioObjectTypeUnsupported},
		{name: "reserved frequency index", data: packedFields(field{2, 5}, field{13, 4}), code: CodeSamplingFrequencyIndexReserved},
		{name: "zero explicit frequency", data: packedFields(field{2, 5}, field{15, 4}, field{0, 24}), code: CodeSamplingFrequencyZero},
		{name: "PCE", data: packedFields(field{2, 5}, field{4, 4}, field{0, 4}), code: CodeProgramConfigElementUnsupported},
		{name: "reserved channels", data: packedFields(field{2, 5}, field{4, 4}, field{8, 4}), code: CodeChannelConfigurationReserved},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := parseAt(t, 4, test.data, Options{})
			if result.Complete || !result.HasErrors() || len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != test.code {
				t.Fatalf("unexpected result: %+v", result)
			}
			assertFatalRecovery(t, result.Diagnostics[0], 4+len(test.data))
		})
	}
}

func TestTruncatedFieldsReturnPartialResultAndPayloadRecovery(t *testing.T) {
	t.Run("empty object type", func(t *testing.T) {
		result := parseAt(t, 6, nil, Options{})
		assertOnlyDiagnostic(t, result, CodeAudioObjectTypeTruncated, 6, 0, 5, 0)
	})

	t.Run("frequency index", func(t *testing.T) {
		result := parseAt(t, 6, []byte{0x10}, Options{}) // AOT 2 plus only three following bits.
		assertOnlyDiagnostic(t, result, CodeSamplingFrequencyIndexTruncated, 7, 0, 4, 3)
		assertSpan(t, result.SamplingFrequency.IndexSpan, 6, 5, 3)
	})

	t.Run("object type escape", func(t *testing.T) {
		result := parseAt(t, 6, []byte{0xf8}, Options{}) // escape marker plus only three suffix bits.
		assertOnlyDiagnostic(t, result, CodeAudioObjectTypeEscapeTruncated, 7, 0, 6, 3)
		if !result.InitialAudioObjectType.Escaped || result.InitialAudioObjectType.Complete || result.InitialAudioObjectType.Name != "" {
			t.Fatal("partial escape state was not preserved")
		}
	})

	t.Run("core coder delay", func(t *testing.T) {
		var bits bitBuilder
		writeHeader(&bits, AudioObjectTypeAACLC, 4, 2)
		bits.write(0, 1)
		bits.write(1, 1)
		result := parseAt(t, 6, bits.bytes(), Options{})
		assertOnlyDiagnostic(t, result, CodeCoreCoderDelayTruncated, 8, 0, 14, 1)
		if !result.GASpecific.CoreCoderDelayPresent || result.GASpecific.CoreCoderDelaySpan.BitLength != 1 {
			t.Fatalf("partial GA field was not preserved: %+v", result.GASpecific)
		}
	})
}

func TestErrorProtectionSpecificConfigFailsExplicitly(t *testing.T) {
	var bits bitBuilder
	writeHeader(&bits, AudioObjectTypeERAACLC, 4, 2)
	bits.write(0, 3)
	bits.write(2, 2)
	result := parseAt(t, 0, bits.bytes(), Options{})
	if result.Complete || len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != CodeErrorProtectionConfigUnsupported {
		t.Fatalf("unexpected result: %+v", result)
	}
	if !result.EPConfigPresent || !result.EPConfigComplete || result.EPConfig != 2 {
		t.Fatalf("epConfig not preserved: %+v", result)
	}
}

func TestReaderAtFailuresAreGoErrors(t *testing.T) {
	span := cursor.Span{BitLength: 16}
	result, err := ParseAudioSpecificConfig(bytes.NewReader([]byte{0x12}), span, Options{})
	if !errors.Is(err, io.EOF) {
		t.Fatalf("error=%v, want wrapped io.EOF", err)
	}
	if len(result.Diagnostics) != 0 || result.Complete || result.BitsConsumed != 5 {
		t.Fatalf("ReaderAt error was treated as malformed syntax: %+v", result)
	}
}

func TestInvalidArgumentsAndHardLimits(t *testing.T) {
	if _, err := ParseAudioSpecificConfig(nil, cursor.Span{}, Options{}); err == nil {
		t.Fatal("nil source was accepted")
	}
	if _, err := ParseAudioSpecificConfig(bytes.NewReader(nil), cursor.Span{BitOffset: 1, BitLength: 7}, Options{}); err == nil {
		t.Fatal("non-byte-aligned span was accepted")
	}
	if _, err := ParseAudioSpecificConfig(bytes.NewReader(nil), cursor.Span{ByteOffset: math.MaxUint64, BitLength: 8}, Options{}); err == nil {
		t.Fatal("overflowing span was accepted")
	}
	tooLarge := cursor.Span{BitLength: uint32((HardMaxPayloadBytes + 1) * 8)}
	if _, err := ParseAudioSpecificConfig(bytes.NewReader(nil), tooLarge, Options{}); err == nil {
		t.Fatal("oversized payload was accepted")
	}
	if _, err := (Options{MaxDiagnostics: 1}).Normalized(); err == nil {
		t.Fatal("too-small diagnostic budget was accepted")
	}
	if _, err := (Options{MaxDiagnostics: HardMaxDiagnostics + 1}).Normalized(); err == nil {
		t.Fatal("oversized diagnostic budget was accepted")
	}
	normalized, err := (Options{}).Normalized()
	if err != nil || normalized.MaxDiagnostics != DefaultMaxDiagnostics {
		t.Fatalf("default normalization = %+v, %v", normalized, err)
	}
}

func TestDiagnosticCollectorPreservesFatalAndLimitMarker(t *testing.T) {
	collector := newDiagnosticCollector(2)
	collector.add(Diagnostic{Code: "W1", Severity: SeverityWarning})
	collector.add(Diagnostic{Code: "W2", Severity: SeverityWarning})
	recovery := uint64(9)
	collector.add(Diagnostic{Code: "F", Severity: SeverityError, RecoveryOffset: &recovery})
	if len(collector.items) != 2 || collector.items[0].Code != "F" || collector.items[1].Code != CodeDiagnosticLimitReached {
		t.Fatalf("unexpected diagnostics: %+v", collector.items)
	}
	if collector.items[1].RecoveryOffset == nil || *collector.items[1].RecoveryOffset != recovery {
		t.Fatalf("limit marker lost recovery: %+v", collector.items[1])
	}
}

func TestParseIsDeterministic(t *testing.T) {
	var bits bitBuilder
	writeHeader(&bits, AudioObjectTypeERBSAC, 4, 2)
	bits.write(0, 1)
	bits.write(0, 1)
	bits.write(1, 1)
	bits.write(3, 5)
	bits.write(511, 11)
	bits.write(1, 1)
	bits.write(1, 2)
	data := bits.bytes()
	first := parseAt(t, 11, data, Options{MaxDiagnostics: 8})
	for iteration := 0; iteration < 100; iteration++ {
		next := parseAt(t, 11, data, Options{MaxDiagnostics: 8})
		if !reflect.DeepEqual(first, next) {
			t.Fatalf("parse changed at iteration %d\nfirst=%+v\nnext=%+v", iteration, first, next)
		}
	}
}

func BenchmarkParseAudioSpecificConfig(b *testing.B) {
	data := []byte{0x12, 0x10}
	reader := bytes.NewReader(data)
	span := cursor.Span{BitLength: uint32(len(data) * 8)}
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	for iteration := 0; iteration < b.N; iteration++ {
		result, err := ParseAudioSpecificConfig(reader, span, Options{})
		if err != nil || !result.Complete {
			b.Fatalf("ParseAudioSpecificConfig() = complete %v, error %v", result.Complete, err)
		}
	}
}

func parseAt(t *testing.T, base int, payload []byte, options Options) Result {
	t.Helper()
	source := make([]byte, base+len(payload)+2)
	copy(source[base:], payload)
	span := cursor.Span{ByteOffset: uint64(base), BitLength: uint32(len(payload) * 8)}
	result, err := ParseAudioSpecificConfig(bytes.NewReader(source), span, options)
	if err != nil {
		t.Fatalf("ParseAudioSpecificConfig() error = %v", err)
	}
	return result
}

func writeHeader(bits *bitBuilder, objectType AudioObjectType, frequencyIndex, channelConfiguration uint64) {
	bits.write(uint64(objectType), 5)
	bits.write(frequencyIndex, 4)
	bits.write(channelConfiguration, 4)
}

type field struct {
	value uint64
	bits  uint8
}

func packedFields(fields ...field) []byte {
	var builder bitBuilder
	for _, field := range fields {
		builder.write(field.value, field.bits)
	}
	return builder.bytes()
}

type bitBuilder struct {
	data   []byte
	length uint64
}

func (b *bitBuilder) write(value uint64, width uint8) {
	for index := int(width) - 1; index >= 0; index-- {
		byteIndex := b.length / 8
		bitIndex := b.length % 8
		if byteIndex == uint64(len(b.data)) {
			b.data = append(b.data, 0)
		}
		bit := byte((value >> uint(index)) & 1)
		b.data[byteIndex] |= bit << (7 - bitIndex)
		b.length++
	}
}

func (b *bitBuilder) bytes() []byte {
	return append([]byte(nil), b.data...)
}

func assertSpan(t *testing.T, got cursor.Span, byteOffset int, bitOffset uint8, bitLength uint32) {
	t.Helper()
	want := cursor.Span{ByteOffset: uint64(byteOffset), BitOffset: bitOffset, BitLength: bitLength}
	if got != want {
		t.Fatalf("span=%+v, want %+v", got, want)
	}
}

func assertOnlyDiagnostic(t *testing.T, result Result, code string, offset int, bitOffset uint8, expected, actual uint64) {
	t.Helper()
	if result.Complete || !result.HasErrors() || len(result.Diagnostics) != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != code || diagnostic.Offset != uint64(offset) || diagnostic.BitOffset != bitOffset ||
		diagnostic.Expected == nil || *diagnostic.Expected != expected || diagnostic.Actual == nil || *diagnostic.Actual != actual {
		t.Fatalf("unexpected diagnostic: %+v", diagnostic)
	}
	end, _, err := result.Span.End()
	if err != nil {
		t.Fatal(err)
	}
	assertFatalRecovery(t, diagnostic, int(end))
}

func assertFatalRecovery(t *testing.T, diagnostic Diagnostic, wantEnd int) {
	t.Helper()
	if diagnostic.RecoveryOffset == nil || *diagnostic.RecoveryOffset != uint64(wantEnd) {
		t.Fatalf("recovery offset=%v, want %d", diagnostic.RecoveryOffset, wantEnd)
	}
}

func assertAllResultSpans(t *testing.T, result Result, base, length int) {
	t.Helper()
	spans := []cursor.Span{
		result.Span, result.InitialAudioObjectType.RawSpan, result.InitialAudioObjectType.EscapeSpan,
		result.InitialAudioObjectType.Span, result.AudioObjectType.RawSpan, result.AudioObjectType.EscapeSpan,
		result.AudioObjectType.Span, result.SamplingFrequency.IndexSpan, result.SamplingFrequency.ExplicitSpan,
		result.ChannelConfiguration.Span, result.ExtensionSamplingFrequency.IndexSpan,
		result.ExtensionSamplingFrequency.ExplicitSpan, result.ExtensionChannelConfiguration.Span,
		result.GASpecific.Span, result.GASpecific.FrameLengthFlagSpan,
		result.GASpecific.DependsOnCoreCoderSpan, result.GASpecific.CoreCoderDelaySpan,
		result.GASpecific.ExtensionFlagSpan, result.GASpecific.LayerNrSpan,
		result.GASpecific.NumOfSubFrameSpan, result.GASpecific.LayerLengthSpan,
		result.GASpecific.SectionDataResilienceFlagSpan, result.GASpecific.ScalefactorDataResilienceSpan,
		result.GASpecific.SpectralDataResilienceFlagSpan, result.GASpecific.ExtensionFlag3Span,
		result.EPConfigSpan, result.SyntaxSpan, result.TrailingSpan,
	}
	for _, span := range spans {
		if span == (cursor.Span{}) && base != 0 {
			continue
		}
		if err := span.Validate(); err != nil {
			t.Fatalf("invalid span %+v: %v", span, err)
		}
		if span.BitLength == 0 {
			continue
		}
		if span.ByteOffset < uint64(base) {
			t.Fatalf("span starts before payload: %+v", span)
		}
		start := (span.ByteOffset-uint64(base))*8 + uint64(span.BitOffset)
		if start+uint64(span.BitLength) > uint64(length*8) {
			t.Fatalf("span outside payload: %+v base=%d length=%d", span, base, length)
		}
	}
}
