package session

import (
	"context"
	"errors"
	"testing"

	"github.com/sevico/muxray/internal/flv"
)

func TestTrendPageCanceledPrefixAndNoReads(t *testing.T) {
	source := newSource(fixture(2))
	source.gateRead(39)
	s := startTest(t, t.Context(), source, uint64(source.reader.Len()), flv.Options{})
	await(t, source.readEntered)
	if _, err := s.TrendPage(0, 512); !errors.Is(err, ErrNotReady) {
		t.Fatalf("running: %v", err)
	}
	s.Cancel()
	if _, err := s.TrendPage(0, 512); !errors.Is(err, ErrNotReady) {
		t.Fatalf("canceling: %v", err)
	}
	source.releaseRead()
	if err := wait(t, s); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	reads := source.reads.Load()
	page, err := s.TrendPage(0, 512)
	if err != nil || page.Total != 1 || page.Next != 1 || len(page.Points) != 1 || page.Points[0].TagID != 0 || page.Points[0].Context != "start" || source.reads.Load() != reads {
		t.Fatalf("canceled prefix: %+v %v", page, err)
	}
	_ = s.Close(t.Context())
	if _, err := s.TrendPage(0, 512); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed: %v", err)
	}
}
