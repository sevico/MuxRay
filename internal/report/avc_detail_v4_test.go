package report

import (
	"bytes"
	"context"
	"errors"
	"github.com/sevico/muxray/internal/avc"
	"github.com/sevico/muxray/internal/avccontext"
	"github.com/sevico/muxray/internal/flv"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func v4Document(t *testing.T, data []byte, selected uint64) (AVCTagDetailDocument, flv.AVCVideo, flv.TagRecord, avccontext.ParameterResolution) {
	t.Helper()
	src := bytes.NewReader(data)
	scan, e := flv.Scan(src, uint64(len(data)), flv.Options{})
	if e != nil || !scan.Complete {
		t.Fatal(e)
	}
	tag := scan.Tags[selected]
	lookup := func(id uint64) (flv.TagRecord, error) { return scan.Tags[id], nil }
	framing, e := avccontext.Resolve(context.Background(), src, tag, lookup, flv.AVCVideoOptions{}, avccontext.Limits{})
	if e != nil {
		t.Fatal(e)
	}
	options := flv.AVCVideoOptions{NALLengthSize: 4, AVC: avc.Options{ParseVUI: true, ParseFullHeaders: true, ParseSliceHeaders: true}}
	params, e := avccontext.ResolveParameters(context.Background(), src, tag, lookup, framing, options, avccontext.ParameterLimits{})
	if e != nil {
		t.Fatal(e)
	}
	options.AVC.ParameterSets = params.Context
	parsed, e := flv.ParseAVCVideo(src, tag, options)
	if e != nil {
		t.Fatal(e)
	}
	return NewAVCTagDetailV4(uint64(len(data)), tag, parsed, params), parsed, tag, params
}
func TestAVCTagDetailV4Goldens(t *testing.T) {
	config := makeVideoFLV(v3ConfigPayload(t))
	nalu := makeVideoFLV(v3NALPayload(0x65, 0xb8, 0x40, 0xf0))
	partial := makeVideoFLV(v3NALPayload(0x65, 0xb8, 0x40))
	for _, tc := range []struct {
		name     string
		data     []byte
		tag      uint64
		complete bool
	}{
		{"flv-avc-detail-v4.json", config, 0, true},
		{"flv-avc-detail-nalu-v4.json", append(append([]byte{}, config...), nalu[13:]...), 1, true},
		{"flv-avc-detail-truncated-v4.json", append(append([]byte{}, config...), partial[13:]...), 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, parsed, tag, params := v4Document(t, tc.data, tc.tag)
			if d.SchemaVersion != 4 || d.Detail.Complete != tc.complete {
				t.Fatalf("envelope %+v diagnostics %+v", d.Detail, d.Diagnostics)
			}
			if tc.tag == 0 {
				f := d.Detail.AVCDecoderConfigurationRecord.PictureParameterSets[0].PictureParameterSet.Full
				if f == nil || !f.Complete || f.RBSPTrailingBits == nil || f.ExtensionPresent {
					t.Fatal("PPS full")
				}
			} else {
				n := d.Detail.NALUnitSequence.NALUnits[0]
				if n.SliceHeaderFull == nil || n.SliceHeaderFull.Complete != tc.complete || !n.SliceHeaderPrefix.PrefixComplete {
					t.Fatal("slice completeness separation")
				}
			}
			generic, e := NewTagDetailV4(uint64(len(tc.data)), tag, parsed, params)
			if e != nil || !reflect.DeepEqual(d, generic) {
				t.Fatal("router", e)
			}
			var encoded bytes.Buffer
			if e := WriteParsedTagDetailJSON(&encoded, d, true); e != nil {
				t.Fatal(e)
			}
			path := filepath.Join("..", "..", "testdata", "golden", tc.name)
			if os.Getenv("MUXRAY_UPDATE_GOLDEN") == "1" {
				if e := os.WriteFile(path, encoded.Bytes(), 0600); e != nil {
					t.Fatal(e)
				}
			}
			want, e := os.ReadFile(path)
			if e != nil || !bytes.Equal(want, encoded.Bytes()) {
				t.Fatalf("golden %s %v", path, e)
			}
			old := NewAVCTagDetailV3(uint64(len(tc.data)), tag, parsed, params)
			var legacy bytes.Buffer
			if e := WriteAVCTagDetailJSON(&legacy, old, false); e != nil {
				t.Fatal(e)
			}
			if bytes.Contains(legacy.Bytes(), []byte(`"full"`)) || bytes.Contains(legacy.Bytes(), []byte(`"sliceHeaderFull"`)) {
				t.Fatal("v4 leaked into v3")
			}
		})
	}
}
func TestAVCTagDetailV4RejectsOtherKinds(t *testing.T) {
	for _, p := range []flv.ParsedTagDetail{nil, (*flv.AVCVideo)(nil), flv.MP3Audio{}, flv.AACAudio{}} {
		r, e := NewTagDetailV4(0, flv.TagRecord{}, p, avccontext.ParameterResolution{})
		if r != nil || !errors.Is(e, ErrDetailV4Unsupported) {
			t.Fatal(r, e)
		}
	}
}
