package document

import (
	"errors"
	"testing"

	"github.com/sevico/muxray/internal/flv"
	"github.com/sevico/muxray/internal/session"
)

func TestTrendPageNoSourceIOReadySummaryAndLatch(t *testing.T) {
	s := &filteredSource{testSource: source(t, fixture(script(), script()))}
	info, err := s.Stat()
	if err != nil {
		t.Fatal(err)
	}
	d := start(t.Context(), s, info, flv.Options{})
	t.Cleanup(func() { _ = d.Close(nil) })
	if err := scanDone(t, d); err != nil {
		t.Fatal(err)
	}
	if _, err := d.StartSummary(1); err != nil {
		t.Fatal(err)
	}
	ready := summaryResult(t, d, 1)
	if ready.Err != nil {
		t.Fatal(ready.Err)
	}
	reads, stats := s.reads.Load(), s.stats.Load()
	page, err := d.TrendPage(1, 512)
	if err != nil || len(page.Points) != 1 || page.ContextScanned != 1 || page.Points[0].Context != "found" || *page.Points[0].PreviousTagID != 0 || s.reads.Load() != reads || s.stats.Load() != stats {
		t.Fatalf("metadata query: %+v %v", page, err)
	}
	got, err := d.GetSummary(1)
	if err != nil || got.State != "ready" || got.Overview == nil || got.RequestID != ready.RequestID {
		t.Fatalf("summary changed: %+v %v", got, err)
	}
	d.mu.Lock()
	d.changed = true
	d.mu.Unlock()
	if _, err := d.TrendPage(0, 512); !errors.Is(err, ErrSourceChanged) {
		t.Fatalf("changed latch: %v", err)
	}
	if s.reads.Load() != reads || s.stats.Load() != stats {
		t.Fatal("latched query touched source")
	}
	_ = d.Close(nil)
	if _, err := d.TrendPage(0, 512); !errors.Is(err, session.ErrClosed) {
		t.Fatalf("closed: %v", err)
	}
}

func TestTrendPageWhileSourceWorkerBlocked(t *testing.T) {
	s := source(t, mp3Data(mp3Payload()))
	d := startTest(t, s, nil, DetailTimeout)
	if err := scanDone(t, d); err != nil {
		t.Fatal(err)
	}
	s.armed.Store(true)
	if _, err := d.StartSummary(1); err != nil {
		t.Fatal(err)
	}
	receive(t, s.entered)
	reads := s.reads.Load()
	page, err := d.TrendPage(0, 512)
	if err != nil || len(page.Points) != 1 || s.reads.Load() != reads {
		t.Fatalf("query blocked on worker: %+v %v", page, err)
	}
	got, err := d.GetSummary(1)
	if err != nil || got.State != "pending" {
		t.Fatalf("query superseded summary: %+v %v", got, err)
	}
	s.armed.Store(false)
	close(s.release)
	if got := summaryResult(t, d, 1); got.Err != nil || got.State != "ready" {
		t.Fatalf("summary did not survive: %+v", got)
	}
}
