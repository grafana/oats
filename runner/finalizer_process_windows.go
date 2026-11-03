//go:build windows

package runner

import (
	"errors"
	"os/exec"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

func startFinalizerProcess(cmd *exec.Cmd) (func() error, func() error, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, nil, err
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		_ = windows.CloseHandle(job)
		return nil, nil, err
	}

	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_SUSPENDED}
	var mu sync.Mutex
	assigned := false
	closed := false
	cmd.Cancel = func() error {
		mu.Lock()
		defer mu.Unlock()
		if closed {
			return nil
		}
		if assigned {
			return windows.TerminateJobObject(job, 1)
		}
		if cmd.Process != nil {
			return cmd.Process.Kill()
		}
		return nil
	}

	mu.Lock()
	if err := cmd.Start(); err != nil {
		mu.Unlock()
		_ = windows.CloseHandle(job)
		return nil, nil, err
	}
	process, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
	if err != nil {
		_ = cmd.Process.Kill()
		closed = true
		mu.Unlock()
		_ = cmd.Wait()
		_ = windows.CloseHandle(job)
		return nil, nil, err
	}
	assignErr := windows.AssignProcessToJobObject(job, process)
	_ = windows.CloseHandle(process)
	if assignErr != nil {
		_ = cmd.Process.Kill()
		closed = true
		mu.Unlock()
		_ = cmd.Wait()
		_ = windows.CloseHandle(job)
		return nil, nil, assignErr
	}
	assigned = true
	if err := resumeSuspendedProcess(cmd.Process.Pid); err != nil {
		_ = windows.TerminateJobObject(job, 1)
		closed = true
		mu.Unlock()
		_ = cmd.Wait()
		_ = windows.CloseHandle(job)
		return nil, nil, err
	}
	mu.Unlock()

	stop := func() error {
		mu.Lock()
		defer mu.Unlock()
		if closed {
			return nil
		}
		return windows.TerminateJobObject(job, 1)
	}
	cleanup := func() error {
		mu.Lock()
		defer mu.Unlock()
		if closed {
			return nil
		}
		closed = true
		return windows.CloseHandle(job)
	}
	return stop, cleanup, nil
}

func resumeSuspendedProcess(processID int) error {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(snapshot)

	entry := windows.ThreadEntry32{Size: uint32(unsafe.Sizeof(windows.ThreadEntry32{}))}
	for err := windows.Thread32First(snapshot, &entry); err == nil; err = windows.Thread32Next(snapshot, &entry) {
		if entry.OwnerProcessID != uint32(processID) {
			continue
		}
		thread, err := windows.OpenThread(windows.THREAD_SUSPEND_RESUME, false, entry.ThreadID)
		if err != nil {
			return err
		}
		_, resumeErr := windows.ResumeThread(thread)
		_ = windows.CloseHandle(thread)
		return resumeErr
	}
	return errors.New("could not find suspended finalizer thread")
}
