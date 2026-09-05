package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/sevico/muxray/internal/flv"
)

func TestGenerateExactFraming(t *testing.T) {
	for _, shape := range [][2]uint64{{13 + 1500, 100}, {2001, 100}, {(1 << 20) + 17, 1025}} {
		var data bytes.Buffer
		if err := generate(context.Background(), &data, shape[0], shape[1]); err != nil {
			t.Fatal(err)
		}
		if uint64(data.Len()) != shape[0] {
			t.Fatal("wrong physical size")
		}
		scan, err := flv.ScanContext(nil, bytes.NewReader(data.Bytes()), shape[0], flv.Options{})
		if err != nil || scan.HasErrors() || scan.Index().Len() != shape[1] {
			t.Fatal("invalid fixture")
		}
		for id, record := range scan.Index().All() {
			if record.Offset != tagOffset(shape[0], shape[1], id) || record.DataSize != payloadSize(shape[0], shape[1], id) {
				t.Fatal("offset/size oracle")
			}
		}
	}
}

func TestGeneratorBoundsFailuresAndNoOverwrite(t *testing.T) {
	for _, shape := range [][2]uint64{{0, 0}, {13, 1}, {maxFixtureSize + 1, 100}, {maxFixtureSize, 1}, {math.MaxUint64, 1}, {100, flv.HardMaxTags + 1}} {
		if err := generate(context.Background(), io.Discard, shape[0], shape[1]); err == nil {
			t.Fatal("accepted invalid shape")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := generate(ctx, io.Discard, 28, 1); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := generate(context.Background(), shortWriter{}, 28, 1); !errors.Is(err, io.ErrShortWrite) {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "fixture.flv")
	args := []string{"generate", "--file", path, "--tags", "100", "--size-bytes", "10000"}
	if err := run(context.Background(), args, io.Discard); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := run(context.Background(), args, io.Discard); err == nil {
		t.Fatal("overwrite allowed")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("existing fixture changed")
	}
	if err := run(context.Background(), []string{"measure", "--file", path, "--tags", "100", "--runs", "2"}, io.Discard); err != nil {
		t.Fatal(err)
	}
}

func TestStats(t *testing.T) {
	got := stats([]float64{10, 1, 3, 2})
	if got.Median != 2.5 || got.P95 != 10 || got.Values[0] != 10 {
		t.Fatal(got)
	}
}

type shortWriter struct{}

func (shortWriter) Write(p []byte) (int, error) { return len(p) - 1, nil }
