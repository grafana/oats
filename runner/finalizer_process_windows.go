//go:build windows

package runner

import (
	"os"
	"os/exec"
	"strconv"
)

func prepareFinalizerProcess(*exec.Cmd) error { return nil }

func killFinalizerProcessTree(process *os.Process) error {
	err := exec.Command("taskkill", "/PID", strconv.Itoa(process.Pid), "/T", "/F").Run()
	if err != nil {
		return process.Kill()
	}
	return nil
}
