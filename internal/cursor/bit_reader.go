package cursor

import (
	"fmt"
	"io"
	"math"
)

// BitReader provides bounded, MSB-first bit reads over an io.ReaderAt.
// It is not safe for concurrent use.
type BitReader struct {
	reader    io.ReaderAt
	base      uint64
	size      uint64
	bytePos   uint64
	bitOffset uint8
}

// NewBitReader returns a bit reader over [0, size).
func NewBitReader(reader io.ReaderAt, size uint64) *BitReader {
	return &BitReader{reader: reader, size: size}
}

// NewBitReaderAt returns a bit reader over [byteOffset, byteOffset+size).
func NewBitReaderAt(reader io.ReaderAt, byteOffset, size uint64) (*BitReader, error) {
	if byteOffset > math.MaxUint64-size {
		return nil, &OffsetError{Offset: byteOffset, Length: size}
	}
	return &BitReader{reader: reader, base: byteOffset, size: size}, nil
}

// Offset returns the current absolute byte and bit offsets.
func (r *BitReader) Offset() (byteOffset uint64, bitOffset uint8) {
	return r.base + r.bytePos, r.bitOffset
}

// SizeBytes returns the size of the reader's bounded range in bytes.
func (r *BitReader) SizeBytes() uint64 { return r.size }

// RemainingBits returns the unread bit count, saturating at math.MaxUint64.
func (r *BitReader) RemainingBits() uint64 {
	if r.bytePos >= r.size {
		return 0
	}
	bytes := r.size - r.bytePos
	if bytes > math.MaxUint64/8 {
		return math.MaxUint64
	}
	bits := bytes * 8
	return bits - uint64(r.bitOffset)
}

// Seek moves to an absolute bit position within the bounded range.
func (r *BitReader) Seek(byteOffset uint64, bitOffset uint8) error {
	if bitOffset >= 8 || byteOffset < r.base {
		return r.bitBoundsError("seek", byteOffset, bitOffset, 0)
	}
	relative := byteOffset - r.base
	if relative > r.size || (relative == r.size && bitOffset != 0) {
		return r.bitBoundsError("seek", byteOffset, bitOffset, 0)
	}
	r.bytePos = relative
	r.bitOffset = bitOffset
	return nil
}

// Skip advances by n bits without reading them.
func (r *BitReader) Skip(n uint64) error {
	nextByte, nextBit, ok := r.advancePosition(n)
	if !ok {
		byteOffset, bitOffset := r.Offset()
		return r.bitBoundsError("skip", byteOffset, bitOffset, n)
	}
	r.bytePos, r.bitOffset = nextByte, nextBit
	return nil
}

// Span returns a source span of bitLength bits at the current position.
func (r *BitReader) Span(bitLength uint32) (Span, error) {
	byteOffset, bitOffset := r.Offset()
	if _, _, ok := r.advancePosition(uint64(bitLength)); !ok {
		return Span{}, r.bitBoundsError("span", byteOffset, bitOffset, uint64(bitLength))
	}
	return NewSpan(byteOffset, bitOffset, bitLength)
}

// ReadBits reads up to 64 bits in MSB-first order.
func (r *BitReader) ReadBits(n uint8) (uint64, error) {
	value, _, err := r.ReadBitsWithSpan(n)
	return value, err
}

// ReadBitsWithSpan reads bits and returns their absolute source span.
func (r *BitReader) ReadBitsWithSpan(n uint8) (uint64, Span, error) {
	if n > 64 {
		return 0, Span{}, &BitCountError{Count: n}
	}

	span, err := r.Span(uint32(n))
	if err != nil {
		return 0, Span{}, err
	}
	if n == 0 {
		return 0, span, nil
	}
	if r.reader == nil {
		return 0, Span{}, ErrNilReader
	}

	windowLength := (uint16(r.bitOffset) + uint16(n) + 7) / 8
	var window [9]byte
	absolute, _ := r.Offset()
	if !readerAtRangeRepresentable(absolute, uint64(windowLength)) {
		return 0, Span{}, &OffsetError{Offset: absolute, Length: uint64(windowLength)}
	}
	read, readErr := r.reader.ReadAt(window[:windowLength], int64(absolute))
	read = clampReadCount(read, int(windowLength))
	if read != int(windowLength) {
		if readErr == nil {
			readErr = io.ErrUnexpectedEOF
		}
		return 0, Span{}, fmt.Errorf("cursor: read %d bits at byte %d bit %d: %w", n, absolute, r.bitOffset, readErr)
	}

	var value uint64
	for i := uint16(0); i < uint16(n); i++ {
		position := uint16(r.bitOffset) + i
		bit := (window[position/8] >> (7 - position%8)) & 1
		value = value<<1 | uint64(bit)
	}

	nextByte, nextBit, _ := r.advancePosition(uint64(n))
	r.bytePos, r.bitOffset = nextByte, nextBit
	return value, span, nil
}

func (r *BitReader) advancePosition(n uint64) (byteOffset uint64, bitOffset uint8, ok bool) {
	byteAdvance := n / 8
	bitSum := uint16(r.bitOffset) + uint16(n%8)
	byteAdvance += uint64(bitSum / 8)
	nextBit := uint8(bitSum % 8)

	if r.bytePos > r.size {
		return 0, 0, false
	}
	remainingBytes := r.size - r.bytePos
	if byteAdvance > remainingBytes || (byteAdvance == remainingBytes && nextBit != 0) {
		return 0, 0, false
	}
	return r.bytePos + byteAdvance, nextBit, true
}

func (r *BitReader) bitBoundsError(op string, byteOffset uint64, bitOffset uint8, need uint64) error {
	return &BitBoundsError{
		Op:         op,
		ByteOffset: byteOffset,
		BitOffset:  bitOffset,
		Need:       need,
		RangeStart: r.base,
		RangeSize:  r.size,
	}
}
