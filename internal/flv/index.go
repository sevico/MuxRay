package flv

import (
	"fmt"
	"iter"
)

const (
	tagBlockSize           = 1024
	DefaultPageSize uint32 = 256
	MaxPageSize     uint32 = 4096
)

// TagIndex is an immutable, source-ordered view of indexed records. Returned
// records/pages are copies. All stops immediately when its consumer breaks.
type TagIndex interface {
	Len() uint64
	Get(id uint64) (TagRecord, bool)
	Page(start uint64, count uint32) (TagPage, error)
	All() iter.Seq2[uint64, TagRecord]
}

// TagPage retains global IDs; Next is the exclusive end and never exceeds Total.
type TagPage struct {
	Start   uint64
	Limit   uint32
	Total   uint64
	Next    uint64
	Records []TagRecord
}

// Index returns the chunked index for ScanContext or a view of the legacy Tags
// slice. Callers must not mutate a legacy slice while reading its view.
func (r ScanResult) Index() TagIndex {
	if r.index != nil {
		return r.index
	}
	return sliceTagIndex(r.Tags)
}

type sliceTagIndex []TagRecord

func (s sliceTagIndex) Len() uint64 { return uint64(len(s)) }
func (s sliceTagIndex) Get(id uint64) (TagRecord, bool) {
	if id >= s.Len() {
		return TagRecord{}, false
	}
	return s[id], true
}
func (s sliceTagIndex) Page(start uint64, count uint32) (TagPage, error) {
	return indexPage(s, start, count)
}
func (s sliceTagIndex) All() iter.Seq2[uint64, TagRecord] {
	return func(yield func(uint64, TagRecord) bool) {
		for id, record := range s {
			if !yield(uint64(id), record) {
				return
			}
		}
	}
}

type chunkedTagIndex struct {
	blocks [][]TagRecord
	length uint64
	limit  uint64
}

func (s *chunkedTagIndex) Len() uint64 { return s.length }
func (s *chunkedTagIndex) Get(id uint64) (TagRecord, bool) {
	if id >= s.length {
		return TagRecord{}, false
	}
	return s.blocks[id/tagBlockSize][id%tagBlockSize], true
}
func (s *chunkedTagIndex) Page(start uint64, count uint32) (TagPage, error) {
	return indexPage(s, start, count)
}
func (s *chunkedTagIndex) All() iter.Seq2[uint64, TagRecord] {
	return func(yield func(uint64, TagRecord) bool) {
		var id uint64
		for _, block := range s.blocks {
			for _, record := range block {
				if !yield(id, record) {
					return
				}
				id++
			}
		}
	}
}

// append is used only by the scanner before publishing the result.
func (s *chunkedTagIndex) append(record TagRecord) {
	if s.length >= s.limit {
		panic("flv: internal tag index limit exceeded")
	}
	if s.length%tagBlockSize == 0 {
		capacity := min(uint64(tagBlockSize), s.limit-s.length)
		s.blocks = append(s.blocks, make([]TagRecord, 0, int(capacity)))
	}
	last := len(s.blocks) - 1
	s.blocks[last] = append(s.blocks[last], record)
	s.length++
}

func indexPage(index TagIndex, start uint64, count uint32) (TagPage, error) {
	if count == 0 || count > MaxPageSize {
		return TagPage{}, fmt.Errorf("flv: page count must be between 1 and %d", MaxPageSize)
	}
	total := index.Len()
	if start > total {
		return TagPage{}, fmt.Errorf("flv: page start %d exceeds indexed tag count %d", start, total)
	}
	length := min(uint64(count), total-start)
	page := TagPage{Start: start, Limit: count, Total: total, Next: start + length, Records: make([]TagRecord, 0, int(length))}
	for id := start; id < page.Next; id++ {
		record, _ := index.Get(id)
		page.Records = append(page.Records, record)
	}
	return page, nil
}
