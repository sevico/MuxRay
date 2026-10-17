//go:build !windows

package picker

import (
	"os"
	"os/exec"
)

func newContainment() (containment, error)         { return nil, ErrUnsupported }
func parentCreated() (string, error)               { return "", ErrUnsupported }
func configureChild(*exec.Cmd, *os.File)           {}
func newResponsePipe() (*os.File, *os.File, error) { return nil, nil, ErrUnsupported }
