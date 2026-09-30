package document

import (
	"errors"
	"os"
	"testing"
	"time"

	"github.com/sevico/muxray/internal/flv"
	"github.com/sevico/muxray/internal/session"
)

func TestSnapshotRetainsEachReadKindWithoutIO(t *testing.T) {
	s := &filteredSource{testSource: source(t, fixture(script(), script()))}
	info, err := s.Stat()
	if err != nil {
		t.Fatal(err)
	}
	d := start(t.Context(), s, info, flv.Options{})
	t.Cleanup(func() { _ = d.Close(nil) })
	if err := scanDone(t, d); err != nil {
		t.Fatal(err)
	}
	if _, err := d.StartDetail(1, 0); err != nil {
		t.Fatal(err)
	}
	if got := result(t, d, 1); got.Err != nil {
		t.Fatal(got.Err)
	}
	if _, err := d.StartHex(2, 24, 4); err != nil {
		t.Fatal(err)
	}
	if got := hexResult(t, d, 2); got.Err != nil {
		t.Fatal(got.Err)
	}
	if _, err := d.GetDetail(1); !errors.Is(err, ErrStaleRequest) {
		t.Fatalf("changed old recovery semantics: %v", err)
	}
	reads, stats := s.reads.Load(), s.stats.Load()
	detail, err := d.SnapshotDetail(1)
	if err != nil || detail.State != "ready" || detail.TagID != 0 || detail.Detail == nil {
		t.Fatalf("detail: %+v %v", detail, err)
	}
	hex, err := d.SnapshotHex(2)
	if err != nil || hex.State != "ready" || len(hex.Window.Bytes) != 4 {
		t.Fatalf("hex: %+v %v", hex, err)
	}
	identity, err := d.SnapshotSourceInfo()
	if err != nil || !os.SameFile(info, identity) {
		t.Fatalf("identity: %v", err)
	}
	detail.Detail.Fields[0].Value = "mutation"
	hex.Window.Bytes[0] ^= 0xff
	if got, _ := d.SnapshotDetail(1); got.Detail.Fields[0].Value == "mutation" {
		t.Fatal("snapshot aliases detail")
	}
	if got, _ := d.SnapshotHex(2); got.Window.Bytes[0] == hex.Window.Bytes[0] {
		t.Fatal("snapshot aliases hex")
	}
	if s.reads.Load() != reads || s.stats.Load() != stats {
		t.Fatal("snapshot getter touched source")
	}
	if _, err := d.StartDetail(3, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := d.SnapshotDetail(1); !errors.Is(err, ErrStaleRequest) {
		t.Fatalf("same-kind admission retained old result: %v", err)
	}
	if got, err := d.SnapshotHex(2); err != nil || got.Window == nil {
		t.Fatalf("cross-kind admission dropped Hex: %+v %v", got, err)
	}
	if got := result(t, d, 3); got.Err != nil {
		t.Fatal(got.Err)
	}
	if _, err := d.StartSummary(4); err != nil {
		t.Fatal(err)
	}
	if _, err := d.SnapshotDetail(3); !errors.Is(err, ErrStaleRequest) {
		t.Fatalf("summary did not clear detail: %v", err)
	}
	if _, err := d.SnapshotHex(2); !errors.Is(err, ErrStaleRequest) {
		t.Fatalf("summary did not clear Hex: %v", err)
	}
	if got := summaryResult(t, d, 4); got.Err != nil {
		t.Fatal(got.Err)
	}
	if _, err := d.StartHex(5, 0, 4); err != nil {
		t.Fatal(err)
	}
	if got := hexResult(t, d, 5); got.Err != nil {
		t.Fatal(got.Err)
	}
	if err := d.Close(nil); err != nil {
		t.Fatal(err)
	}
	if _, err := d.SnapshotHex(5); !errors.Is(err, session.ErrClosed) {
		t.Fatal(err)
	}
	if _, err := d.SnapshotSourceInfo(); !errors.Is(err, session.ErrClosed) {
		t.Fatal(err)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.snapshotDetail.Detail != nil || d.snapshotHex.Window != nil {
		t.Fatal("close retained payload")
	}
}

func TestSnapshotPendingAndSourceChangeClearRetainedReads(t *testing.T) {
	s := source(t, fixture(script()))
	d := startTest(t, s, nil, DetailTimeout)
	if err := scanDone(t, d); err != nil {
		t.Fatal(err)
	}
	if _, err := d.StartDetail(1, 0); err != nil {
		t.Fatal(err)
	}
	if got := result(t, d, 1); got.Err != nil {
		t.Fatal(got.Err)
	}
	if _, err := d.StartHex(2, 0, 4); err != nil {
		t.Fatal(err)
	}
	if got := hexResult(t, d, 2); got.Err != nil {
		t.Fatal(got.Err)
	}
	s.armed.Store(true)
	if _, err := d.StartHex(3, 0, 4); err != nil {
		t.Fatal(err)
	}
	receive(t, s.entered)
	if got, err := d.SnapshotHex(3); err != nil || got.State != "pending" || got.Window != nil {
		t.Fatalf("pending: %+v %v", got, err)
	}
	if _, err := d.SnapshotHex(2); !errors.Is(err, ErrStaleRequest) {
		t.Fatalf("new Hex retained old Hex: %v", err)
	}
	if got, err := d.SnapshotDetail(1); err != nil || got.Detail == nil {
		t.Fatalf("blocked Hex lost detail: %+v %v", got, err)
	}
	info, err := s.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(s.Name(), info.ModTime(), info.ModTime().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	s.armed.Store(false)
	close(s.release)
	if got := hexResult(t, d, 3); !errors.Is(got.Err, ErrSourceChanged) {
		t.Fatalf("source change: %+v", got)
	}
	if _, err := d.SnapshotDetail(1); !errors.Is(err, ErrSourceChanged) {
		t.Fatal(err)
	}
	if _, err := d.SnapshotHex(3); !errors.Is(err, ErrSourceChanged) {
		t.Fatal(err)
	}
	if _, err := d.SnapshotSourceInfo(); !errors.Is(err, ErrSourceChanged) {
		t.Fatal(err)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.snapshotDetail.Detail != nil || d.snapshotHex.Window != nil {
		t.Fatal("source change retained payload")
	}
}
