package detailview

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/sevico/muxray/internal/avc"
	"github.com/sevico/muxray/internal/avccontext"
	"github.com/sevico/muxray/internal/cursor"
	"github.com/sevico/muxray/internal/flv"
)

const desktopHighAVCC = "0164000cffe1001a6764000cacd941419f9f011000000300100000030320f142996001000668ebe3cb22c0fdf8f800"

func avcHex(t *testing.T, value string) []byte {
	t.Helper()
	data, err := hex.DecodeString(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func avcNALPayload(width uint8, units ...[]byte) []byte {
	payload := []byte{0x27, 1, 0, 0, 0}
	for _, n := range units {
		length := uint32(len(n))
		for i := int(width) - 1; i >= 0; i-- {
			payload = append(payload, byte(length>>(8*i)))
		}
		payload = append(payload, n...)
	}
	return payload
}

func avcResolution(width uint8) avccontext.Resolution {
	return avccontext.Resolution{State: avccontext.StateFound, NALLengthSize: width, SourceTagID: 0,
		SourceLengthSpan: cursor.Span{ByteOffset: 9, BitOffset: 6, BitLength: 2}, ScannedTags: 4, Candidates: 2, BytesRead: 70}
}

func parseAVCFixture(t *testing.T, payload []byte, r avccontext.Resolution) *Detail {
	t.Helper()
	data, tag := fixture(t, 9, payload)
	tag.ID = 5
	d, err := ParseWithAVCContext(context.Background(), bytes.NewReader(data), tag, r)
	if err != nil {
		t.Fatal(err)
	}
	assertBounded(t, d)
	for _, f := range d.Fields {
		if f.Span == nil {
			continue
		}
		if f.Name == "sourceLengthSizeMinusOne" {
			if *f.Span != *span(r.SourceLengthSpan) {
				t.Fatalf("context provenance: %+v", f)
			}
			continue
		}
		offset, err := strconv.ParseUint(f.Span.ByteOffset, 10, 64)
		if err != nil || offset < 24 || offset+(uint64(f.Span.BitOffset)+uint64(f.Span.BitLength)+7)/8 > 24+uint64(len(payload)) {
			t.Fatalf("AVC span outside selected payload: %+v", f)
		}
	}
	return d
}

func optionalField(d *Detail, name string) *Field {
	for i := range d.Fields {
		if d.Fields[i].Name == name {
			return &d.Fields[i]
		}
	}
	return nil
}

func TestAVCConfigurationSummariesAndOriginalSpans(t *testing.T) {
	config := avcHex(t, desktopHighAVCC)
	d := parseAVCFixture(t, append([]byte{0x17, 0, 0, 0, 0}, config...), avccontext.Resolution{})
	if d.Kind != "avcVideo" || !d.Complete || d.Truncated || len(d.Diagnostics) != 0 {
		t.Fatalf("config detail: %+v", d)
	}
	if f := find(t, d, "lengthSizeMinusOne"); f.Value != "3" || *f.Span != (Span{ByteOffset: "33", BitOffset: 6, BitLength: 2}) {
		t.Fatalf("length declaration: %+v", f)
	}
	if f := find(t, d, "SPS[0]"); *f.Span != (Span{ByteOffset: "37", BitLength: 26 * 8}) {
		t.Fatalf("full original SPS: %+v", f)
	}
	if f := find(t, d, "PPS[0]"); *f.Span != (Span{ByteOffset: "66", BitLength: 6 * 8}) {
		t.Fatalf("full original PPS: %+v", f)
	}
	if find(t, d, "displayDimensions").Value != "320 × 180" || find(t, d, "codedDimensions").Value != "320 × 192" {
		t.Fatalf("dimensions: %+v", d.Fields)
	}
	if find(t, d, "emulationPreventionBytes").Value != "2" || !strings.HasPrefix(find(t, d, "sourceSegments").Value, "3 ") {
		t.Fatalf("EPB source map summary: %+v", d.Fields)
	}
	for _, f := range d.Fields {
		if strings.Contains(f.Type, "derived") && f.Span != nil {
			t.Fatalf("derived summary has fabricated source span: %+v", f)
		}
		if strings.Contains(f.Name, "picWidthInMbs") || strings.Contains(f.Name, "frameCrop") {
			t.Fatalf("deep RBSP syntax expanded: %+v", f)
		}
	}
	if find(t, d, "AVC context").Value != "not_needed" {
		t.Fatal("configuration should not require earlier context")
	}
	if f := find(t, d, "numSequenceParameterSetExt"); f.Value != "0" || f.Span.BitLength != 8 {
		t.Fatalf("high-profile avcC fields missing: %+v", f)
	}
}

func TestAVCNALLengthWidthsAndProvenance(t *testing.T) {
	for _, width := range []uint8{1, 2, 4} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			r := avcResolution(width)
			d := parseAVCFixture(t, avcNALPayload(width, []byte{0x65, 0x88}, []byte{0x06}), r)
			if !d.Complete || d.Truncated || find(t, d, "AVC context").Value != "found" {
				t.Fatalf("NAL detail: %+v", d)
			}
			if f := find(t, d, "NALLength"); f.Value != "2" || f.Span.ByteOffset != "29" || f.Span.BitLength != uint32(width)*8 {
				t.Fatalf("NAL prefix: %+v", f)
			}
			if f := find(t, d, "nalUnitType"); f.Value != "5 (idrSlice)" || f.Span.ByteOffset != decimal(29+uint64(width)) || f.Span.BitOffset != 3 || f.Span.BitLength != 5 {
				t.Fatalf("NAL type: %+v", f)
			}
			if find(t, d, "nalRefIdc").Value != "3" || find(t, d, "sourceTagId").Value != "0" || find(t, d, "scannedTags").Value != "4" || find(t, d, "candidates").Value != "2" || find(t, d, "bytesRead").Value != "70" {
				t.Fatal("header or context accounting missing")
			}
			if find(t, d, "sourceLengthSizeMinusOne").Value != decimal(uint64(width-1)) || find(t, d, "NAL[1]").Span.ByteOffset != decimal(31+2*uint64(width)) {
				t.Fatal("source width or next NAL boundary incorrect")
			}
		})
	}
}

func TestAVCSignedCompositionAndDerivedTimes(t *testing.T) {
	for _, tc := range []struct {
		dts int64
		cts int32
		pts string
	}{
		{1, -2, "-1"}, {0, -8388608, "-8388608"}, {math.MaxUint32, 8388607, "4303355902"}, {math.MaxInt64, 1, "9223372036854775808"}, {math.MinInt64, -1, "-9223372036854775809"},
	} {
		payload := avcNALPayload(1, []byte{0x65})
		payload[2], payload[3], payload[4] = byte(tc.cts>>16), byte(tc.cts>>8), byte(tc.cts)
		data, tag := fixture(t, 9, payload)
		tag.DTS = tc.dts
		d, err := ParseWithAVCContext(context.Background(), bytes.NewReader(data), tag, avcResolution(1))
		if err != nil {
			t.Fatal(err)
		}
		if !d.Complete || find(t, d, "compositionTime").Value != strconv.FormatInt(int64(tc.cts), 10) || find(t, d, "PTS").Value != tc.pts+" (DTS + compositionTime)" {
			t.Fatalf("signed time: %+v", d.Fields)
		}
		if find(t, d, "DTS").Span != nil || find(t, d, "PTS").Span != nil || *find(t, d, "compositionTime").Span != (Span{ByteOffset: "26", BitLength: 24}) {
			t.Fatal("time provenance")
		}
		assertBounded(t, d)
	}
}

func TestAVCNoContextNeverGuessesAndLookupBudgetIsDistinct(t *testing.T) {
	for _, state := range []string{"", avccontext.StateMissing, avccontext.StateBudgetExhausted} {
		d := parseAVCFixture(t, avcNALPayload(1, []byte{0x65}), avccontext.Resolution{State: state, ScannedTags: 512, Candidates: 32, BytesRead: 123})
		if d.Complete || !hasDiagnostic(d, flv.CodeAVCNALLengthSizeUnavailable) || optionalField(d, "NAL[0]") != nil || optionalField(d, "sourceTagId") != nil {
			t.Fatalf("guessed missing context: %+v", d)
		}
		budget := state == avccontext.StateBudgetExhausted
		if hasDiagnostic(d, CodeAVCContextLookupLimitReached) != budget || d.Truncated != budget {
			t.Fatalf("lookup limit confused with absence: %+v", d)
		}
		if !strings.Contains(find(t, d, "unparsedNALData").Value, "not guessed") {
			t.Fatal("missing no-guess explanation")
		}
	}
}

func TestAVCPartialSyntaxAndUnsupportedSelections(t *testing.T) {
	for _, tc := range []struct {
		name    string
		payload []byte
		code    string
	}{
		{"empty", nil, flv.CodeAVCVideoHeaderTruncated},
		{"packet", []byte{0x17}, flv.CodeAVCPacketTypeTruncated},
		{"composition", []byte{0x17, 1, 0}, flv.CodeAVCCompositionTimeTruncated},
		{"config", []byte{0x17, 0, 0, 0, 0, 1, 66}, avc.CodeRecordHeaderTruncated},
		{"prefix", []byte{0x27, 1, 0, 0, 0, 0}, avc.CodeNALLengthTruncated},
		{"payload", []byte{0x27, 1, 0, 0, 0, 0, 5, 0x65}, avc.CodeNALPayloadTruncated},
		{"zero", []byte{0x27, 1, 0, 0, 0, 0, 0}, avc.CodeNALUnitZeroLength},
		{"forbidden", avcNALPayload(2, []byte{0xe5}), avc.CodeForbiddenZeroBitSet},
		{"unknown", []byte{0x17, 3, 0, 0, 0, 99}, flv.CodeAVCPacketTypeUnknown},
		{"eos extra", []byte{0x17, 2, 0, 0, 0, 99}, flv.CodeAVCEndOfSequencePayloadUnexpected},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := parseAVCFixture(t, tc.payload, avcResolution(2))
			if d.Complete || !hasDiagnostic(d, tc.code) {
				t.Fatalf("missing partial diagnostic %s: %+v", tc.code, d)
			}
			if tc.name == "payload" && (*find(t, d, "NAL[0]").Span != (Span{ByteOffset: "31", BitLength: 8}) || optionalField(d, "NALHeader") != nil) {
				t.Fatal("truncated NAL fabricated unavailable bytes/header")
			}
		})
	}
	for _, payload := range [][]byte{{0x12}, {0x57}} {
		data, tag := fixture(t, 9, payload)
		if _, err := Parse(context.Background(), bytes.NewReader(data), tag); !errors.Is(err, ErrUnsupported) {
			t.Fatalf("unsupported: %v", err)
		}
	}
	d := parseAVCFixture(t, []byte{0x17, 2, 0, 0, 0}, avccontext.Resolution{})
	if !d.Complete || optionalField(d, "endOfSequence") == nil {
		t.Fatal("EOS incomplete")
	}
}

func TestAVCResourceBudgetsAndBoundedJSON(t *testing.T) {
	o := DesktopAVCOptions().AVC
	if o.MaxDiagnostics != 64 || o.MaxParameterSets != 64 || o.MaxNALUnits != 256 || o.MaxParameterSetBytes != 64<<10 || o.MaxRBSPMappingEntries != 4096 {
		t.Fatalf("desktop parser budgets: %+v", o)
	}
	t.Run("fields", func(t *testing.T) {
		units := make([][]byte, 100)
		for i := range units {
			units[i] = []byte{0x65}
		}
		d := parseAVCFixture(t, avcNALPayload(1, units...), avcResolution(1))
		if !d.Complete || !d.Truncated || len(d.Fields) != MaxFields {
			t.Fatalf("field cap: fields=%d complete=%t truncated=%t", len(d.Fields), d.Complete, d.Truncated)
		}
	})
	t.Run("NAL count", func(t *testing.T) {
		units := make([][]byte, 257)
		for i := range units {
			units[i] = []byte{0x65}
		}
		d := parseAVCFixture(t, avcNALPayload(1, units...), avcResolution(1))
		if d.Complete || !d.Truncated || !hasDiagnostic(d, avc.CodeNALUnitLimitReached) {
			t.Fatalf("NAL cap: %+v", d.Diagnostics)
		}
	})
	t.Run("diagnostics", func(t *testing.T) {
		units := make([][]byte, 100)
		for i := range units {
			units[i] = []byte{0xe5}
		}
		d := parseAVCFixture(t, avcNALPayload(1, units...), avcResolution(1))
		if d.Complete || !d.Truncated || len(d.Diagnostics) != MaxDiagnostics || !hasDiagnostic(d, avc.CodeDiagnosticLimitReached) {
			t.Fatalf("diagnostic cap: %+v", d.Diagnostics)
		}
	})
	t.Run("parameter sets", func(t *testing.T) {
		payload := []byte{0x17, 0, 0, 0, 0, 1, 66, 0, 30, 0xff, 0xe0, 65}
		pps := avcHex(t, "68cb83cb20")
		for i := 0; i < 65; i++ {
			payload = append(payload, 0, byte(len(pps)))
			payload = append(payload, pps...)
		}
		d := parseAVCFixture(t, payload, avccontext.Resolution{})
		if d.Complete || !d.Truncated || !hasDiagnostic(d, avc.CodeParameterSetLimitReached) {
			t.Fatalf("parameter-set cap: %+v", d.Diagnostics)
		}
	})
	t.Run("parameter bytes", func(t *testing.T) {
		sps := make([]byte, (64<<10)+2)
		sps[0] = 0x67
		d := parseAVCFixture(t, avcNALPayload(4, sps), avcResolution(4))
		if d.Complete || !d.Truncated || !hasDiagnostic(d, avc.CodeParameterSetTooLarge) {
			t.Fatalf("parameter byte cap: %+v", d.Diagnostics)
		}
		if find(t, d, "NAL[0]").Span.BitLength != uint32(len(sps))*8 {
			t.Fatal("omitted summary lost full original span")
		}
	})
	t.Run("RBSP map", func(t *testing.T) {
		sps := append([]byte{0x67}, bytes.Repeat([]byte{0, 0, 3, 1}, 2200)...)
		d := parseAVCFixture(t, avcNALPayload(4, sps), avcResolution(4))
		if d.Complete || !d.Truncated || !hasDiagnostic(d, avc.CodeRBSPMappingLimitReached) {
			t.Fatalf("mapping cap: %+v", d.Diagnostics)
		}
	})
}

type selectedPayloadReader struct {
	source     io.ReaderAt
	start, end int64
	reads      int
}

func (r *selectedPayloadReader) ReadAt(p []byte, off int64) (int, error) {
	r.reads++
	if off < r.start || off > r.end || int64(len(p)) > r.end-off {
		return 0, errors.New("read outside selected payload")
	}
	return r.source.ReadAt(p, off)
}

func TestAVCLargeIdentitySelectedReadBoundsAndCancellation(t *testing.T) {
	payload := avcNALPayload(1, []byte{0x65, 1, 2, 3})
	data, tag := fixture(t, 9, payload)
	const delta uint64 = 1<<53 + 123
	tag.Offset += delta
	tag.ID = math.MaxUint64
	r := avcResolution(1)
	r.SourceTagID = 1<<53 + 1
	r.SourceLengthSpan.ByteOffset = 1<<53 + 9
	r.ScannedTags = 1<<53 + 3
	r.BytesRead = 1<<53 + 5
	reader := &selectedPayloadReader{source: shiftedReader{base: int64(delta), data: data}, start: int64(delta + 24), end: int64(delta + 24 + uint64(len(payload)))}
	d, err := ParseWithAVCContext(context.Background(), reader, tag, r)
	if err != nil {
		t.Fatal(err)
	}
	if find(t, d, "tagId").Value != "18446744073709551615" || find(t, d, "sourceTagId").Value != "9007199254740993" || find(t, d, "scannedTags").Value != "9007199254740995" || find(t, d, "bytesRead").Value != "9007199254740997" {
		t.Fatal("rounded AVC identity/statistics")
	}
	if find(t, d, "NAL[0]").Span.ByteOffset != decimal(delta+30) || find(t, d, "sourceLengthSizeMinusOne").Span.ByteOffset != "9007199254741001" {
		t.Fatal("rounded AVC provenance")
	}
	assertBounded(t, d)
	for _, before := range []bool{true, false} {
		ctx, cancel := context.WithCancel(context.Background())
		tracked := &trackingReader{source: reader, cancel: cancel}
		if before {
			cancel()
		}
		if _, err := ParseWithAVCContext(ctx, tracked, tag, r); !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation: %v", err)
		}
		if before && tracked.reads != 0 {
			t.Fatal("read after cancellation")
		}
		cancel()
	}
	failure := errors.New("AVC source failed")
	if _, err := ParseWithAVCContext(context.Background(), &trackingReader{failure: failure}, tag, r); !errors.Is(err, failure) {
		t.Fatalf("source failure: %v", err)
	}
}

type opaqueAVCReader struct {
	source                 io.ReaderAt
	opaqueStart, opaqueEnd int64
}

func (r opaqueAVCReader) ReadAt(p []byte, off int64) (int, error) {
	if off < r.opaqueEnd && off+int64(len(p)) > r.opaqueStart {
		return 0, errors.New("opaque slice payload read")
	}
	return r.source.ReadAt(p, off)
}

func TestAVCOpaqueSlicesAndSelectionValidation(t *testing.T) {
	data, tag := fixture(t, 9, avcNALPayload(1, []byte{0x65, 1, 2, 3}))
	reader := opaqueAVCReader{source: bytes.NewReader(data), opaqueStart: 31, opaqueEnd: 34}
	d, err := ParseWithAVCContext(context.Background(), reader, tag, avcResolution(1))
	if err != nil || !d.Complete || find(t, d, "payloadEndOffset").Value != "34" {
		t.Fatalf("opaque slice parsing: detail=%+v error=%v", d, err)
	}
	for _, r := range []avccontext.Resolution{
		{State: avccontext.StateFound, NALLengthSize: 3, SourceLengthSpan: cursor.Span{BitOffset: 6, BitLength: 2}},
		{State: avccontext.StateFound, NALLengthSize: 1, SourceLengthSpan: cursor.Span{BitOffset: 0, BitLength: 8}},
	} {
		if _, err := ParseWithAVCContext(context.Background(), reader, tag, r); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("invalid context: %v", err)
		}
	}
	filtered := tag
	filtered.RawTypeFlags |= 0x20
	if _, err := Parse(context.Background(), reader, filtered); !errors.Is(err, ErrFiltered) {
		t.Fatalf("filtered AVC: %v", err)
	}
	truncated := tag
	truncated.AvailableDataSize--
	if _, err := Parse(context.Background(), reader, truncated); !errors.Is(err, ErrTruncated) {
		t.Fatalf("truncated container AVC: %v", err)
	}
}

func TestAVCVUIDetailsKeepSourceMappingBoundaryAndBudgets(t *testing.T) {
	if DesktopAVCOptions().AVC.ParseVUI {
		t.Fatal("shared summary/context options enabled VUI")
	}
	config := avcHex(t, desktopHighAVCC)
	d := parseAVCFixture(t, append([]byte{0x17, 0, 0, 0, 0}, config...), avccontext.Resolution{})
	for name, want := range map[string]string{"vuiComplete": "true", "spsTrailingBitsValid": "true", "numUnitsInTick": "1", "timeScale": "50"} {
		f := find(t, d, name)
		if f.Value != want || f.Span != nil {
			t.Fatalf("VUI field %s fabricated value/span: %+v", name, f)
		}
	}
	sps := config[8:34]
	partial := parseAVCFixture(t, avcNALPayload(1, sps[:21]), avcResolution(1))
	if partial.Complete || !partial.Truncated || find(t, partial, "summaryComplete").Value != "true" || find(t, partial, "vuiComplete").Value != "false" {
		t.Fatalf("partial VUI state: %+v", partial)
	}
	units := make([][]byte, 30)
	for i := range units {
		units[i] = sps
	}
	capped := parseAVCFixture(t, avcNALPayload(1, units...), avcResolution(1))
	if !capped.Complete || !capped.Truncated || len(capped.Fields) != MaxFields {
		t.Fatalf("VUI field cap: %d %t %t", len(capped.Fields), capped.Complete, capped.Truncated)
	}
}
