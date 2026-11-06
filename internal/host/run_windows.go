//go:build windows

package host

import (
	"os/exec"
	"unsafe"

	"golang.org/x/sys/windows"
)

func prepareCmd(cmd *exec.Cmd) {
	hideWindow(cmd)
	cmd.WaitDelay = waitDelay
}

// trackCmd puts the process in a job that dies together with its children.
// When the process is already in a job that refuses nesting, stop falls back
// to killing that process alone.
func trackCmd(cmd *exec.Cmd) (stop func() error, release func()) {
	job, err := attachJob(cmd.Process.Pid)
	if err != nil {
		return func() error {
			if cmd.Process == nil {
				return nil
			}
			return cmd.Process.Kill()
		}, func() {}
	}
	return func() error {
			return windows.TerminateJobObject(job, 1)
		}, func() {
			_ = windows.CloseHandle(job)
		}
}

func attachJob(pid int) (windows.Handle, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return 0, err
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	); err != nil {
		_ = windows.CloseHandle(job)
		return 0, err
	}
	proc, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		_ = windows.CloseHandle(job)
		return 0, err
	}
	defer func() { _ = windows.CloseHandle(proc) }()
	if err := windows.AssignProcessToJobObject(job, proc); err != nil {
		_ = windows.CloseHandle(job)
		return 0, err
	}
	return job, nil
}
