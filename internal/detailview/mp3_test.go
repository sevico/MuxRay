package detailview

import (
	"bytes"
	"context"
	"errors"
	"io"
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/sevico/muxray/internal/flv"
	"github.com/sevico/muxray/internal/mp3"
)

func TestMP3VersionsAndDerivedValues(t *testing.T) {
	for _, tc := range []struct {
		name, version, bitrate, frequency, channels, samples, length string
		header                                                       []byte
	}{
		{"mpeg1", "3 (mpeg1)", "128", "44100", "2", "1152", "417", []byte{0xff, 0xfb, 0x90, 0x64}},
		{"padded", "3 (mpeg1)", "128", "44100", "2", "1152", "418", []byte{0xff, 0xfb, 0x92, 0x64}},
		{"mpeg2", "2 (mpeg2)", "64", "22050", "1", "576", "208", []byte{0xff, 0xf3, 0x80, 0xc4}},
		{"mpeg2.5", "0 (mpeg2.5)", "32", "8000", "1", "576", "288", []byte{0xff, 0xe3, 0x48, 0xc4}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Bodies are opaque, but declared frame boundaries fit in the tag.
			length, _ := strconv.Atoi(tc.length)
			body := make([]byte, length)
			copy(body, tc.header)
			d := parseFixture(t, 8, append([]byte{0x2f}, body...))
			if d.Kind != "mp3Audio" || !d.Complete || d.Truncated || len(d.Diagnostics) != 0 {
				t.Fatalf("MP3 header result: %+v", d)
			}
			for name, want := range map[string]string{"version": tc.version, "bitrateKbps": tc.bitrate, "samplingFrequencyHz": tc.frequency, "channels": tc.channels, "samplesPerFrame": tc.samples, "frameLengthBytes": tc.length} {
				if f := find(t, d, name); f.Value != want {
					t.Fatalf("%s: %+v, want %s", name, f, want)
				}
			}
			if !strings.Contains(find(t, d, "MP3 audio").Value, "not decoded or validated") || optionalField(d, "unparsedData") != nil {
				t.Fatal("header-only source claimed frame validation or invented remaining data")
			}
			for _, f := range d.Fields {
				if strings.HasPrefix(f.Type, "derived") && f.Span != nil {
					t.Fatalf("derived value has fabricated span: %+v", f)
				}
			}
		})
	}
}

func TestMP3ExactBitSpansAndCRCDeclaration(t *testing.T) {
	d := parseFixture(t, 8, []byte{0x2f, 0xff, 0xfa, 0x90, 0x64, 0xde, 0xad})
	for name, want := range map[string]Span{
		"AudioTagHeader":         {ByteOffset: "24", BitLength: 8},
		"soundFormat":            {ByteOffset: "24", BitLength: 4},
		"MPEG frame header":      {ByteOffset: "25", BitLength: 32},
		"syncWord":               {ByteOffset: "25", BitLength: 11},
		"version":                {ByteOffset: "26", BitOffset: 3, BitLength: 2},
		"layer":                  {ByteOffset: "26", BitOffset: 5, BitLength: 2},
		"protectionBit":          {ByteOffset: "26", BitOffset: 7, BitLength: 1},
		"bitrateIndex":           {ByteOffset: "27", BitLength: 4},
		"samplingFrequencyIndex": {ByteOffset: "27", BitOffset: 4, BitLength: 2},
		"padding":                {ByteOffset: "27", BitOffset: 6, BitLength: 1},
		"channelMode":            {ByteOffset: "28", BitLength: 2},
		"SoundData":              {ByteOffset: "25", BitLength: 48},
		"uninspectedFrames":      {ByteOffset: "25", BitLength: 48},
	} {
		if f := find(t, d, name); f.Span == nil || *f.Span != want {
			t.Fatalf("%s span: %+v, want %+v", name, f.Span, want)
		}
	}
	if d.Complete || !d.Truncated || !hasDiagnostic(d, mp3.CodeCRCTruncated) || find(t, d, "protectionBit").Value != "0" || !strings.HasPrefix(find(t, d, "CRC16").Value, "truncated") {
		t.Fatalf("CRC declaration: %+v", d)
	}
}

func TestMP3FreeFormatAndMalformedSyntax(t *testing.T) {
	d := parseFixture(t, 8, []byte{0x2f, 0xff, 0xfb, 0, 0x64})
	if d.Complete || d.Truncated || find(t, d, "bitrateMode").Value != "free" || !hasDiagnostic(d, mp3.CodeFreeFormatUnresolved) {
		t.Fatalf("free-format syntax: %+v", d)
	}
	if value := find(t, d, "Frame 0").Value; !strings.Contains(value, "declared length=unknown") || !strings.Contains(value, "bitrate=unknown") || strings.Contains(value, "0 kbit/s") {
		t.Fatalf("free format invented derivations: %s", value)
	}
	for _, name := range []string{"bitrateKbps", "frameLengthBytes"} {
		if f := optionalField(d, name); f != nil {
			t.Fatalf("invented free-format derivation: %+v", f)
		}
	}
	for _, tc := range []struct {
		header []byte
		code   string
	}{
		{nil, mp3.CodeFrameHeaderTruncated},
		{[]byte{0xff, 0xfb, 0x90}, mp3.CodeFrameHeaderTruncated},
		{[]byte{0xfe, 0xfb, 0x90, 0x64}, mp3.CodeSyncInvalid},
		{[]byte{0xff, 0xff, 0x90, 0x64}, mp3.CodeLayerNotIII},
		{[]byte{0xff, 0xeb, 0x90, 0x64}, mp3.CodeVersionReserved},
		{[]byte{0xff, 0xfb, 0xfc, 0x64}, mp3.CodeBitrateIndexInvalid},
	} {
		d := parseFixture(t, 8, append([]byte{0x2f}, tc.header...))
		if d.Complete || !hasDiagnostic(d, tc.code) || d.Truncated != (len(tc.header) < 4) {
			t.Fatalf("%s: %+v", tc.code, d)
		}
		if len(tc.header) == 4 {
			if value := find(t, d, "Frame 0").Value; !strings.Contains(value, "declared length=unknown") || !strings.Contains(value, "bitrate=unknown") || !strings.Contains(value, "channels=unknown") {
				t.Fatalf("invalid header invented derivations: %s", value)
			}
		}
		if (optionalField(d, "MPEG frame header") != nil) != (len(tc.header) == 4) {
			t.Fatal("missing or invented fixed header")
		}
		if len(tc.header) < 4 {
			if got := d.Diagnostics[0]; got.Offset != "25" || got.Expected == nil || *got.Expected != "4" || got.Actual == nil || *got.Actual != strconv.Itoa(len(tc.header)) {
				t.Fatalf("truncated diagnostic coordinates/counts: %+v", got)
			}
		}
	}
}

type mp3PrefixReader struct {
	prefix  []byte
	base    int64
	bytes   int
	reads   int
	cancel  context.CancelFunc
	failure error
}

func (r *mp3PrefixReader) ReadAt(p []byte, off int64) (int, error) {
	r.reads++
	if off < r.base || off+int64(len(p)) > r.base+int64(len(r.prefix)) {
		return 0, errors.New("MP3 read escaped fixed five-byte prefix")
	}
	if off == r.base+1 && r.failure != nil {
		return 0, r.failure
	}
	n, err := bytes.NewReader(r.prefix).ReadAt(p, off-r.base)
	r.bytes += n
	if off == r.base+1 && r.cancel != nil {
		r.cancel()
	}
	return n, err
}

type mp3SparseFrameReader struct {
	base         int64
	bytes, reads int
	sideReads    int
}

func (r *mp3SparseFrameReader) ReadAt(p []byte, off int64) (int, error) {
	r.reads++
	at := off - r.base
	if at == 0 && len(p) == 1 {
		p[0] = 0x2f
		r.bytes++
		return 1, nil
	}
	relative := at - 1
	if relative >= 0 && relative%417 == 0 && len(p) == 4 {
		copy(p, []byte{0xff, 0xfb, 0x90, 0x64})
		r.bytes += 4
		return 4, nil
	}
	if relative == 36 && len(p) == 4 {
		clear(p)
		r.bytes += 4
		return 4, nil
	}
	if relative >= 4 && relative%417 == 4 && len(p) == 32 {
		clear(p)
		r.bytes += len(p)
		r.sideReads++
		return len(p), nil
	}
	return 0, errors.New("read escaped fixed headers, side information and first-frame metadata signatures")
}
func TestMP3LargePayloadUsesBoundedHeaders(t *testing.T) {
	_, tag := fixture(t, 8, []byte{0x2f, 0xff, 0xfb, 0x90, 0x64})
	const delta uint64 = 1<<53 + 123
	tag.Offset += delta
	tag.ID = math.MaxUint64
	tag.DataSize, tag.AvailableDataSize = 0xffffff, 0xffffff
	r := &mp3SparseFrameReader{base: int64(delta + 24)}
	d, err := Parse(context.Background(), r, tag)
	if err != nil || d.Complete || !hasDiagnostic(d, mp3.CodeFrameLimitReached) || r.bytes > 280+64*32 || r.reads > 75+64 || r.sideReads != 64 {
		t.Fatalf("bounded frame parse: err=%v bytes=%d reads=%d detail=%+v", err, r.bytes, r.reads, d)
	}
	if find(t, d, "tagId").Value != "18446744073709551615" || find(t, d, "version").Span.ByteOffset != decimal(delta+26) || find(t, d, "bytesCovered").Value != "26688" {
		t.Fatal("large coordinates or frame coverage changed")
	}
	assertBounded(t, d)
}

func TestMP3CancellationAndFrameReadFailures(t *testing.T) {
	prefix := []byte{0x2f, 0xff, 0xfb, 0x90, 0x64}
	_, tag := fixture(t, 8, prefix)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := &mp3PrefixReader{prefix: prefix, base: 24, cancel: cancel}
	if d, err := Parse(ctx, r, tag); !errors.Is(err, context.Canceled) || d != nil {
		t.Fatalf("canceled final header read published result: %+v %v", d, err)
	}
	for _, failure := range []error{io.ErrUnexpectedEOF, errors.New("source failed")} {
		r := &mp3PrefixReader{prefix: prefix, base: 24, failure: failure}
		if d, err := Parse(context.Background(), r, tag); !errors.Is(err, failure) || d != nil {
			t.Fatalf("source error became syntax detail: %+v %v", d, err)
		}
	}
	// Selection-level rejection must happen before any reader access.
	for _, filtered := range []bool{false, true} {
		bad := tag
		want := ErrTruncated
		if filtered {
			bad.RawTypeFlags |= 0x20
			want = ErrFiltered
		} else {
			bad.AvailableDataSize--
		}
		r := &trackingReader{failure: errors.New("must not read")}
		if _, err := Parse(context.Background(), r, bad); !errors.Is(err, want) || r.reads != 0 {
			t.Fatalf("rejected MP3 read source: %v %d", err, r.reads)
		}
	}
	// Ordinary MP3 at 8 kHz is supported; reserved FLV SoundFormat=14 is not.
	data, reserved := fixture(t, uint8(flv.TagTypeAudio), []byte{0xef, 0xff, 0xe3, 0x48, 0xc4})
	if _, err := Parse(context.Background(), bytes.NewReader(data), reserved); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("reserved format: %v", err)
	}
}

type transientAudioReader struct {
	data     []byte
	failRead int
	reads    int
	count    int
	err      error
}

func (r *transientAudioReader) ReadAt(p []byte, off int64) (int, error) {
	r.reads++
	if r.reads == r.failRead {
		if r.count >= 0 {
			return r.count, r.err
		}
		n, _ := bytes.NewReader(r.data).ReadAt(p, off)
		return n, r.err
	}
	return bytes.NewReader(r.data).ReadAt(p, off)
}

func TestDesktopAudioRetainsProbeAndFullReadErrors(t *testing.T) {
	sentinel := errors.New("transient source failure")
	for _, payload := range [][]byte{{0x2f, 0xff, 0xfb, 0x90, 0x64}, {0xaf, 1, 0xde, 0xad}} {
		data, tag := fixture(t, 8, payload)
		for _, tc := range []struct {
			failRead, count int
			failure         error
		}{
			{1, 0, sentinel}, // A failed discriminator probe must not fall back to AAC.
			{1, 0, nil},
			{1, -1, sentinel}, // A full probe with a non-EOF error is still an error.
			{6, -1, sentinel}, // A full MPEG header read must not swallow its I/O error.
			{1, 2, nil},
		} {
			r := &transientAudioReader{data: data, failRead: tc.failRead, count: tc.count, err: tc.failure}
			d, err := Parse(context.Background(), r, tag)
			want := tc.failure
			if want == nil {
				want = io.ErrUnexpectedEOF
				if tc.count == 2 {
					want = ErrInvalidInput
				}
			}
			if d != nil || !errors.Is(err, want) || r.reads != tc.failRead {
				t.Fatalf("payload=%x case=%+v: detail=%+v err=%v reads=%d", payload, tc, d, err, r.reads)
			}
		}
		for _, failRead := range []int{1, 6} {
			r := &transientAudioReader{data: data, failRead: failRead, count: -1, err: io.EOF}
			if d, err := Parse(context.Background(), r, tag); err != nil || d == nil {
				t.Fatalf("full EOF compatibility: detail=%+v err=%v", d, err)
			}
		}
	}
}
