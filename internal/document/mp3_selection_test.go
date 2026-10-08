package document

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/sevico/muxray/internal/detailview"
)

func mp3SelectionPayload() []byte {
	return append([]byte{0x2f}, bytes.Repeat(mp3CompletePayload()[1:], 3)...)
}

func TestMP3FrameSelectionIdentityRecoveryAndAdmission(t *testing.T) {
	s := source(t, mp3Data(mp3SelectionPayload(), mp3CompletePayload()))
	d := startTest(t, s, nil, DetailTimeout)
	if err := scanDone(t, d); err != nil {
		t.Fatal(err)
	}
	index := uint32(2)
	if _, err := d.StartDetailWithMP3Frame(1, 0, &index); err != nil {
		t.Fatal(err)
	}
	index = 0 // The admitted request must own a copy of the selector.
	ready := result(t, d, 1)
	if ready.Err != nil || ready.Detail == nil || field(t, ready.Detail, "selectedFrameIndex").Value != "2" {
		t.Fatalf("selected result %+v", ready)
	}
	reads := s.reads.Load()
	index = 2
	if duplicate, err := d.StartDetailWithMP3Frame(1, 0, &index); err != nil || duplicate.State != "ready" || s.reads.Load() != reads {
		t.Fatalf("same-ID lost-call recovery reread: %+v %v", duplicate, err)
	}
	if _, err := d.StartDetail(1, 0); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("same ID lost selected identity: %v", err)
	}
	index = 1
	if _, err := d.StartDetailWithMP3Frame(1, 0, &index); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("same ID changed frame: %v", err)
	}
	index = 64
	if _, err := d.StartDetailWithMP3Frame(2, 0, &index); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("frame budget expanded: %v", err)
	}
	if retained, err := d.GetDetail(1); err != nil || retained.State != "ready" || field(t, retained.Detail, "selectedFrameIndex").Value != "2" || s.reads.Load() != reads {
		t.Fatalf("rejected admission changed old result: %+v %v", retained, err)
	}
	index = 1
	if _, err := d.StartDetailWithMP3Frame(2, 1, &index); err != nil {
		t.Fatal(err)
	}
	if failed := result(t, d, 2); !errors.Is(failed.Err, detailview.ErrInvalidInput) || failed.Detail != nil {
		t.Fatalf("short Tag fabricated selected frame: %+v", failed)
	}
	// A failed admitted selection is recoverable by repeating its ID, without
	// another read; a fresh preview remains available under the next ID.
	reads = s.reads.Load()
	if duplicate, err := d.StartDetailWithMP3Frame(2, 1, &index); err != nil || !errors.Is(duplicate.Err, detailview.ErrInvalidInput) || s.reads.Load() != reads {
		t.Fatalf("failed replay: %+v %v", duplicate, err)
	}
	if _, err := d.StartDetail(3, 0); err != nil {
		t.Fatal(err)
	}
	if preview := result(t, d, 3); preview.Err != nil || field(t, preview.Detail, "selectedFrameIndex").Value != "preview" {
		t.Fatalf("failed selection contaminated preview: %+v", preview)
	}
}

func TestMP3FrameSelectionKeepsWorkerGuards(t *testing.T) {
	for _, action := range []string{"supersede", "deadline", "source_change"} {
		t.Run(action, func(t *testing.T) {
			s := source(t, mp3Data(mp3SelectionPayload()))
			timeout := DetailTimeout
			if action == "deadline" {
				timeout = 20 * time.Millisecond
			}
			d := startTest(t, s, nil, timeout)
			if err := scanDone(t, d); err != nil {
				t.Fatal(err)
			}
			s.blockOffset = 25
			s.armed.Store(true)
			index := uint32(2)
			if _, err := d.StartDetailWithMP3Frame(1, 0, &index); err != nil {
				t.Fatal(err)
			}
			receive(t, s.entered)
			var want error
			switch action {
			case "supersede":
				index = 0
				if _, err := d.StartDetailWithMP3Frame(2, 0, &index); err != nil {
					t.Fatal(err)
				}
				if pending, err := d.GetDetail(2); err != nil || pending.State != "pending" {
					t.Fatalf("blocked worker bypassed: %+v %v", pending, err)
				}
			case "deadline":
				<-time.After(2 * timeout)
				want = context.DeadlineExceeded
			case "source_change":
				info, err := s.Stat()
				if err != nil {
					t.Fatal(err)
				}
				if err := os.Chtimes(s.Name(), info.ModTime(), info.ModTime().Add(time.Second)); err != nil {
					t.Fatal(err)
				}
				want = ErrSourceChanged
			}
			close(s.release)
			if action == "supersede" {
				if ready := result(t, d, 2); ready.Err != nil || field(t, ready.Detail, "selectedFrameIndex").Value != "0" {
					t.Fatalf("stale frame published: %+v", ready)
				}
				if _, err := d.GetDetail(1); !errors.Is(err, ErrStaleRequest) {
					t.Fatalf("superseded selection retained: %v", err)
				}
			} else if failed := result(t, d, 1); !errors.Is(failed.Err, want) || failed.Detail != nil {
				t.Fatalf("guard failed: %+v want=%v", failed, want)
			}
		})
	}
}
