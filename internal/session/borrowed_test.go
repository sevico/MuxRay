package session

import (
	"context"
	"errors"
	"testing"

	"github.com/sevico/muxray/internal/flv"
)

func TestStartBorrowsSourceUntilWorkerFinishes(t *testing.T) {
	data := fixture(3)
	source := newSource(data)
	source.gateRead(28) // before the second tag
	s := Start(t.Context(), source, uint64(len(data)), flv.Options{})
	t.Cleanup(func() { source.releaseRead(); _ = s.Close(context.Background()) })
	<-source.readEntered
	s.Cancel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.Close(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("blocked borrowed scan: %v", err)
	}
	if source.closes.Load() != 0 {
		t.Fatal("borrower closed active source")
	}
	source.releaseRead()
	if err := wait(t, s); !errors.Is(err, context.Canceled) {
		t.Fatalf("scan outcome: %v", err)
	}
	if source.closes.Load() != 0 {
		t.Fatal("borrower closed caller-owned source after cancellation")
	}
	if err := source.Close(); err != nil || source.closes.Load() != 1 || source.closeDuringRead.Load() {
		t.Fatal("caller could not safely close source after Wait")
	}
}

func TestStartNilBorrowedReaderReturnsFailure(t *testing.T) {
	for _, size := range []uint64{0, 13} {
		s := Start(nil, nil, size, flv.Options{})
		if err := wait(t, s); err == nil || s.Snapshot().State != StateFailed {
			t.Fatalf("nil reader: %v %+v", err, s.Snapshot())
		}
		_ = s.Close(t.Context())
	}
}
