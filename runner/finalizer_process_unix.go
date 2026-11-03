//go:build linux || darwin

package runner

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

func startFinalizerProcess(cmd *exec.Cmd) (func() error, func() error, error) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stop := func() error {
		if cmd.Process == nil {
			return nil
		}
		return killFinalizerProcessTree(cmd.Process)
	}
	cmd.Cancel = stop
	if err := cmd.Start(); err != nil {
		return nil, nil, err
	}
	return stop, stop, nil
}

func killFinalizerProcessTree(process *os.Process) error {
	err := syscall.Kill(-process.Pid, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	if err != nil {
		return process.Kill()
	}
	return nil
}
