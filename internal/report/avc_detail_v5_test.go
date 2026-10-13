package report

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/sevico/muxray/internal/avc"
	"github.com/sevico/muxray/internal/avccontext"
	"github.com/sevico/muxray/internal/flv"
)

func v5Payload(nal []byte) []byte {
	p := []byte{0x27, 1, 0, 0, 0}
	p = binary.BigEndian.AppendUint32(p, uint32(len(nal)))
	return append(p, nal...)
}
func v5Document(t *testing.T, payload []byte) (AVCTagDetailDocument, flv.AVCVideo, flv.TagRecord) {
	t.Helper()
	data := makeVideoFLV(payload)
	src := bytes.NewReader(data)
	scan, e := flv.Scan(src, uint64(len(data)), flv.Options{})
	if e != nil || !scan.Complete {
		t.Fatal(e)
	}
	tag := scan.Tags[0]
	p, e := flv.ParseAVCVideo(src, tag, flv.AVCVideoOptions{NALLengthSize: 4, AVC: avc.Options{ParseSEI: true, ParseVUI: true, ParseFullHeaders: true, ParseSliceHeaders: true}})
	if e != nil {
		t.Fatal(e)
	}
	return NewAVCTagDetailV5(uint64(len(data)), tag, p, avccontext.ParameterResolution{}), p, tag
}
func TestAVCTagDetailV5Goldens(t *testing.T) {
	// Hand-authored recovery syntax cnt0/exact1/broken0/idc2, then binary UUID,
	// application bytes ff00, opaque type1 payload aa, and exact trailing bits.
	valid := []byte{6, 6, 1, 0xd4, 5, 18}
	for i := byte(1); i <= 16; i++ {
		valid = append(valid, i)
	}
	valid = append(valid, 255, 0, 1, 1, 0xaa, 128)
	budget := []byte{6}
	for i := 0; i < 65; i++ {
		budget = append(budget, 1, 0)
	}
	budget = append(budget, 128)
	for _, tc := range []struct {
		name     string
		payload  []byte
		complete bool
	}{
		{"flv-avc-detail-sei-v5.json", v5Payload(valid), true},
		{"flv-avc-detail-sei-truncated-v5.json", v5Payload([]byte{6, 5, 17, 1, 2}), false},
		{"flv-avc-detail-sei-budget-v5.json", v5Payload(budget), false},
		{"flv-avc-detail-sei-config-v5.json", append([]byte{0x17, 0, 0, 0, 0, 1, 66, 0, 30, 255, 225, 0, 5}, append([]byte{6, 6, 1, 0xd4, 128}, 0)...), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, p, tag := v5Document(t, tc.payload)
			if d.SchemaVersion != 5 || d.Detail.Complete != tc.complete {
				t.Fatalf("envelope %+v %+v", d.Detail, d.Diagnostics)
			}
			g, e := NewTagDetailV5(d.Source.SizeBytes, tag, p, avccontext.ParameterResolution{})
			if e != nil || !reflect.DeepEqual(d, g) {
				t.Fatal("router", e)
			}
			var output bytes.Buffer
			if e := WriteParsedTagDetailJSON(&output, d, true); e != nil {
				t.Fatal(e)
			}
			path := filepath.Join("..", "..", "testdata", "golden", tc.name)
			if os.Getenv("MUXRAY_UPDATE_GOLDEN") == "1" {
				if e := os.WriteFile(path, output.Bytes(), 0600); e != nil {
					t.Fatal(e)
				}
			}
			want, e := os.ReadFile(path)
			if e != nil || !bytes.Equal(want, output.Bytes()) {
				t.Fatalf("golden %s %v", path, e)
			}
			for _, old := range []AVCTagDetailDocument{NewAVCTagDetail(d.Source.SizeBytes, tag, p), NewAVCTagDetailV2(d.Source.SizeBytes, tag, p), NewAVCTagDetailV3(d.Source.SizeBytes, tag, p, avccontext.ParameterResolution{}), NewAVCTagDetailV4(d.Source.SizeBytes, tag, p, avccontext.ParameterResolution{})} {
				var b bytes.Buffer
				if e := WriteAVCTagDetailJSON(&b, old, false); e != nil {
					t.Fatal(e)
				}
				if bytes.Contains(b.Bytes(), []byte(`"sei":`)) {
					t.Fatal("v5 leaked into old projection")
				}
			}
		})
	}
}
func TestAVCTagDetailV5RejectsOtherKinds(t *testing.T) {
	for _, p := range []flv.ParsedTagDetail{nil, (*flv.AVCVideo)(nil), flv.MP3Audio{}, flv.AACAudio{}} {
		r, e := NewTagDetailV5(0, flv.TagRecord{}, p, avccontext.ParameterResolution{})
		if r != nil || !errors.Is(e, ErrDetailV5Unsupported) {
			t.Fatal(r, e)
		}
	}
}
