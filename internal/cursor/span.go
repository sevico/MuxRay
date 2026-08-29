package cursor

import "math"

// Span identifies a bit range in the original input.
type Span struct {
	ByteOffset uint64 `json:"byteOffset"`
	BitOffset  uint8  `json:"bitOffset"`
	BitLength  uint32 `json:"bitLength"`
}

// NewSpan constructs and validates a Span.
func NewSpan(byteOffset uint64, bitOffset uint8, bitLength uint32) (Span, error) {
	span := Span{ByteOffset: byteOffset, BitOffset: bitOffset, BitLength: bitLength}
	if err := span.Validate(); err != nil {
		return Span{}, err
	}
	return span, nil
}

// Validate reports whether the span has a normalized start and representable end.
func (s Span) Validate() error {
	if s.BitOffset >= 8 {
		return &SpanError{Span: s, Reason: "bit offset must be less than 8"}
	}

	byteAdvance := (uint64(s.BitOffset) + uint64(s.BitLength)) / 8
	if s.ByteOffset > math.MaxUint64-byteAdvance {
		return &SpanError{Span: s, Reason: "end offset overflows uint64"}
	}
	return nil
}

// End returns the normalized, exclusive bit position at the end of the span.
func (s Span) End() (byteOffset uint64, bitOffset uint8, err error) {
	if err := s.Validate(); err != nil {
		return 0, 0, err
	}

	total := uint64(s.BitOffset) + uint64(s.BitLength)
	return s.ByteOffset + total/8, uint8(total % 8), nil
}

// ByteRange returns the smallest enclosing half-open byte range [start, end).
func (s Span) ByteRange() (start uint64, end uint64, err error) {
	if err := s.Validate(); err != nil {
		return 0, 0, err
	}
	if s.BitLength == 0 {
		return s.ByteOffset, s.ByteOffset, nil
	}

	covered := (uint64(s.BitOffset) + uint64(s.BitLength) + 7) / 8
	if s.ByteOffset > math.MaxUint64-covered {
		return 0, 0, &SpanError{Span: s, Reason: "enclosing byte range overflows uint64"}
	}
	return s.ByteOffset, s.ByteOffset + covered, nil
}

// IsByteAligned reports whether both boundaries lie on byte boundaries.
func (s Span) IsByteAligned() bool {
	return s.BitOffset == 0 && s.BitLength%8 == 0
}

// Empty reports whether the span contains no bits.
func (s Span) Empty() bool { return s.BitLength == 0 }
