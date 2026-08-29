package flv

import (
	"testing"
	"unsafe"
)

func TestTagRecordRemainsCompact(t *testing.T) {
	const maximumRecordSize = uintptr(64)
	if size := unsafe.Sizeof(TagRecord{}); size > maximumRecordSize {
		t.Fatalf("TagRecord size = %d bytes, exceeds compact-index budget %d", size, maximumRecordSize)
	}
}
