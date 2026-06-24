//go:build windows

package updater

import (
	"os/exec"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

type commandProcessOwner struct {
	job  windows.Handle
	once sync.Once
}

func newCommandProcessOwner(enabled bool) (*commandProcessOwner, error) {
	if !enabled {
		return nil, nil
	}
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, err
	}
	owner := &commandProcessOwner{job: job}
	limit := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limit.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&limit)),
		uint32(unsafe.Sizeof(limit)),
	); err != nil {
		owner.Close()
		return nil, err
	}
	return owner, nil
}

func (owner *commandProcessOwner) Assign(cmd *exec.Cmd) error {
	if owner == nil || owner.job == 0 || cmd == nil || cmd.Process == nil {
		return nil
	}
	handle, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
	if err != nil {
		return err
	}
	defer windows.CloseHandle(handle)
	return windows.AssignProcessToJobObject(owner.job, handle)
}

func (owner *commandProcessOwner) Terminate() {
	if owner == nil || owner.job == 0 {
		return
	}
	_ = windows.TerminateJobObject(owner.job, uint32(commandCancelledCode))
}

func (owner *commandProcessOwner) Close() {
	if owner == nil {
		return
	}
	owner.once.Do(func() {
		if owner.job != 0 {
			_ = windows.CloseHandle(owner.job)
			owner.job = 0
		}
	})
}
