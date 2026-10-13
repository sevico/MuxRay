package document

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/sevico/muxray/internal/flv"
)

// The source is an already-open local file. "Cold" means no document frame
// checkpoints, not a cold operating-system page cache. Scan, file creation,
// opening, UI work, dependency resolution and transport polling are excluded.
type navigationBenchmarkSource struct {
	*os.File
	bytes, reads uint64
}

func (s *navigationBenchmarkSource) ReadAt(p []byte, offset int64) (int, error) {
	n, err := s.File.ReadAt(p, offset)
	s.bytes += uint64(n)
	s.reads++
	return n, err
}

func BenchmarkMP3PageNavigation(b *testing.B) {
	data := mp3Data(mp3PagePayload(3000))
	index, err := flv.Scan(bytes.NewReader(data), uint64(len(data)), flv.Options{})
	if err != nil || !index.Complete || len(index.Tags) != 1 {
		b.Fatalf("fixture scan: %v", err)
	}
	path := filepath.Join(b.TempDir(), "3000-frames.flv")
	if err := os.WriteFile(path, data, 0600); err != nil {
		b.Fatal(err)
	}
	for _, warm := range []bool{false, true} {
		name := "checkpoints_cold"
		if warm {
			name = "checkpoints_warm"
		}
		b.Run(name, func(b *testing.B) {
			file, err := os.Open(path)
			if err != nil {
				b.Fatal(err)
			}
			defer file.Close()
			source := &navigationBenchmarkSource{File: file}
			doc := &Document{source: source}
			frame, page := uint32(2600), uint32(2560)
			seek := func() (requests, pages uint64) {
				for requests < 3 {
					detail, staged, err := doc.readMP3Page(context.Background(), index.Tags[0], &frame, page, false)
					if err != nil || detail == nil {
						b.Fatalf("page read: %v", err)
					}
					doc.mp3Pages.commit(staged)
					requests++
					state := ""
					for _, field := range detail.Fields {
						if field.Name == "pageState" {
							state = field.Value
						}
					}
					pages += uint64(len(staged))
					if state == "ready" {
						return requests, pages
					}
					if state != "preparing" {
						b.Fatalf("unexpected page state %q", state)
					}
				}
				b.Fatal("2600-frame seek exceeded three explicit requests")
				return
			}
			if warm {
				seek()
			}
			source.bytes, source.reads = 0, 0
			var requests, pages uint64
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if !warm {
					doc.mp3Pages = nil
				}
				r, p := seek()
				requests, pages = requests+r, pages+p
			}
			b.StopTimer()
			b.ReportMetric(float64(source.bytes)/float64(b.N), "source-B/op")
			b.ReportMetric(float64(source.reads)/float64(b.N), "reads/op")
			b.ReportMetric(float64(requests)/float64(b.N), "requests/op")
			b.ReportMetric(float64(pages)/float64(b.N), "pages/op")
		})
	}
}
