package document

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/sevico/muxray/internal/flv"
	"github.com/sevico/muxray/internal/session"
)

func curveMP3Data(count int) []byte {
	payloads := make([][]byte, count)
	for i := range payloads {
		payloads[i] = mp3Payload()
	}
	data := mp3Data(payloads...)
	for i := 0; i < count; i++ {
		pos, dts := 13+i*20+4, uint32(i*26)
		data[pos], data[pos+1], data[pos+2], data[pos+3] = byte(dts>>16), byte(dts>>8), byte(dts), byte(dts>>24)
	}
	return data
}

func TestMediaCurvesPagesBeyondEventRetentionOwnCopiesWithoutIO(t *testing.T) {
	s := &filteredSource{testSource: source(t, curveMP3Data(600))}
	info, _ := s.Stat()
	d := start(t.Context(), s, info, flv.Options{})
	t.Cleanup(func() { _ = d.Close(nil) })
	if err := scanDone(t, d); err != nil {
		t.Fatal(err)
	}
	if _, err := d.MediaCurves(1, 0, 64); !errors.Is(err, ErrUnknownRequest) {
		t.Fatalf("unrequested curves: %v", err)
	}
	_, _ = d.StartSummary(1)
	if got := summaryResult(t, d, 1); got.Err != nil {
		t.Fatal(got.Err)
	}
	reads, stats := s.reads.Load(), s.stats.Load()
	page, err := d.MediaCurves(1, 1, 1)
	if err != nil || page.Start != 1 || page.Next != 2 || page.Total != 3 || page.IndexedTags != 600 || !page.ScanComplete || !page.Complete || len(page.Points) != 1 {
		t.Fatalf("page: %+v %v", page, err)
	}
	point := page.Points[0]
	if point.Index != 1 || point.StartTag != 256 || point.NextTag != 512 || point.Audio.CoveredBytes != 1280 || point.Audio.CoveredIntervalMS != 6656 || point.Audio.TagID != 256 || !point.Audio.Present || point.Audio != point.MP3 {
		t.Fatalf("curve beyond retained MP3 events: %+v", point)
	}
	page.Points[0].Audio.CoveredBytes = 99
	again, _ := d.MediaCurves(1, 1, 1)
	if !reflect.DeepEqual(again.Points[0], point) || s.reads.Load() != reads || s.stats.Load() != stats {
		t.Fatal("curve query aliases retained values or accesses source")
	}
	end, err := d.MediaCurves(1, 3, 0)
	if err != nil || end.Points == nil || len(end.Points) != 0 || end.Next != 3 || end.Limit != 64 {
		t.Fatalf("empty end: %+v %v", end, err)
	}
	for _, q := range []struct {
		start uint64
		count uint32
	}{{4, 1}, {0, 129}, {^uint64(0), 64}} {
		if _, err := d.MediaCurves(1, q.start, q.count); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("invalid page accepted: %+v %v", q, err)
		}
	}
	_, _ = d.StartDetail(2, 0)
	if got := result(t, d, 2); got.Err != nil {
		t.Fatal(got.Err)
	}
	if _, err := d.MediaCurves(1, 2, 64); err != nil {
		t.Fatalf("detail evicted completed curves: %v", err)
	}
	_, _ = d.StartSummary(3)
	if _, err := d.MediaCurves(1, 0, 64); !errors.Is(err, ErrStaleRequest) {
		t.Fatalf("old summary identity: %v", err)
	}
	if got := summaryResult(t, d, 3); got.Err != nil {
		t.Fatal(got.Err)
	}
	d.mu.Lock()
	d.changed = true
	d.mu.Unlock()
	if _, err := d.MediaCurves(3, 0, 64); !errors.Is(err, ErrSourceChanged) {
		t.Fatalf("source latch: %v", err)
	}
	_ = d.Close(nil)
	if _, err := d.MediaCurves(3, 0, 64); !errors.Is(err, session.ErrClosed) {
		t.Fatalf("closed curves: %v", err)
	}
}

func TestMediaCurvesCancelWaitsForReadAndPublishesNoPartialCollector(t *testing.T) {
	s := source(t, curveMP3Data(600))
	d := startTest(t, s, nil, DetailTimeout)
	if err := scanDone(t, d); err != nil {
		t.Fatal(err)
	}
	s.armed.Store(true)
	_, _ = d.StartSummary(1)
	receive(t, s.entered)
	if _, err := d.MediaCurves(1, 0, 64); !errors.Is(err, session.ErrNotReady) {
		t.Fatalf("pending curves leaked: %v", err)
	}
	_, _ = d.CancelSummary(1)
	if _, err := d.MediaCurves(1, 0, 64); !errors.Is(err, session.ErrNotReady) || s.closes.Load() != 0 {
		t.Fatalf("canceling curves/read ownership: %v", err)
	}
	s.armed.Store(false)
	close(s.release)
	if got := summaryResult(t, d, 1); !errors.Is(got.Err, context.Canceled) {
		t.Fatalf("canceled: %+v", got)
	}
	if _, err := d.MediaCurves(1, 0, 64); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled result published: %v", err)
	}
	_, _ = d.StartSummary(2)
	if got := summaryResult(t, d, 2); got.Err != nil {
		t.Fatal(got.Err)
	}
	if page, err := d.MediaCurves(2, 0, 64); err != nil || len(page.Points) != 3 {
		t.Fatalf("reanalysis: %+v %v", page, err)
	}
}

func TestMediaCurvesPartialScanCoverage(t *testing.T) {
	s := source(t, curveMP3Data(600))
	info, _ := s.Stat()
	d := start(t.Context(), s, info, flv.Options{MaxTags: 300})
	t.Cleanup(func() { _ = d.Close(nil) })
	if err := scanDone(t, d); err != nil {
		t.Fatal(err)
	}
	_, _ = d.StartSummary(1)
	if got := summaryResult(t, d, 1); got.Err != nil {
		t.Fatal(got.Err)
	}
	page, err := d.MediaCurves(1, 0, 128)
	if err != nil || page.ScanComplete || page.Complete || page.IndexedTags != 300 || page.Total != 2 || page.Points[1].NextTag != 300 {
		t.Fatalf("partial prefix mislabeled: %+v %v", page, err)
	}
}
