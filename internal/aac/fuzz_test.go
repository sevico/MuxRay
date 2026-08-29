package aac

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/sevico/muxray/internal/cursor"
)

func FuzzAudioSpecificConfig(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{0x12, 0x10})
	f.Add([]byte{0x11, 0x90})
	f.Add([]byte{0xf9})
	f.Add(packedFields(field{2, 5}, field{15, 4}, field{48_000, 24}, field{2, 4}, field{0, 3}))

	var sbr bitBuilder
	sbr.write(uint64(AudioObjectTypeSBR), 5)
	sbr.write(3, 4)
	sbr.write(2, 4)
	sbr.write(6, 4)
	sbr.write(uint64(AudioObjectTypeAACLC), 5)
	sbr.write(0, 3)
	f.Add(sbr.bytes())

	options := Options{MaxDiagnostics: 16}
	normalized, err := options.Normalized()
	if err != nil {
		f.Fatal(err)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if uint64(len(data)) > HardMaxPayloadBytes {
			return
		}
		const base = 3
		source := make([]byte, base+len(data)+1)
		copy(source[base:], data)
		span := cursor.Span{ByteOffset: base, BitLength: uint32(len(data) * 8)}
		result, err := ParseAudioSpecificConfig(bytes.NewReader(source), span, options)
		if err != nil {
			t.Fatalf("ParseAudioSpecificConfig: %v", err)
		}
		assertFuzzInvariants(t, result, normalized, base, len(data))

		repeated, err := ParseAudioSpecificConfig(bytes.NewReader(source), span, options)
		if err != nil {
			t.Fatalf("repeat ParseAudioSpecificConfig: %v", err)
		}
		if !reflect.DeepEqual(result, repeated) {
			t.Fatalf("non-deterministic parse\nfirst=%+v\nsecond=%+v", result, repeated)
		}
	})
}

func assertFuzzInvariants(t *testing.T, result Result, options Options, base, length int) {
	t.Helper()
	totalBits := uint64(length * 8)
	if result.BitsConsumed > totalBits || result.TrailingBits > totalBits || result.BitsConsumed+result.TrailingBits != totalBits {
		t.Fatalf("invalid bit accounting: consumed=%d trailing=%d total=%d", result.BitsConsumed, result.TrailingBits, totalBits)
	}
	if uint32(len(result.Diagnostics)) > options.MaxDiagnostics {
		t.Fatalf("diagnostics %d exceed limit %d", len(result.Diagnostics), options.MaxDiagnostics)
	}
	if result.Complete && (!result.GASpecificPresent || !result.GASpecific.Complete || result.HasErrors()) {
		t.Fatalf("complete result violates state invariants: %+v", result)
	}
	if result.SyntaxSpan.ByteOffset != uint64(base) || uint64(result.SyntaxSpan.BitLength) != result.BitsConsumed {
		t.Fatalf("syntax span disagrees with accounting: %+v", result)
	}
	assertAllResultSpans(t, result, base, length)
	end := uint64(base + length)
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Offset < uint64(base) || diagnostic.Offset > end || diagnostic.BitOffset > 7 ||
			(diagnostic.Offset == end && diagnostic.BitOffset != 0) {
			t.Fatalf("diagnostic outside payload: %+v", diagnostic)
		}
		if diagnostic.RecoveryOffset != nil && *diagnostic.RecoveryOffset != end {
			t.Fatalf("recovery=%d, want %d", *diagnostic.RecoveryOffset, end)
		}
	}
	if result.InitialAudioObjectType.Name != "" && result.InitialAudioObjectType.Name != result.InitialAudioObjectType.Value.String() {
		t.Fatalf("initial object type name is inconsistent: %+v", result.InitialAudioObjectType)
	}
	if result.AudioObjectType.Name != "" && result.AudioObjectType.Name != result.AudioObjectType.Value.String() {
		t.Fatalf("effective object type name is inconsistent: %+v", result.AudioObjectType)
	}
}
