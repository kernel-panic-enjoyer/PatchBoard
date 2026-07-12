//go:build !windows

package updater

import "os/exec"

const createSuspendedFlag = 0

type commandProcessOwner struct{}

func newCommandProcessOwner(enabled bool) (*commandProcessOwner, error) {
	return nil, nil
}

func (*commandProcessOwner) Assign(command *exec.Cmd) error {
	return nil
}

func (*commandProcessOwner) AssignProcessID(processID uint32) error {
	return nil
}

func (*commandProcessOwner) Resume(command *exec.Cmd) error {
	return nil
}

func (*commandProcessOwner) Terminate() {}

func (*commandProcessOwner) Close() {}
