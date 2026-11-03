//go:build linux || darwin

package runner

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

func prepareFinalizerProcess(cmd *exec.Cmd) error {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return nil
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
