package main

import (
	"context"
	"io"
)

// contextWriter bounds cancellation latency by one underlying Write. Like a
// ReaderAt, an arbitrary blocked writer cannot be interrupted cooperatively.
type contextWriter struct {
	ctx    context.Context
	output io.Writer
}

func (w contextWriter) Write(p []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := w.output.Write(p)
	if canceled := w.ctx.Err(); canceled != nil {
		return n, canceled
	}
	return n, err
}
