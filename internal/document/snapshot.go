package document

import (
	"os"

	"github.com/sevico/muxray/internal/session"
)

// One immutable ready result of each read kind may survive a switch to the
// other kind, solely for diagnostic export. Same-kind admission clears its
// cache, Summary admission clears both, and close/source change clears both.
// The retained objects share the worker's immutable results (<=256 KiB detail,
// <=4 KiB Hex); querying returns independent copies. Existing GetDetail/GetHex
// recovery semantics still consult only the latest shared request namespace.
func (d *Document) clearSnapshotReadsLocked() {
	d.snapshotDetail, d.snapshotHex = DetailResult{}, HexResult{}
}

func (d *Document) snapshotAvailableLocked() error {
	if d.closed || d.ctx.Err() != nil {
		return session.ErrClosed
	}
	if d.changed {
		return ErrSourceChanged
	}
	return nil
}

func (d *Document) SnapshotDetail(id uint64) (DetailResult, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.snapshotAvailableLocked(); err != nil {
		return DetailResult{}, err
	}
	if id == 0 {
		return DetailResult{}, ErrInvalidRequest
	}
	if d.snapshotDetail.RequestID == id {
		return cloneResult(d.snapshotDetail), nil
	}
	if d.request.id == id && d.request.kind == detailWork {
		return cloneResult(d.latest), nil
	}
	if id <= d.request.id {
		return DetailResult{}, ErrStaleRequest
	}
	return DetailResult{}, ErrUnknownRequest
}

func (d *Document) SnapshotHex(id uint64) (HexResult, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.snapshotAvailableLocked(); err != nil {
		return HexResult{}, err
	}
	if id == 0 {
		return HexResult{}, ErrInvalidRequest
	}
	if d.snapshotHex.RequestID == id {
		return cloneHexResult(d.snapshotHex), nil
	}
	if d.request.id == id && d.request.kind == hexWork {
		return cloneHexResult(d.hex), nil
	}
	if id <= d.request.id {
		return HexResult{}, ErrStaleRequest
	}
	return HexResult{}, ErrUnknownRequest
}

// SnapshotSourceInfo returns the initial handle identity, not a new lookup of
// its possibly replaced path. It performs no I/O and may be used by the host
// after capture to refuse saving over the source or another link to it.
func (d *Document) SnapshotSourceInfo() (os.FileInfo, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.snapshotAvailableLocked(); err != nil {
		return nil, err
	}
	return d.initial, nil
}
