package document

import (
	"context"
	"errors"
	"github.com/sevico/muxray/internal/session"
	"os"
	"runtime"
	"testing"
	"time"
)

func codecResult(t *testing.T, d *Document, id uint64) CodecSearchResult {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		r, e := d.GetCodecSearch(id)
		if errors.Is(e, ErrSourceChanged) {
			return CodecSearchResult{RequestID: id, State: "failed", Err: e}
		}
		if e != nil {
			t.Fatal(e)
		}
		if r.State != "pending" && r.State != "canceling" {
			return r
		}
		select {
		case <-deadline:
			t.Fatal("codec search timeout")
		default:
			runtime.Gosched()
		}
	}
}
func TestCodecSearchContinuationIdentityRetention(t *testing.T) {
	payload := mp3PagePayload(3000)
	// Same length, but a channel-mode change at an index beyond the summary prefix.
	payload[1+2600*417+3] = 0xe4
	s := source(t, mp3Data(payload))
	d := startTest(t, s, nil, DetailTimeout)
	if e := scanDone(t, d); e != nil {
		t.Fatal(e)
	}
	var token uint64
	for id := uint64(1); id <= 3; id++ {
		if _, e := d.StartCodecSearch(id, token, "mp3", "parameters"); e != nil {
			t.Fatal(e)
		}
		r := codecResult(t, d, id)
		if r.Err != nil || r.Page == nil {
			t.Fatalf("%+v", r)
		}
		if id < 3 {
			if len(r.Page.Rows) != 0 || r.Page.Exhausted || r.Page.Next.FrameIndex != uint32(id*1024) || r.Page.NextCursor != id {
				t.Fatalf("continuation %+v", r.Page)
			}
		}
		token = r.Page.NextCursor
	}
	r, e := d.GetCodecSearch(3)
	if e != nil || len(r.Page.Rows) != 2 || *r.Page.Rows[0].FrameIndex != 2600 || !r.Page.Exhausted {
		t.Fatalf("late changes %+v %v", r, e)
	}
	r.Page.Rows[0].Values[0].Value = "mutated"
	*r.Page.Rows[0].FrameIndex = 0
	r.Page.Rows[0].Span.BitLength = 0
	if _, e = d.StartHex(4, 0, 4); e != nil {
		t.Fatal(e)
	}
	if h := hexResult(t, d, 4); h.Err != nil {
		t.Fatal(h.Err)
	}
	reads := s.reads.Load()
	saved, e := d.SnapshotCodecSearch(3)
	if e != nil || saved.Page.Rows[0].Values[0].Value == "mutated" || *saved.Page.Rows[0].FrameIndex != 2600 || saved.Page.Rows[0].Span.BitLength == 0 || s.reads.Load() != reads {
		t.Fatalf("snapshot clone %+v %v", saved, e)
	}
	if _, e = d.StartCodecSearch(5, 1, "sei", "all"); !errors.Is(e, ErrUnknownCursor) {
		t.Fatal(e)
	}
	if _, e = d.StartCodecSearch(5, 1, "mp3", "all"); !errors.Is(e, ErrUnknownCursor) {
		t.Fatal(e)
	}
	if _, e = d.StartCodecSearch(5, 1, "mp3", "parameters"); e != nil {
		t.Fatal(e)
	}
	back := codecResult(t, d, 5)
	if back.Page.Start.FrameIndex != 1024 || back.Page.Next.FrameIndex != 2048 {
		t.Fatalf("saved cursor changed %+v", back.Page)
	}
	duplicate, e := d.StartCodecSearch(5, 1, "mp3", "parameters")
	if e != nil || duplicate.State != "ready" {
		t.Fatal(e)
	}
	if _, e = d.StartCodecSearch(5, 0, "mp3", "parameters"); !errors.Is(e, ErrInvalidRequest) {
		t.Fatal(e)
	}
}
func TestCodecSearchCursorEvictionAndRestart(t *testing.T) {
	s := source(t, mp3Data(mp3PagePayload(12000)))
	d := startTest(t, s, nil, DetailTimeout)
	if e := scanDone(t, d); e != nil {
		t.Fatal(e)
	}
	var token uint64
	for id := uint64(1); id <= 10; id++ {
		if _, e := d.StartCodecSearch(id, token, "mp3", "all"); e != nil {
			t.Fatal(e)
		}
		r := codecResult(t, d, id)
		if r.Err != nil || r.Page == nil || r.Page.Exhausted {
			t.Fatalf("%+v", r)
		}
		token = r.Page.NextCursor
	}
	if len(d.codecCursors) != MaxCodecSearchCursors {
		t.Fatal(len(d.codecCursors))
	}
	if _, e := d.StartCodecSearch(11, 1, "mp3", "all"); !errors.Is(e, ErrUnknownCursor) {
		t.Fatal(e)
	}
	if _, e := d.StartCodecSearch(11, 0, "mp3", "all"); e != nil {
		t.Fatal(e)
	}
	codecResult(t, d, 11)
	if len(d.codecCursors) != 1 || d.codecCursors[0].id != 11 {
		t.Fatal("restart retained obsolete states")
	}
	if _, e := d.StartCodecSearch(12, 10, "mp3", "all"); !errors.Is(e, ErrUnknownCursor) {
		t.Fatal(e)
	}
}
func TestCodecSearchWorkerFences(t *testing.T) {
	for _, action := range []string{"cancel", "supersede", "deadline", "source_change", "close"} {
		t.Run(action, func(t *testing.T) {
			s := source(t, mp3Data(mp3PagePayload(3000)))
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
			if _, err := d.StartCodecSearch(1, 0, "mp3", "all"); err != nil {
				t.Fatal(err)
			}
			receive(t, s.entered)
			want := context.Canceled
			switch action {
			case "cancel":
				canceled, err := d.CancelCodecSearch(1)
				if err != nil || canceled.State != "canceling" || canceled.Page != nil {
					t.Fatalf("cancel %+v %v", canceled, err)
				}
				again, err := d.StartCodecSearch(1, 0, "mp3", "all")
				if err != nil || again.State != "canceling" {
					t.Fatalf("recovery %+v %v", again, err)
				}
			case "supersede":
				if _, err := d.StartHex(2, 0, 4); err != nil {
					t.Fatal(err)
				}
				canceled, err := d.GetCodecSearch(1)
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
				if _, err := d.GetCodecSearch(1); !errors.Is(err, session.ErrClosed) {
					t.Fatal(err)
				}
				return
			}
			failed := codecResult(t, d, 1)
			if !errors.Is(failed.Err, want) || failed.Page != nil || failed.State != "failed" || len(d.codecCursors) != 0 {
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

func TestCodecSearchCanceledContinuationKeepsCommittedCursor(t *testing.T) {
	s := source(t, mp3Data(mp3PagePayload(3000)))
	d := startTest(t, s, nil, DetailTimeout)
	if err := scanDone(t, d); err != nil {
		t.Fatal(err)
	}
	if _, err := d.StartCodecSearch(1, 0, "mp3", "all"); err != nil {
		t.Fatal(err)
	}
	first := codecResult(t, d, 1)
	if first.Page == nil || first.Page.Next.FrameIndex != 1024 {
		t.Fatalf("%+v", first)
	}
	s.blockOffset = 25 + 1024*417
	s.armed.Store(true)
	if _, err := d.StartCodecSearch(2, first.Page.NextCursor, "mp3", "all"); err != nil {
		t.Fatal(err)
	}
	receive(t, s.entered)
	if canceled, err := d.CancelCodecSearch(2); err != nil || canceled.State != "canceling" {
		t.Fatalf("%+v %v", canceled, err)
	}
	close(s.release)
	if canceled := codecResult(t, d, 2); !errors.Is(canceled.Err, context.Canceled) || canceled.Page != nil {
		t.Fatalf("%+v", canceled)
	}
	if len(d.codecCursors) != 1 || d.codecCursors[0].id != 1 {
		t.Fatal("cancellation changed committed states")
	}
	if _, err := d.StartCodecSearch(3, first.Page.NextCursor, "mp3", "all"); err != nil {
		t.Fatal(err)
	}
	resumed := codecResult(t, d, 3)
	if resumed.Err != nil || resumed.Page.Start.FrameIndex != 1024 || resumed.Page.Next.FrameIndex != 2048 {
		t.Fatalf("%+v", resumed)
	}
}
