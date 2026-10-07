package main

import (
	"errors"
	"io"
	"os"
)

type detailSource interface {
	io.ReaderAt
	io.Closer
	Stat() (os.FileInfo, error)
}

var errDetailSourceChanged = errors.New("source changed during detail analysis")

// Check the retained handle, not its path: replacing the path does not change
// the source that was scanned. These checks detect observable changes, not an
// immutable byte snapshot. Call only at scan/output boundaries, never per Tag.
func detailSourceUnchanged(source detailSource, initial os.FileInfo) error {
	current, err := source.Stat()
	if err != nil {
		return err
	}
	if !os.SameFile(initial, current) || initial.Size() != current.Size() || !initial.ModTime().Equal(current.ModTime()) {
		return errDetailSourceChanged
	}
	return nil
}
