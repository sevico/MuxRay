package cursor

import (
	"bytes"
	"errors"
	"io"
	"math"
	"testing"
)

func TestBitReaderMSBFirstAndSpans(t *testing.T) {
	t.Parallel()

	r := NewBitReader(bytes.NewReader([]byte{0b10110010, 0b01101100}), 2)
	tests := []struct {
		name     string
		bits     uint8
		want     uint64
		wantSpan Span
	}{
		{name: "first three", bits: 3, want: 0b101, wantSpan: Span{ByteOffset: 0, BitLength: 3}},
		{name: "finish first byte", bits: 5, want: 0b10010, wantSpan: Span{ByteOffset: 0, BitOffset: 3, BitLength: 5}},
		{name: "second byte", bits: 8, want: 0b01101100, wantSpan: Span{ByteOffset: 1, BitLength: 8}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, span, err := r.ReadBitsWithSpan(test.bits)
			if err != nil {
				t.Fatalf("ReadBitsWithSpan(%d) error = %v", test.bits, err)
			}
			if got != test.want {
				t.Fatalf("ReadBitsWithSpan(%d) value = %#b, want %#b", test.bits, got, test.want)
			}
			if span != test.wantSpan {
				t.Fatalf("ReadBitsWithSpan(%d) span = %+v, want %+v", test.bits, span, test.wantSpan)
			}
		})
	}
	if byteOffset, bitOffset := r.Offset(); byteOffset != 2 || bitOffset != 0 {
		t.Fatalf("Offset() = (%d,%d), want (2,0)", byteOffset, bitOffset)
	}
}

func TestBitReaderUnaligned64Bits(t *testing.T) {
	t.Parallel()

	data := bytes.Repeat([]byte{0xff}, 9)
	r := NewBitReader(bytes.NewReader(data), uint64(len(data)))
	if err := r.Skip(1); err != nil {
		t.Fatalf("Skip(1) error = %v", err)
	}
	got, err := r.ReadBits(64)
	if err != nil {
		t.Fatalf("ReadBits(64) error = %v", err)
	}
	if got != math.MaxUint64 {
		t.Fatalf("ReadBits(64) = %#x, want %#x", got, uint64(math.MaxUint64))
	}
	if byteOffset, bitOffset := r.Offset(); byteOffset != 8 || bitOffset != 1 {
		t.Fatalf("Offset() = (%d,%d), want (8,1)", byteOffset, bitOffset)
	}
}

func TestBitReaderAbsoluteRange(t *testing.T) {
	t.Parallel()

	r, err := NewBitReaderAt(bytes.NewReader([]byte{0, 0, 0b11001010, 0b01100000}), 2, 2)
	if err != nil {
		t.Fatalf("NewBitReaderAt() error = %v", err)
	}
	if got := r.SizeBytes(); got != 2 {
		t.Fatalf("SizeBytes() = %d, want 2", got)
	}
	if got := r.RemainingBits(); got != 16 {
		t.Fatalf("RemainingBits() = %d, want 16", got)
	}

	got, span, err := r.ReadBitsWithSpan(6)
	if err != nil {
		t.Fatalf("ReadBitsWithSpan() error = %v", err)
	}
	if got != 0b110010 {
		t.Fatalf("ReadBitsWithSpan() = %#b, want %#b", got, uint64(0b110010))
	}
	wantSpan := Span{ByteOffset: 2, BitLength: 6}
	if span != wantSpan {
		t.Fatalf("span = %+v, want %+v", span, wantSpan)
	}
	if byteOffset, bitOffset := r.Offset(); byteOffset != 2 || bitOffset != 6 {
		t.Fatalf("Offset() = (%d,%d), want (2,6)", byteOffset, bitOffset)
	}

	if err := r.Seek(3, 4); err != nil {
		t.Fatalf("Seek(3,4) error = %v", err)
	}
	got, err = r.ReadBits(4)
	if err != nil {
		t.Fatalf("ReadBits(4) error = %v", err)
	}
	if got != 0 {
		t.Fatalf("ReadBits(4) = %#b, want 0", got)
	}
}

func TestBitReaderSkipSeekAndSpanBoundsAreAtomic(t *testing.T) {
	t.Parallel()

	r := NewBitReader(bytes.NewReader([]byte{0xaa, 0x55}), 2)
	if err := r.Skip(5); err != nil {
		t.Fatalf("Skip(5) error = %v", err)
	}
	if byteOffset, bitOffset := r.Offset(); byteOffset != 0 || bitOffset != 5 {
		t.Fatalf("Offset() = (%d,%d), want (0,5)", byteOffset, bitOffset)
	}
	span, err := r.Span(7)
	if err != nil {
		t.Fatalf("Span(7) error = %v", err)
	}
	if span != (Span{ByteOffset: 0, BitOffset: 5, BitLength: 7}) {
		t.Fatalf("Span(7) = %+v", span)
	}

	tests := []struct {
		name string
		run  func() error
	}{
		{name: "skip", run: func() error { return r.Skip(12) }},
		{name: "seek past end", run: func() error { return r.Seek(2, 1) }},
		{name: "seek bad bit", run: func() error { return r.Seek(0, 8) }},
		{name: "span", run: func() error { _, err := r.Span(12); return err }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			beforeByte, beforeBit := r.Offset()
			err := test.run()
			if !errors.Is(err, ErrOutOfBounds) {
				t.Fatalf("error = %v, want ErrOutOfBounds", err)
			}
			var boundsErr *BitBoundsError
			if !errors.As(err, &boundsErr) {
				t.Fatalf("error type = %T, want *BitBoundsError", err)
			}
			afterByte, afterBit := r.Offset()
			if afterByte != beforeByte || afterBit != beforeBit {
				t.Fatalf("Offset() changed from (%d,%d) to (%d,%d)", beforeByte, beforeBit, afterByte, afterBit)
			}
		})
	}
}

func TestBitReaderReadErrorsAreAtomic(t *testing.T) {
	t.Parallel()

	t.Run("invalid bit count", func(t *testing.T) {
		r := NewBitReader(bytes.NewReader(make([]byte, 16)), 16)
		if _, err := r.ReadBits(65); !errors.Is(err, ErrInvalidBitCount) {
			t.Fatalf("ReadBits(65) error = %v, want ErrInvalidBitCount", err)
		}
		if byteOffset, bitOffset := r.Offset(); byteOffset != 0 || bitOffset != 0 {
			t.Fatalf("Offset() = (%d,%d), want unchanged", byteOffset, bitOffset)
		}
	})

	t.Run("logical bounds", func(t *testing.T) {
		r := NewBitReader(bytes.NewReader([]byte{0}), 1)
		if err := r.Skip(7); err != nil {
			t.Fatal(err)
		}
		if _, err := r.ReadBits(2); !errors.Is(err, ErrOutOfBounds) {
			t.Fatalf("ReadBits(2) error = %v, want ErrOutOfBounds", err)
		}
		if byteOffset, bitOffset := r.Offset(); byteOffset != 0 || bitOffset != 7 {
			t.Fatalf("Offset() = (%d,%d), want unchanged (0,7)", byteOffset, bitOffset)
		}
	})

	t.Run("short backing reader", func(t *testing.T) {
		r := NewBitReader(bytes.NewReader([]byte{0xff}), 2)
		if _, err := r.ReadBits(16); !errors.Is(err, io.EOF) {
			t.Fatalf("ReadBits(16) error = %v, want wrapped io.EOF", err)
		}
		if byteOffset, bitOffset := r.Offset(); byteOffset != 0 || bitOffset != 0 {
			t.Fatalf("Offset() = (%d,%d), want unchanged", byteOffset, bitOffset)
		}
	})

	t.Run("nil reader", func(t *testing.T) {
		r := NewBitReader(nil, 1)
		if _, err := r.ReadBits(1); !errors.Is(err, ErrNilReader) {
			t.Fatalf("ReadBits(1) error = %v, want ErrNilReader", err)
		}
	})
}

func TestBitReaderAcceptsFullReadWithEOF(t *testing.T) {
	t.Parallel()

	r := NewBitReader(fullReadEOFReaderAt{data: []byte{0b10110110}}, 1)
	value, err := r.ReadBits(8)
	if err != nil {
		t.Fatalf("ReadBits(8) error = %v", err)
	}
	if value != 0b10110110 {
		t.Fatalf("ReadBits(8) = %08b, want 10110110", value)
	}
}

func TestBitReaderZeroLengthAndSaturatedRemaining(t *testing.T) {
	t.Parallel()

	r := NewBitReader(nil, math.MaxUint64)
	if got := r.RemainingBits(); got != math.MaxUint64 {
		t.Fatalf("RemainingBits() = %d, want saturation %d", got, uint64(math.MaxUint64))
	}
	value, span, err := r.ReadBitsWithSpan(0)
	if err != nil {
		t.Fatalf("ReadBitsWithSpan(0) error = %v", err)
	}
	if value != 0 || span != (Span{}) {
		t.Fatalf("ReadBitsWithSpan(0) = (%d,%+v), want zero values", value, span)
	}
	if err := r.Seek(math.MaxUint64, 0); err != nil {
		t.Fatalf("Seek(end) error = %v", err)
	}
	if got := r.RemainingBits(); got != 0 {
		t.Fatalf("RemainingBits() at end = %d, want 0", got)
	}
}

func TestNewBitReaderAtOverflow(t *testing.T) {
	t.Parallel()

	if _, err := NewBitReaderAt(bytes.NewReader(nil), math.MaxUint64, 2); !errors.Is(err, ErrOffsetOverflow) {
		t.Fatalf("NewBitReaderAt() error = %v, want ErrOffsetOverflow", err)
	}
}

func FuzzBitReader(f *testing.F) {
	f.Add([]byte{}, uint64(0), uint8(0))
	f.Add([]byte{0x46, 0x4c, 0x56}, uint64(0), uint8(24))
	f.Add([]byte{0xff, 0x00}, uint64(7), uint8(9))
	f.Add([]byte{0}, uint64(math.MaxUint64), uint8(65))

	f.Fuzz(func(t *testing.T, data []byte, skip uint64, count uint8) {
		r := NewBitReader(bytes.NewReader(data), uint64(len(data)))
		beforeByte, beforeBit := r.Offset()
		if err := r.Skip(skip); err != nil {
			if !errors.Is(err, ErrOutOfBounds) {
				t.Fatalf("Skip(%d) error = %v", skip, err)
			}
			afterByte, afterBit := r.Offset()
			if afterByte != beforeByte || afterBit != beforeBit {
				t.Fatalf("failed Skip changed offset")
			}
			return
		}

		beforeByte, beforeBit = r.Offset()
		_, span, err := r.ReadBitsWithSpan(count)
		if err != nil {
			afterByte, afterBit := r.Offset()
			if afterByte != beforeByte || afterBit != beforeBit {
				t.Fatalf("failed read changed offset")
			}
			return
		}
		if span.ByteOffset != beforeByte || span.BitOffset != beforeBit || span.BitLength != uint32(count) {
			t.Fatalf("span = %+v, want start (%d,%d) length %d", span, beforeByte, beforeBit, count)
		}
	})
}
