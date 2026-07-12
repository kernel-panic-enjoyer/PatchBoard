package updater

import (
	"context"
	"os/exec"
)

// startCommandInJobObject starts a command suspended, assigns it to the
// kill-on-close Job Object, and only then lets its initial thread run. Callers
// must configure the command with hiddenSysProcAttrWithFlags(true) first.
// This prevents child processes from escaping ownership during startup.
func startCommandInJobObject(command *exec.Cmd, processOwner *commandProcessOwner) error {
	if err := command.Start(); err != nil {
		return err
	}
	if processOwner == nil {
		return nil
	}
	if err := processOwner.Assign(command); err != nil {
		terminateStartedCommand(command, processOwner)
		_ = command.Wait()
		return err
	}
	if err := processOwner.Resume(command); err != nil {
		terminateStartedCommand(command, processOwner)
		_ = command.Wait()
		return err
	}
	return nil
}

func waitForStartedCommand(ctx context.Context, startedCommand *exec.Cmd, processOwner *commandProcessOwner) error {
	waitResult := make(chan error, 1)
	go func() {
		waitResult <- startedCommand.Wait()
	}()
	select {
	case err := <-waitResult:
		return err
	case <-ctx.Done():
		terminateStartedCommand(startedCommand, processOwner)
		return <-waitResult
	}
}

func terminateStartedCommand(startedCommand *exec.Cmd, processOwner *commandProcessOwner) {
	if processOwner != nil {
		processOwner.Terminate()
		return
	}
	if startedCommand == nil || startedCommand.Process == nil {
		return
	}
	_ = startedCommand.Process.Kill()
}
