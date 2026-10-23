package document

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/sevico/muxray/internal/flv"
	"github.com/sevico/muxray/internal/session"
)

func TestSnapshotTimeSearchCloneNoIOAndLifecycle(t *testing.T) {
	s := &filteredSource{testSource: source(t, timeData(300))}
	info, err := s.Stat()
	if err != nil {
		t.Fatal(err)
	}
	d := start(t.Context(), s, info, flv.Options{})
	t.Cleanup(func() { _ = d.Close(nil) })
	if err := scanDone(t, d); err != nil {
		t.Fatal(err)
	}
	if _, err := d.SnapshotTimeSearch(0); !errors.Is(err, ErrInvalidRequest) {
		t.Fatal(err)
	}
	if _, err := d.SnapshotTimeSearch(1); !errors.Is(err, ErrUnknownRequest) {
		t.Fatal(err)
	}
	if _, err := d.StartTimeSearch(1, 0, "duplicates", 1000); err != nil {
		t.Fatal(err)
	}
	if r := timeResult(t, d, 1); r.Err != nil {
		t.Fatal(r.Err)
	}
	if _, err := d.StartHex(2, 0, 4); err != nil {
		t.Fatal(err)
	}
	if r := hexResult(t, d, 2); r.Err != nil {
		t.Fatal(r.Err)
	}
	reads, stats := s.reads.Load(), s.stats.Load()
	r, err := d.SnapshotTimeSearch(1)
	if err != nil || r.Page == nil || len(r.Page.Rows) != 64 {
		t.Fatalf("%+v %v", r, err)
	}
	r.Page.Rows[0].Kind = "mutated"
	*r.Page.Rows[0].Point.PreviousTagID = 99
	*r.Page.Rows[0].Point.PreviousDTS = 99
	*r.Page.Rows[0].Point.DeltaMS = 99
	r.Page.Rows = nil
	again, err := d.SnapshotTimeSearch(1)
	if err != nil || again.Page.Rows[0].Kind != "duplicate" || *again.Page.Rows[0].Point.PreviousTagID != 0 || *again.Page.Rows[0].Point.PreviousDTS != 0 || *again.Page.Rows[0].Point.DeltaMS != 0 {
		t.Fatal("aliased retained page", err)
	}
	if s.reads.Load() != reads || s.stats.Load() != stats {
		t.Fatal("snapshot touched source")
	}
	if _, err := d.StartTimeSearch(3, 1, "duplicates", 1000); err != nil {
		t.Fatal(err)
	}
	if _, err := d.SnapshotTimeSearch(1); !errors.Is(err, ErrStaleRequest) {
		t.Fatal(err)
	}
	if r := timeResult(t, d, 3); r.Err != nil {
		t.Fatal(r.Err)
	}
	if err := os.Chtimes(s.Name(), info.ModTime(), info.ModTime().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	// An undiscovered change is deliberately not probed during a capture.
	if _, err := d.SnapshotTimeSearch(3); err != nil {
		t.Fatal(err)
	}
	if _, err := d.StartHex(4, 0, 4); err != nil {
		t.Fatal(err)
	}
	if r := hexResult(t, d, 4); !errors.Is(r.Err, ErrSourceChanged) {
		t.Fatal(r.Err)
	}
	if _, err := d.SnapshotTimeSearch(3); !errors.Is(err, ErrSourceChanged) {
		t.Fatal(err)
	}
	if err := d.Close(nil); err != nil {
		t.Fatal(err)
	}
	if _, err := d.SnapshotTimeSearch(3); !errors.Is(err, session.ErrClosed) {
		t.Fatal(err)
	}
}

func TestSnapshotTimeSearchPendingAndCanceledPage(t *testing.T) {
	s := source(t, timeData(300))
	d := startTest(t, s, nil, DetailTimeout)
	if err := scanDone(t, d); err != nil {
		t.Fatal(err)
	}
	s.armed.Store(true)
	if _, err := d.StartHex(1, 0, 4); err != nil {
		t.Fatal(err)
	}
	receive(t, s.entered)
	defer close(s.release)
	if _, err := d.StartTimeSearch(2, 0, "all", 1); err != nil {
		t.Fatal(err)
	}
	r, err := d.SnapshotTimeSearch(2)
	if err != nil || r.State != "pending" || r.Page != nil {
		t.Fatalf("%+v %v", r, err)
	}
	if _, err := d.CancelTimeSearch(2); err != nil {
		t.Fatal(err)
	}
	r, err = d.SnapshotTimeSearch(2)
	if err != nil || r.State != "failed" || !errors.Is(r.Err, context.Canceled) || r.Page != nil {
		t.Fatalf("%+v %v", r, err)
	}
}
