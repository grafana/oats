//go:build !linux && !darwin && !windows

package runner

import (
	"os"
	"os/exec"
)

func prepareFinalizerProcess(*exec.Cmd) error { return nil }

func killFinalizerProcessTree(process *os.Process) error { return process.Kill() }
