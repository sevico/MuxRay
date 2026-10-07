package document

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/sevico/muxray/internal/detailview"
	"github.com/sevico/muxray/internal/session"
)

func mp3Data(payloads ...[]byte) []byte {
	data := fixture(payloads...)
	start := 13
	for _, payload := range payloads {
		data[start] = 8
		start += 15 + len(payload)
	}
	return data
}

func mp3Payload() []byte { return []byte{0x2f, 0xff, 0xfb, 0x90, 0x64} }

func mp3CompletePayload() []byte {
	// One complete 417-byte declared MPEG frame; its body remains opaque.
	payload := make([]byte, 1+417)
	copy(payload, []byte{0x2f, 0xff, 0xfb, 0x90, 0x64})
	return payload
}

func TestDefaultMP3DetailRetainsSourceAndDamagedPrefix(t *testing.T) {
	data := mp3Data(mp3CompletePayload(), mp3CompletePayload())
	// The second payload is incomplete; the first is still eligible for details.
	data = data[:len(data)-7]
	s := source(t, data)
	d := startTest(t, s, nil, DetailTimeout)
	if err := scanDone(t, d); err != nil {
		t.Fatal(err)
	}
	if _, err := d.StartDetail(1, 0); err != nil {
		t.Fatal(err)
	}
	got := result(t, d, 1)
	if got.Err != nil || got.Detail == nil || got.Detail.Kind != "mp3Audio" || !got.Detail.Complete || field(t, got.Detail, "bitrateKbps").Value != "128" {
		t.Fatalf("retained prefix: %+v", got)
	}
	reads := s.reads.Load()
	if _, err := d.StartDetail(1, 0); err != nil {
		t.Fatal(err)
	}
	if again := result(t, d, 1); again.Detail == nil || s.reads.Load() != reads || s.closes.Load() != 0 {
		t.Fatal("idempotent query reread or closed source")
	}
	if _, err := d.StartDetail(2, 1); err != nil {
		t.Fatal(err)
	}
	if got := result(t, d, 2); !errors.Is(got.Err, detailview.ErrTruncated) || got.Detail != nil {
		t.Fatalf("incomplete payload: %+v", got)
	}
}

func TestMP3CanceledScanPrefixRemainsInspectable(t *testing.T) {
	data := mp3Data(mp3CompletePayload(), mp3CompletePayload())
	s := source(t, data)
	s.blockOffset = 13 + 15 + int64(len(mp3CompletePayload())) // second Tag header, after the first complete MP3 payload
	s.armed.Store(true)
	d := startTest(t, s, nil, DetailTimeout)
	receive(t, s.entered)
	d.Cancel()
	close(s.release)
	if err := scanDone(t, d); !errors.Is(err, context.Canceled) {
		t.Fatalf("scan: %v", err)
	}
	if _, err := d.StartDetail(1, 0); err != nil {
		t.Fatal(err)
	}
	if got := result(t, d, 1); got.Err != nil || got.Detail == nil || !got.Detail.Complete || got.Detail.Kind != "mp3Audio" {
		t.Fatalf("canceled scan prefix: %+v", got)
	}
	if d.Snapshot().State != session.StateCanceled || s.closes.Load() != 0 {
		t.Fatal("detail altered scan outcome or file lifetime")
	}
}

func TestMP3HeaderReadSourceChangeDiscardsResult(t *testing.T) {
	s := source(t, mp3Data(mp3Payload()))
	d := startTest(t, s, nil, DetailTimeout)
	if err := scanDone(t, d); err != nil {
		t.Fatal(err)
	}
	s.blockOffset = 25 // the MPEG header, after successful FLV discriminator reads
	s.armed.Store(true)
	if _, err := d.StartDetail(1, 0); err != nil {
		t.Fatal(err)
	}
	receive(t, s.entered)
	info, err := s.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(s.Name(), info.ModTime(), info.ModTime().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	close(s.release)
	if got := result(t, d, 1); !errors.Is(got.Err, ErrSourceChanged) || got.Detail != nil {
		t.Fatalf("source change published MP3: %+v", got)
	}
	reads := s.reads.Load()
	if _, err := d.StartDetail(2, 0); err != nil {
		t.Fatal(err)
	}
	if got := result(t, d, 2); !errors.Is(got.Err, ErrSourceChanged) || got.Detail != nil || s.reads.Load() != reads {
		t.Fatalf("source change was not latched: %+v", got)
	}
}

func TestMP3SupersededHeaderReadCannotPublish(t *testing.T) {
	s := source(t, mp3Data(mp3Payload(), []byte{0x2f, 0xff, 0xf3, 0x80, 0xc4}))
	d := startTest(t, s, nil, DetailTimeout)
	if err := scanDone(t, d); err != nil {
		t.Fatal(err)
	}
	s.blockOffset = 25
	s.armed.Store(true)
	if _, err := d.StartDetail(1, 0); err != nil {
		t.Fatal(err)
	}
	receive(t, s.entered)
	reads := s.reads.Load()
	if _, err := d.StartDetail(2, 1); err != nil {
		t.Fatal(err)
	}
	if got, err := d.GetDetail(2); err != nil || got.State != "pending" || s.reads.Load() != reads {
		t.Fatalf("supersession bypassed blocked header: %+v %v", got, err)
	}
	close(s.release)
	got := result(t, d, 2)
	if got.Err != nil || got.Detail == nil || got.TagID != 1 || field(t, got.Detail, "bitrateKbps").Value != "64" {
		t.Fatalf("stale MP3 result: %+v", got)
	}
	if _, err := d.GetDetail(1); !errors.Is(err, ErrStaleRequest) {
		t.Fatalf("stale query: %v", err)
	}
}

func TestMP3CloseWaitsForBlockedHeaderRead(t *testing.T) {
	s := source(t, mp3Data(mp3Payload()))
	d := startTest(t, s, nil, DetailTimeout)
	if err := scanDone(t, d); err != nil {
		t.Fatal(err)
	}
	s.blockOffset = 25
	s.armed.Store(true)
	if _, err := d.StartDetail(1, 0); err != nil {
		t.Fatal(err)
	}
	receive(t, s.entered)
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	if err := d.Close(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("close: %v", err)
	}
	if s.closes.Load() != 0 || d.Snapshot().State.Terminal() {
		t.Fatal("blocked source released early")
	}
	if _, err := d.StartDetail(2, 0); !errors.Is(err, session.ErrClosed) {
		t.Fatalf("closed admission: %v", err)
	}
	if _, err := d.Page(0, 1); !errors.Is(err, session.ErrClosed) {
		t.Fatalf("closed page: %v", err)
	}
	close(s.release)
	if err := d.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if s.closes.Load() != 1 || !d.Snapshot().State.Terminal() {
		t.Fatal("cleanup did not finish exactly once")
	}
}
