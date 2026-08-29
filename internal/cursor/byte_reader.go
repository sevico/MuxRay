package cursor

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
)

// ByteReader provides bounded, big-endian reads over an io.ReaderAt.
// It is not safe for concurrent use.
type ByteReader struct {
	reader io.ReaderAt
	size   uint64
	offset uint64
}

// MaxReadBytes is the hard ceiling for one allocating ReadBytes call. Larger
// data must be processed in chunks or through a separately budgeted buffer.
const MaxReadBytes uint64 = 16 << 20

// NewByteReader returns a reader limited to size bytes.
func NewByteReader(reader io.ReaderAt, size uint64) *ByteReader {
	return &ByteReader{reader: reader, size: size}
}

// Offset returns the current byte offset.
func (r *ByteReader) Offset() uint64 { return r.offset }

// Size returns the declared byte size.
func (r *ByteReader) Size() uint64 { return r.size }

// Remaining returns the bytes between the current offset and the end.
func (r *ByteReader) Remaining() uint64 {
	if r.offset >= r.size {
		return 0
	}
	return r.size - r.offset
}

// Seek moves to an absolute byte offset within the declared range.
func (r *ByteReader) Seek(offset uint64) error {
	if offset > r.size {
		return &BoundsError{Op: "seek", Offset: offset, Size: r.size}
	}
	r.offset = offset
	return nil
}

// Skip advances by n bytes without reading them.
func (r *ByteReader) Skip(n uint64) error {
	if n > r.Remaining() {
		return &BoundsError{Op: "skip", Offset: r.offset, Need: n, Size: r.size}
	}
	r.offset += n
	return nil
}

// ReadBytes reads exactly n bytes and advances only after a complete read.
func (r *ByteReader) ReadBytes(n uint64) ([]byte, error) {
	start := r.offset
	if n > r.Remaining() {
		return nil, &BoundsError{Op: "read", Offset: start, Need: n, Size: r.size}
	}
	if n == 0 {
		return []byte{}, nil
	}
	if n > uint64(maxInt()) {
		return nil, &LengthError{Length: n}
	}
	if n > MaxReadBytes {
		return nil, &ReadLimitError{Length: n, Limit: MaxReadBytes}
	}
	if r.reader == nil {
		return nil, ErrNilReader
	}
	if !readerAtRangeRepresentable(start, n) {
		return nil, &OffsetError{Offset: start, Length: n}
	}

	buf := make([]byte, int(n))
	read, err := r.reader.ReadAt(buf, int64(start))
	read = clampReadCount(read, len(buf))
	if read != len(buf) {
		if err == nil {
			err = io.ErrUnexpectedEOF
		}
		return buf[:read], fmt.Errorf("cursor: read %d bytes at byte %d: %w", n, start, err)
	}

	r.offset += n
	return buf, nil
}

// ReadInto fills destination and advances only after a complete read. It avoids
// allocating and is intended for fixed-size parser headers and caller-owned
// buffers. On error, destination may contain partial bytes but the cursor
// position remains unchanged.
func (r *ByteReader) ReadInto(destination []byte) error {
	start := r.offset
	length := uint64(len(destination))
	if length > r.Remaining() {
		return &BoundsError{Op: "read", Offset: start, Need: length, Size: r.size}
	}
	if length == 0 {
		return nil
	}
	if r.reader == nil {
		return ErrNilReader
	}
	if !readerAtRangeRepresentable(start, length) {
		return &OffsetError{Offset: start, Length: length}
	}

	read, err := r.reader.ReadAt(destination, int64(start))
	read = clampReadCount(read, len(destination))
	if read != len(destination) {
		if err == nil {
			err = io.ErrUnexpectedEOF
		}
		return fmt.Errorf("cursor: read %d bytes at byte %d: %w", length, start, err)
	}

	r.offset += length
	return nil
}

// ReadU8 reads an unsigned 8-bit integer.
func (r *ByteReader) ReadU8() (uint8, error) {
	buf, err := r.ReadBytes(1)
	if err != nil {
		return 0, err
	}
	return buf[0], nil
}

// ReadU16BE reads an unsigned 16-bit big-endian integer.
func (r *ByteReader) ReadU16BE() (uint16, error) {
	buf, err := r.ReadBytes(2)
	if err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint16(buf), nil
}

// ReadU24BE reads an unsigned 24-bit big-endian integer in a uint32.
func (r *ByteReader) ReadU24BE() (uint32, error) {
	buf, err := r.ReadBytes(3)
	if err != nil {
		return 0, err
	}
	return uint32(buf[0])<<16 | uint32(buf[1])<<8 | uint32(buf[2]), nil
}

// ReadU32BE reads an unsigned 32-bit big-endian integer.
func (r *ByteReader) ReadU32BE() (uint32, error) {
	buf, err := r.ReadBytes(4)
	if err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint32(buf), nil
}

// ReadU64BE reads an unsigned 64-bit big-endian integer.
func (r *ByteReader) ReadU64BE() (uint64, error) {
	buf, err := r.ReadBytes(8)
	if err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint64(buf), nil
}

func maxInt() int { return int(^uint(0) >> 1) }

func readerAtRangeRepresentable(offset, length uint64) bool {
	if length == 0 {
		return true
	}
	if offset > math.MaxInt64 {
		return false
	}
	return length-1 <= uint64(math.MaxInt64)-offset
}

func clampReadCount(read, length int) int {
	if read < 0 {
		return 0
	}
	if read > length {
		return length
	}
	return read
}
