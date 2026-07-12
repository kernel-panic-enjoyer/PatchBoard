package updater

import (
	"context"
	"fmt"
	"os/exec"
	"sync"
	"time"
)

const (
	commandTerminationWaitTimeout = 10 * time.Second
	commandOutputDrainTimeout     = 5 * time.Second
)

// commandLifecycleTimeoutError distinguishes an unsuccessful termination or
// pipe-drain phase from a command's own exit status. Callers can report a
// useful diagnostic without waiting indefinitely for an uncooperative child.
type commandLifecycleTimeoutError struct {
	Phase string
	Cause error
}

func (err *commandLifecycleTimeoutError) Error() string {
	if err.Cause == nil {
		return fmt.Sprintf("timed out waiting for %s", err.Phase)
	}
	return fmt.Sprintf("timed out waiting for %s: %v", err.Phase, err.Cause)
}

func (err *commandLifecycleTimeoutError) Unwrap() error {
	return err.Cause
}

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
	return waitForCommandExitWithTimeout(ctx, startedCommand.Wait, func() {
		terminateStartedCommand(startedCommand, processOwner)
	}, commandTerminationWaitTimeout)
}

func waitForCommandExitWithTimeout(ctx context.Context, wait func() error, terminate func(), timeout time.Duration) error {
	waitResult := make(chan error, 1)
	go func() {
		waitResult <- wait()
	}()
	select {
	case err := <-waitResult:
		return err
	case <-ctx.Done():
		terminate()
		select {
		case err := <-waitResult:
			return err
		case <-time.After(timeout):
			return &commandLifecycleTimeoutError{Phase: "process exit after cancellation", Cause: ctx.Err()}
		}
	}
}

func waitForCommandOutputReaders(outputReaders *sync.WaitGroup, closePipes func(), cause error) error {
	drainDone := make(chan struct{})
	go func() {
		outputReaders.Wait()
		close(drainDone)
	}()
	return waitForOutputDrainWithTimeout(drainDone, closePipes, cause, commandOutputDrainTimeout)
}

func waitForOutputDrainWithTimeout(drainDone <-chan struct{}, closePipes func(), cause error, timeout time.Duration) error {
	select {
	case <-drainDone:
		return nil
	case <-time.After(timeout):
		closePipes()
		phase := "output drain"
		if cause != nil {
			phase += " after cancellation"
		}
		return &commandLifecycleTimeoutError{Phase: phase, Cause: cause}
	}
}

func waitForCommandChannel[T any](resultCh <-chan T, closePipes func(), cause error, timeout time.Duration) (T, error) {
	select {
	case result := <-resultCh:
		return result, nil
	case <-time.After(timeout):
		closePipes()
		var zero T
		phase := "command pipe drain"
		if cause != nil {
			phase += " after cancellation"
		}
		return zero, &commandLifecycleTimeoutError{Phase: phase, Cause: cause}
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
