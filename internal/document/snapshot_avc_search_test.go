package document

import (
	"errors"
	"os"
	"testing"
	"time"

	"github.com/sevico/muxray/internal/flv"
	"github.com/sevico/muxray/internal/session"
)

func TestSnapshotAVCSearchNoIOCloneAndSupersession(t *testing.T) {
	s := &filteredSource{testSource: source(t, avcData(4, 0, true))}
	info, err := s.Stat()
	if err != nil {
		t.Fatal(err)
	}
	d := start(t.Context(), s, info, flv.Options{})
	t.Cleanup(func() { _ = d.Close(nil) })
	if err := scanDone(t, d); err != nil {
		t.Fatal(err)
	}
	if _, err := d.SnapshotAVCSearch(0); !errors.Is(err, ErrInvalidRequest) {
		t.Fatal(err)
	}
	if _, err := d.SnapshotAVCSearch(1); !errors.Is(err, ErrUnknownRequest) {
		t.Fatal(err)
	}
	if _, err := d.StartAVCSearch(1, 0, "all"); err != nil {
		t.Fatal(err)
	}
	if got := searchResult(t, d, 1); got.Err != nil {
		t.Fatal(got.Err)
	}
	if _, err := d.StartHex(2, 0, 4); err != nil {
		t.Fatal(err)
	}
	if got := hexResult(t, d, 2); got.Err != nil {
		t.Fatal(got.Err)
	}
	reads, stats := s.reads.Load(), s.stats.Load()
	got, err := d.SnapshotAVCSearch(1)
	if err != nil || got.State != "ready" || got.Page == nil || len(got.Page.Rows) != 1 {
		t.Fatalf("retained: %+v %v", got, err)
	}
	got.Page.Rows[0].ObservedIDR = "mutated"
	got.Page.Rows[0].Span.BitLength = 0
	*got.Page.Rows[0].DeclaredFrameType = 0
	again, err := d.SnapshotAVCSearch(1)
	if err != nil || again.Page.Rows[0].ObservedIDR != "yes" || again.Page.Rows[0].Span.BitLength != 5 || *again.Page.Rows[0].DeclaredFrameType != 2 {
		t.Fatal("retained result aliases caller")
	}
	if s.reads.Load() != reads || s.stats.Load() != stats {
		t.Fatal("snapshot touched source")
	}
	if _, err := d.StartAVCSearch(3, 0, "unknown"); err != nil {
		t.Fatal(err)
	}
	if _, err := d.SnapshotAVCSearch(1); !errors.Is(err, ErrStaleRequest) {
		t.Fatal(err)
	}
	if got := searchResult(t, d, 3); got.Err != nil {
		t.Fatal(got.Err)
	}
	if err := d.Close(nil); err != nil {
		t.Fatal(err)
	}
	if _, err := d.SnapshotAVCSearch(3); !errors.Is(err, session.ErrClosed) {
		t.Fatal(err)
	}
}

func TestSnapshotAVCSearchPendingAndChangedSource(t *testing.T) {
	s := source(t, avcData(4, 0, true))
	d := startTest(t, s, nil, DetailTimeout)
	if err := scanDone(t, d); err != nil {
		t.Fatal(err)
	}
	s.blockOffset = 24
	s.armed.Store(true)
	if _, err := d.StartAVCSearch(1, 0, "all"); err != nil {
		t.Fatal(err)
	}
	receive(t, s.entered)
	got, err := d.SnapshotAVCSearch(1)
	if err != nil || got.State != "pending" || got.Page != nil {
		t.Fatalf("pending: %+v %v", got, err)
	}
	info, err := s.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(s.Name(), info.ModTime(), info.ModTime().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	s.armed.Store(false)
	close(s.release)
	if got := searchResult(t, d, 1); !errors.Is(got.Err, ErrSourceChanged) {
		t.Fatalf("source change: %+v", got)
	}
	if _, err := d.SnapshotAVCSearch(1); !errors.Is(err, ErrSourceChanged) {
		t.Fatal(err)
	}
}
