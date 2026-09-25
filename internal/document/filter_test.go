package document

import (
	"errors"
	"os"
	"sync/atomic"
	"testing"

	"github.com/sevico/muxray/internal/flv"
	"github.com/sevico/muxray/internal/session"
)

type filteredSource struct {
	*testSource
	stats atomic.Int32
}

func (s *filteredSource) Stat() (os.FileInfo, error) { s.stats.Add(1); return s.File.Stat() }

func TestFilteredPageNoSourceIOAndReadySummaryRetained(t *testing.T) {
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
	filter := flv.TagFilter{Type: "script", Structure: "all"}
	page, err := d.FilteredPage(0, 100, filter)
	if err != nil || len(page.Records) != 2 || !page.Exhausted || s.reads.Load() != reads || s.stats.Load() != stats {
		t.Fatalf("query touched source: %+v %v reads=%d stats=%d", page, err, s.reads.Load()-reads, s.stats.Load()-stats)
	}
	got, err := d.GetSummary(1)
	if err != nil || got.State != "ready" || got.Overview == nil || got.RequestID != ready.RequestID {
		t.Fatalf("query invalidated summary: %+v %v", got, err)
	}
	d.mu.Lock()
	d.changed = true
	d.mu.Unlock()
	if _, err := d.FilteredPage(0, 100, filter); !errors.Is(err, ErrSourceChanged) {
		t.Fatalf("source-change latch: %v", err)
	}
	if s.reads.Load() != reads || s.stats.Load() != stats {
		t.Fatal("latch query touched source")
	}
	_ = d.Close(nil)
	if _, err := d.FilteredPage(0, 100, filter); !errors.Is(err, session.ErrClosed) {
		t.Fatalf("closed: %v", err)
	}
}

func TestFilteredPageDuringBlockedSummaryDoesNotCancelWork(t *testing.T) {
	data := fixture(mp3Payload())
	data[13] = byte(flv.TagTypeAudio)
	s := source(t, data)
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
	page, err := d.FilteredPage(0, 100, flv.TagFilter{Type: "all", Structure: "all"})
	if err != nil || len(page.Records) != 1 || s.reads.Load() != reads {
		t.Fatalf("metadata blocked behind source read: %+v %v", page, err)
	}
	got, err := d.GetSummary(1)
	if err != nil || got.State != "pending" {
		t.Fatalf("query superseded active work: %+v %v", got, err)
	}
	s.armed.Store(false)
	close(s.release)
	if got := summaryResult(t, d, 1); got.Err != nil || got.State != "ready" {
		t.Fatalf("Summary completion: %+v", got)
	}
}
