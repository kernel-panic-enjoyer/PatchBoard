//go:build !windows

package updater

import "os/exec"

type commandProcessOwner struct{}

func newCommandProcessOwner(bool) (*commandProcessOwner, error) {
	return nil, nil
}

func (owner *commandProcessOwner) Assign(*exec.Cmd) error {
	return nil
}

func (owner *commandProcessOwner) Terminate() {}

func (owner *commandProcessOwner) Close() {}
