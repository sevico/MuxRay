package avccontext

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/sevico/muxray/internal/avc"
	"github.com/sevico/muxray/internal/flv"
)

type parameterBits string

func (b *parameterBits) bits(v uint32, n uint) {
	for ; n > 0; n-- {
		if v&(1<<(n-1)) != 0 {
			*b += "1"
		} else {
			*b += "0"
		}
	}
}
func (b *parameterBits) ue(v uint32) {
	v++
	n := uint(0)
	for w := v; w > 1; w >>= 1 {
		n++
	}
	*b += parameterBits(strings.Repeat("0", int(n)))
	b.bits(v, n+1)
}
func (b parameterBits) nal(header byte) []byte {
	raw := []byte{header}
	b += parameterBits(strings.Repeat("0", (8-len(b)%8)%8))
	for i := 0; i < len(b); i += 8 {
		var v byte
		for j := 0; j < 8; j++ {
			v = v<<1 | byte(b[i+j]-'0')
		}
		if len(raw) >= 3 && raw[len(raw)-1] == 0 && raw[len(raw)-2] == 0 && v <= 3 {
			raw = append(raw, 3)
		}
		raw = append(raw, v)
	}
	return raw
}
func parameterSPS(id uint32, invalid bool) []byte {
	var b parameterBits
	b.bits(66, 8)
	b.bits(0, 8)
	b.bits(30, 8)
	b.ue(id)
	if invalid {
		b.ue(13)
	} else {
		b.ue(0)
	}
	b.ue(0)
	b.ue(0)
	b.ue(1)
	b.bits(0, 1)
	b.ue(19)
	b.ue(14)
	b.bits(1, 1)
	b.bits(1, 1)
	b.bits(0, 1)
	b.bits(0, 1)
	b.bits(1, 1)
	return b.nal(0x67)
}
func parameterPPS(id, sps uint32, invalid bool) []byte {
	var b parameterBits
	b.ue(id)
	b.ue(sps)
	b.bits(0, 1)
	b.bits(0, 1)
	if invalid {
		b.ue(1)
	} else {
		b.ue(0)
	}
	b.ue(0)
	b.ue(0)
	b.bits(0, 1)
	b.bits(0, 2)
	b.ue(0)
	b.ue(0)
	b.ue(0)
	b.bits(1, 1)
	b.bits(0, 1)
	b.bits(0, 1)
	b.bits(1, 1)
	return b.nal(0x68)
}
func parameterConfig(width byte, sps, pps [][]byte) packet {
	p := []byte{0x17, 0, 0, 0, 0, 1, 66, 0, 30, 0xfc | (width - 1), 0xe0 | byte(len(sps))}
	for _, n := range sps {
		p = append(p, byte(len(n)>>8), byte(len(n)))
		p = append(p, n...)
	}
	p = append(p, byte(len(pps)))
	for _, n := range pps {
		p = append(p, byte(len(n)>>8), byte(len(n)))
		p = append(p, n...)
	}
	return packet{9, p}
}
func parameterNAL(width byte, nals ...[]byte) packet {
	p := []byte{0x27, 1, 0, 0, 0}
	for _, n := range nals {
		var size [4]byte
		binary.BigEndian.PutUint32(size[:], uint32(len(n)))
		p = append(p, size[4-width:]...)
		p = append(p, n...)
	}
	return packet{9, p}
}
func baseParameterConfig() packet {
	return parameterConfig(4, [][]byte{parameterSPS(0, false)}, [][]byte{parameterPPS(0, 0, false)})
}
func parameterFraming() Resolution {
	return Resolution{State: StateFound, SourceTagID: 0, NALLengthSize: 4}
}

func TestParametersReplayUpdatesAndReplacementInSourceOrder(t *testing.T) {
	for _, tc := range []struct {
		name            string
		packets         []packet
		sps, pps, pairs uint32
	}{
		{"audio and script do not interrupt", []packet{{8, []byte{0x2f}}, {18, []byte{5}}}, 1, 1, 1},
		{"in-band SPS and PPS", []packet{parameterNAL(4, parameterSPS(1, false), parameterPPS(0, 1, false))}, 2, 1, 1},
		{"last update wins", []packet{parameterNAL(4, parameterPPS(0, 1, false), parameterPPS(0, 0, false))}, 1, 1, 1},
		{"missing referenced SPS stays missing", []packet{parameterNAL(4, parameterPPS(0, 0, false), parameterPPS(0, 1, false))}, 1, 1, 0},
		{"configuration discards previous SPS", []packet{parameterConfig(4, [][]byte{parameterSPS(1, false)}, [][]byte{parameterPPS(0, 0, false)})}, 1, 1, 0},
		{"known bad PPS discards that slot", []packet{parameterNAL(4, parameterPPS(1, 0, false), parameterPPS(0, 0, true))}, 1, 1, 1},
		{"unreadable PPS clears all PPS", []packet{parameterNAL(4, parameterPPS(1, 0, false), []byte{0x68})}, 1, 0, 0},
		{"known bad SPS discards that slot", []packet{parameterNAL(4, parameterSPS(1, false), parameterSPS(0, true))}, 1, 1, 0},
		{"known truncated SPS discards that slot", []packet{parameterNAL(4, parameterSPS(1, false), parameterSPS(0, false)[:5])}, 1, 1, 0},
		{"unreadable SPS clears all SPS", []packet{parameterNAL(4, parameterSPS(1, false), []byte{0x67})}, 0, 1, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			packets := append([]packet{baseParameterConfig()}, tc.packets...)
			packets = append(packets, nalu())
			data, index := fixture(t, packets...)
			got, err := ResolveParameters(t.Context(), bytes.NewReader(data), selected(index), lookup(index), parameterFraming(), flv.AVCVideoOptions{}, ParameterLimits{})
			if err != nil || got.State != StateFound || got.Context == nil || got.SPSCount != tc.sps || got.PPSCount != tc.pps || got.Context.UsablePPSCount() != tc.pairs {
				t.Fatalf("got=%+v err=%v", got, err)
			}
			if got.StartTagID != 0 || got.NextTagID != index.Len()-1 || got.SelectedTagID != index.Len()-1 || got.ScannedTags != index.Len()-1 {
				t.Fatalf("coverage=%+v", got)
			}
		})
	}
}

func TestParametersBarriersNeverActivateOlderConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name string
		p    packet
	}{
		{"broken newer configuration", packet{9, []byte{0x17, 0, 0, 0, 0, 1, 66}}},
		{"EOS", packet{9, []byte{0x17, 2, 0, 0, 0}}},
		{"codec change", packet{9, []byte{0x12, 1, 0, 0, 0}}},
		{"filtered video", packet{0x29, []byte{0x27, 1, 0, 0, 0}}},
		{"short header", packet{9, []byte{0x27}}},
		{"short composition time", packet{9, []byte{0x27, 1}}},
		{"unknown packet", packet{9, []byte{0x27, 9, 0, 0, 0}}},
		{"truncated NAL", packet{9, []byte{0x27, 1, 0, 0, 0, 0, 0, 0, 2, 0x67}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data, index := fixture(t, baseParameterConfig(), tc.p, nalu())
			framing, err := Resolve(t.Context(), bytes.NewReader(data), selected(index), lookup(index), flv.AVCVideoOptions{}, Limits{})
			if err != nil || framing.State != StateFound {
				t.Fatalf("framing=%+v err=%v", framing, err)
			}
			got, err := ResolveParameters(t.Context(), bytes.NewReader(data), selected(index), lookup(index), framing, flv.AVCVideoOptions{}, ParameterLimits{})
			if err != nil || got.State != StateInvalidated || got.Context != nil || got.BarrierCount != 1 || got.NextTagID != 2 {
				t.Fatalf("got=%+v err=%v", got, err)
			}
			// A subsequent complete configuration can establish a fresh generation.
			data, index = fixture(t, baseParameterConfig(), tc.p, baseParameterConfig(), nalu())
			got, err = ResolveParameters(t.Context(), bytes.NewReader(data), selected(index), lookup(index), parameterFraming(), flv.AVCVideoOptions{}, ParameterLimits{})
			if err != nil || got.State != StateFound || got.Context == nil || got.Context.UsablePPSCount() != 1 {
				t.Fatalf("recovery=%+v err=%v", got, err)
			}
		})
	}
}

func TestParametersWidthAgreementAndSelectedExclusion(t *testing.T) {
	data, index := fixture(t, baseParameterConfig(), parameterConfig(1, [][]byte{parameterSPS(1, false)}, nil), parameterNAL(1, parameterPPS(0, 1, false)), parameterNAL(1, []byte{0x68}))
	got, err := ResolveParameters(t.Context(), bytes.NewReader(data), selected(index), lookup(index), parameterFraming(), flv.AVCVideoOptions{}, ParameterLimits{})
	if err != nil || got.State != StateInvalidated || got.Context != nil {
		t.Fatalf("stale width=%+v err=%v", got, err)
	}
	framing := parameterFraming()
	framing.NALLengthSize = 1
	got, err = ResolveParameters(t.Context(), bytes.NewReader(data), selected(index), lookup(index), framing, flv.AVCVideoOptions{}, ParameterLimits{})
	if err != nil || got.State != StateFound || got.SPSCount != 1 || got.PPSCount != 1 || got.Context.UsablePPSCount() != 1 {
		t.Fatalf("width replay=%+v err=%v", got, err)
	}
	// A NAL-level EOS also clears prior slots, in source order within its packet.
	for _, nals := range [][][]byte{{{10}}, {{11}, parameterSPS(1, false), parameterPPS(1, 1, false)}} {
		data, index = fixture(t, baseParameterConfig(), parameterNAL(4, nals...), nalu())
		got, err = ResolveParameters(t.Context(), bytes.NewReader(data), selected(index), lookup(index), parameterFraming(), flv.AVCVideoOptions{}, ParameterLimits{})
		if err != nil || len(nals) == 1 && (got.Context != nil || got.State != StateInvalidated) || len(nals) > 1 && (got.Context == nil || got.Context.UsablePPSCount() != 1) {
			t.Fatalf("NAL EOS=%+v err=%v", got, err)
		}
	}
}

func TestParametersBudgetsDoNotPublishPartialContext(t *testing.T) {
	data, index := fixture(t, baseParameterConfig(), packet{18, []byte{5}}, nalu())
	full, err := ResolveParameters(t.Context(), bytes.NewReader(data), selected(index), lookup(index), parameterFraming(), flv.AVCVideoOptions{}, ParameterLimits{})
	if err != nil || full.State != StateFound {
		t.Fatalf("baseline=%+v %v", full, err)
	}
	for _, limits := range []ParameterLimits{{MaxTags: 1}, {MaxBytes: 1}, {MaxBytes: full.BytesRead - 1}} {
		got, err := ResolveParameters(t.Context(), bytes.NewReader(data), selected(index), lookup(index), parameterFraming(), flv.AVCVideoOptions{}, limits)
		if err != nil || got.State != StateBudgetExhausted || got.Context != nil || got.SPSCount != 0 || got.PPSCount != 0 || limits.MaxBytes != 0 && got.BytesRead > limits.MaxBytes {
			t.Fatalf("limits=%+v got=%+v err=%v", limits, got, err)
		}
	}
	got, err := ResolveParameters(t.Context(), bytes.NewReader(data), selected(index), lookup(index), parameterFraming(), flv.AVCVideoOptions{}, ParameterLimits{MaxTags: full.ScannedTags, MaxBytes: full.BytesRead})
	if err != nil || got.State != StateFound {
		t.Fatalf("exact budget=%+v %v", got, err)
	}
}

func TestParametersReadOnlyHeadersAndParameterSets(t *testing.T) {
	slice := append([]byte{0x65}, make([]byte, 1<<20)...)
	data, index := fixture(t, baseParameterConfig(), parameterNAL(4, slice), nalu())
	tag, _ := index.Get(1)
	start := int64(tag.PayloadSpan().ByteOffset) + 5 + 4 + 1
	end := int64(tag.PayloadSpan().ByteOffset) + int64(tag.DataSize)
	var reads uint64
	source := readerFunc(func(p []byte, off int64) (int, error) {
		if off < end && off+int64(len(p)) > start {
			t.Fatalf("slice body read off=%d size=%d", off, len(p))
		}
		n, err := bytes.NewReader(data).ReadAt(p, off)
		reads += uint64(n)
		return n, err
	})
	got, err := ResolveParameters(t.Context(), source, selected(index), lookup(index), parameterFraming(), flv.AVCVideoOptions{AVC: avc.Options{ParseSliceHeaders: true, ParseVUI: true}}, ParameterLimits{})
	if err != nil || got.State != StateFound || got.BytesRead != reads || reads > 256 || got.NALPackets != 1 {
		t.Fatalf("got=%+v reads=%d err=%v", got, reads, err)
	}
}

func TestParametersReadErrorsAndCancellation(t *testing.T) {
	data, index := fixture(t, baseParameterConfig(), nalu())
	sourceError := errors.New("source unavailable")
	for _, tc := range []struct {
		name   string
		source io.ReaderAt
		want   error
	}{
		{"error", readerFunc(func([]byte, int64) (int, error) { return 0, sourceError }), sourceError},
		{"full non EOF error", readerFunc(func(p []byte, off int64) (int, error) {
			n, _ := bytes.NewReader(data).ReadAt(p, off)
			return n, sourceError
		}), sourceError},
		{"short nil", readerFunc(func(p []byte, _ int64) (int, error) { return len(p) - 1, nil }), io.ErrUnexpectedEOF},
		{"invalid count", readerFunc(func(p []byte, _ int64) (int, error) { return len(p) + 1, nil }), ErrInvalidInput},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ResolveParameters(t.Context(), tc.source, selected(index), lookup(index), parameterFraming(), flv.AVCVideoOptions{}, ParameterLimits{})
			if !errors.Is(err, tc.want) || got.Context != nil {
				t.Fatalf("got=%+v err=%v", got, err)
			}
		})
	}
	ctx, cancel := context.WithCancel(t.Context())
	calls := 0
	source := readerFunc(func(p []byte, off int64) (int, error) {
		calls++
		n, err := bytes.NewReader(data).ReadAt(p, off)
		cancel()
		return n, err
	})
	got, err := ResolveParameters(ctx, source, selected(index), lookup(index), parameterFraming(), flv.AVCVideoOptions{}, ParameterLimits{})
	if !errors.Is(err, context.Canceled) || got.Context != nil || calls != 1 || got.BytesRead != 2 {
		t.Fatalf("cancel=%+v calls=%d err=%v", got, calls, err)
	}
	source = readerFunc(func(p []byte, off int64) (int, error) {
		n, err := bytes.NewReader(data).ReadAt(p, off)
		if n == len(p) {
			err = io.EOF
		}
		return n, err
	})
	got, err = ResolveParameters(t.Context(), source, selected(index), lookup(index), parameterFraming(), flv.AVCVideoOptions{}, ParameterLimits{})
	if err != nil || got.State != StateFound {
		t.Fatalf("full EOF=%+v err=%v", got, err)
	}
}

func TestParametersMissingInvalidSelectionAndIncompleteVideo(t *testing.T) {
	data, index := fixture(t, baseParameterConfig(), nalu())
	for _, framing := range []Resolution{{State: StateMissing}, {State: StateBudgetExhausted}} {
		got, err := ResolveParameters(t.Context(), bytes.NewReader(data), selected(index), nil, framing, flv.AVCVideoOptions{}, ParameterLimits{})
		if err != nil || got.State != framing.State || got.Context != nil || got.ScannedTags != 0 {
			t.Fatalf("missing=%+v err=%v", got, err)
		}
	}
	for _, get := range []Lookup{nil, func(uint64) (flv.TagRecord, error) { return flv.TagRecord{}, io.ErrClosedPipe }, func(id uint64) (flv.TagRecord, error) { tag, _ := index.Get(id); tag.ID++; return tag, nil }} {
		got, err := ResolveParameters(t.Context(), bytes.NewReader(data), selected(index), get, parameterFraming(), flv.AVCVideoOptions{}, ParameterLimits{})
		if err == nil || got.Context != nil {
			t.Fatalf("bad lookup=%+v err=%v", got, err)
		}
	}
	first, _ := index.Get(0)
	got, err := ResolveParameters(t.Context(), bytes.NewReader(data), first, nil, parameterFraming(), flv.AVCVideoOptions{}, ParameterLimits{})
	if err != nil || got.State != StateNotNeeded {
		t.Fatalf("not needed=%+v err=%v", got, err)
	}
	data, index = fixture(t, baseParameterConfig(), nalu(), nalu())
	get := lookup(index)
	got, err = ResolveParameters(t.Context(), bytes.NewReader(data), selected(index), func(id uint64) (flv.TagRecord, error) {
		tag, err := get(id)
		if id == 1 {
			// A scanner-produced incomplete record has no PayloadComplete flag.
			tag = flv.TagRecord{ID: tag.ID, Offset: tag.Offset, DataSize: tag.DataSize, AvailableDataSize: tag.DataSize - 1, RawTypeFlags: 9}
		}
		return tag, err
	}, parameterFraming(), flv.AVCVideoOptions{}, ParameterLimits{})
	if err != nil || got.State != StateInvalidated || got.Context != nil || got.BarrierCount != 1 {
		t.Fatalf("incomplete=%+v err=%v", got, err)
	}
}

func TestParametersDefaultBudgetAndCancellationAfterConfiguration(t *testing.T) {
	packets := []packet{baseParameterConfig()}
	for i := uint64(0); i < DefaultParameterMaxTags; i++ {
		packets = append(packets, packet{18, []byte{5}})
	}
	packets = append(packets, nalu())
	data, index := fixture(t, packets...)
	got, err := ResolveParameters(t.Context(), bytes.NewReader(data), selected(index), lookup(index), parameterFraming(), flv.AVCVideoOptions{}, ParameterLimits{})
	if err != nil || got.State != StateBudgetExhausted || got.Context != nil || got.ScannedTags != DefaultParameterMaxTags || got.NextTagID != DefaultParameterMaxTags {
		t.Fatalf("default budget=%+v err=%v", got, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	get := lookup(index)
	got, err = ResolveParameters(ctx, bytes.NewReader(data), selected(index), func(id uint64) (flv.TagRecord, error) {
		if id == 1 {
			cancel()
		}
		return get(id)
	}, parameterFraming(), flv.AVCVideoOptions{}, ParameterLimits{})
	if !errors.Is(err, context.Canceled) || got.Configurations != 1 || got.Context != nil || got.SPSCount != 0 || got.PPSCount != 0 {
		t.Fatalf("cancel after config=%+v err=%v", got, err)
	}
}

func TestParametersDefaultByteBudgetCountsActualSourceReads(t *testing.T) {
	// Each individual parameter set fits its parser budget; their aggregate
	// source reads exceed the independent replay budget well before 8192 tags.
	largeSPS := append(parameterSPS(0, false), bytes.Repeat([]byte{0xff}, 60000)...)
	packets := []packet{baseParameterConfig()}
	for range 18 {
		packets = append(packets, parameterNAL(4, largeSPS))
	}
	packets = append(packets, nalu())
	data, index := fixture(t, packets...)
	var reads uint64
	source := readerFunc(func(p []byte, off int64) (int, error) {
		n, err := bytes.NewReader(data).ReadAt(p, off)
		reads += uint64(n)
		return n, err
	})
	got, err := ResolveParameters(t.Context(), source, selected(index), lookup(index), parameterFraming(), flv.AVCVideoOptions{}, ParameterLimits{})
	if err != nil || got.State != StateBudgetExhausted || got.Context != nil || got.BytesRead != reads || reads > DefaultParameterMaxBytes || reads < DefaultParameterMaxBytes-65536 {
		t.Fatalf("default bytes=%+v read=%d err=%v", got, reads, err)
	}
}
