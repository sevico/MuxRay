package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/sevico/muxray/internal/report"
)

func TestDetailV3LongAVCReplaysMoreThanOneSegment(t *testing.T) {
	data := makeSingleTagFLV(9, cliV3Config(t))
	const selected = 18001
	for i := 1; i < selected; i++ {
		data = appendFLVTag(data, 9, cliV3Slice(0x65, 0xb8, 0x40))
	}
	data = appendFLVTag(data, 9, cliV3Slice(0x65, 0xb8, 0x40))
	path := filepath.Join(t.TempDir(), "long.flv")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	if code := run([]string{"detail", "--schema-version", "3", "--tag", strconv.Itoa(selected), path}, &out, &stderr); code != exitOK {
		t.Fatalf("long detail %d: %s %s", code, stderr.String(), out.String())
	}
	var document report.AVCTagDetailDocument
	if err := json.Unmarshal(out.Bytes(), &document); err != nil {
		t.Fatal(err)
	}
	p := document.Detail.ParameterContext
	if !document.Detail.Complete || p.State != "found" || p.StartTagID != 0 || p.NextTagID != selected || p.ScannedTags != selected || p.Configurations != 1 {
		t.Fatalf("cumulative context: %+v", p)
	}
	slice := document.Detail.NALUnitSequence.NALUnits[0].SliceHeaderPrefix
	if slice == nil || !slice.PrefixComplete || slice.ReferencedPPSSpan == nil || slice.ReferencedPPSSpan.ByteOffset >= document.Tag.Offset {
		t.Fatal("long replay lost parameter provenance")
	}
}
