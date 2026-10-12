package document

import (
	"context"
	"errors"
	"github.com/sevico/muxray/internal/detailview"
	"os"
	"testing"
	"time"
)

func TestMP3DependencyAdmissionIdentityAndDefaults(t *testing.T) {
	s := source(t, mp3Data(mp3SelectionPayload()))
	d := startTest(t, s, nil, DetailTimeout)
	if err := scanDone(t, d); err != nil {
		t.Fatal(err)
	}
	if _, err := d.StartDetailWithMP3Dependency(1, 0, nil, true); err != nil {
		t.Fatal(err)
	}
	got := result(t, d, 1)
	if got.Err != nil || field(t, got.Detail, "targetFrameIndex").Value != "0" || field(t, got.Detail, "selectedFrameIndex").Value != "0" {
		t.Fatalf("default %+v", got)
	}
	reads := s.reads.Load()
	zero := uint32(0)
	if duplicate, err := d.StartDetailWithMP3Dependency(1, 0, &zero, true); err != nil || duplicate.State != "ready" || s.reads.Load() != reads {
		t.Fatalf("duplicate %+v %v", duplicate, err)
	}
	if _, err := d.StartDetailWithMP3Dependency(1, 0, &zero, false); !errors.Is(err, ErrInvalidRequest) {
		t.Fatal(err)
	}
	bad := uint32(64)
	if _, err := d.StartDetailWithMP3Dependency(2, 0, &bad, true); !errors.Is(err, ErrInvalidRequest) {
		t.Fatal(err)
	}
	if old, err := d.GetDetail(1); err != nil || old.State != "ready" {
		t.Fatalf("rejected request polluted %+v %v", old, err)
	}
	if _, err := d.StartDetailWithMP3Dependency(2, 0, nil, false); err != nil {
		t.Fatal(err)
	}
	preview := result(t, d, 2)
	if preview.Err != nil || field(t, preview.Detail, "selectedFrameIndex").Value != "preview" {
		t.Fatalf("preview %+v", preview)
	}
	for _, f := range preview.Detail.Fields {
		if f.Name == "MP3 dependency sources" {
			t.Fatal("opt-in leaked into preview")
		}
	}
}
func TestMP3DependencyWorkerFences(t *testing.T) {
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
			s.blockOffset = -1
			s.armed.Store(true)
			index := uint32(2)
			if _, err := d.StartDetailWithMP3Dependency(1, 0, &index, true); err != nil {
				t.Fatal(err)
			}
			receive(t, s.entered)
			var want error
			switch action {
			case "supersede":
				if _, err := d.StartDetail(2, 0); err != nil {
					t.Fatal(err)
				}
				if pending, err := d.GetDetail(2); err != nil || pending.State != "pending" {
					t.Fatalf("bypass %+v %v", pending, err)
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
			}
			close(s.release)
			if action == "supersede" {
				if got := result(t, d, 2); got.Err != nil || field(t, got.Detail, "selectedFrameIndex").Value != "preview" {
					t.Fatalf("superseded %+v", got)
				}
				if _, err := d.GetDetail(1); !errors.Is(err, ErrStaleRequest) {
					t.Fatal(err)
				}
			} else if got := result(t, d, 1); !errors.Is(got.Err, want) || got.Detail != nil {
				t.Fatalf("guard %+v want%v", got, want)
			}
		})
	}
}

func TestMP3DependencyRejectsUnselectableTarget(t *testing.T) {
	s := source(t, mp3Data(mp3CompletePayload()))
	d := startTest(t, s, nil, DetailTimeout)
	if err := scanDone(t, d); err != nil {
		t.Fatal(err)
	}
	index := uint32(2)
	if _, err := d.StartDetailWithMP3Dependency(1, 0, &index, true); err != nil {
		t.Fatal(err)
	}
	got := result(t, d, 1)
	if !errors.Is(got.Err, detailview.ErrInvalidInput) || got.Detail != nil {
		t.Fatalf("unselectable frame was projected %+v", got)
	}
	reads := s.reads.Load()
	if again, err := d.StartDetailWithMP3Dependency(1, 0, &index, true); err != nil || !errors.Is(again.Err, detailview.ErrInvalidInput) || s.reads.Load() != reads {
		t.Fatalf("failed selection replay %+v %v", again, err)
	}
	if _, err := d.StartDetailWithMP3Dependency(2, 0, nil, true); err != nil {
		t.Fatal(err)
	}
	if next := result(t, d, 2); next.Err != nil || field(t, next.Detail, "selectedFrameIndex").Value != "0" {
		t.Fatalf("failed selection contaminated next %+v", next)
	}
}
