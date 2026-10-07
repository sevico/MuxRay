//go:build !windows

package picker

import "os/exec"

func newContainment() (containment, error) { return nil, ErrUnsupported }
func parentCreated() (string, error)       { return "", ErrUnsupported }
func configureChild(*exec.Cmd)             {}
