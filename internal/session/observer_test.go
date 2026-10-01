package session

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/sevico/muxray/internal/flv"
)

func TestStartWithTagObserverRetainsFailedPrefixAndBorrowsSource(t *testing.T) {
	data := fixture(3)
	source := newSource(data)
	wantErr := errors.New("tag observer failed")
	var records []flv.TagRecord
	s := StartWithTagObserver(nil, source, uint64(len(data)), flv.Options{}, func(ctx context.Context, record flv.TagRecord) error {
		if ctx == nil || ctx.Err() != nil || record.ID != uint64(len(records)) {
			return errors.New("invalid context or observer sequence")
		}
		var tagType [1]byte
		if _, err := source.ReadAt(tagType[:], int64(record.Offset)); err != nil {
			return err
		}
		if tagType[0] != record.RawTypeFlags {
			return errors.New("borrowed source unavailable to observer")
		}
		records = append(records, record)
		if record.ID == 1 {
			return wantErr
		}
		return nil
	})
	t.Cleanup(func() { _ = s.Close(context.Background()) })
	if err := wait(t, s); !errors.Is(err, wantErr) {
		t.Fatalf("observer error: %v", err)
	}
	if snapshot := s.Snapshot(); snapshot.State != StateFailed || snapshot.Complete || !snapshot.HasErrors || snapshot.Progress.TagCount != 2 || snapshot.Progress.BytesScanned != 43 {
		t.Fatalf("failed snapshot: %+v", snapshot)
	}
	page, err := s.Page(0, 3)
	if err != nil || len(page.Records) != 2 || len(records) != 2 {
		t.Fatalf("committed prefix: %+v err=%v", page, err)
	}
	for id, record := range page.Records {
		if record != records[id] {
			t.Fatal("observer record not retained")
		}
	}
	if err := s.Close(nil); !errors.Is(err, wantErr) || source.closes.Load() != 0 {
		t.Fatalf("borrowed source closed after observer failure: closes=%d err=%v", source.closes.Load(), err)
	}
	if err := source.Close(); err != nil || source.closes.Load() != 1 {
		t.Fatal("caller could not close borrowed source")
	}
}

func TestStartWithTagObserverCancellationAndCloseWait(t *testing.T) {
	for _, action := range []string{"cancel", "parent", "close"} {
		t.Run(action, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			data := fixture(3)
			source := newSource(data)
			entered, canceled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
			var releaseOnce sync.Once
			releaseObserver := func() { releaseOnce.Do(func() { close(release) }) }
			s := StartWithTagObserver(ctx, source, uint64(len(data)), flv.Options{}, func(observeCtx context.Context, record flv.TagRecord) error {
				close(entered)
				<-observeCtx.Done()
				close(canceled)
				<-release // Cancellation must not falsely complete a blocked observer.
				return errors.New("observer exited after cancellation")
			})
			t.Cleanup(func() {
				releaseObserver()
				cleanupCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
				defer stop()
				_ = s.Close(cleanupCtx)
			})
			await(t, entered)
			waitCtx, stop := expiredContext()
			defer stop()
			switch action {
			case "cancel":
				s.Cancel()
			case "parent":
				cancel()
			case "close":
				if err := s.Close(waitCtx); !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("close did not wait for observer: %v", err)
				}
			}
			await(t, canceled)
			if snapshot := s.Snapshot(); snapshot.State != StateCanceling || snapshot.Closed != (action == "close") || source.closes.Load() != 0 {
				t.Fatalf("blocked observer lifecycle: %+v closes=%d", snapshot, source.closes.Load())
			}
			if err := s.Wait(waitCtx); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("Wait ignored blocked observer: %v", err)
			}
			releaseObserver()
			if err := wait(t, s); !errors.Is(err, context.Canceled) {
				t.Fatalf("worker context cancellation lost: %v", err)
			}
			if snapshot := s.Snapshot(); snapshot.State != StateCanceled || snapshot.Complete || snapshot.Progress.TagCount != 1 || snapshot.Progress.BytesScanned != 28 || source.closes.Load() != 0 {
				t.Fatalf("canceled observer prefix: %+v closes=%d", snapshot, source.closes.Load())
			}
			if action != "close" {
				page, err := s.Page(0, 3)
				if err != nil || len(page.Records) != 1 || page.Records[0].ID != 0 {
					t.Fatalf("canceled committed record: %+v err=%v", page, err)
				}
			} else if _, err := s.Result(); !errors.Is(err, ErrClosed) {
				t.Fatalf("closed result accessible: %v", err)
			}
			_ = s.Close(nil)
			if source.closes.Load() != 0 {
				t.Fatal("session closed borrowed source")
			}
			if err := source.Close(); err != nil || source.closes.Load() != 1 {
				t.Fatal("caller could not close borrowed source after observer exited")
			}
		})
	}
}
