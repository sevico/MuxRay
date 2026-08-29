package cursor

import (
	"bytes"
	"errors"
	"io"
	"math"
	"testing"
)

func TestByteReaderMetadataAndMovement(t *testing.T) {
	t.Parallel()

	r := NewByteReader(bytes.NewReader(make([]byte, 10)), 10)
	if got := r.Size(); got != 10 {
		t.Fatalf("Size() = %d, want 10", got)
	}
	if got := r.Offset(); got != 0 {
		t.Fatalf("Offset() = %d, want 0", got)
	}
	if got := r.Remaining(); got != 10 {
		t.Fatalf("Remaining() = %d, want 10", got)
	}
	if err := r.Seek(3); err != nil {
		t.Fatalf("Seek(3) error = %v", err)
	}
	if err := r.Skip(4); err != nil {
		t.Fatalf("Skip(4) error = %v", err)
	}
	if got := r.Offset(); got != 7 {
		t.Fatalf("Offset() = %d, want 7", got)
	}
	if got := r.Remaining(); got != 3 {
		t.Fatalf("Remaining() = %d, want 3", got)
	}
	if err := r.Seek(10); err != nil {
		t.Fatalf("Seek(end) error = %v", err)
	}
	if got := r.Remaining(); got != 0 {
		t.Fatalf("Remaining() at end = %d, want 0", got)
	}
}

func TestByteReaderMovementBoundsAreAtomic(t *testing.T) {
	t.Parallel()

	r := NewByteReader(bytes.NewReader(make([]byte, 5)), 5)
	if err := r.Seek(2); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		run  func() error
	}{
		{name: "seek", run: func() error { return r.Seek(6) }},
		{name: "skip", run: func() error { return r.Skip(4) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			before := r.Offset()
			err := test.run()
			if !errors.Is(err, ErrOutOfBounds) {
				t.Fatalf("error = %v, want ErrOutOfBounds", err)
			}
			var boundsErr *BoundsError
			if !errors.As(err, &boundsErr) {
				t.Fatalf("error type = %T, want *BoundsError", err)
			}
			if got := r.Offset(); got != before {
				t.Fatalf("Offset() after error = %d, want unchanged %d", got, before)
			}
		})
	}
}

func TestByteReaderReadBytes(t *testing.T) {
	t.Parallel()

	r := NewByteReader(bytes.NewReader([]byte{0, 1, 2, 3, 4}), 5)
	got, err := r.ReadBytes(3)
	if err != nil {
		t.Fatalf("ReadBytes(3) error = %v", err)
	}
	if !bytes.Equal(got, []byte{0, 1, 2}) {
		t.Fatalf("ReadBytes(3) = %v, want [0 1 2]", got)
	}
	if got := r.Offset(); got != 3 {
		t.Fatalf("Offset() = %d, want 3", got)
	}

	empty, err := r.ReadBytes(0)
	if err != nil {
		t.Fatalf("ReadBytes(0) error = %v", err)
	}
	if empty == nil || len(empty) != 0 {
		t.Fatalf("ReadBytes(0) = %#v, want non-nil empty slice", empty)
	}
	if got := r.Offset(); got != 3 {
		t.Fatalf("Offset() after empty read = %d, want 3", got)
	}

	before := r.Offset()
	got, err = r.ReadBytes(3)
	if !errors.Is(err, ErrOutOfBounds) {
		t.Fatalf("ReadBytes beyond end error = %v, want ErrOutOfBounds", err)
	}
	if got != nil {
		t.Fatalf("ReadBytes beyond end = %v, want nil", got)
	}
	if r.Offset() != before {
		t.Fatalf("Offset() after bounds error = %d, want %d", r.Offset(), before)
	}
}

func TestByteReaderAllocatingReadLimit(t *testing.T) {
	r := NewByteReader(zeroReaderAt{}, MaxReadBytes)
	data, err := r.ReadBytes(MaxReadBytes)
	if err != nil {
		t.Fatalf("ReadBytes(exact limit) error = %v", err)
	}
	if uint64(len(data)) != MaxReadBytes || r.Offset() != MaxReadBytes {
		t.Fatalf("exact-limit read length=%d offset=%d", len(data), r.Offset())
	}

	r = NewByteReader(zeroReaderAt{}, MaxReadBytes+1)
	if _, err := r.ReadBytes(MaxReadBytes + 1); !errors.Is(err, ErrReadLimit) {
		t.Fatalf("ReadBytes(over limit) error = %v, want ErrReadLimit", err)
	} else {
		var limitErr *ReadLimitError
		if !errors.As(err, &limitErr) || limitErr.Length != MaxReadBytes+1 || limitErr.Limit != MaxReadBytes {
			t.Fatalf("ReadLimitError = %+v", limitErr)
		}
	}
	if r.Offset() != 0 {
		t.Fatalf("over-limit read changed offset to %d", r.Offset())
	}
}

func TestByteReaderReadInto(t *testing.T) {
	t.Parallel()

	r := NewByteReader(bytes.NewReader([]byte{1, 2, 3, 4}), 4)
	destination := make([]byte, 3)
	if err := r.ReadInto(destination); err != nil {
		t.Fatalf("ReadInto() error = %v", err)
	}
	if !bytes.Equal(destination, []byte{1, 2, 3}) || r.Offset() != 3 {
		t.Fatalf("ReadInto() = %v at offset %d, want [1 2 3] at 3", destination, r.Offset())
	}

	short := NewByteReader(bytes.NewReader([]byte{9}), 2)
	partial := make([]byte, 2)
	if err := short.ReadInto(partial); !errors.Is(err, io.EOF) {
		t.Fatalf("partial ReadInto() error = %v, want wrapped io.EOF", err)
	}
	if partial[0] != 9 || short.Offset() != 0 {
		t.Fatalf("partial ReadInto() = %v at offset %d, want partial data and unchanged offset", partial, short.Offset())
	}

	before := r.Offset()
	if err := r.ReadInto(make([]byte, 2)); !errors.Is(err, ErrOutOfBounds) {
		t.Fatalf("bounded ReadInto() error = %v, want ErrOutOfBounds", err)
	}
	if r.Offset() != before {
		t.Fatalf("failed ReadInto() changed offset from %d to %d", before, r.Offset())
	}
}

func TestByteReaderIntegers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		data  []byte
		read  func(*ByteReader) (uint64, error)
		want  uint64
		width uint64
	}{
		{name: "u8", data: []byte{0xab}, read: func(r *ByteReader) (uint64, error) { v, e := r.ReadU8(); return uint64(v), e }, want: 0xab, width: 1},
		{name: "u16", data: []byte{0x12, 0x34}, read: func(r *ByteReader) (uint64, error) { v, e := r.ReadU16BE(); return uint64(v), e }, want: 0x1234, width: 2},
		{name: "u24", data: []byte{0x12, 0x34, 0x56}, read: func(r *ByteReader) (uint64, error) { v, e := r.ReadU24BE(); return uint64(v), e }, want: 0x123456, width: 3},
		{name: "u32", data: []byte{0x12, 0x34, 0x56, 0x78}, read: func(r *ByteReader) (uint64, error) { v, e := r.ReadU32BE(); return uint64(v), e }, want: 0x12345678, width: 4},
		{name: "u64", data: []byte{0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd, 0xef}, read: func(r *ByteReader) (uint64, error) { return r.ReadU64BE() }, want: 0x0123456789abcdef, width: 8},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			r := NewByteReader(bytes.NewReader(test.data), uint64(len(test.data)))
			got, err := test.read(r)
			if err != nil {
				t.Fatalf("read error = %v", err)
			}
			if got != test.want {
				t.Fatalf("read = %#x, want %#x", got, test.want)
			}
			if r.Offset() != test.width {
				t.Fatalf("Offset() = %d, want %d", r.Offset(), test.width)
			}
		})
	}
}

func TestByteReaderBackingReadFailureIsAtomic(t *testing.T) {
	t.Parallel()

	r := NewByteReader(bytes.NewReader([]byte{1, 2}), 4)
	got, err := r.ReadBytes(4)
	if !errors.Is(err, io.EOF) {
		t.Fatalf("ReadBytes() error = %v, want wrapped io.EOF", err)
	}
	if !bytes.Equal(got, []byte{1, 2}) {
		t.Fatalf("ReadBytes() partial data = %v, want [1 2]", got)
	}
	if got := r.Offset(); got != 0 {
		t.Fatalf("Offset() after backing read failure = %d, want 0", got)
	}
}

func TestByteReaderAcceptsFullReadWithEOF(t *testing.T) {
	t.Parallel()

	r := NewByteReader(fullReadEOFReaderAt{data: []byte{0x12, 0x34, 0x56, 0x78}}, 4)
	value, err := r.ReadU32BE()
	if err != nil {
		t.Fatalf("ReadU32BE() error = %v", err)
	}
	if value != 0x12345678 || r.Offset() != 4 {
		t.Fatalf("ReadU32BE() = %#x at offset %d", value, r.Offset())
	}

	r = NewByteReader(fullReadEOFReaderAt{data: []byte{9, 8}}, 2)
	destination := make([]byte, 2)
	if err := r.ReadInto(destination); err != nil {
		t.Fatalf("ReadInto() error = %v", err)
	}
	if !bytes.Equal(destination, []byte{9, 8}) || r.Offset() != 2 {
		t.Fatalf("ReadInto() = %v at offset %d", destination, r.Offset())
	}
}

func TestByteReaderNilAndRepresentationErrors(t *testing.T) {
	t.Parallel()

	nilReader := NewByteReader(nil, 1)
	if _, err := nilReader.ReadU8(); !errors.Is(err, ErrNilReader) {
		t.Fatalf("nil reader error = %v, want ErrNilReader", err)
	}

	offsetReader := NewByteReader(bytes.NewReader(nil), math.MaxUint64)
	if err := offsetReader.Seek(uint64(math.MaxInt64) + 1); err != nil {
		t.Fatalf("Seek() error = %v", err)
	}
	if _, err := offsetReader.ReadBytes(1); !errors.Is(err, ErrOffsetOverflow) {
		t.Fatalf("unrepresentable offset error = %v, want ErrOffsetOverflow", err)
	}
	if got := offsetReader.Offset(); got != uint64(math.MaxInt64)+1 {
		t.Fatalf("Offset() after representation error = %d, want unchanged", got)
	}

	lengthReader := NewByteReader(bytes.NewReader(nil), math.MaxUint64)
	if _, err := lengthReader.ReadBytes(uint64(maxInt()) + 1); !errors.Is(err, ErrLengthOverflow) {
		t.Fatalf("unrepresentable length error = %v, want ErrLengthOverflow", err)
	}
}

func FuzzByteReader(f *testing.F) {
	f.Add([]byte{}, uint64(0), uint64(0))
	f.Add([]byte{0x46, 0x4c, 0x56}, uint64(0), uint64(3))
	f.Add([]byte{0, 1, 2, 3}, uint64(3), uint64(2))
	f.Add([]byte{0xff}, uint64(math.MaxUint64), uint64(math.MaxUint64))

	f.Fuzz(func(t *testing.T, data []byte, seek uint64, count uint64) {
		r := NewByteReader(bytes.NewReader(data), uint64(len(data)))
		before := r.Offset()
		if err := r.Seek(seek); err != nil {
			if !errors.Is(err, ErrOutOfBounds) {
				t.Fatalf("Seek(%d) error = %v", seek, err)
			}
			if r.Offset() != before {
				t.Fatalf("failed Seek changed offset to %d", r.Offset())
			}
			return
		}

		before = r.Offset()
		got, err := r.ReadBytes(count)
		if err != nil {
			if r.Offset() != before {
				t.Fatalf("failed ReadBytes changed offset from %d to %d", before, r.Offset())
			}
			return
		}
		if uint64(len(got)) != count {
			t.Fatalf("ReadBytes length = %d, want %d", len(got), count)
		}
		if r.Offset() != before+count {
			t.Fatalf("successful ReadBytes offset = %d, want %d", r.Offset(), before+count)
		}
	})
}

type fullReadEOFReaderAt struct {
	data []byte
}

type zeroReaderAt struct{}

func (zeroReaderAt) ReadAt(destination []byte, _ int64) (int, error) {
	clear(destination)
	return len(destination), nil
}

func (r fullReadEOFReaderAt) ReadAt(destination []byte, offset int64) (int, error) {
	if offset < 0 || offset >= int64(len(r.data)) {
		return 0, io.EOF
	}
	read := copy(destination, r.data[offset:])
	return read, io.EOF
}
