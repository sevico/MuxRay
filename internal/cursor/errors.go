package cursor

import (
	"errors"
	"fmt"
)

var (
	// ErrOutOfBounds identifies attempts to move or read outside a cursor's range.
	ErrOutOfBounds = errors.New("cursor: out of bounds")
	// ErrInvalidSpan identifies a Span that cannot represent a source range.
	ErrInvalidSpan = errors.New("cursor: invalid span")
	// ErrInvalidBitCount identifies bit reads larger than 64 bits.
	ErrInvalidBitCount = errors.New("cursor: invalid bit count")
	// ErrNilReader identifies a cursor with no backing reader.
	ErrNilReader = errors.New("cursor: nil ReaderAt")
	// ErrOffsetOverflow identifies a range that io.ReaderAt cannot address.
	ErrOffsetOverflow = errors.New("cursor: ReaderAt offset overflow")
	// ErrLengthOverflow identifies a byte slice length that int cannot represent.
	ErrLengthOverflow = errors.New("cursor: byte length overflow")
	// ErrReadLimit identifies an allocating read above the parser safety ceiling.
	ErrReadLimit = errors.New("cursor: allocating read limit exceeded")
)

// BoundsError describes a byte operation outside a ByteReader's declared size.
type BoundsError struct {
	Op     string
	Offset uint64
	Need   uint64
	Size   uint64
}

func (e *BoundsError) Error() string {
	if e.Op == "seek" {
		return fmt.Sprintf("cursor: %s offset %d outside byte range [0,%d]", e.Op, e.Offset, e.Size)
	}
	return fmt.Sprintf("cursor: %s at byte %d needs %d bytes (size %d)", e.Op, e.Offset, e.Need, e.Size)
}

func (e *BoundsError) Is(target error) bool { return target == ErrOutOfBounds }

// BitBoundsError describes a bit operation outside a BitReader's byte range.
type BitBoundsError struct {
	Op         string
	ByteOffset uint64
	BitOffset  uint8
	Need       uint64
	RangeStart uint64
	RangeSize  uint64
}

func (e *BitBoundsError) Error() string {
	return fmt.Sprintf(
		"cursor: %s at byte %d bit %d needs %d bits (range starts at %d and contains %d bytes)",
		e.Op,
		e.ByteOffset,
		e.BitOffset,
		e.Need,
		e.RangeStart,
		e.RangeSize,
	)
}

func (e *BitBoundsError) Is(target error) bool { return target == ErrOutOfBounds }

// SpanError describes why a Span is invalid.
type SpanError struct {
	Span   Span
	Reason string
}

func (e *SpanError) Error() string {
	return fmt.Sprintf(
		"cursor: invalid span at byte %d bit %d with length %d: %s",
		e.Span.ByteOffset,
		e.Span.BitOffset,
		e.Span.BitLength,
		e.Reason,
	)
}

func (e *SpanError) Is(target error) bool { return target == ErrInvalidSpan }

// BitCountError describes an unsupported ReadBits width.
type BitCountError struct {
	Count uint8
}

func (e *BitCountError) Error() string {
	return fmt.Sprintf("cursor: cannot read %d bits at once; maximum is 64", e.Count)
}

func (e *BitCountError) Is(target error) bool { return target == ErrInvalidBitCount }

// OffsetError describes a byte range that cannot be passed to io.ReaderAt.
type OffsetError struct {
	Offset uint64
	Length uint64
}

func (e *OffsetError) Error() string {
	return fmt.Sprintf("cursor: byte range at %d with length %d exceeds ReaderAt's int64 offsets", e.Offset, e.Length)
}

func (e *OffsetError) Is(target error) bool { return target == ErrOffsetOverflow }

// LengthError describes a requested allocation that int cannot represent.
type LengthError struct {
	Length uint64
}

func (e *LengthError) Error() string {
	return fmt.Sprintf("cursor: byte length %d cannot be represented by int", e.Length)
}

func (e *LengthError) Is(target error) bool { return target == ErrLengthOverflow }

// ReadLimitError describes an allocating read above MaxReadBytes. Callers
// should stream or use a separately budgeted caller-owned buffer instead.
type ReadLimitError struct {
	Length uint64
	Limit  uint64
}

func (e *ReadLimitError) Error() string {
	return fmt.Sprintf("cursor: allocating read length %d exceeds limit %d", e.Length, e.Limit)
}

func (e *ReadLimitError) Is(target error) bool { return target == ErrReadLimit }
