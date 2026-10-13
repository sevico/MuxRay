package detailview

import (
	"bytes"
	"context"
	"encoding/hex"
	"strconv"
	"testing"

	"github.com/sevico/muxray/internal/avccontext"
)

func seiProjection(t *testing.T, nal []byte, full bool) (*Detail, []byte) {
	t.Helper()
	data, tag := fixture(t, 9, avcNALPayload(4, nal))
	var detail *Detail
	var err error
	if full {
		detail, err = ParseWithAVCFullParameters(context.Background(), bytes.NewReader(data), tag, avcResolution(4), avccontext.ParameterResolution{})
	} else {
		detail, err = ParseWithAVCParameters(context.Background(), bytes.NewReader(data), tag, avcResolution(4), avccontext.ParameterResolution{})
	}
	if err != nil {
		t.Fatal(err)
	}
	assertBounded(t, detail)
	return detail, data
}

func TestSEISelectedDetailRecoverySourceAndLegacyDepth(t *testing.T) {
	// recovery_frame_cnt=0, exact_match=1, broken_link=0, changing_slice_group_idc=0; alignment 100.
	nal := []byte{6, 6, 1, 0xc4, 0x80}
	old, _ := seiProjection(t, nal, false)
	if optionalField(old, "recoveryFrameCnt") != nil {
		t.Fatal("SEI leaked into legacy selected detail")
	}
	detail, data := seiProjection(t, nal, true)
	if !detail.Complete || find(t, detail, "seiComplete").Value != "true" || find(t, detail, "recoveryFrameCnt").Value != "0" {
		t.Fatalf("%+v", detail)
	}
	field := find(t, detail, "exactMatchFlag")
	if field.Span == nil || field.Span.BitOffset != 1 || field.Span.BitLength != 1 || field.Value != "true" {
		t.Fatalf("%+v", field)
	}
	offset, _ := strconv.ParseUint(field.Span.ByteOffset, 10, 64)
	if data[offset] != 0xc4 {
		t.Fatal("recovery field is not mapped to authored payload")
	}
	if DesktopFullAVCOptions().AVC.ParseSEI {
		t.Fatal("context replay must not parse SEI")
	}
}

func TestSEIUUIDDiscontiguousSourceExcludesEPB(t *testing.T) {
	uuid := []byte{0, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14}
	// Insert 03 only in EBSP; UUID itself contains 00 00 01.
	nal := append([]byte{6, 5, 18, 0, 0, 3}, uuid[2:]...)
	nal = append(nal, 0xaa, 0xbb, 0x80)
	detail, data := seiProjection(t, nal, true)
	field := find(t, detail, "uuid")
	if !detail.Complete || field.Value != hex.EncodeToString(uuid) || field.Span != nil {
		t.Fatalf("%+v / %+v", detail, field)
	}
	var rebuilt []byte
	for _, f := range detail.Fields {
		if f.ParentID == field.ID && f.Type == "RBSP source segment" {
			if f.Span == nil || f.Span.BitOffset != 0 || f.Span.BitLength%8 != 0 {
				t.Fatal("invalid segment")
			}
			off, _ := strconv.ParseUint(f.Span.ByteOffset, 10, 64)
			rebuilt = append(rebuilt, data[off:off+uint64(f.Span.BitLength)/8]...)
		}
	}
	if !bytes.Equal(rebuilt, uuid) {
		t.Fatalf("source parts %x, want %x", rebuilt, uuid)
	}
	if f := find(t, detail, "userDataPreview"); f.Value != "aabb" || f.Span == nil {
		t.Fatalf("%+v", f)
	}
}

func TestSEIUnknownPartialAndPresentationLimitsStayExplicit(t *testing.T) {
	unknown, _ := seiProjection(t, []byte{6, 200, 2, 0x11, 0x22, 0x80}, true)
	if !unknown.Complete || find(t, unknown, "syntaxSupported").Value != "false" || find(t, unknown, "seiPayload").Span == nil {
		t.Fatalf("%+v", unknown)
	}
	partial, _ := seiProjection(t, []byte{6, 5, 16, 1, 2, 3}, true)
	if partial.Complete || find(t, partial, "payloadComplete").Value != "false" || optionalField(partial, "uuid") != nil {
		t.Fatalf("%+v", partial)
	}
	nal := []byte{6}
	for i := 0; i < 65; i++ {
		nal = append(nal, 200, 0)
	}
	nal = append(nal, 0x80)
	limited, _ := seiProjection(t, nal, true)
	if limited.Complete || !limited.Truncated {
		t.Fatalf("%+v", limited)
	}
}
