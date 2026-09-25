package flv

import "fmt"

const (
	MaxFilteredTags uint32 = 100
	FilterScanLimit uint32 = 4096
)

// TagFilter combines container metadata predicates. DTS bounds are inclusive
// raw FLV milliseconds; they do not unwrap timestamps or imply sorted DTS.
type TagFilter struct {
	Type      string
	DTSMin    *uint32
	DTSMax    *uint32
	Structure string
}

func (f TagFilter) Validate() error {
	switch f.Type {
	case "all", "audio", "video", "script", "other":
	default:
		return fmt.Errorf("flv: invalid filter type")
	}
	switch f.Structure {
	case "all", "payload_incomplete", "previous_size_missing", "previous_size_invalid":
	default:
		return fmt.Errorf("flv: invalid structure filter")
	}
	if f.DTSMin != nil && f.DTSMax != nil && *f.DTSMin > *f.DTSMax {
		return fmt.Errorf("flv: minimum DTS exceeds maximum DTS")
	}
	return nil
}

func (f TagFilter) matches(r TagRecord) bool {
	if f.Type == "other" {
		if r.Type().Known() {
			return false
		}
	} else if f.Type != "all" && r.Type().String() != f.Type {
		return false
	}
	if f.DTSMin != nil && r.DTS < int64(*f.DTSMin) || f.DTSMax != nil && r.DTS > int64(*f.DTSMax) {
		return false
	}
	switch f.Structure {
	case "payload_incomplete":
		return !r.PayloadComplete()
	case "previous_size_missing":
		return !r.PreviousTagSizePresent()
	case "previous_size_invalid":
		return r.PreviousTagSizePresent() && !r.PreviousTagSizeValid()
	default:
		return true
	}
}

// FilteredTagPage records the source cursor independently of the match count.
// An empty page can have Exhausted=false. IndexedTotal is never a match total.
type FilteredTagPage struct {
	Start        uint64
	Next         uint64
	IndexedTotal uint64
	Limit        uint32
	ScanLimit    uint32
	Scanned      uint32
	Exhausted    bool
	Records      []TagRecord
}

// FilterTags examines at most FilterScanLimit immutable index records, stopping
// as soon as count matches are found. It copies only matches and reads no source
// bytes. Source order and global IDs survive repeated or regressing timestamps.
func FilterTags(index TagIndex, start uint64, count uint32, filter TagFilter) (FilteredTagPage, error) {
	if err := filter.Validate(); err != nil {
		return FilteredTagPage{}, err
	}
	if count == 0 || count > MaxFilteredTags {
		return FilteredTagPage{}, fmt.Errorf("flv: filtered count must be between 1 and %d", MaxFilteredTags)
	}
	total := index.Len()
	if start > total {
		return FilteredTagPage{}, fmt.Errorf("flv: filtered start exceeds indexed tag count")
	}
	page := FilteredTagPage{Start: start, Next: start, IndexedTotal: total, Limit: count, ScanLimit: FilterScanLimit, Records: make([]TagRecord, 0, int(min(uint64(count), total-start)))}
	for page.Next < total && page.Scanned < FilterScanLimit && uint32(len(page.Records)) < count {
		record, _ := index.Get(page.Next)
		page.Next++
		page.Scanned++
		if filter.matches(record) {
			page.Records = append(page.Records, record)
		}
	}
	page.Exhausted = page.Next == total
	return page, nil
}
