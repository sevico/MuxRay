package document

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sevico/muxray/internal/analysis"
	"github.com/sevico/muxray/internal/cursor"
	"github.com/sevico/muxray/internal/session"
	"github.com/sevico/muxray/internal/summaryview"
)

func summaryResult(t *testing.T, d *Document, id uint64) SummaryResult {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		got, err := d.GetSummary(id)
		if err != nil {
			t.Fatal(err)
		}
		if got.State != "pending" && got.State != "canceling" {
			return got
		}
		select {
		case <-deadline:
			t.Fatal("summary did not finish")
		default:
			runtime.Gosched()
		}
	}
}

func TestSummarySharedIndexCoreParityRetentionAndCopies(t *testing.T) {
	payloads := make([][]byte, 600)
	for i := range payloads {
		payloads[i] = mp3Payload()
	}
	s := source(t, mp3Data(payloads...))
	d := startTest(t, s, nil, DetailTimeout)
	if err := scanDone(t, d); err != nil {
		t.Fatal(err)
	}
	scan, err := d.scan.Result()
	if err != nil || scan.Tags != nil {
		t.Fatalf("expected shared chunked index: %v", err)
	}
	index := scan.Index()
	wantRaw, err := analysis.AnalyzeV3(t.Context(), cursor.WithContext(t.Context(), s), scan, summaryview.DesktopOptionsV3())
	if err != nil {
		t.Fatal(err)
	}
	want, err := summaryview.ProjectV3(scan, wantRaw)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.StartSummary(1); err != nil {
		t.Fatal(err)
	}
	got := summaryResult(t, d, 1)
	if got.Err != nil || got.State != "ready" || got.Processed != 600 || got.Total != 600 || !reflect.DeepEqual(got.Overview, &want.Overview) {
		t.Fatalf("summary differs from core: %+v", got)
	}
	after, err := d.scan.Result()
	if err != nil || after.Index() != index || after.Tags != nil {
		t.Fatal("summary replaced or materialized the shared index")
	}
	for _, list := range got.Overview.Lists {
		if list.Kind == "mp3" && (list.Observed == nil || *list.Observed != "600" || list.Retained != "256" || !list.Truncated) {
			t.Fatalf("retained count confused with observed: %+v", list)
		}
	}
	reads := s.reads.Load()
	for start := uint64(0); start < 256; {
		page, err := d.SummaryEvents(1, "mp3", start, 0)
		if err != nil || page.Limit != 100 || page.Total != 256 || page.Start != start || page.Next <= start || !reflect.DeepEqual(page.Events, want.Events["mp3"][start:page.Next]) {
			t.Fatalf("retained page: %+v %v", page, err)
		}
		start = page.Next
	}
	if page, err := d.SummaryEvents(1, "mp3", 256, 1); err != nil || page.Events == nil || len(page.Events) != 0 || page.Next != 256 {
		t.Fatalf("empty end page: %+v %v", page, err)
	}
	for _, tc := range []struct {
		kind  string
		start uint64
		count uint32
	}{{"invalid", 0, 1}, {"mp3", 257, 1}, {"mp3", 0, 101}} {
		if _, err := d.SummaryEvents(1, tc.kind, tc.start, tc.count); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("invalid page accepted: %+v %v", tc, err)
		}
	}
	if duplicate, err := d.StartSummary(1); err != nil || duplicate.State != "ready" {
		t.Fatalf("idempotent start: %+v %v", duplicate, err)
	}
	if canceled, err := d.CancelSummary(1); err != nil || canceled.State != "ready" {
		t.Fatalf("late cancel changed ready cache: %+v %v", canceled, err)
	}
	for i := range got.Overview.Metrics {
		if got.Overview.Metrics[i].Value != nil {
			*got.Overview.Metrics[i].Value = "mutated"
		}
	}
	for i := range got.Overview.Lists {
		if got.Overview.Lists[i].Observed != nil {
			*got.Overview.Lists[i].Observed = "mutated"
		}
	}
	page, _ := d.SummaryEvents(1, "mp3", 0, 1)
	*page.Events[0].TagID = "mutated"
	*page.Events[0].DTS = "mutated"
	for i := range page.Events[0].Values {
		if page.Events[0].Values[i].Value != nil {
			*page.Events[0].Values[i].Value = "mutated"
		}
	}
	again, _ := d.GetSummary(1)
	page, _ = d.SummaryEvents(1, "mp3", 0, 1)
	if !reflect.DeepEqual(again.Overview, &want.Overview) || !reflect.DeepEqual(page.Events, want.Events["mp3"][:1]) || s.reads.Load() != reads {
		t.Fatal("public queries exposed cache storage or performed source reads")
	}
	if _, err := d.StartDetail(2, 0); err != nil || result(t, d, 2).Err != nil {
		t.Fatalf("detail after summary: %v", err)
	}
	if _, err := d.StartHex(3, 24, 5); err != nil || hexResult(t, d, 3).Err != nil {
		t.Fatalf("Hex after summary: %v", err)
	}
	if cached, err := d.GetSummary(1); err != nil || !reflect.DeepEqual(cached.Overview, &want.Overview) {
		t.Fatalf("Detail/Hex evicted ready summary: %v", err)
	}
	if _, err := d.SummaryEvents(1, "mp3", 200, 100); err != nil {
		t.Fatal(err)
	}
	if _, err := d.StartSummary(1); !errors.Is(err, ErrStaleRequest) {
		t.Fatalf("old start bypassed global generations: %v", err)
	}
	if _, err := d.StartSummary(3); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("same ID changed work kind: %v", err)
	}
	if _, err := d.GetSummary(0); !errors.Is(err, ErrInvalidRequest) {
		t.Fatal(err)
	}
	if _, err := d.CancelSummary(4); !errors.Is(err, ErrUnknownRequest) {
		t.Fatal(err)
	}
	if _, err := d.StartSummary(4); err != nil || summaryResult(t, d, 4).Err != nil {
		t.Fatalf("new summary: %v", err)
	}
	if _, err := d.GetSummary(1); !errors.Is(err, ErrStaleRequest) {
		t.Fatalf("reanalysis retained obsolete summary: %v", err)
	}
}

func TestSummaryCanceledScanPrefixCanFinishAnalysis(t *testing.T) {
	s := source(t, mp3Data(mp3Payload(), mp3Payload()))
	s.blockOffset = 33
	s.armed.Store(true)
	d := startTest(t, s, nil, DetailTimeout)
	receive(t, s.entered)
	if _, err := d.StartSummary(1); !errors.Is(err, session.ErrNotReady) {
		t.Fatalf("summary admitted during scan: %v", err)
	}
	d.Cancel()
	close(s.release)
	if err := scanDone(t, d); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := d.StartSummary(1); err != nil {
		t.Fatal(err)
	}
	got := summaryResult(t, d, 1)
	if got.Err != nil || got.State != "ready" || got.Processed != 1 || got.Total != 1 || got.Overview == nil || got.Overview.IndexedTags != "1" || got.Overview.ScanComplete || !got.Overview.ScanHasErrors {
		t.Fatalf("committed-prefix analysis: %+v", got)
	}
	if s.closes.Load() != 0 || d.Snapshot().State != session.StateCanceled {
		t.Fatal("summary changed scan outcome or closed source")
	}
}

func TestSummaryActiveCancellationWaitsAndKeepsIndex(t *testing.T) {
	s := source(t, mp3Data(mp3Payload()))
	d := startTest(t, s, nil, DetailTimeout)
	if err := scanDone(t, d); err != nil {
		t.Fatal(err)
	}
	s.blockOffset = 25
	s.armed.Store(true)
	if _, err := d.StartSummary(1); err != nil {
		t.Fatal(err)
	}
	receive(t, s.entered)
	reads := s.reads.Load()
	for i := 0; i < 2; i++ {
		got, err := d.CancelSummary(1)
		if err != nil || got.State != "canceling" || got.Overview != nil || got.Processed != 0 || got.Total != 1 {
			t.Fatalf("cancel released physical read or published half Tag: %+v %v", got, err)
		}
	}
	if _, err := d.SummaryEvents(1, "mp3", 0, 1); !errors.Is(err, session.ErrNotReady) {
		t.Fatalf("canceling has event page: %v", err)
	}
	if duplicate, err := d.StartSummary(1); err != nil || duplicate.State != "canceling" || s.reads.Load() != reads || s.closes.Load() != 0 {
		t.Fatalf("same ID restarted canceled I/O: %+v %v", duplicate, err)
	}
	close(s.release)
	got := summaryResult(t, d, 1)
	if !errors.Is(got.Err, context.Canceled) || got.Overview != nil || got.Processed != 0 {
		t.Fatalf("canceled aggregate published: %+v", got)
	}
	if page, err := d.Page(0, 1); err != nil || page.Total != 1 {
		t.Fatalf("scan lost: %v", err)
	}
	if _, err := d.StartSummary(2); err != nil || summaryResult(t, d, 2).Err != nil {
		t.Fatalf("explicit new request could not finish: %v", err)
	}
}

func TestSummaryQueuedCancellationAndActiveSupersession(t *testing.T) {
	for _, queued := range []bool{true, false} {
		t.Run(strconv.FormatBool(queued), func(t *testing.T) {
			s := source(t, mp3Data(mp3Payload()))
			d := startTest(t, s, nil, DetailTimeout)
			if err := scanDone(t, d); err != nil {
				t.Fatal(err)
			}
			s.blockOffset = 25
			s.armed.Store(true)
			id := uint64(1)
			if queued {
				if _, err := d.StartDetail(1, 0); err != nil {
					t.Fatal(err)
				}
				receive(t, s.entered)
				id = 2
			}
			if _, err := d.StartSummary(id); err != nil {
				t.Fatal(err)
			}
			if !queued {
				receive(t, s.entered)
			}
			reads := s.reads.Load()
			if queued {
				if got, err := d.CancelSummary(id); err != nil || got.State != "failed" || !errors.Is(got.Err, context.Canceled) {
					t.Fatalf("queued cancel: %+v %v", got, err)
				}
			}
			if _, err := d.StartHex(id+1, 24, 1); err != nil {
				t.Fatal(err)
			}
			got, err := d.GetSummary(id)
			wantState := "canceling"
			if queued {
				wantState = "failed"
			}
			if err != nil || got.State != wantState || s.reads.Load() != reads {
				t.Fatalf("new read bypassed active worker: %+v %v", got, err)
			}
			close(s.release)
			if got := hexResult(t, d, id+1); got.Err != nil {
				t.Fatal(got.Err)
			}
			if got := summaryResult(t, d, id); got.State != "failed" || !errors.Is(got.Err, context.Canceled) || got.Overview != nil {
				t.Fatalf("superseded summary became ready: %+v", got)
			}
			if s.reads.Load() != reads+1 {
				t.Fatal("queued summary performed source reads")
			}
		})
	}
}

func TestSummaryBlockedCloseOwnsSourceUntilRelease(t *testing.T) {
	s := source(t, mp3Data(mp3Payload()))
	d := startTest(t, s, nil, DetailTimeout)
	if err := scanDone(t, d); err != nil {
		t.Fatal(err)
	}
	s.blockOffset = 25
	s.armed.Store(true)
	if _, err := d.StartSummary(1); err != nil {
		t.Fatal(err)
	}
	receive(t, s.entered)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := d.Close(ctx); !errors.Is(err, context.Canceled) || s.closes.Load() != 0 {
		t.Fatalf("wait timeout released active source: %v", err)
	}
	if got := d.Snapshot(); !got.Closed || got.State != session.StateCanceling {
		t.Fatalf("premature close status: %+v", got)
	}
	if _, err := d.GetSummary(1); !errors.Is(err, session.ErrClosed) {
		t.Fatal(err)
	}
	if _, err := d.StartSummary(2); !errors.Is(err, session.ErrClosed) {
		t.Fatal(err)
	}
	if _, err := d.SummaryEvents(1, "mp3", 0, 1); !errors.Is(err, session.ErrClosed) {
		t.Fatal(err)
	}
	close(s.release)
	if err := d.Close(t.Context()); err != nil || s.closes.Load() != 1 {
		t.Fatalf("orderly close: %v", err)
	}
	d.mu.Lock()
	cleared := d.summary.Overview == nil && d.summaryReport == nil && d.pending == nil && d.active == nil
	d.mu.Unlock()
	if !cleared {
		t.Fatal("closed document retained summary storage")
	}
}

func TestSummarySourceChangeInvalidatesReadyAndActiveResults(t *testing.T) {
	for _, during := range []bool{false, true} {
		t.Run(strconv.FormatBool(during), func(t *testing.T) {
			s := source(t, mp3Data(mp3Payload()))
			d := startTest(t, s, nil, DetailTimeout)
			if err := scanDone(t, d); err != nil {
				t.Fatal(err)
			}
			if during {
				s.blockOffset = 25
				s.armed.Store(true)
			}
			if _, err := d.StartSummary(1); err != nil {
				t.Fatal(err)
			}
			if during {
				receive(t, s.entered)
			} else if got := summaryResult(t, d, 1); got.Err != nil {
				t.Fatal(got.Err)
			}
			info, err := s.Stat()
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Chtimes(s.Name(), info.ModTime(), info.ModTime().Add(time.Second)); err != nil {
				t.Fatal(err)
			}
			if during {
				close(s.release)
			} else {
				if _, err := d.StartHex(2, 24, 5); err != nil {
					t.Fatal(err)
				}
				if got := hexResult(t, d, 2); !errors.Is(got.Err, ErrSourceChanged) {
					t.Fatalf("change detection: %+v", got)
				}
			}
			if got := summaryResult(t, d, 1); !errors.Is(got.Err, ErrSourceChanged) || got.Overview != nil {
				t.Fatalf("stale summary visible: %+v", got)
			}
			reads := s.reads.Load()
			if _, err := d.SummaryEvents(1, "mp3", 0, 1); !errors.Is(err, ErrSourceChanged) {
				t.Fatal(err)
			}
			if _, err := d.MediaCurves(1, 0, 64); !errors.Is(err, ErrSourceChanged) {
				t.Fatalf("source change left curves available: %v", err)
			}
			if err := os.Chtimes(s.Name(), info.ModTime(), info.ModTime()); err != nil {
				t.Fatal(err)
			}
			if _, err := d.StartSummary(3); err != nil {
				t.Fatal(err)
			}
			if got := summaryResult(t, d, 3); !errors.Is(got.Err, ErrSourceChanged) || got.Overview != nil || s.reads.Load() != reads {
				t.Fatalf("restored mtime cleared change latch: %+v", got)
			}
		})
	}
}

func TestSummaryDeadlineDiscardsAggregate(t *testing.T) {
	s := source(t, mp3Data(mp3Payload()))
	d := startTest(t, s, nil, DetailTimeout)
	if err := scanDone(t, d); err != nil {
		t.Fatal(err)
	}
	d.mu.Lock()
	d.summaryTimeout = 0 // deterministic already-expired cooperative deadline
	d.mu.Unlock()
	reads := s.reads.Load()
	if _, err := d.StartSummary(1); err != nil {
		t.Fatal(err)
	}
	if got := summaryResult(t, d, 1); !errors.Is(got.Err, context.DeadlineExceeded) || got.Overview != nil || s.reads.Load() != reads {
		t.Fatalf("expired summary published data: %+v", got)
	}
}

func TestSummaryPageByteBudgetAdvancesAndClones(t *testing.T) {
	value := strings.Repeat("x", 256)
	events := make([]summaryview.Event, 100)
	for i := range events {
		events[i] = summaryview.Event{ID: strconv.Itoa(i), Offset: "0", Values: make([]summaryview.Metric, 24)}
		for j := range events[i].Values {
			events[i].Values[j] = summaryview.Metric{Section: "test", Name: "value", Value: &value}
		}
	}
	d := &Document{ctx: t.Context(), request: work{id: 1, kind: summaryWork}, summary: SummaryResult{RequestID: 1, State: "ready"}, summaryReport: &summaryview.Report{Events: map[string][]summaryview.Event{"mp3": events}}}
	var visited uint64
	for visited < uint64(len(events)) {
		page, err := d.SummaryEvents(1, "mp3", visited, 100)
		encoded, jsonErr := json.Marshal(page)
		if err != nil || jsonErr != nil || page.Next <= visited || len(page.Events) >= 100 || len(encoded) > summaryPageJSONBytes || page.Total != 100 {
			t.Fatalf("byte-bounded page: %+v %v %v bytes=%d", page, err, jsonErr, len(encoded))
		}
		*page.Events[0].Values[0].Value = "mutated"
		if value != strings.Repeat("x", 256) {
			t.Fatal("page copied pointer storage")
		}
		visited = page.Next
	}
}
