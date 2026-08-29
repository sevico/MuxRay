package cursor

import (
	"errors"
	"math"
	"testing"
)

func TestSpanValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		span    Span
		wantErr bool
	}{
		{name: "empty", span: Span{}},
		{name: "last bit offset", span: Span{ByteOffset: 12, BitOffset: 7, BitLength: 1}},
		{name: "largest length", span: Span{ByteOffset: 12, BitOffset: 7, BitLength: math.MaxUint32}},
		{name: "bad bit offset", span: Span{BitOffset: 8}, wantErr: true},
		{name: "end overflow", span: Span{ByteOffset: math.MaxUint64, BitOffset: 7, BitLength: 1}, wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := test.span.Validate()
			if (err != nil) != test.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, test.wantErr)
			}
			if test.wantErr {
				var spanErr *SpanError
				if !errors.As(err, &spanErr) {
					t.Fatalf("Validate() error type = %T, want *SpanError", err)
				}
				if !errors.Is(err, ErrInvalidSpan) {
					t.Fatalf("Validate() error %v does not match ErrInvalidSpan", err)
				}
			}
		})
	}
}

func TestNewSpan(t *testing.T) {
	t.Parallel()

	got, err := NewSpan(42, 3, 11)
	if err != nil {
		t.Fatalf("NewSpan() error = %v", err)
	}
	want := Span{ByteOffset: 42, BitOffset: 3, BitLength: 11}
	if got != want {
		t.Fatalf("NewSpan() = %+v, want %+v", got, want)
	}

	got, err = NewSpan(1, 9, 1)
	if !errors.Is(err, ErrInvalidSpan) {
		t.Fatalf("NewSpan() error = %v, want ErrInvalidSpan", err)
	}
	if got != (Span{}) {
		t.Fatalf("NewSpan() on error = %+v, want zero Span", got)
	}
}

func TestSpanEnd(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		span     Span
		wantByte uint64
		wantBit  uint8
	}{
		{name: "empty unaligned", span: Span{ByteOffset: 5, BitOffset: 3}, wantByte: 5, wantBit: 3},
		{name: "within byte", span: Span{ByteOffset: 5, BitOffset: 2, BitLength: 3}, wantByte: 5, wantBit: 5},
		{name: "at boundary", span: Span{ByteOffset: 5, BitOffset: 2, BitLength: 6}, wantByte: 6, wantBit: 0},
		{name: "across bytes", span: Span{ByteOffset: 5, BitOffset: 6, BitLength: 13}, wantByte: 7, wantBit: 3},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			gotByte, gotBit, err := test.span.End()
			if err != nil {
				t.Fatalf("End() error = %v", err)
			}
			if gotByte != test.wantByte || gotBit != test.wantBit {
				t.Fatalf("End() = (%d,%d), want (%d,%d)", gotByte, gotBit, test.wantByte, test.wantBit)
			}
		})
	}
}

func TestSpanByteRangeAndHelpers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		span      Span
		wantStart uint64
		wantEnd   uint64
		aligned   bool
		empty     bool
		wantErr   bool
	}{
		{name: "empty", span: Span{ByteOffset: 9, BitOffset: 4}, wantStart: 9, wantEnd: 9, empty: true},
		{name: "aligned", span: Span{ByteOffset: 9, BitLength: 16}, wantStart: 9, wantEnd: 11, aligned: true},
		{name: "one unaligned bit", span: Span{ByteOffset: 9, BitOffset: 7, BitLength: 1}, wantStart: 9, wantEnd: 10},
		{name: "unaligned two bytes", span: Span{ByteOffset: 9, BitOffset: 7, BitLength: 2}, wantStart: 9, wantEnd: 11},
		{name: "exclusive end overflow", span: Span{ByteOffset: math.MaxUint64, BitLength: 1}, wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			start, end, err := test.span.ByteRange()
			if (err != nil) != test.wantErr {
				t.Fatalf("ByteRange() error = %v, wantErr %v", err, test.wantErr)
			}
			if !test.wantErr && (start != test.wantStart || end != test.wantEnd) {
				t.Fatalf("ByteRange() = [%d,%d), want [%d,%d)", start, end, test.wantStart, test.wantEnd)
			}
			if got := test.span.IsByteAligned(); got != test.aligned {
				t.Errorf("IsByteAligned() = %v, want %v", got, test.aligned)
			}
			if got := test.span.Empty(); got != test.empty {
				t.Errorf("Empty() = %v, want %v", got, test.empty)
			}
		})
	}
}
