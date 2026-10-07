package report

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/sevico/muxray/internal/avccontext"
	"github.com/sevico/muxray/internal/flv"
	"github.com/sevico/muxray/internal/mp3"
)

// Structural Layer III frames test side-info fields and physical dependency
// windows; their zero-filled main data is not a decoder conformance sample.
const mp3V3LongSideHex = "005a5064055a50221b4c0c80ab5a0443698190156d40886d303202ade8110da6"
const mp3V3ShortSideHex = "645a5064055a56844e5c0c80ab5ad089cb8190156d5a1139703202adeb42272e"

func reportMP3V3Frame(t testing.TB, side string) []byte {
	t.Helper()
	raw, err := hex.DecodeString(side)
	if err != nil {
		t.Fatal(err)
	}
	frame := make([]byte, 417)
	copy(frame, []byte{0xff, 0xfb, 0x90, 0})
	copy(frame[4:], raw)
	return frame
}
func reportMP3V3(t *testing.T, payload []byte) (MP3TagDetailDocumentV2, flv.MP3Audio, flv.TagRecord, []byte) {
	t.Helper()
	data, tag, _ := parseMP3TagDetail(t, payload)
	parsed, err := flv.ParseMP3Audio(bytes.NewReader(data), tag, mp3.Options{InspectSideInfo: true})
	if err != nil {
		t.Fatal(err)
	}
	return NewMP3TagDetailV3(uint64(len(data)), tag, parsed), parsed, tag, data
}

func TestMP3SideInfoV3Goldens(t *testing.T) {
	first, second := reportMP3V3Frame(t, mp3V3LongSideHex), reportMP3V3Frame(t, mp3V3ShortSideHex)
	for _, tc := range []struct {
		name     string
		payload  []byte
		complete bool
		frames   int
		state    string
	}{
		{"flv-mp3-side-info-v3.json", append(append([]byte{0x2f}, first...), second...), true, 2, "none"},
		{"flv-mp3-side-info-truncated-v3.json", append([]byte{0x2f}, first[:13]...), false, 1, "unknown"},
		{"flv-mp3-side-info-before-tag-v3.json", append([]byte{0x2f}, second...), true, 1, "before_tag"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, parsed, tag, data := reportMP3V3(t, tc.payload)
			if d.SchemaVersion != 3 || d.DocumentType != MP3TagDetailDocumentType || d.Detail.Complete != tc.complete || d.Detail.Frames == nil || len(d.Detail.Frames.Items) != tc.frames {
				t.Fatalf("v3 envelope %+v", d)
			}
			frame := d.Detail.Frames.Items[0]
			if frame.SideInfo == nil || frame.SideInfo.Complete != tc.complete || frame.Dependency == nil || frame.Dependency.State != tc.state {
				t.Fatalf("side/dependency %+v", frame)
			}
			assertReportSpan(t, frame.SideInfo.MainDataBegin.Span, tag.PayloadSpan().ByteOffset+5, 0, 9)
			checkMP3V3MappedSpans(t, reflect.ValueOf(d), data)
			if tc.frames == 2 {
				long := frame.SideInfo.Granules[0].Channels[0]
				short := d.Detail.Frames.Items[1].SideInfo.Granules[0].Channels[0]
				if long.Part23Length.Value != 100 || long.BigValues.Value != 10 || long.BlockType != nil || long.MixedBlockFlag != nil || len(long.SubblockGain) != 0 || long.Region0Count.Value != 6 {
					t.Fatal("long block mapped incorrectly")
				}
				if short.BlockType.Value != 2 || !short.MixedBlockFlag.Value || short.Region0Count != nil || short.Region1Count != nil || len(short.TableSelect) != 2 || len(short.SubblockGain) != 3 {
					t.Fatal("short block mapped incorrectly")
				}
				dep := d.Detail.Frames.Items[1].Dependency
				if dep.State != "within_tag" || dep.KnownPreviousBytes != 381 || *dep.Part23Bits != 400 || *dep.AvailableBits != 4648 {
					t.Fatalf("second-frame dependency %+v", dep)
				}
			}
			if tc.state == "before_tag" && (*frame.Dependency.MissingPreviousBytes != 200 || len(d.Diagnostics) != 0) {
				t.Fatal("before-tag reference is not malformed audio")
			}
			if !tc.complete {
				ch := frame.SideInfo.Granules[0].Channels[0]
				if ch.Part23Length == nil || ch.Region0Count != nil || ch.Region1Count != nil || ch.Preflag != nil || frame.Dependency.Part23Bits != nil {
					t.Fatal("truncation invented absent syntax")
				}
				if findMP3ReportDiagnostic(d.Diagnostics, mp3.CodeSideInfoTruncated) == nil {
					t.Fatal("missing side-info truncation diagnostic")
				}
			}
			for _, value := range []flv.ParsedTagDetail{parsed, &parsed} {
				routed, err := NewTagDetailV3(uint64(len(data)), tag, value, avccontext.ParameterResolution{})
				if err != nil || !reflect.DeepEqual(routed, d) {
					t.Fatalf("generic MP3 router %v", err)
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
				t.Fatalf("golden %s: %v", tc.name, err)
			}
			var roundTrip MP3TagDetailDocumentV2
			if err := json.Unmarshal(encoded.Bytes(), &roundTrip); err != nil {
				t.Fatal(err)
			}
			var round bytes.Buffer
			if err := WriteParsedTagDetailJSON(&round, roundTrip, true); err != nil || !bytes.Equal(round.Bytes(), encoded.Bytes()) {
				t.Fatalf("v3 JSON round-trip %v", err)
			}
			for _, version := range []int{1, 2} {
				old, err := flv.ParseMP3Audio(bytes.NewReader(data), tag, mp3.Options{InspectFrames: version == 2})
				if err != nil {
					t.Fatal(err)
				}
				var legacy ParsedTagDetailDocument = NewMP3TagDetail(uint64(len(data)), tag, old)
				if version == 2 {
					legacy = NewMP3TagDetailV2(uint64(len(data)), tag, old)
				}
				var encoded bytes.Buffer
				if err := WriteParsedTagDetailJSON(&encoded, legacy, false); err != nil {
					t.Fatal(err)
				}
				for _, key := range []string{`"sideInfo"`, `"dependency"`, `"mainDataBegin"`, mp3.CodeSideInfoTruncated} {
					if bytes.Contains(encoded.Bytes(), []byte(key)) {
						t.Fatalf("v3 key leaked to v%d: %s", version, key)
					}
				}
			}
		})
	}
}

func checkMP3V3MappedSpans(t testing.TB, value reflect.Value, data []byte) {
	t.Helper()
	if !value.IsValid() {
		return
	}
	if value.Kind() == reflect.Pointer {
		if !value.IsNil() {
			checkMP3V3MappedSpans(t, value.Elem(), data)
		}
		return
	}
	if value.Type() == reflect.TypeOf(MP3UnsignedField{}) {
		f := value.Interface().(MP3UnsignedField)
		if sourceMP3V3Bits(t, data, f.Span) != f.Value {
			t.Fatalf("incorrect source integer %+v", f)
		}
		return
	}
	if value.Type() == reflect.TypeOf(MP3BooleanField{}) {
		f := value.Interface().(MP3BooleanField)
		if (sourceMP3V3Bits(t, data, f.Span) != 0) != f.Value {
			t.Fatalf("incorrect source flag %+v", f)
		}
		return
	}
	switch value.Kind() {
	case reflect.Struct:
		for i := 0; i < value.NumField(); i++ {
			checkMP3V3MappedSpans(t, value.Field(i), data)
		}
	case reflect.Slice:
		for i := 0; i < value.Len(); i++ {
			checkMP3V3MappedSpans(t, value.Index(i), data)
		}
	}
}
func sourceMP3V3Bits(t testing.TB, data []byte, s Span) uint64 {
	t.Helper()
	start := s.ByteOffset*8 + uint64(s.BitOffset)
	if s.BitLength == 0 || s.BitLength > 64 || s.BitOffset > 7 || start+uint64(s.BitLength) > uint64(len(data))*8 {
		t.Fatalf("span outside source %+v", s)
	}
	var out uint64
	for i := uint32(0); i < s.BitLength; i++ {
		bit := start + uint64(i)
		out = out<<1 | uint64(data[bit/8]>>(7-bit%8)&1)
	}
	return out
}

func TestMP3SideInfoV3LSFConditionalFieldsAndTypedNil(t *testing.T) {
	frame := make([]byte, 208)
	copy(frame, []byte{0xff, 0xf3, 0x80, 0xc0}) // MPEG-2 mono, 64kbps, 22050Hz.
	d, _, _, _ := reportMP3V3(t, append([]byte{0x2f}, frame...))
	if !d.Detail.Complete {
		t.Fatalf("LSF: %+v", d.Diagnostics)
	}
	s := d.Detail.Frames.Items[0].SideInfo
	if len(s.SCFSIChannels) != 0 || len(s.Granules) != 1 || len(s.Granules[0].Channels) != 1 || s.Granules[0].Channels[0].Preflag != nil || s.DeclaredBytes != 9 {
		t.Fatalf("LSF fields %+v", s)
	}
	var nilMP3 *flv.MP3Audio
	if value, err := NewTagDetailV3(0, flv.TagRecord{}, nilMP3, avccontext.ParameterResolution{}); value != nil || !errors.Is(err, ErrDetailV3Unsupported) {
		t.Fatal("nil MP3 accepted")
	}
}
