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

func searchResult(t *testing.T, d *Document, id uint64) AVCSearchResult {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		r, err := d.GetAVCSearch(id)
		if err != nil {
			t.Fatal(err)
		}
		if r.State != "pending" && r.State != "canceling" {
			return r
		}
		select {
		case <-deadline:
			t.Fatal("search did not finish")
		default:
			runtime.Gosched()
		}
	}
}
func TestAVCSearchIdentityClonesAndRetention(t *testing.T) {
	s := source(t, avcData(4, 3, true))
	d := startTest(t, s, nil, DetailTimeout)
	if err := scanDone(t, d); err != nil {
		t.Fatal(err)
	}
	if _, err := d.GetAVCSearch(1); !errors.Is(err, ErrUnknownRequest) {
		t.Fatal(err)
	}
	if _, err := d.StartAVCSearch(1, 0, "all"); err != nil {
		t.Fatal(err)
	}
	got := searchResult(t, d, 1)
	if got.Err != nil || got.Page == nil || len(got.Page.Rows) != 1 || got.Page.Next != 5 || got.Page.Rows[0].ObservedIDR != "yes" || !got.Page.ScanComplete {
		t.Fatalf("%+v", got)
	}
	got.Page.Rows[0].Span.ByteOffset = 0
	*got.Page.Rows[0].DeclaredFrameType = 9
	got.Page.Rows[0].ObservedIDR = "mutated"
	reads := s.reads.Load()
	again, err := d.StartAVCSearch(1, 0, "all")
	if err != nil || again.State != "ready" || again.Page.Rows[0].Span.ByteOffset == 0 || *again.Page.Rows[0].DeclaredFrameType != 2 || again.Page.Rows[0].ObservedIDR != "yes" || s.reads.Load() != reads {
		t.Fatalf("clone/recovery %+v %v", again, err)
	}
	for _, q := range []struct {
		start  uint64
		filter string
	}{{1, "all"}, {0, "unknown"}, {0, ""}, {6, "all"}} {
		if _, err := d.StartAVCSearch(1, q.start, q.filter); !errors.Is(err, ErrInvalidRequest) {
			t.Fatal(err)
		}
	}
	if _, err := d.StartAVCSearch(2, 6, "all"); !errors.Is(err, ErrInvalidRequest) {
		t.Fatal(err)
	}
	if _, err := d.StartDetail(2, 4); err != nil || result(t, d, 2).Err != nil {
		t.Fatal(err)
	}
	if _, err := d.StartHex(3, 24, 2); err != nil || hexResult(t, d, 3).Err != nil {
		t.Fatal(err)
	}
	cached, err := d.GetAVCSearch(1)
	if err != nil || cached.State != "ready" || cached.Page.Rows[0].ObservedIDR != "yes" {
		t.Fatalf("retained %+v %v", cached, err)
	}
	if canceled, err := d.CancelAVCSearch(1); err != nil || canceled.State != "ready" {
		t.Fatalf("late cancel %+v %v", canceled, err)
	}
	if _, err := d.StartAVCSearch(1, 0, "all"); !errors.Is(err, ErrStaleRequest) {
		t.Fatal(err)
	}
	if _, err := d.StartAVCSearch(3, 0, "all"); !errors.Is(err, ErrInvalidRequest) {
		t.Fatal(err)
	}
	if _, err := d.StartAVCSearch(4, 5, "all"); err != nil {
		t.Fatal(err)
	}
	end := searchResult(t, d, 4)
	if end.Err != nil || end.Page.Next != 5 || end.Page.Scanned != 0 || !end.Page.Exhausted || len(end.Page.Rows) != 0 {
		t.Fatalf("end %+v", end)
	}
	if _, err := d.GetAVCSearch(1); !errors.Is(err, ErrStaleRequest) {
		t.Fatal(err)
	}
}
func TestAVCSearchWorkerFences(t *testing.T) {
	for _, action := range []string{"cancel", "supersede", "deadline", "source_change", "close"} {
		t.Run(action, func(t *testing.T) {
			s := source(t, avcData(4, 0, true))
			timeout := DetailTimeout
			if action == "deadline" {
				timeout = 20 * time.Millisecond
			}
			d := startTest(t, s, nil, timeout)
			if err := scanDone(t, d); err != nil {
				t.Fatal(err)
			}
			s.blockOffset = 24
			s.armed.Store(true)
			if _, err := d.StartAVCSearch(1, 0, "all"); err != nil {
				t.Fatal(err)
			}
			receive(t, s.entered)
			want := context.Canceled
			switch action {
			case "cancel":
				canceled, err := d.CancelAVCSearch(1)
				if err != nil || canceled.State != "canceling" || canceled.Page != nil {
					t.Fatalf("cancel %+v %v", canceled, err)
				}
				again, err := d.StartAVCSearch(1, 0, "all")
				if err != nil || again.State != "canceling" {
					t.Fatalf("recovery %+v %v", again, err)
				}
			case "supersede":
				if _, err := d.StartHex(2, 0, 4); err != nil {
					t.Fatal(err)
				}
				canceled, err := d.GetAVCSearch(1)
				if err != nil || canceled.State != "canceling" || canceled.Page != nil {
					t.Fatalf("superseded %+v %v", canceled, err)
				}
				if queued, err := d.GetHex(2); err != nil || queued.State != "pending" {
					t.Fatalf("worker bypass %+v %v", queued, err)
				}
			case "deadline":
				<-time.After(2 * timeout)
				want = context.DeadlineExceeded
			case "source_change":
				info, _ := s.Stat()
				if err := os.Chtimes(s.Name(), info.ModTime(), info.ModTime().Add(time.Second)); err != nil {
					t.Fatal(err)
				}
				want = ErrSourceChanged
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
				if _, err := d.GetAVCSearch(1); !errors.Is(err, session.ErrClosed) {
					t.Fatal(err)
				}
				return
			}
			failed := searchResult(t, d, 1)
			if !errors.Is(failed.Err, want) || failed.Page != nil || failed.State != "failed" {
				t.Fatalf("guard %+v want%v", failed, want)
			}
			if action == "supersede" {
				if next := hexResult(t, d, 2); next.Err != nil {
					t.Fatal(next.Err)
				}
			}
		})
	}
}
func TestAVCSearchQueuedCancellationAndPartialIndex(t *testing.T) {
	s := source(t, avcData(4, 0, true))
	d := startTest(t, s, nil, DetailTimeout)
	if err := scanDone(t, d); err != nil {
		t.Fatal(err)
	}
	s.blockOffset = 0
	s.armed.Store(true)
	if _, err := d.StartHex(1, 0, 4); err != nil {
		t.Fatal(err)
	}
	receive(t, s.entered)
	if _, err := d.StartAVCSearch(2, 0, "all"); err != nil {
		t.Fatal(err)
	}
	reads := s.reads.Load()
	canceled, err := d.CancelAVCSearch(2)
	if err != nil || canceled.State != "failed" || !errors.Is(canceled.Err, context.Canceled) || canceled.Page != nil || s.reads.Load() != reads {
		t.Fatalf("queued %+v %v", canceled, err)
	}
	close(s.release)
	// A source changed during a subsequent read invalidates a retained ready page.
	s2 := source(t, avcData(4, 0, true))
	d2 := startTest(t, s2, nil, DetailTimeout)
	if err := scanDone(t, d2); err != nil {
		t.Fatal(err)
	}
	if _, err := d2.StartAVCSearch(1, 0, "all"); err != nil {
		t.Fatal(err)
	}
	searchResult(t, d2, 1)
	info, _ := s2.Stat()
	if err := os.Chtimes(s2.Name(), info.ModTime(), info.ModTime().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := d2.StartHex(2, 0, 4); err != nil {
		t.Fatal(err)
	}
	hexResult(t, d2, 2)
	lost, err := d2.GetAVCSearch(1)
	if err != nil || !errors.Is(lost.Err, ErrSourceChanged) || lost.Page != nil {
		t.Fatalf("source changed retained %+v %v", lost, err)
	}
	// Container limit yields an exhausted retained prefix, never whole-file proof.
	s3 := source(t, avcData(4, 3, true))
	info, _ = s3.Stat()
	d3 := start(t.Context(), s3, info, flv.Options{MaxTags: 2})
	defer d3.Close(context.Background())
	if err := scanDone(t, d3); err != nil {
		t.Fatal(err)
	}
	if _, err := d3.StartAVCSearch(1, 0, "all"); err != nil {
		t.Fatal(err)
	}
	partial := searchResult(t, d3, 1)
	if partial.Page == nil || partial.Page.ScanComplete || !partial.Page.Exhausted || partial.Page.Total != 2 {
		t.Fatalf("partial %+v", partial)
	}
}
