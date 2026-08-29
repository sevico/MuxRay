package avc

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"math"
	"math/bits"
	"reflect"
	"testing"

	"github.com/sevico/muxray/internal/cursor"
)

const (
	highConfigurationHex     = "0164000cffe1001a6764000cacd941419f9f011000000300100000030320f142996001000668ebe3cb22c0fdf8f800"
	baselineConfigurationHex = "0142c00bffe100166742c00bd902c4ec0440000003004000000c83c50a9201000568cb83cb20"
)

func TestParseHighConfigurationAndDimensions(t *testing.T) {
	configuration := mustHex(t, highConfigurationHex)
	result := parseConfigurationAt(t, 11, configuration, Options{})
	if !result.Complete || !result.FramingComplete || result.HasErrors() {
		t.Fatalf("unexpected result: %+v", result)
	}
	if result.BytesConsumed != uint64(len(configuration)) || result.LengthSizeMinusOne != 3 || result.LengthSize != 4 {
		t.Fatalf("unexpected accounting or length size: %+v", result)
	}
	if result.AVCProfileIndication != 100 || result.AVCLevelIndication != 12 ||
		!result.HighProfileFieldsPresent || result.ChromaFormat != 1 ||
		result.BitDepthLumaMinus8 != 0 || result.BitDepthChromaMinus8 != 0 || result.NumSequenceParameterSetExt != 0 {
		t.Fatalf("unexpected high-profile fields: %+v", result)
	}
	if len(result.SequenceParameterSets) != 1 || len(result.PictureParameterSets) != 1 {
		t.Fatalf("unexpected parameter-set counts: SPS=%d PPS=%d", len(result.SequenceParameterSets), len(result.PictureParameterSets))
	}
	sps := result.SequenceParameterSets[0].SequenceParameterSet
	if sps == nil || !sps.Complete || sps.ProfileIDC != 100 || sps.LevelIDC != 12 ||
		sps.SequenceParameterSetID != 0 || sps.CodedWidth != 320 || sps.CodedHeight != 192 ||
		sps.DisplayWidth != 320 || sps.DisplayHeight != 180 {
		t.Fatalf("unexpected SPS summary: %+v", sps)
	}
	if len(sps.RBSP.EmulationPreventionBytes) != 2 || len(sps.RBSP.SourceSegments) != 3 {
		t.Fatalf("unexpected RBSP map: %+v", sps.RBSP)
	}
	pps := result.PictureParameterSets[0].PictureParameterSet
	if pps == nil || !pps.Complete || pps.PictureParameterSetID != 0 || pps.SequenceParameterSetID != 0 {
		t.Fatalf("unexpected PPS summary: %+v", pps)
	}
	assertOuterSpan(t, result.SyntaxSpan, 11, len(configuration))
}

func TestParseBaselineConfigurationAndDimensions(t *testing.T) {
	configuration := mustHex(t, baselineConfigurationHex)
	result := parseConfigurationAt(t, 3, configuration, Options{})
	if !result.Complete || result.HighProfileFieldsPresent || len(result.Diagnostics) != 0 {
		t.Fatalf("unexpected result: %+v", result)
	}
	sps := result.SequenceParameterSets[0].SequenceParameterSet
	if sps == nil || sps.ProfileIDC != 66 || sps.HighProfileFieldsPresent ||
		sps.DisplayWidth != 176 || sps.DisplayHeight != 144 {
		t.Fatalf("unexpected baseline SPS: %+v", sps)
	}
	if len(sps.RBSP.EmulationPreventionBytes) != 1 {
		t.Fatalf("emulation-prevention bytes=%d, want 1", len(sps.RBSP.EmulationPreventionBytes))
	}
}

func TestParseEmptyParameterSetArrays(t *testing.T) {
	result := parseConfigurationAt(t, 0, mustHex(t, "0142001effe000"), Options{})
	if !result.Complete || !result.FramingComplete || result.BytesConsumed != 7 || result.LengthSize != 4 ||
		len(result.SequenceParameterSets) != 0 || len(result.PictureParameterSets) != 0 {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestModernConfigurationExtensionPredicate(t *testing.T) {
	// ISO/IEC 14496-15:2019 carries the extension for every profile other
	// than Baseline (66), Main (77), and Extended (88), including profile 244.
	result := parseConfigurationAt(t, 2, mustHex(t, "01f4000cffe000fdf8f800"), Options{})
	if !result.Complete || !result.HighProfileFieldsPresent || result.AVCProfileIndication != 244 ||
		result.ChromaFormat != 1 || result.BytesConsumed != 11 {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestConfigurationVersionStopsParsing(t *testing.T) {
	result := parseConfigurationAt(t, 5, mustHex(t, baselineConfigurationHex), Options{})
	// Reparse after changing version without hiding the absolute-offset check.
	data := mustHex(t, baselineConfigurationHex)
	data[0] = 2
	result = parseConfigurationAt(t, 5, data, Options{})
	assertDiagnostic(t, result.Diagnostics, CodeConfigurationVersionUnexpected)
	if result.Complete || result.FramingComplete || result.BytesConsumed != 1 || result.TrailingSpan.BitLength != uint32((len(data)-1)*8) {
		t.Fatalf("unexpected recovery: %+v", result)
	}
}

func TestConfigurationWarningsAndSemanticErrors(t *testing.T) {
	t.Run("reserved bits remain a warning", func(t *testing.T) {
		data := mustHex(t, baselineConfigurationHex)
		data[4] = 0x03 // lengthSizeMinusOne=3, reserved bits all zero.
		data[5] = 0x01 // one SPS, reserved bits all zero.
		result := parseConfigurationAt(t, 0, data, Options{})
		if !result.Complete || result.HasErrors() {
			t.Fatalf("warning made result incomplete: %+v", result)
		}
		assertDiagnostic(t, result.Diagnostics, CodeReservedBitsUnexpected)
	})

	t.Run("reserved length size", func(t *testing.T) {
		data := mustHex(t, baselineConfigurationHex)
		data[4] = 0xfe
		result := parseConfigurationAt(t, 0, data, Options{})
		if result.Complete || !result.FramingComplete || result.LengthSize != 0 {
			t.Fatalf("unexpected result: %+v", result)
		}
		assertDiagnostic(t, result.Diagnostics, CodeLengthSizeReserved)
	})

	t.Run("avcC bit depth", func(t *testing.T) {
		data := mustHex(t, highConfigurationHex)
		data[len(data)-3] = 0xff
		result := parseConfigurationAt(t, 0, data, Options{})
		if result.Complete || !result.FramingComplete {
			t.Fatalf("unexpected result: %+v", result)
		}
		assertDiagnostic(t, result.Diagnostics, CodeConfigurationBitDepthInvalid)
	})

	t.Run("SPS reserved warning has no recovery", func(t *testing.T) {
		data := mustHex(t, baselineConfigurationHex)
		data[10] |= 0x01
		result := parseConfigurationAt(t, 0, data, Options{})
		if !result.Complete {
			t.Fatalf("warning made result incomplete: %+v", result)
		}
		for _, diagnostic := range result.Diagnostics {
			if diagnostic.Code == CodeSPSReservedBitsUnexpected {
				if diagnostic.RecoveryOffset != nil {
					t.Fatalf("warning has recovery offset: %+v", diagnostic)
				}
				return
			}
		}
		t.Fatal("SPS reserved warning was not emitted")
	})
}

func TestConfigurationTruncationReturnsPartialResult(t *testing.T) {
	complete := mustHex(t, baselineConfigurationHex)
	for length := 0; length < len(complete); length++ {
		t.Run(string(rune(length)), func(t *testing.T) {
			result := parseConfigurationAt(t, 7, complete[:length], Options{})
			if result.Complete || result.FramingComplete || !result.HasErrors() {
				t.Fatalf("length %d unexpectedly complete: %+v", length, result)
			}
			if result.BytesConsumed > uint64(length) || uint64(result.SyntaxSpan.BitLength)/8 != result.BytesConsumed {
				t.Fatalf("length %d invalid accounting: %+v", length, result)
			}
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Severity == SeverityError && (diagnostic.RecoveryOffset == nil || *diagnostic.RecoveryOffset != uint64(7+length)) {
					t.Fatalf("length %d bad recovery: %+v", length, diagnostic)
				}
			}
		})
	}
}

func TestConfigurationParameterSetLimitStillScansFraming(t *testing.T) {
	data := mustHex(t, highConfigurationHex)
	result := parseConfigurationAt(t, 0, data, Options{MaxParameterSets: 1})
	if !result.FramingComplete || result.Complete || result.BytesConsumed != uint64(len(data)) ||
		len(result.SequenceParameterSets) != 1 || len(result.PictureParameterSets) != 0 {
		t.Fatalf("unexpected result: %+v", result)
	}
	assertDiagnostic(t, result.Diagnostics, CodeParameterSetLimitReached)
}

func TestParameterSetArrayNALTypeTolerance(t *testing.T) {
	t.Run("declarative SEI", func(t *testing.T) {
		data := mustHex(t, baselineConfigurationHex)
		data[8] = 0x06
		result := parseConfigurationAt(t, 0, data, Options{})
		if !result.Complete {
			t.Fatalf("declarative SEI rejected: %+v", result)
		}
		for _, diagnostic := range result.Diagnostics {
			if diagnostic.Code == CodeParameterSetNALTypeUnexpected {
				t.Fatalf("declarative SEI was diagnosed as unexpected: %+v", diagnostic)
			}
		}
	})

	t.Run("other NAL type warns", func(t *testing.T) {
		data := mustHex(t, baselineConfigurationHex)
		data[8] = 0x61
		result := parseConfigurationAt(t, 0, data, Options{})
		if !result.Complete || result.HasErrors() {
			t.Fatalf("warning made framing fail: %+v", result)
		}
		assertDiagnostic(t, result.Diagnostics, CodeParameterSetNALTypeUnexpected)
	})
}

func TestDiagnosticLimitMarkerHasRecovery(t *testing.T) {
	data := mustHex(t, baselineConfigurationHex)
	data[4] = 0x03
	data[5] = 0x01
	data[10] |= 0x01
	result := parseConfigurationAt(t, 6, data, Options{MaxDiagnostics: 2})
	if len(result.Diagnostics) != 2 || result.Diagnostics[1].Code != CodeDiagnosticLimitReached ||
		result.Diagnostics[1].Severity != SeverityError || result.Diagnostics[1].RecoveryOffset == nil ||
		*result.Diagnostics[1].RecoveryOffset != uint64(6+len(data)) {
		t.Fatalf("unexpected diagnostic marker: %+v", result.Diagnostics)
	}
}

func TestParseLengthPrefixedNALUnits(t *testing.T) {
	data := mustHex(t, "0000000265aa0000000109")
	const base = 13
	result := parseNALsAt(t, base, data, 4, Options{})
	if !result.Complete || !result.FramingComplete || result.BytesConsumed != 11 || len(result.NALUnits) != 2 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if result.NALUnits[0].Length != 2 || result.NALUnits[0].Header.NALUnitType != NALUnitTypeIDRSlice ||
		result.NALUnits[1].Length != 1 || result.NALUnits[1].Header.NALUnitType != NALUnitTypeAccessUnitDelimiter {
		t.Fatalf("unexpected NALs: %+v", result.NALUnits)
	}
	assertOuterSpan(t, result.NALUnits[0].LengthSpan, base, 4)
	assertOuterSpan(t, result.NALUnits[0].Span, base+4, 2)
	assertOuterSpan(t, result.NALUnits[1].LengthSpan, base+6, 4)
	assertOuterSpan(t, result.NALUnits[1].Span, base+10, 1)
}

func TestRealAccessUnitBoundaries(t *testing.T) {
	// Boundary sizes from a real FFmpeg 7.1.1/libx264 High-profile access
	// unit: 684-byte SEI followed by a 3293-byte IDR slice.
	data := make([]byte, 4+684+4+3293)
	binary.BigEndian.PutUint32(data[0:4], 684)
	data[4] = 0x06
	secondPrefix := 4 + 684
	binary.BigEndian.PutUint32(data[secondPrefix:secondPrefix+4], 3293)
	data[secondPrefix+4] = 0x65
	result := parseNALsAt(t, 4, data, 4, Options{})
	if !result.Complete || result.BytesConsumed != 3985 || len(result.NALUnits) != 2 ||
		result.NALUnits[0].Length != 684 || result.NALUnits[0].Header.NALUnitType != NALUnitTypeSEI ||
		result.NALUnits[1].Length != 3293 || result.NALUnits[1].Header.NALUnitType != NALUnitTypeIDRSlice {
		t.Fatalf("unexpected access-unit boundaries: %+v", result)
	}
	assertOuterSpan(t, result.NALUnits[1].LengthSpan, 4+secondPrefix, 4)
	assertOuterSpan(t, result.NALUnits[1].Span, 4+secondPrefix+4, 3293)
}

func TestNALSequenceMalformedAndLimits(t *testing.T) {
	tests := []struct {
		name       string
		hex        string
		lengthSize uint8
		options    Options
		code       string
		framing    bool
	}{
		{name: "prefix truncated", hex: "0000", lengthSize: 4, code: CodeNALLengthTruncated},
		{name: "payload truncated", hex: "0000000365aa", lengthSize: 4, code: CodeNALPayloadTruncated},
		{name: "zero length", hex: "00000000", lengthSize: 4, code: CodeNALUnitZeroLength, framing: true},
		{name: "forbidden bit", hex: "01e7", lengthSize: 1, code: CodeForbiddenZeroBitSet, framing: true},
		{name: "limit", hex: "0000000265aa0000000109", lengthSize: 4, options: Options{MaxNALUnits: 1}, code: CodeNALUnitLimitReached},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := parseNALsAt(t, 9, mustHex(t, test.hex), test.lengthSize, test.options)
			if result.Complete || result.FramingComplete != test.framing {
				t.Fatalf("unexpected result: %+v", result)
			}
			assertDiagnostic(t, result.Diagnostics, test.code)
		})
	}
}

func TestNALLengthSizeArgument(t *testing.T) {
	for _, lengthSize := range []uint8{0, 3, 5} {
		if _, err := ParseLengthPrefixedNALUnits(bytes.NewReader(nil), cursor.Span{}, lengthSize, Options{}); err == nil {
			t.Fatalf("lengthSize %d accepted", lengthSize)
		}
	}
}

func TestRBSPSourceMappingAndValidation(t *testing.T) {
	nal := mustHex(t, "6700000301aa00000302")
	const base = 5
	source := append(make([]byte, base), nal...)
	options, err := (Options{}).Normalized()
	if err != nil {
		t.Fatal(err)
	}
	diagnostics := newDiagnosticCollector(options.MaxDiagnostics)
	data, err := decodeRBSP(bytes.NewReader(source), cursor.Span{ByteOffset: base, BitLength: uint32(len(nal) * 8)}, options, newParseBudget(options), diagnostics)
	if err != nil || !data.valid || len(diagnostics.items) != 0 {
		t.Fatalf("decodeRBSP() = valid %v diagnostics %+v error %v", data.valid, diagnostics.items, err)
	}
	if len(data.bytes) != 7 || len(data.view.EmulationPreventionBytes) != 2 || len(data.view.SourceSegments) != 3 {
		t.Fatalf("unexpected map: %+v bytes=%x", data.view, data.bytes)
	}
	mapped := newRBSPParser(data, diagnostics).mappedSpan(0, uint32(len(data.bytes)*8))
	if len(mapped.SourceSpans) != 3 || sumSpanBits(mapped.SourceSpans) != mapped.BitLength {
		t.Fatalf("mapped span is not split around prevention bytes: %+v", mapped)
	}
	for _, prevention := range data.view.EmulationPreventionBytes {
		for _, sourceSpan := range mapped.SourceSpans {
			if sourceSpan.ByteOffset == prevention.ByteOffset {
				t.Fatalf("prevention byte leaked into mapped field: prevention=%+v mapped=%+v", prevention, mapped)
			}
		}
	}

	invalid := []string{"67000000", "67000001", "67000002", "67000003", "6700000304"}
	for _, encoded := range invalid {
		nal = mustHex(t, encoded)
		diagnostics = newDiagnosticCollector(options.MaxDiagnostics)
		data, err = decodeRBSP(bytes.NewReader(nal), cursor.Span{BitLength: uint32(len(nal) * 8)}, options, newParseBudget(options), diagnostics)
		if err != nil || data.valid {
			t.Fatalf("invalid EBSP %s accepted: valid=%v error=%v", encoded, data.valid, err)
		}
		assertDiagnostic(t, diagnostics.items, CodeEmulationPreventionInvalid)
	}
	for _, encoded := range []string{"6700000300", "6700000301", "6700000302", "6700000303"} {
		nal = mustHex(t, encoded)
		diagnostics = newDiagnosticCollector(options.MaxDiagnostics)
		data, err = decodeRBSP(bytes.NewReader(nal), cursor.Span{BitLength: uint32(len(nal) * 8)}, options, newParseBudget(options), diagnostics)
		if err != nil || !data.valid || len(diagnostics.items) != 0 {
			t.Fatalf("valid EBSP %s rejected: valid=%v diagnostics=%+v error=%v", encoded, data.valid, diagnostics.items, err)
		}
	}
}

func TestRBSPMappingBudget(t *testing.T) {
	nal := mustHex(t, "6700000300000300000300")
	options, err := (Options{MaxRBSPMappingEntries: 2}).Normalized()
	if err != nil {
		t.Fatal(err)
	}
	diagnostics := newDiagnosticCollector(options.MaxDiagnostics)
	data, err := decodeRBSP(bytes.NewReader(nal), cursor.Span{BitLength: uint32(len(nal) * 8)}, options, newParseBudget(options), diagnostics)
	if err != nil || data.valid || len(data.bytes) != 0 || len(data.view.SourceSegments) != 0 || len(data.view.EmulationPreventionBytes) != 0 {
		t.Fatalf("mapping budget result: data=%+v diagnostics=%+v error=%v", data, diagnostics.items, err)
	}
	assertDiagnostic(t, diagnostics.items, CodeRBSPMappingLimitReached)
}

func TestUnsignedExpGolombUint32Boundary(t *testing.T) {
	parse := func(encoded []byte) (uint32, MappedSpan, bool, []Diagnostic) {
		offsets := make([]uint64, len(encoded))
		for index := range offsets {
			offsets[index] = uint64(index)
		}
		diagnostics := newDiagnosticCollector(DefaultMaxDiagnostics, uint64(len(encoded)))
		parser := newRBSPParser(rbspData{
			bytes: encoded, sourceOffsets: offsets, valid: true, end: uint64(len(encoded)),
		}, diagnostics)
		value, span, ok := parser.readUE()
		return value, span, ok, diagnostics.items
	}

	var maximum avcBitBuilder
	maximum.writeUE(math.MaxUint32)
	value, span, ok, diagnostics := parse(maximum.bytes())
	if !ok || value != math.MaxUint32 || span.BitLength != 65 || len(diagnostics) != 0 {
		t.Fatalf("max uint32 ue(v) = value=%d span=%+v ok=%v diagnostics=%+v", value, span, ok, diagnostics)
	}

	var overflow avcBitBuilder
	overflow.write(0, 32)
	overflow.write(1, 1)
	overflow.write(1, 32) // suffix 1 decodes to math.MaxUint32 + 1.
	value, span, ok, diagnostics = parse(overflow.bytes())
	if ok || value != 0 || span.BitLength != 0 {
		t.Fatalf("overflowing ue(v) exposed a value: value=%d span=%+v ok=%v", value, span, ok)
	}
	assertDiagnostic(t, diagnostics, CodeExpGolombOverflow)
}

func TestTruncatedSPSFieldHasNoDecodedSpan(t *testing.T) {
	// The final byte contains eight leading zero bits but no terminating one
	// for seq_parameter_set_id.
	nal := mustHex(t, "6742001e00")
	options, err := (Options{}).Normalized()
	if err != nil {
		t.Fatal(err)
	}
	diagnostics := newDiagnosticCollector(options.MaxDiagnostics, uint64(len(nal)))
	result, err := parseSequenceParameterSet(
		bytes.NewReader(nal),
		cursor.Span{BitLength: uint32(len(nal) * 8)},
		options,
		newParseBudget(options),
		diagnostics,
	)
	if err != nil {
		t.Fatalf("parseSequenceParameterSet: %v", err)
	}
	if result.Complete || result.SequenceParameterSetIDSpan.BitLength != 0 {
		t.Fatalf("truncated seq_parameter_set_id was exposed: %+v", result)
	}
	assertDiagnostic(t, diagnostics.items, CodeRBSPSyntaxTruncated)
}

func TestSyntheticHighProfileSPSBranches(t *testing.T) {
	var rbsp avcBitBuilder
	rbsp.write(100, 8)
	rbsp.write(0, 6)
	rbsp.write(0, 2)
	rbsp.write(40, 8)
	rbsp.writeUE(0)
	rbsp.writeUE(3)
	rbsp.write(1, 1)
	rbsp.writeUE(0)
	rbsp.writeUE(0)
	rbsp.write(0, 1)
	rbsp.write(1, 1)
	for index := 0; index < 12; index++ {
		if index == 0 {
			rbsp.write(1, 1)
			for element := 0; element < 16; element++ {
				rbsp.writeSE(0)
			}
		} else {
			rbsp.write(0, 1)
		}
	}
	rbsp.writeUE(0)
	rbsp.writeUE(1)
	rbsp.write(1, 1)
	rbsp.writeSE(-1)
	rbsp.writeSE(1)
	rbsp.writeUE(2)
	rbsp.writeSE(0)
	rbsp.writeSE(-2)
	rbsp.writeUE(1)
	rbsp.write(0, 1)
	rbsp.writeUE(19)
	rbsp.writeUE(5)
	rbsp.write(0, 1)
	rbsp.write(1, 1)
	rbsp.write(1, 1)
	rbsp.write(1, 1)
	rbsp.writeUE(0)
	rbsp.writeUE(0)
	rbsp.writeUE(0)
	rbsp.writeUE(6)
	rbsp.write(0, 1)
	rbsp.write(1, 1) // rbsp_stop_one_bit, left in RemainingRBSPBits.

	nal := append([]byte{0x67}, escapeRBSP(rbsp.bytes())...)
	options, err := (Options{}).Normalized()
	if err != nil {
		t.Fatal(err)
	}
	diagnostics := newDiagnosticCollector(options.MaxDiagnostics, uint64(len(nal)))
	result, err := parseSequenceParameterSet(bytes.NewReader(nal), cursor.Span{BitLength: uint32(len(nal) * 8)}, options, newParseBudget(options), diagnostics)
	if err != nil || !result.Complete || len(diagnostics.items) != 0 {
		t.Fatalf("synthetic SPS = %+v diagnostics=%+v error=%v", result, diagnostics.items, err)
	}
	if !result.HighProfileFieldsPresent || result.ChromaFormatIDC != 3 || !result.SeparateColourPlaneFlag ||
		result.PicOrderCntType != 1 || !result.DeltaPicOrderAlwaysZeroFlag || result.OffsetForNonRefPic != -1 ||
		result.OffsetForTopToBottomField != 1 || result.NumRefFramesInPicOrderCntCycle != 2 ||
		result.FrameMbsOnlyFlag || !result.MBAdaptiveFrameFieldFlag || result.ChromaArrayType != 0 ||
		result.CodedWidth != 320 || result.CodedHeight != 192 || result.DisplayWidth != 320 || result.DisplayHeight != 180 {
		t.Fatalf("unexpected synthetic SPS summary: %+v", result)
	}
}

func TestSemanticSPSAndPPSErrorsAreIncomplete(t *testing.T) {
	t.Run("invalid chroma format stops SPS", func(t *testing.T) {
		var rbsp avcBitBuilder
		rbsp.write(100, 8)
		rbsp.write(0, 8)
		rbsp.write(40, 8)
		rbsp.writeUE(0)
		rbsp.writeUE(4)
		result, diagnostics := parseSyntheticSPS(t, rbsp.bytes())
		if result.Complete {
			t.Fatalf("invalid SPS complete: %+v", result)
		}
		assertDiagnostic(t, diagnostics, CodeSPSChromaFormatInvalid)
	})

	t.Run("invalid PPS identifiers", func(t *testing.T) {
		var rbsp avcBitBuilder
		rbsp.writeUE(256)
		rbsp.writeUE(32)
		rbsp.write(0, 2)
		nal := append([]byte{0x68}, escapeRBSP(rbsp.bytes())...)
		options, _ := (Options{}).Normalized()
		diagnostics := newDiagnosticCollector(options.MaxDiagnostics, uint64(len(nal)))
		result, err := parsePictureParameterSet(bytes.NewReader(nal), cursor.Span{BitLength: uint32(len(nal) * 8)}, options, newParseBudget(options), diagnostics)
		if err != nil || result.Complete {
			t.Fatalf("invalid PPS = %+v error=%v", result, err)
		}
		assertDiagnostic(t, diagnostics.items, CodePPSIDOutOfRange)
		assertDiagnostic(t, diagnostics.items, CodePPSReferencedSPSOutOfRange)
	})
}

func TestReaderAtFailuresAreGoErrors(t *testing.T) {
	sentinel := errors.New("storage failure")
	reader := failingReaderAt{err: sentinel}
	if _, err := ParseDecoderConfigurationRecord(reader, cursor.Span{BitLength: 8}, Options{}); !errors.Is(err, sentinel) {
		t.Fatalf("configuration error=%v, want sentinel", err)
	}
	if _, err := ParseLengthPrefixedNALUnits(reader, cursor.Span{BitLength: 8}, 1, Options{}); !errors.Is(err, sentinel) {
		t.Fatalf("NAL error=%v, want sentinel", err)
	}
}

func TestOptionsAndInputValidation(t *testing.T) {
	if _, err := (Options{MaxDiagnostics: 1}).Normalized(); err == nil {
		t.Fatal("accepted too few diagnostics")
	}
	if _, err := (Options{MaxNALUnits: HardMaxNALUnits + 1}).Normalized(); err == nil {
		t.Fatal("accepted excessive NAL-unit limit")
	}
	if _, err := (Options{MaxRBSPMappingEntries: HardMaxRBSPMappingEntries + 1}).Normalized(); err == nil {
		t.Fatal("accepted excessive RBSP mapping limit")
	}
	if _, err := (Options{MaxParameterSets: HardMaxParameterSets + 1}).Normalized(); err == nil {
		t.Fatal("accepted excessive parameter-set limit")
	}
	if _, err := (Options{MaxParameterSetBytes: HardMaxParameterSetBytes + 1}).Normalized(); err == nil {
		t.Fatal("accepted excessive parameter-set byte limit")
	}
	if _, err := (Options{MaxDiagnostics: HardMaxDiagnostics + 1}).Normalized(); err == nil {
		t.Fatal("accepted excessive diagnostic limit")
	}
	if _, err := ParseDecoderConfigurationRecord(nil, cursor.Span{}, Options{}); err == nil {
		t.Fatal("accepted nil source")
	}
	if _, err := ParseDecoderConfigurationRecord(bytes.NewReader(nil), cursor.Span{BitOffset: 1, BitLength: 7}, Options{}); err == nil {
		t.Fatal("accepted unaligned span")
	}
	tooLarge := cursor.Span{BitLength: uint32((HardMaxPayloadBytes + 1) * 8)}
	if _, err := ParseDecoderConfigurationRecord(bytes.NewReader(nil), tooLarge, Options{}); err == nil {
		t.Fatal("accepted excessive payload span")
	}
	overflow := cursor.Span{ByteOffset: math.MaxUint64, BitLength: 8}
	if _, err := ParseDecoderConfigurationRecord(bytes.NewReader(nil), overflow, Options{}); err == nil {
		t.Fatal("accepted overflowing payload span")
	}
}

func TestNALUnitTypeNamesAndSequenceHasErrors(t *testing.T) {
	for value := NALUnitType(0); value <= 31; value++ {
		if value.String() == "" {
			t.Fatalf("NAL type %d has empty name", value)
		}
	}
	wantNames := map[NALUnitType]string{
		NALUnitTypeDepthParameterSet:      "depthParameterSet",
		NALUnitTypeAuxiliaryCodedSlice:    "auxiliaryCodedSlice",
		NALUnitTypeCodedSliceExtension:    "codedSliceExtension",
		NALUnitTypeDepthViewCodedSliceExt: "depthViewCodedSliceExtension",
		17:                                "reserved", 18: "reserved", 22: "reserved", 23: "reserved",
		24: "unspecified", 31: "unspecified",
	}
	for value, want := range wantNames {
		if got := value.String(); got != want {
			t.Errorf("NAL type %d name = %q, want %q", value, got, want)
		}
	}
	if (NALUnitSequence{Complete: true}).HasErrors() {
		t.Fatal("complete sequence reported errors")
	}
	if !(NALUnitSequence{}).HasErrors() {
		t.Fatal("incomplete sequence did not report errors")
	}
}

func TestDeterministicParsing(t *testing.T) {
	data := mustHex(t, highConfigurationHex)
	first := parseConfigurationAt(t, 17, data, Options{MaxDiagnostics: 8})
	for iteration := 0; iteration < 50; iteration++ {
		next := parseConfigurationAt(t, 17, data, Options{MaxDiagnostics: 8})
		if !reflect.DeepEqual(first, next) {
			t.Fatalf("iteration %d changed result", iteration)
		}
	}
}

func BenchmarkParseDecoderConfigurationRecord(b *testing.B) {
	data, _ := hex.DecodeString(highConfigurationHex)
	reader := bytes.NewReader(data)
	span := cursor.Span{BitLength: uint32(len(data) * 8)}
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	for iteration := 0; iteration < b.N; iteration++ {
		result, err := ParseDecoderConfigurationRecord(reader, span, Options{})
		if err != nil || !result.Complete {
			b.Fatalf("parse = complete %v, error %v", result.Complete, err)
		}
	}
}

func BenchmarkParseLengthPrefixedNALUnits(b *testing.B) {
	data, _ := hex.DecodeString("0000000265aa0000000109")
	reader := bytes.NewReader(data)
	span := cursor.Span{BitLength: uint32(len(data) * 8)}
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	for iteration := 0; iteration < b.N; iteration++ {
		result, err := ParseLengthPrefixedNALUnits(reader, span, 4, Options{})
		if err != nil || !result.Complete {
			b.Fatalf("parse = complete %v, error %v", result.Complete, err)
		}
	}
}

func parseConfigurationAt(t *testing.T, base int, payload []byte, options Options) ConfigurationRecord {
	t.Helper()
	source := make([]byte, base+len(payload)+2)
	copy(source[base:], payload)
	span := cursor.Span{ByteOffset: uint64(base), BitLength: uint32(len(payload) * 8)}
	result, err := ParseDecoderConfigurationRecord(bytes.NewReader(source), span, options)
	if err != nil {
		t.Fatalf("ParseDecoderConfigurationRecord() error = %v", err)
	}
	return result
}

func parseNALsAt(t *testing.T, base int, payload []byte, lengthSize uint8, options Options) NALUnitSequence {
	t.Helper()
	source := make([]byte, base+len(payload)+2)
	copy(source[base:], payload)
	span := cursor.Span{ByteOffset: uint64(base), BitLength: uint32(len(payload) * 8)}
	result, err := ParseLengthPrefixedNALUnits(bytes.NewReader(source), span, lengthSize, options)
	if err != nil {
		t.Fatalf("ParseLengthPrefixedNALUnits() error = %v", err)
	}
	return result
}

func mustHex(t *testing.T, encoded string) []byte {
	t.Helper()
	data, err := hex.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func assertDiagnostic(t *testing.T, diagnostics []Diagnostic, code string) {
	t.Helper()
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == code {
			return
		}
	}
	t.Fatalf("diagnostic %s not found in %+v", code, diagnostics)
}

func assertOuterSpan(t *testing.T, span cursor.Span, offset, length int) {
	t.Helper()
	want := cursor.Span{ByteOffset: uint64(offset), BitLength: uint32(length * 8)}
	if span != want {
		t.Fatalf("span=%+v, want %+v", span, want)
	}
}

func sumSpanBits(spans []cursor.Span) uint32 {
	var total uint32
	for _, span := range spans {
		total += span.BitLength
	}
	return total
}

func parseSyntheticSPS(t *testing.T, rbsp []byte) (SequenceParameterSet, []Diagnostic) {
	t.Helper()
	nal := append([]byte{0x67}, escapeRBSP(rbsp)...)
	options, err := (Options{}).Normalized()
	if err != nil {
		t.Fatal(err)
	}
	diagnostics := newDiagnosticCollector(options.MaxDiagnostics, uint64(len(nal)))
	result, err := parseSequenceParameterSet(bytes.NewReader(nal), cursor.Span{BitLength: uint32(len(nal) * 8)}, options, newParseBudget(options), diagnostics)
	if err != nil {
		t.Fatal(err)
	}
	return result, diagnostics.items
}

func escapeRBSP(rbsp []byte) []byte {
	result := make([]byte, 0, len(rbsp))
	zeroCount := 0
	for _, value := range rbsp {
		if zeroCount >= 2 && value <= 3 {
			result = append(result, 3)
			zeroCount = 0
		}
		result = append(result, value)
		if value == 0 {
			zeroCount++
		} else {
			zeroCount = 0
		}
	}
	return result
}

type avcBitBuilder struct {
	data   []byte
	length uint64
}

func (b *avcBitBuilder) write(value uint64, width uint8) {
	for index := int(width) - 1; index >= 0; index-- {
		byteIndex := b.length / 8
		bitIndex := b.length % 8
		if byteIndex == uint64(len(b.data)) {
			b.data = append(b.data, 0)
		}
		b.data[byteIndex] |= byte((value>>uint(index))&1) << (7 - bitIndex)
		b.length++
	}
}

func (b *avcBitBuilder) writeUE(value uint32) {
	codeNum := uint64(value) + 1
	width := uint8(bits.Len64(codeNum))
	b.write(0, width-1)
	b.write(codeNum, width)
}

func (b *avcBitBuilder) writeSE(value int32) {
	var codeNum uint32
	if value <= 0 {
		codeNum = uint32(-int64(value) * 2)
	} else {
		codeNum = uint32(value*2 - 1)
	}
	b.writeUE(codeNum)
}

func (b *avcBitBuilder) bytes() []byte { return append([]byte(nil), b.data...) }

type failingReaderAt struct{ err error }

func (r failingReaderAt) ReadAt(_ []byte, _ int64) (int, error) { return 0, r.err }

var _ io.ReaderAt = failingReaderAt{}
