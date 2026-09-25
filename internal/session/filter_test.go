package session

import (
	"context"
	"errors"
	"testing"

	"github.com/sevico/muxray/internal/flv"
)

func TestFilteredPageCanceledPrefixAndNoReads(t *testing.T) {
	source := newSource(fixture(2))
	source.gateRead(39)
	s := startTest(t, t.Context(), source, uint64(source.reader.Len()), flv.Options{})
	await(t, source.readEntered)
	filter := flv.TagFilter{Type: "script", Structure: "all"}
	if _, err := s.FilteredPage(0, 100, filter); !errors.Is(err, ErrNotReady) {
		t.Fatalf("running: %v", err)
	}
	s.Cancel()
	if _, err := s.FilteredPage(0, 100, filter); !errors.Is(err, ErrNotReady) {
		t.Fatalf("canceling: %v", err)
	}
	source.releaseRead()
	if err := wait(t, s); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	reads := source.reads.Load()
	page, err := s.FilteredPage(0, 100, filter)
	if err != nil || page.IndexedTotal != 1 || !page.Exhausted || len(page.Records) != 1 || page.Records[0].ID != 0 || source.reads.Load() != reads {
		t.Fatalf("canceled metadata: %+v %v", page, err)
	}
	_ = s.Close(t.Context())
	if _, err := s.FilteredPage(0, 100, filter); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed: %v", err)
	}
}
