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

// Structural Baseline SPS/PPS and IDR prefix: 320x240, frame_num and POC
// width 4, no VUI, PPS extensions absent. The slice is a prefix fixture,
// deliberately not an encoded picture or a decoder conformance sample.
const v3ConfigHex = "0142001effe100086742001ef40a0fc801000468ce3c80"

func v3ConfigPayload(t *testing.T) []byte {
	return append([]byte{0x17, 0, 0, 0, 0}, mustDecodeHex(t, v3ConfigHex)...)
}
func v3NALPayload(nal ...byte) []byte {
	return append([]byte{0x27, 1, 0, 0, 0, 0, 0, 0, byte(len(nal))}, nal...)
}
func v3Document(t *testing.T, data []byte, selected uint64) (AVCTagDetailDocument, flv.AVCVideo, flv.TagRecord, avccontext.ParameterResolution) {
	t.Helper()
	source := bytes.NewReader(data)
	scan, err := flv.Scan(source, uint64(len(data)), flv.Options{})
	if err != nil || !scan.Complete {
		t.Fatalf("scan: %v %+v", err, scan.Diagnostics)
	}
	tag := scan.Tags[selected]
	lookup := func(id uint64) (flv.TagRecord, error) {
		if id >= uint64(len(scan.Tags)) {
			return flv.TagRecord{}, avccontext.ErrInvalidInput
		}
		return scan.Tags[id], nil
	}
	framing, err := avccontext.Resolve(context.Background(), source, tag, lookup, flv.AVCVideoOptions{}, avccontext.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	params, err := avccontext.ResolveParameters(context.Background(), source, tag, lookup, framing, flv.AVCVideoOptions{}, avccontext.ParameterLimits{})
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := flv.ParseAVCVideo(source, tag, flv.AVCVideoOptions{NALLengthSize: 4, AVC: avc.Options{ParseVUI: true, ParsePPS: true, ParseSliceHeaders: true, ParameterSets: params.Context}})
	if err != nil {
		t.Fatal(err)
	}
	return NewAVCTagDetailV3(uint64(len(data)), tag, parsed, params), parsed, tag, params
}

func TestAVCTagDetailV3Goldens(t *testing.T) {
	config := makeVideoFLV(v3ConfigPayload(t))
	nalu := makeVideoFLV(v3NALPayload(0x65, 0xb8, 0x40))
	withContext := append(append([]byte{}, config...), nalu[13:]...)
	for _, tc := range []struct {
		name     string
		data     []byte
		tag      uint64
		complete bool
	}{
		{"flv-avc-detail-v3.json", config, 0, true},
		{"flv-avc-detail-nalu-v3.json", withContext, 1, true},
		{"flv-avc-detail-missing-v3.json", nalu, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, parsed, tag, params := v3Document(t, tc.data, tc.tag)
			if d.SchemaVersion != 3 || d.Detail.Complete != tc.complete || d.Detail.ParameterContext == nil {
				t.Fatalf("envelope %+v", d)
			}
			if d.Detail.AVCDecoderConfigurationRecord != nil {
				p := d.Detail.AVCDecoderConfigurationRecord.PictureParameterSets[0].PictureParameterSet
				if p.Prefix == nil || !p.Prefix.PrefixComplete || p.Prefix.NumSliceGroupsMinus1.Value != 0 || p.Prefix.PicInitQPMinus26.Value != 0 {
					t.Fatal("PPS syntax missing")
				}
			} else {
				s := d.Detail.NALUnitSequence.NALUnits[0].SliceHeaderPrefix
				if s == nil || s.PrefixComplete != tc.complete || s.SliceTypeName != "I" || s.PictureParameterSetID.Value != 0 {
					t.Fatalf("slice prefix %+v", s)
				}
				if tc.complete {
					if s.FrameNum.Value != 0 || s.PicOrderCntLSB.Value != 0 || s.ReferencedSPSSpan == nil || s.ReferencedSPSSpan.ByteOffset >= tag.PayloadSpan().ByteOffset {
						t.Fatal("lost preceding source provenance")
					}
				} else if s.FrameNum != nil || s.ReferencedSPSID != nil || len(d.Diagnostics) == 0 {
					t.Fatal("invented missing context")
				}
			}
			generic, err := NewTagDetailV3(uint64(len(tc.data)), tag, parsed, params)
			if err != nil || !reflect.DeepEqual(generic, d) {
				t.Fatalf("router: %v", err)
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
				t.Fatalf("golden %s: %v", tc.name, err)
			}
			for _, legacy := range []AVCTagDetailDocument{NewAVCTagDetail(uint64(len(tc.data)), tag, parsed), NewAVCTagDetailV2(uint64(len(tc.data)), tag, parsed)} {
				var old bytes.Buffer
				if err := WriteAVCTagDetailJSON(&old, legacy, false); err != nil {
					t.Fatal(err)
				}
				for _, key := range []string{`"prefix"`, `"sliceHeaderPrefix"`, `"parameterContext"`} {
					if bytes.Contains(old.Bytes(), []byte(key)) {
						t.Fatalf("v3 key %s leaked into schema %d", key, legacy.SchemaVersion)
					}
				}
			}
		})
	}
}

func TestAVCTagDetailV3TruncatedPrefixRetainsPartialFields(t *testing.T) {
	config := makeVideoFLV(v3ConfigPayload(t))
	nalu := makeVideoFLV(v3NALPayload(0x65, 0xb8))
	data := append(config, nalu[13:]...)
	d, _, tag, _ := v3Document(t, data, 1)
	s := d.Detail.NALUnitSequence.NALUnits[0].SliceHeaderPrefix
	if d.Detail.Complete || s.PrefixComplete || s.SliceTypeName != "I" || s.PictureParameterSetID == nil || s.FrameNum != nil || s.PicOrderCntLSB != nil {
		t.Fatalf("partial %+v", s)
	}
	if len(d.Diagnostics) == 0 || d.Diagnostics[0].Code != avc.CodeRBSPSyntaxTruncated || *d.Diagnostics[0].RecoveryOffset != tag.PayloadSpan().ByteOffset+uint64(tag.DataSize) {
		t.Fatalf("diagnostics %+v", d.Diagnostics)
	}
}

func TestAVCTagDetailV3RejectsOtherKindsAndNil(t *testing.T) {
	var nilAVC *flv.AVCVideo
	for _, value := range []flv.ParsedTagDetail{nil, nilAVC, flv.AACAudio{}, flv.ScriptData{}} {
		result, err := NewTagDetailV3(0, flv.TagRecord{}, value, avccontext.ParameterResolution{})
		if result != nil || !errors.Is(err, ErrDetailV3Unsupported) {
			t.Fatalf("unsupported %T: %v", value, err)
		}
	}
}
