//go:build !linux && !darwin && !windows

package runner

import (
	"errors"
	"os/exec"
)

func startFinalizerProcess(*exec.Cmd) (func() error, func() error, error) {
	return nil, nil, errors.New("fixture finalizer process-tree cleanup is unsupported on this platform")
}
