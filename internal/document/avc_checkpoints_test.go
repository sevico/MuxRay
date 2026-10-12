package document

import (
	"context"
	"encoding/hex"
	"errors"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/sevico/muxray/internal/avccontext"
)

func checkpointAVCData(t *testing.T, count int, broken ...int) ([]byte, []int64) {
	t.Helper()
	config, err := hex.DecodeString("0142001effe100086742001ef40a0fc801000468ce3c80")
	if err != nil {
		t.Fatal(err)
	}
	payloads := make([][]byte, count+1)
	payloads[0] = append([]byte{0x17, 0, 0, 0, 0}, config...)
	for i := 1; i < count; i++ {
		payloads[i] = []byte{0x27, 1, 0, 0, 0, 0, 0, 0, 1, 9}
	}
	payloads[count] = []byte{0x27, 1, 0, 0, 0, 0, 0, 0, 4, 0x65, 0xb8, 0x40, 0xa8}
	for _, id := range broken {
		payloads[id] = []byte{0x17, 0, 0, 0, 0, 1, 66}
	}
	return indexedAVCData(payloads...)
}

func TestAVCResumedReplayDoesNotSkipBrokenReplacement(t *testing.T) {
	data, _ := checkpointAVCData(t, 9000, 8192)
	s := source(t, data)
	d := startTest(t, s, nil, DetailTimeout)
	if err := scanDone(t, d); err != nil {
		t.Fatal(err)
	}
	first := requestCheckpointDetail(t, d, 1, 9000)
	if field(t, first.Detail, "nextTagId").Value != "8192" {
		t.Fatalf("first segment %+v", first.Detail)
	}
	got := requestCheckpointDetail(t, d, 2, 9000)
	if field(t, got.Detail, "AVC context").Value != "found" || field(t, got.Detail, "Parameter context").Value != "invalidated" || got.Detail.Complete {
		t.Fatalf("reused stale parameters: %+v", got.Detail)
	}
	for _, f := range got.Detail.Fields {
		if f.Name == "referencedSPS" || f.Name == "referencedPPS" {
			t.Fatal("broken replacement reused cached references")
		}
	}
}

func checkpointCount(d *Document) uint32 {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.avcParameters.Count()
}

func requestCheckpointDetail(t *testing.T, d *Document, id, tag uint64) DetailResult {
	t.Helper()
	if _, err := d.StartDetail(id, tag); err != nil {
		t.Fatal(err)
	}
	got := result(t, d, id)
	if got.Err != nil || got.Detail == nil {
		t.Fatalf("detail %+v", got)
	}
	return got
}

func TestAVCParameterSegmentsContinueAndReuseEarlierCheckpoints(t *testing.T) {
	const selected = 17001
	data, _ := checkpointAVCData(t, selected)
	s := source(t, data)
	d := startTest(t, s, nil, DetailTimeout)
	if err := scanDone(t, d); err != nil {
		t.Fatal(err)
	}
	if d.timeout != 3*time.Second {
		t.Fatal("detail deadline changed")
	}
	for i, next := range []uint64{8192, 16384, selected} {
		got := requestCheckpointDetail(t, d, uint64(i+1), selected)
		state := "budget_exhausted"
		if i == 2 {
			state = "found"
		}
		if field(t, got.Detail, "Parameter context").Value != state || field(t, got.Detail, "nextTagId").Value != strconv.FormatUint(next, 10) || got.Detail.Complete != (i == 2) || checkpointCount(d) != uint32(i+1) {
			t.Fatalf("segment %d: %+v", i, got.Detail)
		}
		readCount := s.reads.Load()
		// A transport retry with the same request ID is still idempotent.
		retried, err := d.StartDetail(uint64(i+1), selected)
		if err != nil || retried.State != "ready" || s.reads.Load() != readCount {
			t.Fatalf("idempotency: %+v %v", retried, err)
		}
	}
	got := requestCheckpointDetail(t, d, 4, selected)
	if !got.Detail.Complete || field(t, got.Detail, "replayedTags").Value != "0" || checkpointCount(d) != 3 {
		t.Fatalf("exact checkpoint reuse: %+v", got.Detail)
	}
	got = requestCheckpointDetail(t, d, 5, 9000)
	if field(t, got.Detail, "Parameter context").Value != "found" || field(t, got.Detail, "replayedTags").Value != "808" {
		t.Fatalf("backward nearest checkpoint: %+v", got.Detail)
	}
	got = requestCheckpointDetail(t, d, 6, 4000)
	if field(t, got.Detail, "Parameter context").Value != "found" || field(t, got.Detail, "replayedTags").Value != "4000" {
		t.Fatalf("no future checkpoint reuse: %+v", got.Detail)
	}
	if err := d.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if checkpointCount(d) != 0 || s.closes.Load() != 1 {
		t.Fatal("close retained checkpoints or source")
	}
}

func TestAVCSupersededDetailCannotCommitFinishedSegment(t *testing.T) {
	const selected = 17001
	data, offsets := checkpointAVCData(t, selected)
	s := source(t, data)
	d := startTest(t, s, nil, DetailTimeout)
	if err := scanDone(t, d); err != nil {
		t.Fatal(err)
	}
	requestCheckpointDetail(t, d, 1, selected)
	s.blockOffset = offsets[selected] + 5 // selected NAL length, after replay completes
	s.armed.Store(true)
	if _, err := d.StartDetail(2, selected); err != nil {
		t.Fatal(err)
	}
	receive(t, s.entered)
	if checkpointCount(d) != 1 {
		t.Fatal("segment committed before selected parse/source validation")
	}
	if _, err := d.StartDetail(3, 0); err != nil {
		t.Fatal(err)
	}
	s.armed.Store(false)
	close(s.release)
	if got := result(t, d, 3); got.Err != nil || !got.Detail.Complete {
		t.Fatalf("replacement %+v", got)
	}
	if checkpointCount(d) != 1 {
		t.Fatal("superseded request committed its checkpoint")
	}
	got := requestCheckpointDetail(t, d, 4, selected)
	if field(t, got.Detail, "nextTagId").Value != "16384" || field(t, got.Detail, "Parameter context").Value != avccontext.StateBudgetExhausted {
		t.Fatalf("superseded progress reused: %+v", got.Detail)
	}
}

func TestAVCSourceChangeDiscardsExistingAndUncommittedCheckpoints(t *testing.T) {
	const selected = 9000
	data, offsets := checkpointAVCData(t, selected)
	s := source(t, data)
	d := startTest(t, s, nil, DetailTimeout)
	if err := scanDone(t, d); err != nil {
		t.Fatal(err)
	}
	requestCheckpointDetail(t, d, 1, selected)
	s.blockOffset = offsets[selected] + 5
	s.armed.Store(true)
	if _, err := d.StartDetail(2, selected); err != nil {
		t.Fatal(err)
	}
	receive(t, s.entered)
	info, err := s.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chtimes(s.Name(), info.ModTime(), info.ModTime().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	s.armed.Store(false)
	close(s.release)
	if got := result(t, d, 2); !errors.Is(got.Err, ErrSourceChanged) || got.Detail != nil {
		t.Fatalf("changed source %+v", got)
	}
	if checkpointCount(d) != 0 {
		t.Fatal("source change retained checkpoints")
	}
	reads := s.reads.Load()
	if _, err = d.StartDetail(3, selected); err != nil {
		t.Fatal(err)
	}
	if got := result(t, d, 3); !errors.Is(got.Err, ErrSourceChanged) || s.reads.Load() != reads {
		t.Fatalf("changed source reused/read %+v", got)
	}
}

func TestAVCDeadlineDoesNotCommitCompletedReplay(t *testing.T) {
	data, offsets := checkpointAVCData(t, 3)
	s := source(t, data)
	d := startTest(t, s, nil, 100*time.Millisecond)
	if err := scanDone(t, d); err != nil {
		t.Fatal(err)
	}
	s.blockOffset = offsets[3] + 5
	s.armed.Store(true)
	if _, err := d.StartDetail(1, 3); err != nil {
		t.Fatal(err)
	}
	receive(t, s.entered)
	select {
	case <-time.After(150 * time.Millisecond):
	case <-t.Context().Done():
		t.Fatal(t.Context().Err())
	}
	s.armed.Store(false)
	close(s.release)
	if got := result(t, d, 1); !errors.Is(got.Err, context.DeadlineExceeded) || got.Detail != nil {
		t.Fatalf("deadline %+v", got)
	}
	if checkpointCount(d) != 0 {
		t.Fatal("timed-out request committed checkpoint")
	}
}
