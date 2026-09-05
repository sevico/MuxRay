package cursor

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
)

func TestContextReader(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	source := &cancelReadAt{cancel: cancel}
	reader := NewByteReader(WithContext(ctx, source), 1)
	var p [1]byte
	if err := reader.ReadInto(p[:]); !errors.Is(err, context.Canceled) || reader.Offset() != 0 {
		t.Fatalf("canceled full read advanced cursor: %v", err)
	}
	if _, err := WithContext(ctx, source).ReadAt(p[:], 0); !errors.Is(err, context.Canceled) || source.calls != 1 {
		t.Fatal("pre-canceled read reached source")
	}
	if _, err := WithContext(nil, nil).ReadAt(p[:], 0); !errors.Is(err, ErrNilReader) {
		t.Fatal("nil source error lost")
	}
	if n, err := WithContext(nil, bytes.NewReader([]byte{42})).ReadAt(p[:], 0); err != nil || n != 1 || p[0] != 42 {
		t.Fatal("ordinary read changed")
	}
	if n, err := WithContext(nil, bytes.NewReader(nil)).ReadAt(p[:], 0); !errors.Is(err, io.EOF) || n != 0 {
		t.Fatal("EOF changed")
	}
}

type cancelReadAt struct {
	cancel context.CancelFunc
	calls  int
}

func (r *cancelReadAt) ReadAt(p []byte, _ int64) (int, error) {
	r.calls++
	clear(p)
	r.cancel()
	return len(p), io.EOF
}
