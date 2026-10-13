package report

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/sevico/muxray/internal/avc"
	"github.com/sevico/muxray/internal/avccontext"
	"github.com/sevico/muxray/internal/flv"
)

func TestAVCTimingV6GoldensAndLegacyProjection(t *testing.T) {
	// The baseline SPS explicitly has no VUI. Buffering-period ID 0 is
	// sufficient for an empty picture-timing payload; no HRD values are guessed.
	config := makeVideoFLV(v3ConfigPayload(t))
	valid := makeVideoFLV(v5Payload([]byte{6, 0, 1, 0xc0, 1, 0, 0x80}))
	// Authored HRD widths 8/8/7/6 and clock fields from the independent
	// timing_navigation_oracle; these bytes contain no encoded picture.
	hrdConfig := makeVideoFLV(mustDecodeHex(t, "17000000000142001effe100176742001ef6146d0800001f480007530700080201ce631401000568ce0b5070"))
	hrdNAL := makeVideoFLV(v5Payload(mustDecodeHex(t, "0600038881c0010d2526b44a1752a7bd442170e20580")))
	for _, tc := range []struct {
		name     string
		data     []byte
		selected uint64
		complete bool
	}{
		{"flv-avc-detail-timing-v6.json", append(append([]byte{}, config...), valid[13:]...), 1, true},
		{"flv-avc-detail-timing-hrd-v6.json", append(append([]byte{}, hrdConfig...), hrdNAL[13:]...), 1, true},
		{"flv-avc-detail-timing-missing-v6.json", makeVideoFLV(v5Payload([]byte{6, 1, 0, 0x80})), 0, false},
		{"flv-avc-detail-timing-truncated-v6.json", append(append([]byte{}, config...), makeVideoFLV(v5Payload([]byte{6, 0, 1}))[13:]...), 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := bytes.NewReader(tc.data)
			scan, err := flv.Scan(source, uint64(len(tc.data)), flv.Options{})
			if err != nil || !scan.Complete {
				t.Fatal(err)
			}
			tag := scan.Tags[tc.selected]
			lookup := func(id uint64) (flv.TagRecord, error) { return scan.Tags[id], nil }
			options := flv.AVCVideoOptions{NALLengthSize: 4, AVC: avc.Options{ParseVUI: true, ParseFullHeaders: true, ParseSliceHeaders: true, ParseSEI: true, ParseSEITiming: true}}
			framing, err := avccontext.Resolve(context.Background(), source, tag, lookup, options, avccontext.Limits{})
			if err != nil {
				t.Fatal(err)
			}
			parameters, err := avccontext.ResolveParameters(context.Background(), source, tag, lookup, framing, options, avccontext.ParameterLimits{})
			if err != nil {
				t.Fatal(err)
			}
			options.AVC.ParameterSets = parameters.Context
			p, err := flv.ParseAVCVideo(source, tag, options)
			if err != nil {
				t.Fatal(err)
			}
			d := NewAVCTagDetailV6(uint64(len(tc.data)), tag, p, parameters)
			if d.SchemaVersion != 6 || d.Detail.Complete != tc.complete {
				t.Fatalf("completion %+v diagnostics %+v", d.Detail, d.Diagnostics)
			}
			generic, err := NewTagDetailV6(uint64(len(tc.data)), tag, p, parameters)
			if err != nil || !reflect.DeepEqual(d, generic) {
				t.Fatal("router", err)
			}
			if tc.complete {
				messages := d.Detail.NALUnitSequence.NALUnits[0].SEI.Messages
				bp, pt := messages[0].BufferingPeriod, messages[1].PictureTiming
				if bp == nil || pt == nil || bp.Context.State != "resolved" || pt.Context.State != "resolved" || bp.SequenceParameterSetID.Value != 0 || pt.Context.Source != "bufferingPeriod" || pt.Context.SPSSpan.ByteOffset >= tag.Offset {
					t.Fatalf("wrong provenance or invented HRD: %+v %+v", bp, pt)
				}
				if tc.name == "flv-avc-detail-timing-hrd-v6.json" {
					if pt.CPBRemovalDelay.Value != 37 || pt.DPBOutputDelay.Value != 19 || pt.PicStruct.Value != 5 || len(pt.ClockTimestamps) != 3 || pt.ClockTimestamps[0].TimeOffset.Value != -17 || bp.NALInitialDelays[0].InitialCPBRemovalDelay.Value != 17 {
						t.Fatalf("wrong clock fields: %+v", pt)
					}
				} else if pt.CPBRemovalDelay != nil || len(pt.ClockTimestamps) != 0 {
					t.Fatal("invented HRD fields")
				}
			}
			var encoded bytes.Buffer
			if err := WriteParsedTagDetailJSON(&encoded, d, true); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join("..", "..", "testdata", "golden", tc.name)
			if os.Getenv("MUXRAY_UPDATE_GOLDEN") == "1" {
				if err := os.WriteFile(path, encoded.Bytes(), 0600); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(want, encoded.Bytes()) {
				t.Fatalf("golden %s: %v", path, err)
			}
			var old bytes.Buffer
			legacy := NewAVCTagDetailV5(uint64(len(tc.data)), tag, p, parameters)
			for _, m := range legacy.Detail.NALUnitSequence.NALUnits[0].SEI.Messages {
				if m.Kind != "unknown" || m.SyntaxSupported {
					t.Fatal("v6 kind leaked into v5 projection")
				}
			}
			if err := WriteAVCTagDetailJSON(&old, legacy, false); err != nil {
				t.Fatal(err)
			}
			for _, field := range [][]byte{[]byte(`"bufferingPeriod":`), []byte(`"pictureTiming":`), []byte(`"clockTimestamps":`)} {
				if bytes.Contains(old.Bytes(), field) {
					t.Fatal("v6 syntax leaked into v5 projection")
				}
			}
		})
	}
}

func TestAVCTagDetailV6RejectsOtherKinds(t *testing.T) {
	for _, parsed := range []flv.ParsedTagDetail{nil, (*flv.AVCVideo)(nil), flv.MP3Audio{}, flv.AACAudio{}} {
		got, err := NewTagDetailV6(0, flv.TagRecord{}, parsed, avccontext.ParameterResolution{})
		if got != nil || !errors.Is(err, ErrDetailV6Unsupported) {
			t.Fatal(got, err)
		}
	}
}
