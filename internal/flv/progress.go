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
	if progress == nil {
		return ScanContext(ctx, source, size, options)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	progress(ScanProgress{SourceSize: size})
	result, err := scan(ctx, source, size, options, true, progress)
	progress(ScanProgress{
		SourceSize: size, BytesScanned: result.BytesScanned,
		TagCount: result.Index().Len(), DiagnosticCount: uint32(len(result.Diagnostics)),
	})
	return result, err
}
