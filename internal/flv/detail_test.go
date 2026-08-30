package flv

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/sevico/muxray/internal/aac"
	"github.com/sevico/muxray/internal/amf0"
	"github.com/sevico/muxray/internal/mp3"
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

func TestParseTagDetailDispatchesMP3Audio(t *testing.T) {
	data, tag := scriptDataFixture(t, byte(TagTypeAudio), []byte{0x2f, 0xff, 0xfb, 0x90, 0x64})

	parsed, err := ParseTagDetail(bytes.NewReader(data), tag, DetailOptions{})
	if err != nil {
		t.Fatalf("ParseTagDetail: %v", err)
	}
	detail, ok := parsed.(MP3Audio)
	if !ok {
		t.Fatalf("detail type = %T, want flv.MP3Audio", parsed)
	}
	if parsed.DetailKind() != DetailKindMP3Audio || !parsed.IsComplete() || detail.FrameHeader.Header.BitrateKbps != 128 {
		t.Fatalf("generic MP3 detail state: %+v", detail)
	}
}

func TestParseTagDetailAudioDispatchConsumesOnlySelectedOptions(t *testing.T) {
	mp3Data, mp3Tag := scriptDataFixture(t, byte(TagTypeAudio), []byte{0x2f, 0xff, 0xfb, 0x90, 0x64})
	parsed, err := ParseTagDetail(bytes.NewReader(mp3Data), mp3Tag, DetailOptions{AAC: aac.Options{MaxDiagnostics: 1}})
	if err != nil {
		t.Fatalf("MP3 dispatch consumed invalid AAC options: %v", err)
	}
	if _, ok := parsed.(MP3Audio); !ok {
		t.Fatalf("MP3 detail type = %T", parsed)
	}

	aacData, aacTag := scriptDataFixture(t, byte(TagTypeAudio), []byte{0xaf, 0x01})
	parsed, err = ParseTagDetail(bytes.NewReader(aacData), aacTag, DetailOptions{MP3: mp3.Options{MaxDiagnostics: 1}})
	if err != nil {
		t.Fatalf("AAC dispatch consumed invalid MP3 options: %v", err)
	}
	if _, ok := parsed.(AACAudio); !ok {
		t.Fatalf("AAC detail type = %T", parsed)
	}
}

func TestParseTagDetailMP3ProbeAcceptsFullReadWithEOF(t *testing.T) {
	data, tag := scriptDataFixture(t, byte(TagTypeAudio), []byte{0x2f, 0xff, 0xfb, 0x90, 0x64})
	parsed, err := ParseTagDetail(fullReadEOFDetailReaderAt{data: data}, tag, DetailOptions{})
	if err != nil {
		t.Fatalf("ParseTagDetail: %v", err)
	}
	if _, ok := parsed.(MP3Audio); !ok || !parsed.IsComplete() {
		t.Fatalf("full-read EOF probe failed: %+v", parsed)
	}
}

func TestParseTagDetailPreservesEmptyAudioAACContract(t *testing.T) {
	data, tag := scriptDataFixture(t, byte(TagTypeAudio), nil)
	parsed, err := ParseTagDetail(bytes.NewReader(data), tag, DetailOptions{})
	if err != nil {
		t.Fatalf("ParseTagDetail: %v", err)
	}
	detail, ok := parsed.(AACAudio)
	if !ok || detail.Complete || !hasAACDiagnostic(detail.Diagnostics, CodeAACAudioHeaderTruncated) {
		t.Fatalf("empty audio compatibility changed: %+v", parsed)
	}
}

func TestParseTagDetailDispatchesAVCVideo(t *testing.T) {
	data, tag := scriptDataFixture(t, byte(TagTypeVideo), []byte{0x17, 0x02, 0, 0, 0})

	parsed, err := ParseTagDetail(bytes.NewReader(data), tag, DetailOptions{})
	if err != nil {
		t.Fatalf("ParseTagDetail: %v", err)
	}
	detail, ok := parsed.(AVCVideo)
	if !ok {
		t.Fatalf("detail type = %T, want flv.AVCVideo", parsed)
	}
	if parsed.DetailKind() != DetailKindAVCVideo || !parsed.IsComplete() {
		t.Fatalf("generic detail state: kind=%q complete=%v", parsed.DetailKind(), parsed.IsComplete())
	}
	if detail.PacketType != AVCPacketTypeEndOfSequence || !detail.EndOfSequencePresent {
		t.Fatalf("AVC detail was not preserved: %+v", detail)
	}
}

type fullReadEOFDetailReaderAt struct{ data []byte }

func (reader fullReadEOFDetailReaderAt) ReadAt(output []byte, offset int64) (int, error) {
	if offset < 0 || offset >= int64(len(reader.data)) {
		return 0, io.EOF
	}
	return copy(output, reader.data[offset:]), io.EOF
}

func TestParseTagDetailRejectsUnsupportedAudioCodec(t *testing.T) {
	data, tag := scriptDataFixture(t, byte(TagTypeAudio), []byte{0xbf, 0x01})
	parsed, err := ParseTagDetail(bytes.NewReader(data), tag, DetailOptions{})
	if _, ok := parsed.(AACAudio); !ok {
		t.Fatalf("partial detail = %T, want flv.AACAudio", parsed)
	}
	if !errors.Is(err, ErrAACAudioWrongSoundFormat) {
		t.Fatalf("error = %v, want errors.Is(_, ErrAACAudioWrongSoundFormat)", err)
	}
}

func TestParseTagDetailRejectsUnsupportedVideoSelection(t *testing.T) {
	for _, test := range []struct {
		name    string
		payload []byte
		wantErr error
	}{
		{name: "non AVC codec", payload: []byte{0x12, 0, 0, 0, 0}, wantErr: ErrAVCVideoWrongCodecID},
		{name: "command frame", payload: []byte{0x57, 0}, wantErr: ErrAVCVideoCommandFrame},
	} {
		t.Run(test.name, func(t *testing.T) {
			data, tag := scriptDataFixture(t, byte(TagTypeVideo), test.payload)
			parsed, err := ParseTagDetail(bytes.NewReader(data), tag, DetailOptions{})
			if _, ok := parsed.(AVCVideo); !ok {
				t.Fatalf("partial detail = %T, want flv.AVCVideo", parsed)
			}
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("error = %v, want errors.Is(_, %v)", err, test.wantErr)
			}
		})
	}
}

func TestParseTagDetailRejectsUnsupportedTagTypesWithoutReading(t *testing.T) {
	for _, test := range []struct {
		name     string
		tagType  TagType
		typeName string
	}{
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
