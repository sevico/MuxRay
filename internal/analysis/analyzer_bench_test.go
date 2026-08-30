package analysis

import (
	"context"
	"fmt"
	"io"
	"testing"

	"github.com/sevico/muxray/internal/flv"
)

func BenchmarkAnalyzeInterleavedAACAVCNALU(b *testing.B) {
	const (
		pairCount       = 1 << 14
		mediaBodyBytes  = 512
		millisecondsPer = 20
	)
	audioPayload := make([]byte, 2+mediaBodyBytes)
	audioPayload[0], audioPayload[1] = 0xaf, 1
	videoInterPayload := make([]byte, 5+mediaBodyBytes)
	videoInterPayload[0], videoInterPayload[1] = 0x27, 1
	videoKeyPayload := append([]byte(nil), videoInterPayload...)
	videoKeyPayload[0] = 0x17
	for index := 2; index < len(audioPayload); index++ {
		audioPayload[index] = byte(index)
	}
	for index := 5; index < len(videoInterPayload); index++ {
		videoInterPayload[index] = byte(index)
		videoKeyPayload[index] = byte(index)
	}

	tags := make([]analysisFixtureTag, 0, pairCount*2)
	for index := 0; index < pairCount; index++ {
		dts := uint32(index * millisecondsPer)
		tags = append(tags, analysisFixtureTag{
			rawTypeFlags: byte(flv.TagTypeAudio), dts: dts, payload: audioPayload,
		})
		videoPayload := videoInterPayload
		if index%60 == 0 {
			videoPayload = videoKeyPayload
		}
		tags = append(tags, analysisFixtureTag{
			rawTypeFlags: byte(flv.TagTypeVideo), dts: dts, payload: videoPayload,
		})
	}
	data, scan := scanAnalysisFixture(b, tags...)
	source := newAnalysisFramingOnlyReaderAt(data, scan)
	ctx := context.Background()
	wantSourceBytesPerOperation := uint64(pairCount * (2 + 5))

	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		result, err := Analyze(ctx, source, scan, Options{})
		if err != nil {
			b.Fatalf("Analyze: %v", err)
		}
		if !result.Complete || result.Audio.MediaTagCount != pairCount || result.Video.MediaTagCount != pairCount {
			b.Fatalf("unexpected result: complete=%v audio=%d video=%d", result.Complete, result.Audio.MediaTagCount, result.Video.MediaTagCount)
		}
	}
	b.StopTimer()
	b.ReportMetric(float64(len(scan.Tags)), "tags/op")
	b.ReportMetric(float64(len(data)), "indexed-B/op")
	b.ReportMetric(float64(wantSourceBytesPerOperation), "source-B/op")
	if want := uint64(b.N) * wantSourceBytesPerOperation; source.bytesRead != want {
		b.Fatalf("Analyze read %d source bytes, want framing-only total %d", source.bytesRead, want)
	}
}

// analysisFramingOnlyReaderAt allows reads inside FLV AAC/AVC framing prefixes
// and fails if Analyze starts reading ordinary media payload bytes.
type analysisFramingOnlyReaderAt struct {
	data       []byte
	allowedEnd map[int64]int64
	bytesRead  uint64
}

func newAnalysisFramingOnlyReaderAt(data []byte, scan flv.ScanResult) *analysisFramingOnlyReaderAt {
	reader := &analysisFramingOnlyReaderAt{
		data:       data,
		allowedEnd: make(map[int64]int64, len(scan.Tags)*5),
	}
	for _, tag := range scan.Tags {
		var prefixBytes uint64
		switch tag.Type() {
		case flv.TagTypeAudio:
			prefixBytes = 2
		case flv.TagTypeVideo:
			prefixBytes = 5
		default:
			continue
		}
		start := int64(tag.PayloadSpan().ByteOffset)
		end := start + int64(prefixBytes)
		for offset := start; offset < end; offset++ {
			reader.allowedEnd[offset] = end
		}
	}
	return reader
}

func (reader *analysisFramingOnlyReaderAt) ReadAt(destination []byte, offset int64) (int, error) {
	allowedEnd, ok := reader.allowedEnd[offset]
	if !ok || offset < 0 || int64(len(destination)) > allowedEnd-offset {
		return 0, fmt.Errorf("benchmark source rejected non-framing read at offset %d length %d", offset, len(destination))
	}
	n := copy(destination, reader.data[offset:])
	reader.bytesRead += uint64(n)
	if n != len(destination) {
		return n, io.EOF
	}
	return n, nil
}
