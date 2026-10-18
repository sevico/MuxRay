package document

import (
	"context"
	"errors"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/sevico/muxray/internal/flv"
	"github.com/sevico/muxray/internal/session"
)

func timeResult(t *testing.T, d *Document, id uint64) TimeSearchResult {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		r, err := d.GetTimeSearch(id)
		if errors.Is(err, ErrSourceChanged) {
			return TimeSearchResult{RequestID: id, State: "failed", Err: err}
		}
		if err != nil {
			t.Fatal(err)
		}
		if r.State != "pending" && r.State != "canceling" {
			return r
		}
		select {
		case <-deadline:
			t.Fatal("time search timeout")
		default:
			runtime.Gosched()
		}
	}
}
func timeData(count int) []byte { return fixture(make([][]byte, count)...) }

func TestTimeSearchNoSourceIOIdentityAndClones(t *testing.T) {
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
	reads, stats := s.reads.Load(), s.stats.Load()
	if _, err := d.StartTimeSearch(1, 0, "duplicates", 1000); err != nil {
		t.Fatal(err)
	}
	r := timeResult(t, d, 1)
	if r.Err != nil || r.Page == nil || r.Page.Next != 65 || r.Page.NextCursor != 1 || !r.Page.ScanComplete {
		t.Fatalf("first %+v", r)
	}
	if s.reads.Load() != reads || s.stats.Load() != stats {
		t.Fatal("metadata search did source I/O")
	}
	*r.Page.Rows[0].Point.PreviousTagID, *r.Page.Rows[0].Point.PreviousDTS, *r.Page.Rows[0].Point.DeltaMS = 99, 99, 99
	r.Page.Rows[0].Kind = "mutated"
	again, err := d.StartTimeSearch(1, 0, "duplicates", 1000)
	if err != nil || again.State != "ready" || *again.Page.Rows[0].Point.PreviousTagID != 0 || *again.Page.Rows[0].Point.PreviousDTS != 0 || *again.Page.Rows[0].Point.DeltaMS != 0 || again.Page.Rows[0].Kind != "duplicate" {
		t.Fatalf("clone %+v %v", again, err)
	}
	if _, err = d.StartTimeSearch(1, 0, "all", 1000); !errors.Is(err, ErrInvalidRequest) {
		t.Fatal(err)
	}
	if _, err = d.StartTimeSearch(2, 1, "duplicates", 1001); !errors.Is(err, ErrUnknownCursor) {
		t.Fatal(err)
	}
	if _, err = d.StartHex(2, 0, 4); err != nil {
		t.Fatal(err)
	}
	if r := hexResult(t, d, 2); r.Err != nil {
		t.Fatal(r.Err)
	}
	saved, err := d.GetTimeSearch(1)
	if err != nil || saved.State != "ready" || saved.Page == nil {
		t.Fatalf("navigation lost result %+v %v", saved, err)
	}
	if _, err = d.StartTimeSearch(1, 1, "duplicates", 1000); !errors.Is(err, ErrStaleRequest) {
		t.Fatal(err)
	}
	for id := uint64(3); id < 5; id++ {
		if _, err = d.StartTimeSearch(id, 1, "duplicates", 1000); err != nil {
			t.Fatal(err)
		}
		next := timeResult(t, d, id)
		if next.Err != nil || next.Page.Start != 65 || next.Page.Next != 129 || *next.Page.Rows[0].Point.PreviousTagID != 64 {
			t.Fatalf("immutable cursor %+v", next)
		}
	}
	if _, err := d.GetTimeSearch(3); !errors.Is(err, ErrStaleRequest) {
		t.Fatal(err)
	}
}
func TestTimeSearchCursorEvictionAndRestart(t *testing.T) {
	d := startTest(t, source(t, timeData(2000)), nil, DetailTimeout)
	if err := scanDone(t, d); err != nil {
		t.Fatal(err)
	}
	var token uint64
	for id := uint64(1); id <= 10; id++ {
		if _, err := d.StartTimeSearch(id, token, "all", 1); err != nil {
			t.Fatal(err)
		}
		r := timeResult(t, d, id)
		if r.Err != nil || r.Page == nil || r.Page.Exhausted {
			t.Fatalf("%+v", r)
		}
		token = r.Page.NextCursor
	}
	if len(d.timeCursors) != MaxTimeSearchCursors {
		t.Fatal(len(d.timeCursors))
	}
	if _, err := d.StartTimeSearch(11, 1, "all", 1); !errors.Is(err, ErrUnknownCursor) {
		t.Fatal(err)
	}
	if _, err := d.StartTimeSearch(11, 0, "regressions", 1); err != nil {
		t.Fatal(err)
	}
	r := timeResult(t, d, 11)
	if r.Err != nil || !r.Page.Exhausted || r.Page.NextCursor != 0 || len(d.timeCursors) != 0 {
		t.Fatalf("restart %+v", r)
	}
	if _, err := d.StartTimeSearch(12, 10, "all", 1); !errors.Is(err, ErrUnknownCursor) {
		t.Fatal(err)
	}
}
func TestTimeSearchPendingWorkerFences(t *testing.T) {
	for _, action := range []string{"cancel", "supersede", "source_change", "close"} {
		t.Run(action, func(t *testing.T) {
			s := source(t, timeData(300))
			d := startTest(t, s, nil, DetailTimeout)
			if err := scanDone(t, d); err != nil {
				t.Fatal(err)
			}
			if _, err := d.StartTimeSearch(1, 0, "all", 1000); err != nil {
				t.Fatal(err)
			}
			first := timeResult(t, d, 1)
			if first.Page == nil {
				t.Fatal(first)
			}
			s.armed.Store(true)
			if _, err := d.StartHex(2, 0, 4); err != nil {
				t.Fatal(err)
			}
			receive(t, s.entered)
			token := uint64(1)
			if action == "cancel" {
				token = 0
			} // a canceled restart must retain old cursors
			if _, err := d.StartTimeSearch(3, token, "all", 1000); err != nil {
				t.Fatal(err)
			}
			if r, err := d.GetTimeSearch(3); err != nil || r.State != "pending" {
				t.Fatalf("worker bypass %+v %v", r, err)
			}
			switch action {
			case "cancel":
				r, err := d.CancelTimeSearch(3)
				if err != nil || !errors.Is(r.Err, context.Canceled) || r.Page != nil {
					t.Fatalf("cancel %+v %v", r, err)
				}
				if _, err = d.StartTimeSearch(3, token, "all", 1000); err != nil {
					t.Fatal(err)
				}
			case "supersede":
				if _, err := d.StartHex(4, 0, 4); err != nil {
					t.Fatal(err)
				}
			case "source_change":
				info, err := s.Stat()
				if err != nil {
					t.Fatal(err)
				}
				if err := os.Chtimes(s.Name(), info.ModTime(), info.ModTime().Add(time.Second)); err != nil {
					t.Fatal(err)
				}
			case "close":
				ctx, cancel := context.WithTimeout(t.Context(), time.Millisecond)
				defer cancel()
				if err := d.Close(ctx); !errors.Is(err, context.DeadlineExceeded) {
					t.Fatal(err)
				}
				if s.closes.Load() != 0 {
					t.Fatal("closed under reader")
				}
			}
			close(s.release)
			if action == "close" {
				if err := d.Close(t.Context()); err != nil {
					t.Fatal(err)
				}
				if _, err := d.GetTimeSearch(3); !errors.Is(err, session.ErrClosed) {
					t.Fatal(err)
				}
				if len(d.timeCursors) != 0 {
					t.Fatal("closed retained cursor")
				}
				return
			}
			if action == "source_change" {
				// The blocked Hex performs the existing source check; this search
				// only honors that latch and cannot publish its queued metadata.
				r := timeResult(t, d, 3)
				if !errors.Is(r.Err, ErrSourceChanged) || len(d.timeCursors) != 0 {
					t.Fatalf("latch %+v", r)
				}
				if _, err := d.StartTimeSearch(4, 0, "all", 1000); !errors.Is(err, ErrSourceChanged) {
					t.Fatal(err)
				}
				return
			}
			r := timeResult(t, d, 3)
			if !errors.Is(r.Err, context.Canceled) || r.Page != nil || len(d.timeCursors) != 1 {
				t.Fatalf("fence %+v", r)
			}
			if _, err := d.StartTimeSearch(5, 1, "all", 1000); err != nil {
				t.Fatal(err)
			}
			r = timeResult(t, d, 5)
			if r.Err != nil || r.Page.Start != 65 || r.Page.Next != 129 || *r.Page.Rows[0].Point.PreviousTagID != 64 {
				t.Fatalf("canceled work mutated cursor %+v", r)
			}
		})
	}
}
func TestTimeSearchDeadlineCannotCommit(t *testing.T) {
	d := startTest(t, source(t, timeData(300)), nil, -time.Nanosecond)
	if err := scanDone(t, d); err != nil {
		t.Fatal(err)
	}
	if _, err := d.StartTimeSearch(1, 0, "all", 1); err != nil {
		t.Fatal(err)
	}
	r := timeResult(t, d, 1)
	if !errors.Is(r.Err, context.DeadlineExceeded) || r.Page != nil || len(d.timeCursors) != 0 {
		t.Fatalf("deadline %+v", r)
	}
}
