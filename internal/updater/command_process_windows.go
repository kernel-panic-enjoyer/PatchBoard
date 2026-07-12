//go:build windows

package updater

import (
	"fmt"
	"os/exec"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

const createSuspendedFlag = 0x00000004

var procNtResumeProcess = windows.NewLazySystemDLL("ntdll.dll").NewProc("NtResumeProcess")

type commandProcessOwner struct {
	jobHandle windows.Handle
	closeOnce sync.Once
}

func newCommandProcessOwner(enabled bool) (*commandProcessOwner, error) {
	if !enabled {
		return nil, nil
	}
	jobHandle, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, err
	}
	processOwner := &commandProcessOwner{jobHandle: jobHandle}
	killOnCloseLimit := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	killOnCloseLimit.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(
		jobHandle,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&killOnCloseLimit)),
		uint32(unsafe.Sizeof(killOnCloseLimit)),
	); err != nil {
		processOwner.Close()
		return nil, err
	}
	return processOwner, nil
}

func (processOwner *commandProcessOwner) Assign(command *exec.Cmd) error {
	if processOwner == nil || processOwner.jobHandle == 0 || command == nil || command.Process == nil {
		return nil
	}
	return processOwner.AssignProcessID(uint32(command.Process.Pid))
}

func (processOwner *commandProcessOwner) AssignProcessID(processID uint32) error {
	if processOwner == nil || processOwner.jobHandle == 0 || processID == 0 {
		return nil
	}
	processHandle, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, processID)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(processHandle)
	return windows.AssignProcessToJobObject(processOwner.jobHandle, processHandle)
}

func (processOwner *commandProcessOwner) Resume(command *exec.Cmd) error {
	if processOwner == nil || processOwner.jobHandle == 0 || command == nil || command.Process == nil {
		return nil
	}
	processHandle, err := windows.OpenProcess(windows.PROCESS_SUSPEND_RESUME, false, uint32(command.Process.Pid))
	if err != nil {
		return err
	}
	defer windows.CloseHandle(processHandle)
	status, _, _ := procNtResumeProcess.Call(uintptr(processHandle))
	if status != 0 {
		return fmt.Errorf("resume Job Object-owned process failed with NTSTATUS %#x", status)
	}
	return nil
}

func (processOwner *commandProcessOwner) Terminate() {
	if processOwner == nil || processOwner.jobHandle == 0 {
		return
	}
	_ = windows.TerminateJobObject(processOwner.jobHandle, uint32(commandCancelledCode))
}

func (processOwner *commandProcessOwner) Close() {
	if processOwner == nil {
		return
	}
	processOwner.closeOnce.Do(func() {
		if processOwner.jobHandle != 0 {
			_ = windows.CloseHandle(processOwner.jobHandle)
			processOwner.jobHandle = 0
		}
	})
}
