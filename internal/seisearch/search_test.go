package seisearch

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/sevico/muxray/internal/avc"
	"github.com/sevico/muxray/internal/cursor"
	"github.com/sevico/muxray/internal/flv"
)

type packet struct {
	kind byte
	data []byte
}

func fixture(t *testing.T, packets ...packet) ([]byte, Lookup, uint64) {
	t.Helper()
	data := []byte{'F', 'L', 'V', 1, 1, 0, 0, 0, 9, 0, 0, 0, 0}
	for _, p := range packets {
		header := make([]byte, 11)
		header[0] = p.kind
		header[1], header[2], header[3] = byte(len(p.data)>>16), byte(len(p.data)>>8), byte(len(p.data))
		data = append(data, header...)
		data = append(data, p.data...)
		var size [4]byte
		binary.BigEndian.PutUint32(size[:], uint32(len(p.data)+11))
		data = append(data, size[:]...)
	}
	scan, err := flv.Scan(bytes.NewReader(data), uint64(len(data)), flv.Options{})
	if err != nil || !scan.Complete {
		t.Fatalf("fixture scan: %+v %v", scan.Diagnostics, err)
	}
	lookup := func(id uint64) (flv.TagRecord, error) {
		if id >= uint64(len(scan.Tags)) {
			return flv.TagRecord{}, ErrInvalidInput
		}
		return scan.Tags[id], nil
	}
	return data, lookup, uint64(len(scan.Tags))
}

func config(width byte, sets ...[]byte) packet {
	p := []byte{0x17, 0, 0, 0, 0, 1, 66, 0, 30, 0xfc | (width - 1), 0xe0 | byte(len(sets))}
	for _, set := range sets {
		p = append(p, byte(len(set)>>8), byte(len(set)))
		p = append(p, set...)
	}
	return packet{9, append(p, 0)}
}

func nalu(width byte, units ...[]byte) packet {
	p := []byte{0x27, 1, 0, 0, 0}
	for _, unit := range units {
		var size [4]byte
		binary.BigEndian.PutUint32(size[:], uint32(len(unit)))
		p = append(p, size[4-width:]...)
		p = append(p, unit...)
	}
	return packet{9, p}
}

func messages(count int) []byte {
	p := []byte{6}
	for range count {
		p = append(p, 200, 1, 0x55)
	}
	return append(p, 0x80)
}

func firstPage(t *testing.T, filter string, packets ...packet) *Page {
	t.Helper()
	data, lookup, total := fixture(t, packets...)
	p, err := Search(t.Context(), bytes.NewReader(data), lookup, State{}, total, true, filter)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestMessagePaginationDoesNotRereadOrLoseRows(t *testing.T) {
	data, lookup, total := fixture(t, config(4), nalu(4, messages(64), messages(64), messages(64), messages(64)))
	var state State
	seen := map[[2]uint32]bool{}
	for page := 0; page < 4; page++ {
		source := io.ReaderAt(bytes.NewReader(data))
		if page != 0 {
			source = failReader{err: errors.New("pending rows reread source")}
		}
		p, err := Search(t.Context(), source, lookup, state, total, false, "all")
		if err != nil || len(p.Rows) != 64 || p.ScanComplete || p.Pending != uint32((3-page)*64) || p.BytesRead > MaxBytes {
			t.Fatalf("page %d: %+v %v", page, p, err)
		}
		if page != 0 && (p.BytesRead != 0 || p.Scanned != 0) {
			t.Fatal("pending rows consume scan work")
		}
		if p.Exhausted != (page == 3) || page != 3 && p.Next != 1 {
			t.Fatalf("wrong continuation %+v", p)
		}
		for _, row := range p.Rows {
			key := [2]uint32{*row.NALIndex, *row.MessageIndex}
			if row.TagID != 1 || seen[key] || row.Kind != "unknown" || row.Status != "opaque" || !row.Complete || row.PayloadType == nil || *row.PayloadType != 200 {
				t.Fatalf("duplicate or incomplete row %+v", row)
			}
			seen[key] = true
		}
		state = p.State
	}
	if len(seen) != 256 || state.Position() != total {
		t.Fatalf("lost rows %d", len(seen))
	}
}

func TestPerNALAndPerTagMessageLimitsStayVisible(t *testing.T) {
	for _, units := range [][][]byte{{messages(65)}, {messages(64), messages(64), messages(64), messages(64), messages(1)}} {
		data, lookup, total := fixture(t, config(4), nalu(4, units...))
		var state State
		var rows []Row
		for pages := 0; pages < 6; pages++ {
			p, err := Search(t.Context(), bytes.NewReader(data), lookup, state, total, true, "all")
			if err != nil || len(p.Rows) > MaxResults || int(p.Pending) > MaxPending || p.BytesRead > MaxBytes {
				t.Fatalf("page %+v %v", p, err)
			}
			rows = append(rows, p.Rows...)
			state = p.State
			if p.Exhausted {
				break
			}
		}
		last := rows[len(rows)-1]
		if last.Kind != "coverage" || last.Complete || last.Reason != "sei_message_limit" || last.NALIndex != nil || last.MessageIndex != nil {
			t.Fatalf("missing coverage record %+v", last)
		}
		if len(rows) != min(64*len(units), 256)+1 {
			t.Fatalf("lost message rows: %d", len(rows))
		}
	}
	for _, filter := range []string{"unknown", "bufferingPeriod", "pictureTiming"} {
		p := firstPage(t, filter, config(4), nalu(4, messages(65)))
		if len(p.Rows) != 1 || p.Rows[0].Kind != "coverage" || !p.Exhausted {
			t.Fatalf("filter hid coverage gap: %s %+v", filter, p)
		}
	}
}

type bitWriter string

func (b *bitWriter) bits(v uint32, n uint) {
	for ; n > 0; n-- {
		*b += bitWriter(string('0' + rune((v>>(n-1))&1)))
	}
}
func (b *bitWriter) ue(v uint32) {
	v++
	n := uint(0)
	for x := v; x > 1; x >>= 1 {
		n++
	}
	*b += bitWriter(strings.Repeat("0", int(n)))
	b.bits(v, n+1)
}
func (b bitWriter) nal(header byte) []byte {
	p := []byte{header}
	b += bitWriter(strings.Repeat("0", (8-len(b)%8)%8))
	for i := 0; i < len(b); i += 8 {
		var v byte
		for j := 0; j < 8; j++ {
			v = v<<1 | byte(b[i+j]-'0')
		}
		if len(p) >= 3 && p[len(p)-1] == 0 && p[len(p)-2] == 0 && v <= 3 {
			p = append(p, 3)
		}
		p = append(p, v)
	}
	return p
}
func timingSPS() []byte {
	var b bitWriter
	b.bits(66, 8)
	b.bits(0, 8)
	b.bits(30, 8)
	for range 4 {
		b.ue(0)
	}
	b.ue(1)
	b.bits(0, 1)
	b.ue(19)
	b.ue(14)
	b.bits(1, 1)
	b.bits(1, 1)
	b.bits(0, 1)
	b.bits(1, 1)
	b.bits(0, 5)
	b.bits(1, 1)
	b.ue(0)
	b.bits(0, 8)
	b.ue(0)
	b.ue(0)
	b.bits(1, 1)
	b.bits(7, 5)
	b.bits(7, 5)
	b.bits(7, 5)
	b.bits(0, 5)
	b.bits(0, 4)
	b.bits(1, 1)
	return b.nal(0x67)
}
func bp() []byte  { return []byte{6, 0, 3, 0x8a, 0x03, 0xc0, 0x80} }
func pic() []byte { return []byte{6, 1, 2, 12, 3, 0x80} }

func TestTimingContextSurvivesPagesAndKeepsSourceEvidence(t *testing.T) {
	packets := []packet{config(4, timingSPS()), nalu(4, bp())}
	for range 254 {
		packets = append(packets, packet{8, []byte{0x2f}})
	}
	packets = append(packets, nalu(4, pic()))
	data, lookup, total := fixture(t, packets...)
	p, err := Search(t.Context(), bytes.NewReader(data), lookup, State{}, total, true, "pictureTiming")
	if err != nil || p.Scanned != 256 || p.Next != 256 || len(p.Rows) != 0 || p.Exhausted {
		t.Fatalf("first page %+v %v", p, err)
	}
	last, err := Search(t.Context(), bytes.NewReader(data), lookup, p.State, total, true, "pictureTiming")
	if err != nil || !last.Exhausted || len(last.Rows) != 1 {
		t.Fatalf("continuation %+v %v", last, err)
	}
	row := last.Rows[0]
	if !row.Complete || row.ContextState != "resolved" || row.ContextSource != "bufferingPeriod" || row.SPSID == nil || *row.SPSID != 0 || row.SPSSpan == nil || row.EvidenceSpan == nil || row.SourceSpan == nil || row.EvidenceSpan.ByteOffset >= row.SourceSpan.ByteOffset || row.SPSSpan.ByteOffset >= row.EvidenceSpan.ByteOffset {
		t.Fatalf("timing source lost %+v", row)
	}
	if p.State.Position() != 256 {
		t.Fatal("input state mutated")
	}
}

func TestTimingUnknownAndInvalidatingBarriers(t *testing.T) {
	for _, barrier := range []packet{
		config(4, timingSPS()), nalu(4, timingSPS()), nalu(4, []byte{0x62}), nalu(4, []byte{0x65}),
		{9, []byte{0x17, 2, 0, 0, 0}}, {9, []byte{0x12, 1, 0, 0, 0}}, {0x29, []byte{0x27, 1, 0, 0, 0}},
		{9, []byte{0x27, 9, 0, 0, 0}}, {9, []byte{0x27}}, {9, []byte{0x27, 1, 0, 0, 0, 0, 0, 0, 2, 0x67}},
		nalu(4, messages(65)),
	} {
		p := firstPage(t, "unknown", config(4, timingSPS()), nalu(4, bp()), barrier, nalu(4, pic()))
		if len(p.Rows) == 0 || !p.Exhausted {
			t.Fatalf("barrier %x: %+v", barrier.data, p)
		}
		row := p.Rows[len(p.Rows)-1]
		if row.TagID != 3 || row.Complete || row.Kind != "coverage" && row.ContextState != "missing" {
			t.Fatalf("stale timing after %x: %+v", barrier.data, row)
		}
	}
	missing := firstPage(t, "unknown", config(4, timingSPS()), nalu(4, pic()))
	if len(missing.Rows) != 1 || missing.Rows[0].Reason != "timing_context_missing" {
		t.Fatalf("single SPS was guessed: %+v", missing)
	}
	knownOpaque := firstPage(t, "unknown", config(4), nalu(4, messages(1)))
	if len(knownOpaque.Rows) != 0 {
		t.Fatal("opaque supported framing was called incomplete")
	}
}

func largeSEI() []byte {
	p := []byte{6, 200}
	for left := 60000; ; left -= 255 {
		if left < 255 {
			p = append(p, byte(left))
			break
		}
		p = append(p, 255)
	}
	p = append(p, bytes.Repeat([]byte{0x55}, 60000)...)
	return append(p, 0x80)
}

func TestByteBudgetRollsBackTagThenMakesProgress(t *testing.T) {
	large := largeSEI()
	packets := []packet{config(4)}
	for range 5 {
		packets = append(packets, nalu(4, large, large, large, large))
	}
	data, lookup, total := fixture(t, packets...)
	p, err := Search(t.Context(), bytes.NewReader(data), lookup, State{}, total, true, "all")
	if err != nil || p.BytesRead > MaxBytes || p.Scanned != 6 || p.Next != 5 || len(p.Rows) != 16 || p.Exhausted {
		t.Fatalf("partial budget %+v %v", p, err)
	}
	q, err := Search(t.Context(), bytes.NewReader(data), lookup, p.State, total, true, "all")
	if err != nil || !q.Exhausted || q.Scanned != 1 || len(q.Rows) != 4 || q.BytesRead > MaxBytes {
		t.Fatalf("retry %+v %v", q, err)
	}
	for _, row := range q.Rows {
		if row.TagID != 5 || !row.Complete {
			t.Fatalf("retry lost rows %+v", row)
		}
	}
	// Each parameter NAL consumes its bounded 64 KiB source prefix. This Tag
	// alone exceeds a full batch; one retry must produce a gap and advance.
	hugeSPS := append([]byte{0x67}, bytes.Repeat([]byte{0x55}, 65534)...)
	var units [][]byte
	for range 17 {
		units = append(units, hugeSPS)
	}
	data, lookup, total = fixture(t, config(4), nalu(4, units...), nalu(4, pic()))
	p, err = Search(t.Context(), bytes.NewReader(data), lookup, State{}, total, true, "all")
	if err != nil || p.Next != 1 || p.BytesRead > MaxBytes {
		t.Fatalf("oversized first %+v %v", p, err)
	}
	q, err = Search(t.Context(), bytes.NewReader(data), lookup, p.State, total, true, "all")
	if err != nil || q.Next <= p.Next || len(q.Rows) == 0 || q.Rows[0].Reason != "byte_limit" || q.BytesRead > MaxBytes {
		t.Fatalf("oversized retry %+v %v", q, err)
	}
}

type failReader struct{ err error }

func (r failReader) ReadAt([]byte, int64) (int, error) { return 0, r.err }

type callbackReader struct {
	source io.ReaderAt
	after  func()
	err    error
}

type nthFailureReader struct {
	source io.ReaderAt
	calls  int
	at     int
	err    error
}

func (r *nthFailureReader) ReadAt(p []byte, offset int64) (int, error) {
	r.calls++
	n, err := r.source.ReadAt(p, offset)
	if r.calls == r.at {
		return n, r.err
	}
	return n, err
}

func (r callbackReader) ReadAt(p []byte, offset int64) (int, error) {
	n, err := r.source.ReadAt(p, offset)
	if r.after != nil {
		r.after()
	}
	if r.err != nil {
		return n, r.err
	}
	return n, err
}

func TestCancelIOAndFilterMismatchNeverPublishState(t *testing.T) {
	data, lookup, total := fixture(t, config(4), nalu(4, messages(1)))
	ctx, cancel := context.WithCancel(t.Context())
	boom := errors.New("source failed")
	probe := &nthFailureReader{source: bytes.NewReader(data)}
	if _, err := Search(t.Context(), probe, lookup, State{}, total, true, "all"); err != nil {
		t.Fatal(err)
	}
	for at := 1; at <= probe.calls; at++ {
		source := &nthFailureReader{source: bytes.NewReader(data), at: at, err: boom}
		if p, err := Search(t.Context(), source, lookup, State{}, total, true, "all"); !errors.Is(err, boom) || p != nil {
			t.Fatalf("full-read error at call %d swallowed: %v", at, err)
		}
	}
	for _, source := range []io.ReaderAt{failReader{boom}, callbackReader{source: bytes.NewReader(data), err: boom}, callbackReader{source: bytes.NewReader(data), after: cancel}} {
		p, err := Search(ctx, source, lookup, State{}, total, true, "all")
		if err == nil || p != nil {
			t.Fatalf("error published a continuation: %+v %v", p, err)
		}
	}
	p, err := Search(t.Context(), callbackReader{source: bytes.NewReader(data), err: io.EOF}, lookup, State{}, total, true, "all")
	if err != nil || !p.Exhausted || len(p.Rows) != 1 {
		t.Fatalf("full bytes with EOF %+v %v", p, err)
	}
	if changed, err := Search(t.Context(), bytes.NewReader(data), lookup, p.State, total, true, "unknown"); err == nil || changed != nil {
		t.Fatal("filter changed under an existing continuation")
	}
	if canceled, err := Search(ctx, bytes.NewReader(data), lookup, p.State, total, true, "all"); !errors.Is(err, context.Canceled) || canceled != nil {
		t.Fatal("terminal cancellation swallowed")
	}
}

func TestCloneIsolationAndPhysicalEnvelopeAcrossEPB(t *testing.T) {
	sei := []byte{6, 200, 3, 0, 0, 3, 1, 0x80}
	p := firstPage(t, "all", config(4), nalu(4, sei))
	if len(p.Rows) != 1 || p.Rows[0].SourceSpan == nil || p.Rows[0].SourceSpan.BitLength != 6*8 {
		t.Fatalf("incorrect physical envelope %+v", p)
	}
	p = firstPage(t, "all", config(4, timingSPS()), nalu(4, bp(), messages(64)))
	if p.Pending != 1 {
		t.Fatalf("missing pending row %+v", p)
	}
	before := p.State.Clone()
	copy := p.State.Clone()
	*copy.pending[0].NALIndex = 999
	copy.pending[0].SourceSpan.ByteOffset = 999
	copy.sets.AddNALUnit(avc.NALUnit{})
	if !reflect.DeepEqual(p.State, before) {
		t.Fatal("clone mutated committed state")
	}
	// Returned rows also own their optional fields independently of pending.
	data, lookup, total := fixture(t, config(4), nalu(4, messages(64), messages(64)))
	q, err := Search(t.Context(), bytes.NewReader(data), lookup, State{}, total, true, "all")
	if err != nil {
		t.Fatal(err)
	}
	q.Rows[0].SourceSpan.ByteOffset = 999
	if q.State.pending[0].SourceSpan.ByteOffset == 999 {
		t.Fatal("page rows alias pending state")
	}
}

func TestInvalidInputsAndEmptyScan(t *testing.T) {
	data, lookup, total := fixture(t, config(4))
	badLookup := func(id uint64) (flv.TagRecord, error) { tag, err := lookup(id); tag.ID++; return tag, err }
	for _, input := range []struct {
		state  State
		total  uint64
		filter string
		lookup Lookup
	}{
		{State{}, total, "bogus", lookup}, {State{next: total + 1}, total, "all", lookup}, {State{}, total, "all", badLookup},
	} {
		p, err := Search(t.Context(), bytes.NewReader(data), input.lookup, input.state, input.total, true, input.filter)
		if err == nil || p != nil {
			t.Fatal("invalid input accepted")
		}
	}
	p, err := Search(nil, bytes.NewReader(nil), lookup, State{}, 0, false, "all")
	if err != nil || !p.Exhausted || p.ScanComplete || p.Scanned != 0 || p.BytesRead != 0 {
		t.Fatalf("empty scan %+v %v", p, err)
	}
}

func TestMappedSpanEnvelopeKeepsBitOffsets(t *testing.T) {
	var span *cursor.Span
	addMappedSpan(&span, avc.MappedSpan{SourceSpans: []cursor.Span{{ByteOffset: 30, BitOffset: 3, BitLength: 3}, {ByteOffset: 32, BitOffset: 1, BitLength: 4}}})
	addMappedSpan(&span, avc.MappedSpan{SourceSpans: []cursor.Span{{ByteOffset: 29, BitOffset: 2, BitLength: 2}}})
	if span == nil || *span != (cursor.Span{ByteOffset: 29, BitOffset: 2, BitLength: 27}) {
		t.Fatalf("envelope %+v", span)
	}
}
