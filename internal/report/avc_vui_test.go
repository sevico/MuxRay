package report

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/sevico/muxray/internal/avc"
	"github.com/sevico/muxray/internal/flv"
)

func parseAVCVUI(t *testing.T, payload []byte, width uint8) (AVCTagDetailDocument, AVCTagDetailDocument) {
	t.Helper()
	data, tag, legacy := parseAVCTagDetail(t, payload, width)
	parsed, err := flv.ParseAVCVideo(bytes.NewReader(data), tag, flv.AVCVideoOptions{AVC: avc.Options{ParseVUI: true}, NALLengthSize: width})
	if err != nil {
		t.Fatal(err)
	}
	return NewAVCTagDetail(uint64(len(data)), tag, legacy), NewAVCTagDetailV2(uint64(len(data)), tag, parsed)
}

func TestAVCTagDetailV2Golden(t *testing.T) {
	payload := append([]byte{0x17, 0, 0, 0, 0}, mustDecodeHex(t, avcCHigh320x180Hex)...)
	legacy, document := parseAVCVUI(t, payload, 0)
	if document.SchemaVersion != 2 || document.DocumentType != AVCTagDetailDocumentType || !document.Detail.Complete {
		t.Fatalf("v2: %+v", document)
	}
	v := document.Detail.AVCDecoderConfigurationRecord.SequenceParameterSets[0].SequenceParameterSet.VUI
	if v == nil || !v.Present || !v.Complete || !v.TrailingBitsValid || v.RemainingRBSPBits != 0 || v.NumUnitsInTick.Value != 1 || v.TimeScale.Value != 50 || len(v.TimeScale.Span.SourceSpans) < 2 {
		t.Fatalf("VUI: %+v", v)
	}
	if legacy.Detail.AVCDecoderConfigurationRecord.SequenceParameterSets[0].SequenceParameterSet.VUI != nil {
		t.Fatal("VUI leaked into v1 projection")
	}
	var output bytes.Buffer
	if err := WriteParsedTagDetailJSON(&output, document, true); err != nil {
		t.Fatal(err)
	}
	goldenPath := filepath.Join("..", "..", "testdata", "golden", "flv-avc-detail-v2.json")
	if os.Getenv("MUXRAY_UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(goldenPath, output.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(output.Bytes(), want) {
		t.Fatal("AVC v2 differs from golden")
	}
	var roundTrip AVCTagDetailDocument
	if err := json.Unmarshal(output.Bytes(), &roundTrip); err != nil {
		t.Fatal(err)
	}
	var again bytes.Buffer
	if err := WriteParsedTagDetailJSON(&again, roundTrip, true); err != nil || !bytes.Equal(output.Bytes(), again.Bytes()) {
		t.Fatalf("v2 round trip: %v", err)
	}
}

func TestAVCTagDetailV2PartialVUIAndInBandSPS(t *testing.T) {
	config := mustDecodeHex(t, avcCHigh320x180Hex)
	sps := config[8:34]
	for _, size := range []int{10, 21, len(sps)} {
		payload := []byte{0x27, 1, 0, 0, 0}
		var n [4]byte
		binary.BigEndian.PutUint32(n[:], uint32(size))
		payload = append(payload, n[:]...)
		payload = append(payload, sps[:size]...)
		legacy, document := parseAVCVUI(t, payload, 4)
		if !legacy.Detail.Complete {
			t.Fatalf("phase-one fixture incomplete at %d", size)
		}
		s := document.Detail.NALUnitSequence.NALUnits[0].SequenceParameterSet
		if s.VUI == nil || !s.Complete {
			t.Fatalf("VUI partial changed base: %+v", s)
		}
		if size == len(sps) {
			if !document.Detail.Complete || !s.VUI.Complete {
				t.Fatal("in-band complete SPS not expanded")
			}
		} else {
			if document.Detail.Complete || s.VUI.Complete || len(document.Diagnostics) == 0 {
				t.Fatal("partial VUI promoted to complete")
			}
			if size == 10 && (s.VUI.NumUnitsInTick != nil || s.VUI.TimeScale != nil) {
				t.Fatal("unread timing values invented")
			}
		}
	}
}

func TestAVCTagDetailV2PartialGolden(t *testing.T) {
	sps := mustDecodeHex(t, avcCHigh320x180Hex)[8:29] // Complete base SPS, truncated VUI timing.
	payload := []byte{0x27, 1, 0, 0, 0, 0, 0, 0, byte(len(sps))}
	payload = append(payload, sps...)
	_, document := parseAVCVUI(t, payload, 4)
	if document.Detail.Complete || document.Detail.NALUnitSequence.NALUnits[0].SequenceParameterSet.VUI.Complete {
		t.Fatal("fixture must retain incomplete VUI")
	}
	var output bytes.Buffer
	if err := WriteParsedTagDetailJSON(&output, document, true); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join("..", "..", "testdata", "golden", "flv-avc-detail-truncated-v2.json")
	if os.Getenv("MUXRAY_UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(path, output.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(want, output.Bytes()) {
		t.Fatalf("partial VUI golden: %v", err)
	}
}
