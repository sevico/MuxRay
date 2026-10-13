package avc

import (
	"bytes"
	"encoding/hex"
	"testing"

	"github.com/sevico/muxray/internal/cursor"
)

type timingBenchmarkReader struct {
	*bytes.Reader
	bytes, reads uint64
}

func (r *timingBenchmarkReader) ReadAt(p []byte, offset int64) (int, error) {
	n, err := r.Reader.ReadAt(p, offset)
	r.bytes += uint64(n)
	r.reads++
	return n, err
}

var timingBenchmarkContext *ParameterSetContext

func BenchmarkSEITiming(b *testing.B) {
	// Raw bytes independently authored by timing_navigation_oracle.py:
	// two NAL HRD schedules, one VCL schedule, buffering_period then
	// picture_timing with three clock flags, full/partial timestamps and -17.
	configuration, err := hex.DecodeString("0142001effe100206742001ef6146d0800001f480007530680020080110fce731b00080201ce632801000568ce0b5070")
	if err != nil {
		b.Fatal(err)
	}
	packet, err := hex.DecodeString("0000001a06000788818e050881c0010d2526b44a1752a7bd442170e20580")
	if err != nil {
		b.Fatal(err)
	}
	data := append(append([]byte(nil), configuration...), packet...)
	configurationSpan := cursor.Span{BitLength: uint32(len(configuration) * 8)}
	packetSpan := cursor.Span{ByteOffset: uint64(len(configuration)), BitLength: uint32(len(packet) * 8)}
	options := Options{ParseSEI: true, ParseSEITiming: true, ParseVUI: true, ParsePPS: true, ParseSliceHeaders: true, ParseFullHeaders: true}
	reader := &timingBenchmarkReader{Reader: bytes.NewReader(data)}
	readContext := func() *ParameterSetContext {
		r, e := ParseDecoderConfigurationRecord(reader, configurationSpan, options)
		if e != nil || !r.Complete {
			b.Fatalf("timing configuration: %v %+v", e, r.Diagnostics)
		}
		context := NewParameterSetContext()
		context.AddConfiguration(r)
		return context
	}
	context := readContext()
	for _, cold := range []bool{true, false} {
		name := "parse_with_existing_context"
		if cold {
			name = "parse_including_configuration"
		}
		b.Run(name, func(b *testing.B) {
			reader.bytes, reader.reads = 0, 0
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				sets := context
				if cold {
					sets = readContext()
				}
				selected := options
				selected.ParameterSets = sets
				r, e := ParseLengthPrefixedNALUnits(reader, packetSpan, 4, selected)
				if e != nil || !r.Complete || len(r.NALUnits) != 1 || len(r.NALUnits[0].SEI.Messages) != 2 {
					b.Fatalf("timing parse: %v %+v", e, r.Diagnostics)
				}
				clocks := r.NALUnits[0].SEI.Messages[1].PictureTiming.ClockTimestamps
				if len(clocks) != 3 || clocks[0].TimeOffset == nil || clocks[0].TimeOffset.Value != -17 {
					b.Fatal("timing workload lost its timestamp syntax")
				}
			}
			b.StopTimer()
			b.ReportMetric(float64(reader.bytes)/float64(b.N), "source-B/op")
			b.ReportMetric(float64(reader.reads)/float64(b.N), "reads/op")
		})
	}
	b.Run("compact_context_clone", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			timingBenchmarkContext = context.CompactClone()
		}
		b.ReportMetric(0, "source-B/op")
		b.ReportMetric(0, "reads/op")
	})
}
