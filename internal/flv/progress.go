package flv

import (
	"context"
	"io"
)

// ScanProgress is a value-only snapshot, not access to the index under
// construction. BytesScanned is the logical cursor, not physical I/O or an
// estimate of remaining work. DiagnosticCount counts retained diagnostics.
type ScanProgress struct {
	SourceSize      uint64
	BytesScanned    uint64
	TagCount        uint64
	DiagnosticCount uint32
}

// ScanContextWithProgress behaves like ScanContext and synchronously calls
// progress initially, after the initial container structure, every 1,024
// committed tags, and once with exact final counters before returning on every
// normal exit. Intermediate counters may lag; blocked I/O prevents updates.
// The callback must return promptly and must not panic. Nil disables updates.
// There are no background goroutines or live index access in this API.
func ScanContextWithProgress(ctx context.Context, source io.ReaderAt, size uint64, options Options, progress func(ScanProgress)) (ScanResult, error) {
	return ScanContextWithObserver(ctx, source, size, options, progress, nil)
}

// TagObserver receives each committed record by value, in index order, on the
// scanning goroutine. It may perform bounded I/O using the scan context and a
// caller-owned source. It must not panic or wait for the scan to finish. An
// error stops scanning and preserves the committed record and earlier prefix;
// cancellation takes precedence over the observer's error. Nil disables it.
type TagObserver func(context.Context, TagRecord) error

// ScanContextWithObserver behaves like ScanContextWithProgress and calls observe
// synchronously after each tag is committed, including a partial payload or
// truncated trailer. Progress and observation use the same goroutine, so a
// blocked observer also prevents intermediate progress and scan completion.
// The exact final progress is delivered before every normal return, including
// observer errors and cancellation. With a nil observer, no payload I/O is added.
func ScanContextWithObserver(ctx context.Context, source io.ReaderAt, size uint64, options Options, progress func(ScanProgress), observe TagObserver) (ScanResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if progress != nil {
		progress(ScanProgress{SourceSize: size})
	}
	result, err := scan(ctx, source, size, options, true, progress, observe)
	if progress != nil {
		progress(ScanProgress{
			SourceSize: size, BytesScanned: result.BytesScanned,
			TagCount: result.Index().Len(), DiagnosticCount: uint32(len(result.Diagnostics)),
		})
	}
	return result, err
}
