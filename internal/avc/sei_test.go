package avc

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"io"
	"math"
	"reflect"
	"testing"

	"github.com/sevico/muxray/internal/cursor"
)

func seiMessageBytes(kind uint32, payload []byte) []byte {
	var data []byte
	for _, v := range []uint32{kind, uint32(len(payload))} {
		for v >= 255 {
			data = append(data, 255)
			v -= 255
		}
		data = append(data, byte(v))
	}
	return append(data, payload...)
}
func seiNAL(messages ...[]byte) []byte {
	var rbsp []byte
	for _, m := range messages {
		rbsp = append(rbsp, m...)
	}
	return append([]byte{6}, escapeRBSP(append(rbsp, 128))...)
}
func parseSEIPacket(t *testing.T, nals ...[]byte) NALUnitSequence {
	t.Helper()
	p := prefixPacket(nals...)
	r, err := ParseLengthPrefixedNALUnits(bytes.NewReader(p), cursor.Span{BitLength: uint32(len(p) * 8)}, 4, Options{ParseSEI: true})
	if err != nil {
		t.Fatal(err)
	}
	assertSpansByReflection(t, reflect.ValueOf(r), 0, len(p))
	return r
}
func seiHasCode(r NALUnitSequence, code string) bool {
	for _, d := range r.Diagnostics {
		if d.Code == code {
			return true
		}
	}
	return false
}

func TestSEIMultipleMessagesMappingAndOpaquePayload(t *testing.T) {
	user := append(bytes.Repeat([]byte{0}, 16), bytes.Repeat([]byte{0x21}, 80)...)
	unknown := bytes.Repeat([]byte{0x11}, 300)
	nal := seiNAL(seiMessageBytes(6, []byte{0xd4}), seiMessageBytes(5, user), seiMessageBytes(511, unknown), seiMessageBytes(1, nil))
	r := parseSEIPacket(t, nal)
	s := r.NALUnits[0].SEI
	if !r.Complete || !s.Complete || !s.FramingComplete || len(s.Messages) != 4 || len(s.RBSP.EmulationPreventionBytes) == 0 {
		t.Fatalf("result %+v diagnostics %+v", s, r.Diagnostics)
	}
	if s.BytesRead != uint32(len(nal)-1) || s.SyntaxBits != s.RBSP.ByteLength*8 || s.RBSPTrailingBits.BitLength != 8 {
		t.Fatal("accounting")
	}
	m := s.Messages[0]
	p := m.RecoveryPoint
	if m.Kind != "recoveryPoint" || !m.SyntaxSupported || p.RecoveryFrameCnt.Value != 0 || !p.ExactMatchFlag.Value || p.BrokenLinkFlag.Value || p.ChangingSliceGroupIDC.Value != 2 || p.PayloadAlignmentBits.BitLength != 3 {
		t.Fatal("recovery fields")
	}
	if p.ChangingSliceGroupIDC.Span.RBSPBitOffset != 19 || !reflect.DeepEqual(p.ChangingSliceGroupIDC.Span.SourceSpans, []cursor.Span{{ByteOffset: 7, BitOffset: 3, BitLength: 2}}) {
		t.Fatalf("source map %+v", p.ChangingSliceGroupIDC.Span)
	}
	u := s.Messages[1].UserDataUnregistered
	if u.UUID.Hex != hex.EncodeToString(user[:16]) || len(u.UUID.Span.SourceSpans) < 2 || u.DataPreview.Hex != hex.EncodeToString(user[16:80]) || u.DataBytes != 80 || !u.PreviewTruncated {
		t.Fatal("UUID/preview")
	}
	if m := s.Messages[2]; m.Kind != "unknown" || m.SyntaxSupported || !m.Complete || m.PayloadType.Value != 511 || m.PayloadType.Span.BitLength != 24 || m.PayloadSize.Value != 300 || m.PayloadSize.Span.BitLength != 16 || m.PayloadSpan.BitLength != 2400 {
		t.Fatalf("unknown %+v", m)
	}
	if m := s.Messages[3]; !m.Complete || m.PayloadSpan != nil {
		t.Fatal("empty opaque payload")
	}
}

func TestSEIRecoveryPayloadExtensionAndAlignment(t *testing.T) {
	var b avcBitBuilder
	b.writeUE(0)
	b.write(1, 1)
	b.write(0, 1)
	b.write(2, 2)
	b.write(0x35a, 11)
	b.write(1, 1)
	r := parseSEIPacket(t, seiNAL(seiMessageBytes(6, b.bytes())))
	p := r.NALUnits[0].SEI.Messages[0].RecoveryPoint
	if !r.Complete || p.PayloadExtensionBits.BitLength != 11 || p.PayloadAlignmentBits.BitLength != 8 {
		t.Fatalf("extension %+v %+v", p, r.Diagnostics)
	}
	// ue(1) is 3 bits, so its flags finish at bit7; ue(3) is5 and
	// finishes at9. The supported syntax always has an odd bit length.
	// A payload-aligned extension marker still must lie after that syntax.
	for _, payload := range [][]byte{{0xd0}, {0xd0, 0}, {0}} {
		r = parseSEIPacket(t, seiNAL(seiMessageBytes(6, payload)))
		if r.Complete {
			t.Fatalf("accepted invalid alignment/syntax %x", payload)
		}
	}
}

func TestSEIMalformedAndLegacyOptIn(t *testing.T) {
	var big avcBitBuilder
	big.writeUE(65536)
	big.write(0, 4)
	big.write(1, 1)
	cases := []struct {
		name string
		nal  []byte
		code string
	}{
		{"empty", []byte{6, 128}, CodeSEISyntaxInvalid},
		{"type continuation", []byte{6, 255}, CodeSEIHeaderTruncated},
		{"size continuation", []byte{6, 5, 255}, CodeSEIHeaderTruncated},
		{"body", []byte{6, 5, 17, 1, 2}, CodeSEIPayloadTruncated},
		{"short UUID", seiNAL(seiMessageBytes(5, []byte{1, 2, 3})), CodeSEISyntaxInvalid},
		{"group range", seiNAL(seiMessageBytes(6, []byte{0xdc})), CodeSEISyntaxInvalid},
		{"frame range", seiNAL(seiMessageBytes(6, big.bytes())), CodeSEISyntaxInvalid},
		{"no trailing", []byte{6, 1, 1, 1}, CodeSEITrailingBitsInvalid},
		{"bad EPB", []byte{6, 1, 4, 0, 0, 3, 4, 128}, CodeEmulationPreventionInvalid},
		{"missing EPB", []byte{6, 1, 3, 0, 0, 1, 128}, CodeEmulationPreventionInvalid},
		{"ref idc", []byte{0x26, 1, 0, 128}, CodeSEISyntaxInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := parseSEIPacket(t, tc.nal)
			if r.Complete || r.NALUnits[0].SEI.Complete || !seiHasCode(r, tc.code) {
				t.Fatalf("%s %+v", tc.code, r.Diagnostics)
			}
			packet := prefixPacket(tc.nal)
			old, e := ParseLengthPrefixedNALUnits(bytes.NewReader(packet), cursor.Span{BitLength: uint32(len(packet) * 8)}, 4, Options{})
			if e != nil || !old.Complete || old.NALUnits[0].SEI != nil || len(old.Diagnostics) != 0 {
				t.Fatalf("legacy inspected SEI: %+v %v", old, e)
			}
		})
	}
}

func TestSEIBudgetsAndMaximumReadChunks(t *testing.T) {
	large := seiNAL(seiMessageBytes(10, bytes.Repeat([]byte{0x55}, int(MaxSEINALBytes))))
	packet := prefixPacket(large, large, large, large, large)
	reader := &vuiReader{Reader: bytes.NewReader(packet)}
	r, e := ParseLengthPrefixedNALUnits(reader, cursor.Span{BitLength: uint32(len(packet) * 8)}, 4, Options{ParseSEI: true})
	if e != nil || !seiHasCode(r, CodeSEIByteLimit) || r.Complete {
		t.Fatal(r.Diagnostics, e)
	}
	var total uint32
	for _, n := range r.NALUnits {
		if n.SEI.BytesRead > MaxSEINALBytes {
			t.Fatal("NAL budget")
		}
		total += n.SEI.BytesRead
	}
	if total != MaxSEIBytesPerParse || r.NALUnits[4].SEI.BytesRead != 0 {
		t.Fatal("cumulative budget", total)
	}
	for _, read := range reader.reads {
		if read.Bytes > 4096 {
			t.Fatal("unbounded read", read)
		}
	}
	var messages [][]byte
	for i := 0; i < 65; i++ {
		messages = append(messages, seiMessageBytes(1, nil))
	}
	r = parseSEIPacket(t, seiNAL(messages...))
	if !seiHasCode(r, CodeSEIMessageLimit) || len(r.NALUnits[0].SEI.Messages) != 64 {
		t.Fatal("message budget")
	}
	one := seiNAL(messages[:64]...)
	r = parseSEIPacket(t, one, one, one, one, one)
	if !r.NALUnits[3].SEI.Complete || r.NALUnits[4].SEI.BytesRead != 0 || !seiHasCode(r, CodeSEIMessageLimit) {
		t.Fatal("cumulative message budget")
	}
	packet = prefixPacket(seiNAL(seiMessageBytes(5, bytes.Repeat([]byte{0}, 16))))
	r, e = ParseLengthPrefixedNALUnits(bytes.NewReader(packet), cursor.Span{BitLength: uint32(len(packet) * 8)}, 4, Options{ParseSEI: true, MaxRBSPMappingEntries: 1})
	if e != nil || r.Complete || !seiHasCode(r, CodeRBSPMappingLimitReached) || r.NALUnits[0].SEI.RBSP.ByteLength == 0 {
		t.Fatal("mapping prefix lost", r, e)
	}
}

func TestSEISourceErrorsIncludingFullReadError(t *testing.T) {
	packet := prefixPacket(seiNAL(seiMessageBytes(10, bytes.Repeat([]byte{7}, 5000))))
	for _, err := range []error{context.Canceled, errors.New("storage failure")} {
		r, e := ParseLengthPrefixedNALUnits(prefixFailureReader{data: packet, after: 4096, err: err}, cursor.Span{BitLength: uint32(len(packet) * 8)}, 4, Options{ParseSEI: true})
		if !errors.Is(e, err) || r.Complete || r.NALUnits[0].SEI.BytesRead != uint32(len(packet)-5) {
			t.Fatal("lost source error", e)
		}
	}
	_, e := ParseLengthPrefixedNALUnits(bytes.NewReader(packet[:len(packet)-3]), cursor.Span{BitLength: uint32(len(packet) * 8)}, 4, Options{ParseSEI: true})
	if !errors.Is(e, io.EOF) {
		t.Fatal("short source was treated as malformed syntax", e)
	}
	options, _ := (Options{ParseSEI: true}).Normalized()
	sentinel := errors.New("source must not receive a negative wrapped offset")
	_, read, e := readSEIRBSP(failingReaderAt{err: sentinel}, cursor.Span{ByteOffset: math.MaxInt64, BitLength: 16}, options, newParseBudget(options), newDiagnosticCollector(options.MaxDiagnostics))
	if e == nil || errors.Is(e, sentinel) || read != 0 {
		t.Fatal("SEI source offset wrapped into int64", read, e)
	}
}

func FuzzSEI(f *testing.F) {
	for _, v := range [][]byte{{6, 1, 0, 128}, {6, 6, 1, 0xd4, 128}, {6, 255}, {6, 5, 16, 0, 0, 3, 0}} {
		f.Add(v)
	}
	f.Fuzz(func(t *testing.T, nal []byte) {
		if len(nal) > 256<<10 {
			return
		}
		nal = append([]byte{6}, nal...)
		a := parseSEIPacket(t, nal)
		b := parseSEIPacket(t, nal)
		if !reflect.DeepEqual(a, b) {
			t.Fatal("non-deterministic")
		}
		s := a.NALUnits[0].SEI
		if s.BytesRead > MaxSEINALBytes || len(s.Messages) > int(MaxSEIMessagesPerNAL) || s.SyntaxBits > MaxSEINALBytes*8 {
			t.Fatal("unbounded")
		}
	})
}

func TestSEIExactByteBudgetAndEveryTruncation(t *testing.T) {
	// 65278 payload bytes + 1 type byte + 256 size bytes + one trailing byte.
	boundary := seiNAL(seiMessageBytes(10, bytes.Repeat([]byte{0x55}, 65278)))
	if len(boundary)-1 != int(MaxSEINALBytes) {
		t.Fatalf("bad boundary fixture %d", len(boundary)-1)
	}
	r := parseSEIPacket(t, boundary)
	if !r.Complete || r.NALUnits[0].SEI.BytesRead != MaxSEINALBytes {
		t.Fatalf("exact budget rejected %+v", r.Diagnostics)
	}
	// No prefix ending in 0x80 may be mistaken for the physical NAL end.
	cutoff := append(append([]byte{}, boundary...), 0x80)
	r = parseSEIPacket(t, cutoff)
	if r.Complete || r.NALUnits[0].SEI.FramingComplete || !seiHasCode(r, CodeSEIByteLimit) {
		t.Fatal("accepted budget prefix as complete NAL")
	}
	valid := seiNAL(seiMessageBytes(5, bytes.Repeat([]byte{0}, 19)), seiMessageBytes(6, []byte{0xd4}))
	for i := 1; i < len(valid); i++ {
		r = parseSEIPacket(t, valid[:i])
		if r.Complete || r.NALUnits[0].SEI.Complete {
			t.Fatalf("accepted truncated SEI %d/%d", i, len(valid))
		}
	}
}

func TestSEIRecoveryAcrossEPBAndErrorIsolation(t *testing.T) {
	var b avcBitBuilder
	b.writeUE(65535)
	b.write(0, 4)
	b.write(0, 24)
	b.write(1, 1)
	nal := seiNAL(seiMessageBytes(6, b.bytes()))
	r := parseSEIPacket(t, nal)
	f := r.NALUnits[0].SEI.Messages[0].RecoveryPoint.RecoveryFrameCnt
	if !r.Complete || f.Value != 65535 || f.Span.BitLength != 33 || len(r.NALUnits[0].SEI.Messages[0].RecoveryPoint.PayloadExtensionBits.SourceSpans) < 2 {
		t.Fatalf("mapped max recovery %+v %+v", f, r.Diagnostics)
	}
	packet := prefixPacket(nal)
	var code uint64
	for _, s := range f.Span.SourceSpans {
		for i := uint32(0); i < s.BitLength; i++ {
			bit := uint64(s.BitOffset) + uint64(i)
			code = code<<1 | uint64((packet[s.ByteOffset+bit/8]>>(7-bit%8))&1)
		}
	}
	if code != 65536 {
		t.Fatalf("source UE value %d", code)
	}
	r = parseSEIPacket(t, seiNAL(seiMessageBytes(6, []byte{0xdc}), seiMessageBytes(6, []byte{0xd4})))
	s := r.NALUnits[0].SEI
	if r.Complete || !s.FramingComplete || s.Messages[0].Complete || !s.Messages[1].Complete {
		t.Fatal("known syntax error poisoned following message framing")
	}
}
