package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Mutations happen synchronously at a boundary Stat, so the tests do not rely
// on scheduling, file size, or sleeps to race the scanner/parser.
type detailBoundaryFile struct {
	*os.File
	checks, closes int
	check          func(int) (os.FileInfo, error)
}

func (f *detailBoundaryFile) Stat() (os.FileInfo, error) { f.checks++; return f.check(f.checks) }
func (f *detailBoundaryFile) Close() error               { f.closes++; return f.File.Close() }

func TestDetailV3SourceChangesNeverWriteJSON(t *testing.T) {
	for _, boundary := range []int{2, 3} {
		for _, change := range []string{"mtime", "size", "identity", "stat failure"} {
			t.Run(strconv.Itoa(boundary)+"/"+change, func(t *testing.T) {
				data := appendFLVTag(makeSingleTagFLV(9, cliV3Config(t)), 9, cliV3Slice(0x65, 0xb8, 0x40))
				path := filepath.Join(t.TempDir(), "source.flv")
				if err := os.WriteFile(path, data, 0600); err != nil {
					t.Fatal(err)
				}
				initial, err := os.Stat(path)
				if err != nil {
					t.Fatal(err)
				}
				var source *detailBoundaryFile
				open := func(path string) (detailSource, error) {
					f, err := os.Open(path)
					if err != nil {
						return nil, err
					}
					source = &detailBoundaryFile{File: f}
					source.check = func(call int) (os.FileInfo, error) {
						if call == boundary {
							switch change {
							case "mtime":
								if err := os.Chtimes(path, initial.ModTime(), initial.ModTime().Add(2*time.Second)); err != nil {
									t.Fatal(err)
								}
							case "size":
								if err := os.Truncate(path, initial.Size()+1); err != nil {
									t.Fatal(err)
								}
							case "identity":
								other := filepath.Join(t.TempDir(), "other.flv")
								if err := os.WriteFile(other, data, 0600); err != nil {
									t.Fatal(err)
								}
								if err := os.Chtimes(other, initial.ModTime(), initial.ModTime()); err != nil {
									t.Fatal(err)
								}
								return os.Stat(other)
							case "stat failure":
								return nil, errors.New("synthetic stat failure")
							}
						}
						return f.Stat()
					}
					return source, nil
				}
				var out, stderr bytes.Buffer
				code := runDetailWithSource(context.Background(), []string{"--schema-version", "3", "--tag", "1", path}, &out, &stderr, open)
				marker := "after scan"
				if boundary == 3 {
					marker = "before detail output"
				}
				if code != exitFailure || out.Len() != 0 || !strings.Contains(stderr.String(), marker) || source.checks != boundary || source.closes != 1 {
					t.Fatalf("code=%d output=%d checks=%d closes=%d stderr=%s", code, out.Len(), source.checks, source.closes, stderr.String())
				}
				if change != "stat failure" && !strings.Contains(stderr.String(), "source changed") {
					t.Fatalf("missing source-change reason: %s", stderr.String())
				}
			})
		}
	}
}

func TestDetailSourceChecksAreV3OnlyAndNotPerTagOrSegment(t *testing.T) {
	data := makeSingleTagFLV(9, cliV3Config(t))
	const selected = 9000
	for range selected {
		data = appendFLVTag(data, 9, cliV3Slice(0x65, 0xb8, 0x40))
	}
	path := filepath.Join(t.TempDir(), "long.flv")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	for _, version := range []string{"1", "2", "3"} {
		var source *detailBoundaryFile
		open := func(path string) (detailSource, error) {
			f, err := os.Open(path)
			if err != nil {
				return nil, err
			}
			source = &detailBoundaryFile{File: f, check: func(int) (os.FileInfo, error) { return f.Stat() }}
			return source, nil
		}
		var out, stderr bytes.Buffer
		code := runDetailWithSource(context.Background(), []string{"--schema-version", version, "--tag", strconv.Itoa(selected), path}, &out, &stderr, open)
		want := 1
		if version == "3" {
			want = 3
		}
		if code != exitOK || out.Len() == 0 || source.checks != want || source.closes != 1 {
			t.Fatalf("v%s code=%d checks=%d closes=%d stderr=%s", version, code, source.checks, source.closes, stderr.String())
		}
	}
}
