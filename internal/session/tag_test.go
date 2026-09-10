package session

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/sevico/muxray/internal/flv"
)

func TestTagLookupReturnsIndependentTerminalRecords(t *testing.T) {
	data := fixture(1026)
	source := newSource(data)
	source.gateRead(13)
	s := Start(t.Context(), source, uint64(len(data)), flv.Options{})
	t.Cleanup(func() { source.releaseRead(); _ = s.Close(context.Background()) })
	<-source.readEntered
	if _, err := s.Tag(0); !errors.Is(err, ErrNotReady) {
		t.Fatalf("live lookup: %v", err)
	}
	source.releaseRead()
	if err := wait(t, s); err != nil {
		t.Fatal(err)
	}
	for _, id := range []uint64{0, 1023, 1024, 1025} {
		tag, err := s.Tag(id)
		if err != nil || tag.ID != id || tag.Offset != 13+15*id {
			t.Fatalf("tag %d: %+v %v", id, tag, err)
		}
		tag.ID = math.MaxUint64
		if unchanged, err := s.Tag(id); err != nil || unchanged.ID != id {
			t.Fatalf("lookup exposed mutable record: %+v %v", unchanged, err)
		}
	}
	for _, id := range []uint64{1026, math.MaxUint64} {
		if _, err := s.Tag(id); !errors.Is(err, ErrTagNotFound) {
			t.Fatalf("unindexed tag: %v", err)
		}
	}
	if err := s.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Tag(0); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed lookup: %v", err)
	}
}

func TestTagLookupAllowsCanceledCommittedPrefix(t *testing.T) {
	data := fixture(3)
	source := newSource(data)
	source.gateRead(28)
	s := Start(t.Context(), source, uint64(len(data)), flv.Options{})
	t.Cleanup(func() { source.releaseRead(); _ = s.Close(context.Background()) })
	<-source.readEntered
	s.Cancel()
	source.releaseRead()
	if err := wait(t, s); !errors.Is(err, context.Canceled) {
		t.Fatalf("scan: %v", err)
	}
	if tag, err := s.Tag(0); err != nil || tag.ID != 0 {
		t.Fatalf("committed tag: %+v %v", tag, err)
	}
	if _, err := s.Tag(1); !errors.Is(err, ErrTagNotFound) {
		t.Fatalf("uncommitted tag: %v", err)
	}
}
