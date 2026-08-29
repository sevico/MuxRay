package flv

import (
	"bytes"
	"errors"
	"testing"

	"github.com/sevico/muxray/internal/amf0"
)

func TestParseTagDetailDispatchesScriptData(t *testing.T) {
	payload := append([]byte{byte(amf0.TypeString), 0, 10}, []byte("onMetaData")...)
	payload = append(payload, byte(amf0.TypeNull))
	data, tag := scriptDataFixture(t, byte(TagTypeScript), payload)

	parsed, err := ParseTagDetail(bytes.NewReader(data), tag, DetailOptions{})
	if err != nil {
		t.Fatalf("ParseTagDetail: %v", err)
	}
	detail, ok := parsed.(ScriptData)
	if !ok {
		t.Fatalf("detail type = %T, want flv.ScriptData", parsed)
	}
	if parsed.DetailKind() != DetailKindAMF0ScriptData || !parsed.IsComplete() {
		t.Fatalf("generic detail state: kind=%q complete=%v", parsed.DetailKind(), parsed.IsComplete())
	}
	if detail.EventName != "onMetaData" || detail.TagID != tag.ID {
		t.Fatalf("script detail was not preserved: %+v", detail)
	}
}

func TestParseTagDetailDispatchesAACAudio(t *testing.T) {
	data, tag := scriptDataFixture(t, byte(TagTypeAudio), []byte{0xaf, 0x00, 0x12, 0x10})

	parsed, err := ParseTagDetail(bytes.NewReader(data), tag, DetailOptions{})
	if err != nil {
		t.Fatalf("ParseTagDetail: %v", err)
	}
	detail, ok := parsed.(AACAudio)
	if !ok {
		t.Fatalf("detail type = %T, want flv.AACAudio", parsed)
	}
	if parsed.DetailKind() != DetailKindAACAudio || !parsed.IsComplete() {
		t.Fatalf("generic detail state: kind=%q complete=%v", parsed.DetailKind(), parsed.IsComplete())
	}
	if detail.PacketType != AACPacketTypeSequenceHeader || !detail.ConfigPresent || detail.Config.AudioObjectType.Value != 2 {
		t.Fatalf("AAC detail was not preserved: %+v", detail)
	}
}

func TestParseTagDetailRejectsUnsupportedAudioCodec(t *testing.T) {
	data, tag := scriptDataFixture(t, byte(TagTypeAudio), []byte{0x2f, 0x01})
	parsed, err := ParseTagDetail(bytes.NewReader(data), tag, DetailOptions{})
	if _, ok := parsed.(AACAudio); !ok {
		t.Fatalf("partial detail = %T, want flv.AACAudio", parsed)
	}
	if !errors.Is(err, ErrAACAudioWrongSoundFormat) {
		t.Fatalf("error = %v, want errors.Is(_, ErrAACAudioWrongSoundFormat)", err)
	}
}

func TestParseTagDetailRejectsUnsupportedTagTypesWithoutReading(t *testing.T) {
	for _, test := range []struct {
		name     string
		tagType  TagType
		typeName string
	}{
		{name: "video", tagType: TagTypeVideo, typeName: "video"},
		{name: "unknown", tagType: 7, typeName: "unknown"},
	} {
		t.Run(test.name, func(t *testing.T) {
			parsed, err := ParseTagDetail(nil, TagRecord{RawTypeFlags: byte(test.tagType)}, DetailOptions{})
			if parsed != nil {
				t.Fatalf("detail = %T, want nil", parsed)
			}
			if !errors.Is(err, ErrTagDetailUnsupported) {
				t.Fatalf("error = %v, want errors.Is(_, ErrTagDetailUnsupported)", err)
			}
			want := "tag type " + test.typeName
			if err == nil || !bytes.Contains([]byte(err.Error()), []byte(want)) {
				t.Fatalf("error = %v, want stable type context %q", err, want)
			}
		})
	}
}
