package session

import (
	"context"
	"errors"
	"math"
	"sync"
	"testing"

	"github.com/sevico/muxray/internal/flv"
)

func TestDiagnosticPages(t *testing.T) {
	data := fixture(2050)
	data[12] = 1
	for i := range 2050 {
		data[13+15*i+10] = 1 // one numeric warning per tag
	}
	source := newSource(data)
	s := startTest(t, nil, source, uint64(len(data)), flv.Options{MaxDiagnostics: 2051})
	if err := wait(t, s); err != nil {
		t.Fatal(err)
	}
	for _, bounds := range []struct {
		start uint64
		count uint32
	}{{0, 1}, {1023, 3}, {1024, 1025}, {2050, 4096}, {2051, 1}} {
		page, err := s.DiagnosticPage(bounds.start, bounds.count)
		next := min(bounds.start+uint64(bounds.count), 2051)
		if err != nil || page.Start != bounds.start || page.Limit != bounds.count || page.Next != next || page.Total != 2051 || uint64(len(page.Records)) != next-bounds.start || page.Records == nil {
			t.Fatalf("page: %+v %v", page, err)
		}
		if len(page.Records) > 0 {
			page.Records[0].Code = "changed"
			*page.Records[0].Expected = 55
			*page.Records[0].Actual = 66
		}
	}
	for _, bounds := range []struct {
		start uint64
		count uint32
	}{{0, 0}, {0, 4097}, {2052, 1}, {math.MaxUint64, 1}} {
		if _, err := s.DiagnosticPage(bounds.start, bounds.count); err == nil {
			t.Fatalf("invalid bounds accepted: %+v", bounds)
		}
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 20 {
				page, err := s.DiagnosticPage(0, 1)
				if err != nil || page.Records[0].Code != flv.CodePreviousTagSize0NonZero || *page.Records[0].Expected != 0 || *page.Records[0].Actual != 1 {
					t.Errorf("diagnostic ownership: %+v %v", page, err)
					return
				}
				*page.Records[0].Actual = 42
			}
		})
	}
	wg.Wait()
	retained, _ := s.DiagnosticPage(0, 1)
	_ = s.Close(nil)
	if *retained.Records[0].Actual != 1 {
		t.Fatal("close invalidated returned diagnostics")
	}
	if _, err := s.DiagnosticPage(0, 1); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed diagnostics: %v", err)
	}
}

func TestDiagnosticPageActiveAndEmpty(t *testing.T) {
	source := newSource(fixture(1))
	source.gateRead(13)
	s := startTest(t, nil, source, uint64(source.reader.Len()), flv.Options{})
	await(t, source.readEntered)
	if _, err := s.DiagnosticPage(0, 1); !errors.Is(err, ErrNotReady) {
		t.Fatalf("active diagnostics: %v", err)
	}
	s.Cancel()
	source.releaseRead()
	if err := wait(t, s); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	page, err := s.DiagnosticPage(0, 1)
	if err != nil || page.Records == nil || len(page.Records) != 0 || page.Total != 0 || page.Next != 0 {
		t.Fatalf("empty diagnostics: %+v %v", page, err)
	}
}
