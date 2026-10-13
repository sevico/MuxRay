package document

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

func mp3PagePayload(count int) []byte {
	return append([]byte{0x2f}, bytes.Repeat(mp3CompletePayload()[1:], count)...)
}

func TestMP3PagesFarSeekContinuationAndCheckpointReuse(t *testing.T) {
	s := source(t, mp3Data(mp3PagePayload(3000)))
	d := startTest(t, s, nil, DetailTimeout)
	if err := scanDone(t, d); err != nil {
		t.Fatal(err)
	}
	page, index := uint32(2688), uint32(2700)
	for id := uint64(1); id <= 3; id++ {
		if _, err := d.StartDetailWithMP3Page(id, 0, &index, &page, false); err != nil {
			t.Fatal(err)
		}
		r := result(t, d, id)
		if r.Err != nil || r.Detail == nil {
			t.Fatalf("read %+v", r)
		}
		if field(t, r.Detail, "requestedPageStart").Value != "2688" || field(t, r.Detail, "requestedFrameIndex").Value != "2700" {
			t.Fatal("identity lost")
		}
		if id < 3 {
			if field(t, r.Detail, "pageState").Value != "preparing" || field(t, r.Detail, "nextFrameIndex").Value != strconv.FormatUint(id*1024, 10) || field(t, r.Detail, "hasNext").Value != "false" {
				t.Fatalf("progress %+v", r.Detail.Fields[:20])
			}
			for _, f := range r.Detail.Fields {
				if f.Name == "Frame inspection" {
					t.Fatal("preparation exposed wrong-page syntax")
				}
			}
		} else {
			if field(t, r.Detail, "pageState").Value != "ready" || field(t, r.Detail, "selectedFrameIndex").Value != "2700" || field(t, r.Detail, "nextPageStart").Value != "2752" {
				t.Fatal("far page not reached")
			}
			f := field(t, r.Detail, "Frame 2700")
			if f.Span == nil || f.Span.ByteOffset != strconv.Itoa(25+2700*417) {
				t.Fatalf("source span %+v", f)
			}
			if !strings.Contains(field(t, r.Detail, "sideInfoOmittedFrames").Value, "63;") {
				t.Fatal("selected side info missing")
			}
		}
	}
	reads := s.reads.Load()
	if _, err := d.StartDetailWithMP3Page(3, 0, &index, &page, false); err != nil || s.reads.Load() != reads {
		t.Fatalf("same-ID reread %v", err)
	}
	page = 2752
	index = 2752
	if _, err := d.StartDetailWithMP3Page(4, 0, &index, &page, false); err != nil {
		t.Fatal(err)
	}
	r := result(t, d, 4)
	if r.Err != nil || field(t, r.Detail, "replayPages").Value != "1" {
		t.Fatalf("next page repeated prefix %+v", r)
	}
	// Caller mutation cannot change admitted identity, and a cached cursor is
	// never committed by an invalid admission.
	page = 2753
	if _, err := d.StartDetailWithMP3Page(5, 0, nil, &page, false); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("unaligned %v", err)
	}
	page = ^uint32(0) - 63
	if _, err := d.StartDetailWithMP3Page(5, 0, nil, &page, false); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("impossible frame index %v", err)
	}
}

func TestMP3PagedDependencyUsesPreviousPagePhysicalSlots(t *testing.T) {
	payload := mp3PagePayload(130)
	offset := 1 + 65*417
	payload[offset+4], payload[offset+5] = 0xff, 0x80
	s := source(t, mp3Data(payload))
	d := startTest(t, s, nil, DetailTimeout)
	if err := scanDone(t, d); err != nil {
		t.Fatal(err)
	}
	page, index := uint32(64), uint32(65)
	if _, err := d.StartDetailWithMP3Page(1, 0, &index, &page, true); err != nil {
		t.Fatal(err)
	}
	r := result(t, d, 1)
	if r.Err != nil {
		t.Fatal(r.Err)
	}
	if field(t, r.Detail, "dependencyState").Value != "resolved" || field(t, r.Detail, "dependencyReason").Value != "within_tag" || field(t, r.Detail, "targetFrameIndex").Value != "65" {
		t.Fatalf("dependency %+v", r.Detail.Fields)
	}
	var sources []string
	for _, f := range r.Detail.Fields {
		if f.Name == "sourceFrameIndex" {
			sources = append(sources, f.Value)
		}
	}
	if strings.Join(sources, ",") != "63,64,65" {
		t.Fatalf("physical source indices %v", sources)
	}
	if field(t, r.Detail, "selectedFrameIndex").Value != "65" {
		t.Fatal("dependency lost selected detail")
	}
}

func TestMP3PageStopsWithoutInventingTarget(t *testing.T) {
	payload := append(mp3PagePayload(70), 0xff, 0xfb, 0, 0x64)
	s := source(t, mp3Data(payload))
	d := startTest(t, s, nil, DetailTimeout)
	if err := scanDone(t, d); err != nil {
		t.Fatal(err)
	}
	page, index := uint32(128), uint32(129)
	if _, err := d.StartDetailWithMP3Page(1, 0, &index, &page, false); err != nil {
		t.Fatal(err)
	}
	r := result(t, d, 1)
	if r.Err != nil || field(t, r.Detail, "pageState").Value != "stopped" || field(t, r.Detail, "pageStopReason").Value != "free_format" || field(t, r.Detail, "nextPageStart").Value != "none" {
		t.Fatalf("stop %+v", r)
	}
	for _, f := range r.Detail.Fields {
		if f.Name == "Frame inspection" {
			t.Fatal("invented target")
		}
	}
}

func TestMP3PageCancellationAndSourceChangeDiscardCheckpoints(t *testing.T) {
	for _, action := range []string{"supersede", "deadline", "source_change"} {
		t.Run(action, func(t *testing.T) {
			s := source(t, mp3Data(mp3PagePayload(200)))
			timeout := DetailTimeout
			if action == "deadline" {
				timeout = 20 * time.Millisecond
			}
			d := startTest(t, s, nil, timeout)
			if err := scanDone(t, d); err != nil {
				t.Fatal(err)
			}
			s.blockOffset = 25 + 64*417
			s.armed.Store(true)
			page := uint32(128)
			if _, err := d.StartDetailWithMP3Page(1, 0, nil, &page, false); err != nil {
				t.Fatal(err)
			}
			receive(t, s.entered)
			var want error
			switch action {
			case "supersede":
				if _, err := d.StartDetail(2, 0); err != nil {
					t.Fatal(err)
				}
			case "deadline":
				<-time.After(2 * timeout)
				want = context.DeadlineExceeded
			case "source_change":
				info, err := s.Stat()
				if err != nil {
					t.Fatal(err)
				}
				if err = os.Chtimes(s.Name(), info.ModTime(), info.ModTime().Add(time.Second)); err != nil {
					t.Fatal(err)
				}
				want = ErrSourceChanged
			}
			close(s.release)
			if action == "supersede" {
				if r := result(t, d, 2); r.Err != nil {
					t.Fatal(r.Err)
				}
			} else if r := result(t, d, 1); !errors.Is(r.Err, want) || r.Detail != nil {
				t.Fatalf("guard %+v", r)
			}
			d.mu.Lock()
			count := len(d.mp3Pages)
			d.mu.Unlock()
			if count != 0 {
				t.Fatal("failed/superseded page committed checkpoints")
			}
		})
	}
}

func TestMP3PageCheckpointCap(t *testing.T) {
	s := source(t, mp3Data(mp3PagePayload(5000)))
	d := startTest(t, s, nil, DetailTimeout)
	if err := scanDone(t, d); err != nil {
		t.Fatal(err)
	}
	page := uint32(4928)
	for id := uint64(1); id <= 5; id++ {
		if _, err := d.StartDetailWithMP3Page(id, 0, nil, &page, false); err != nil {
			t.Fatal(err)
		}
		r := result(t, d, id)
		if r.Err != nil {
			t.Fatal(r.Err)
		}
		if id == 5 && field(t, r.Detail, "pageState").Value != "ready" {
			t.Fatal("not ready")
		}
	}
	d.mu.Lock()
	count := len(d.mp3Pages)
	d.mu.Unlock()
	if count != MP3PageCheckpointLimit {
		t.Fatalf("checkpoint count %d", count)
	}
}
