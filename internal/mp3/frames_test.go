package mp3

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"testing"

	"github.com/sevico/muxray/internal/cursor"
)

func sequence(t *testing.T, data []byte, options Options) FrameSequence {
	t.Helper()
	r, e := ParseFrameSequence(bytes.NewReader(data), byteSpan(0, uint64(len(data))), options)
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func mpegFrame(header [4]byte) []byte {
	h := decodeFrameHeader(0, header)
	deriveFrameHeader(&h)
	b := make([]byte, h.FrameLengthBytes)
	copy(b, header[:])
	return b
}

var stereoHeader = [4]byte{0xff, 0xfb, 0x90, 0x64}

func hasCode(r FrameSequence, code string) bool {
	for _, d := range r.Diagnostics {
		if d.Code == code {
			return true
		}
	}
	return false
}

func TestFrameSequenceBoundariesAndStops(t *testing.T) {
	frame := mpegFrame(stereoHeader)
	data := append(append([]byte{}, frame...), frame...)
	good := sequence(t, data, Options{})
	if !good.Complete || good.CompleteFrames != 2 || len(good.Frames) != 2 || good.BytesCovered != 834 || good.BytesRead != 16 || good.Frames[1].Span.ByteOffset != 417 {
		t.Fatalf("two frames: %+v", good)
	}
	for _, tc := range []struct {
		name       string
		data       []byte
		opts       Options
		code, stop string
		complete   uint32
	}{
		{"truncated body", data[:len(data)-1], Options{}, CodeFrameBodyTruncated, "body_truncated", 1},
		{"short next header", append(append([]byte{}, frame...), 0xff), Options{}, CodeFrameHeaderTruncated, "header_truncated", 1},
		{"frame budget", data, Options{MaxFrames: 1}, CodeFrameLimitReached, "frame_limit", 1},
		{"free format", []byte{0xff, 0xfb, 0, 0x64}, Options{}, CodeFrameBoundaryUnresolved, "free_format", 0},
		{"no sync search", append([]byte{0}, data...), Options{}, CodeSyncInvalid, "invalid_header", 0},
		{"empty", nil, Options{}, CodeFrameHeaderTruncated, "header_truncated", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := sequence(t, tc.data, tc.opts)
			if r.Complete || !hasCode(r, tc.code) || r.StopReason != tc.stop || r.CompleteFrames != tc.complete {
				t.Fatalf("stop: %+v", r)
			}
		})
	}
	if _, e := (Options{MaxFrames: HardMaxFrames + 1}).Normalized(); e == nil {
		t.Fatal("accepted unbounded frame count")
	}
	// Maximum MPEG-2.5 frame size reaches byte coverage before 1024 frames.
	huge := mpegFrame([4]byte{0xff, 0xe3, 0xe8, 0x64})
	r := sequence(t, bytes.Repeat(huge, 800), Options{MaxFrames: HardMaxFrames})
	if r.Complete || !hasCode(r, CodeFrameByteLimitReached) || r.BytesCovered > MaxFrameScanBytes || r.BytesRead > uint64(4*HardMaxFrames+8) {
		t.Fatalf("byte budget: %+v", r)
	}
}

func TestFrameSequenceCRCIndependentVector(t *testing.T) {
	// CRC-16/ANSI MSB-first, init ffff, independently computed using polynomial
	// long division: MPEG header bytes 90 64 + 32 zero side-information bytes.
	frame := mpegFrame([4]byte{0xff, 0xfa, 0x90, 0x64})
	binary.BigEndian.PutUint16(frame[4:6], 0x576f)
	r := sequence(t, frame, Options{})
	if !r.Complete || r.Frames[0].CRC.Status != "match" || r.Frames[0].CRC.Computed != 0x576f {
		t.Fatalf("CRC vector: %+v", r)
	}
	frame[len(frame)-1] = 1 // main data is excluded from this CRC.
	if r := sequence(t, frame, Options{}); !r.Complete {
		t.Fatal("CRC unexpectedly covered main data")
	}
	frame[6] = 1
	r = sequence(t, frame, Options{})
	if r.Complete || !hasCode(r, CodeCRCMismatch) || r.CompleteFrames != 1 || r.Frames[0].CRC.Status != "mismatch" {
		t.Fatalf("CRC corruption: %+v", r)
	}
	r = sequence(t, frame[:8], Options{})
	if !hasCode(r, CodeCRCTruncated) || !hasCode(r, CodeFrameBodyTruncated) {
		t.Fatalf("truncated CRC: %+v", r)
	}
	for _, h := range [][4]byte{{0xff, 0xfa, 0x90, 0xc4}, {0xff, 0xf2, 0x80, 0x64}, {0xff, 0xe2, 0x48, 0xc4}} {
		f := mpegFrame(h)
		header := decodeFrameHeader(0, h)
		n := sideInfoBytes(header)
		binary.BigEndian.PutUint16(f[4:6], referenceCRC(append(h[2:], f[6:6+n]...)))
		if r := sequence(t, f, Options{}); !r.Complete || r.Frames[0].CRC.Status != "match" {
			t.Fatalf("version/channel CRC: %+v", r)
		}
	}
}

// This test oracle uses polynomial division on the augmented message, unlike
// the production byte/bit feedback loop. No production CRC helper is called.
func referenceCRC(data []byte) uint16 {
	remainder := uint32(0xffff)
	for _, b := range data {
		remainder = (remainder << 8) ^ uint32(b)<<16
		for bit := 23; bit >= 16; bit-- {
			if remainder&(1<<bit) != 0 {
				remainder ^= uint32(0x18005) << (bit - 16)
			}
		}
	}
	return uint16(remainder)
}

func TestFrameSequenceVBRMetadata(t *testing.T) {
	for _, kind := range []string{"Xing", "Info"} {
		t.Run(kind, func(t *testing.T) {
			f := mpegFrame(stereoHeader)
			copy(f[36:], kind)
			binary.BigEndian.PutUint32(f[40:], 15)
			binary.BigEndian.PutUint32(f[44:], 9)
			binary.BigEndian.PutUint32(f[48:], 123456)
			binary.BigEndian.PutUint32(f[152:], 78)
			r := sequence(t, f, Options{})
			v := r.VBR
			if !r.Complete || v == nil || !v.Complete || v.Kind != kind || *v.DeclaredFrames != 9 || *v.DeclaredBytes != 123456 || *v.Quality != 78 || v.TOCSpan.ByteOffset != 52 || v.TOCSpan.BitLength != 800 || r.CompleteFrames != 1 {
				t.Fatalf("VBR declarations: %+v %+v", r, v)
			}
		})
	}
	// Xing offset is version/channel dependent, and does not shift for CRC.
	for _, h := range [][4]byte{{0xff, 0xfb, 0x90, 0xc4}, {0xff, 0xf3, 0x80, 0x64}, {0xff, 0xe3, 0x48, 0xc4}, {0xff, 0xfa, 0x90, 0x64}} {
		f := mpegFrame(h)
		header := decodeFrameHeader(0, h)
		offset := 4 + sideInfoBytes(header)
		copy(f[offset:], "Info")
		if header.CRCProtected {
			binary.BigEndian.PutUint16(f[4:6], referenceCRC(append(h[2:], f[6:6+sideInfoBytes(header)]...)))
		}
		r := sequence(t, f, Options{})
		if !r.Complete || r.VBR == nil || r.VBR.Span.ByteOffset != uint64(offset) {
			t.Fatalf("metadata offset: %+v", r)
		}
	}
	f := mpegFrame(stereoHeader)
	copy(f[36:], "VBRI")
	binary.BigEndian.PutUint16(f[40:], 1)
	binary.BigEndian.PutUint16(f[42:], 12)
	binary.BigEndian.PutUint16(f[44:], 99)
	binary.BigEndian.PutUint32(f[46:], 123456)
	binary.BigEndian.PutUint32(f[50:], 123)
	r := sequence(t, f, Options{})
	if !r.Complete || r.VBR == nil || *r.VBR.Version != 1 || *r.VBR.DeclaredFrames != 123 || *r.VBR.Delay != 12 || *r.VBR.Quality != 99 {
		t.Fatalf("VBRI: %+v", r)
	}
	binary.BigEndian.PutUint16(f[40:], 2)
	r = sequence(t, f, Options{})
	if r.Complete || r.VBR.Complete || !hasCode(r, CodeVBRIVersionUnsupported) {
		t.Fatalf("VBRI unknown: %+v", r)
	}
	// Marker found, but conditional fields cannot escape first-frame boundary.
	f = mpegFrame([4]byte{0xff, 0xf3, 0x10, 0x64})
	copy(f[21:], "Xing") // 26-byte frame: flags do not fit.
	r = sequence(t, f, Options{})
	if r.Complete || r.VBR == nil || !hasCode(r, CodeVBRHeaderTruncated) {
		t.Fatalf("VBR truncation: %+v", r)
	}
	f = mpegFrame(stereoHeader)
	copy(f[36:], "Xing")
	binary.BigEndian.PutUint32(f[40:], 16)
	r = sequence(t, f, Options{})
	if r.Complete || !hasCode(r, CodeVBRFlagsUnsupported) {
		t.Fatalf("unknown flags: %+v", r)
	}
}

type sequenceErrorReader struct {
	data []byte
	at   int64
	full bool
	err  error
}

func (r sequenceErrorReader) ReadAt(p []byte, off int64) (int, error) {
	if off == r.at {
		if r.full {
			copy(p, r.data[off:])
			return len(p), r.err
		}
		return 0, r.err
	}
	return bytes.NewReader(r.data).ReadAt(p, off)
}
func TestFrameSequenceSourceErrors(t *testing.T) {
	f := mpegFrame(stereoHeader)
	copy(f[36:], "Xing")
	for _, at := range []int64{0, 36, 40} {
		for _, full := range []bool{false, true} {
			for _, failure := range []error{io.ErrUnexpectedEOF, errors.New("source failed")} {
				_, err := ParseFrameSequence(sequenceErrorReader{f, at, full, failure}, byteSpan(0, uint64(len(f))), Options{})
				if !errors.Is(err, failure) {
					t.Fatalf("source failure became syntax: at=%d full=%t error=%v", at, full, err)
				}
			}
		}
	}
	for _, span := range []cursor.Span{{BitOffset: 1, BitLength: 32}, {BitLength: uint32((HardMaxPayloadBytes + 1) * 8)}} {
		if _, err := ParseFrameSequence(bytes.NewReader(f), span, Options{}); err == nil {
			t.Fatal("invalid range accepted")
		}
	}
}

func FuzzFrameSequence(f *testing.F) {
	f.Add(mpegFrame(stereoHeader))
	f.Add([]byte{0xff, 0xfb, 0, 0x64})
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			t.Skip()
		}
		r, err := ParseFrameSequence(bytes.NewReader(data), byteSpan(0, uint64(len(data))), Options{MaxFrames: 8, MaxDiagnostics: 4})
		if err != nil {
			t.Fatal(err)
		}
		if len(r.Frames) > 8 || len(r.Diagnostics) > 4 || r.BytesCovered > uint64(len(data)) {
			t.Fatal("budget escaped")
		}
		for _, frame := range r.Frames {
			_, end, _ := frame.Span.ByteRange()
			if end > uint64(len(data)) {
				t.Fatal("frame escaped tag")
			}
		}
	})
}
