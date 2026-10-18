package session

import (
	"context"
	"errors"
	"testing"

	"github.com/sevico/muxray/internal/flv"
	"github.com/sevico/muxray/internal/timesearch"
)

func TestTimeSearchCanceledPrefixAndNoReads(t *testing.T) {
	source := newSource(fixture(2))
	source.gateRead(39)
	s := startTest(t, t.Context(), source, uint64(source.reader.Len()), flv.Options{})
	await(t, source.readEntered)
	if _, _, err := s.TimeSearch(t.Context(), timesearch.State{}, "all", 1000); !errors.Is(err, ErrNotReady) {
		t.Fatal(err)
	}
	s.Cancel()
	source.releaseRead()
	if err := wait(t, s); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	reads := source.reads.Load()
	page, _, err := s.TimeSearch(t.Context(), timesearch.State{}, "all", 1000)
	if err != nil || page.Total != 1 || page.Next != 1 || page.ScanComplete || !page.Exhausted || page.Scanned != 1 || source.reads.Load() != reads {
		t.Fatalf("prefix %+v %v", page, err)
	}
	_ = s.Close(t.Context())
	if _, _, err = s.TimeSearch(t.Context(), timesearch.State{}, "all", 1000); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
}
