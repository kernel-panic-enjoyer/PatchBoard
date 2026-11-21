package updater

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sync"
	"time"
)

const (
	commandTerminationWaitTimeout = 10 * time.Second
	commandOutputDrainTimeout     = 5 * time.Second
)

var (
	wingetMutationStallTimeout      = 3 * time.Minute
	commandActivitySamplingInterval = 5 * time.Second
)

type commandProcessActivitySnapshot struct {
	ProcessorTime   int64
	ActiveProcesses uint32
}

type commandStallError struct {
	IdleFor time.Duration
}

func (err *commandStallError) Error() string {
	return fmt.Sprintf("command made no observable progress for %s", err.IdleFor.Round(time.Second))
}

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
	return waitForStartedCommandWithTermination(ctx, startedCommand, processOwner, nil)
}

func waitForStartedCommandWithTermination(ctx context.Context, startedCommand *exec.Cmd, processOwner *commandProcessOwner, beforeTerminate func()) error {
	return waitForCommandExitWithTimeout(ctx, startedCommand.Wait, func() {
		if beforeTerminate != nil {
			beforeTerminate()
		}
		terminateStartedCommand(startedCommand, processOwner)
	}, commandTerminationWaitTimeout)
}

func waitForStartedCommandWithActivity(
	ctx context.Context,
	startedCommand *exec.Cmd,
	processOwner *commandProcessOwner,
	activity <-chan struct{},
	stallTimeout time.Duration,
	beforeTerminate func(),
) error {
	if processOwner == nil || stallTimeout <= 0 {
		return waitForStartedCommandWithTermination(ctx, startedCommand, processOwner, beforeTerminate)
	}
	return waitForCommandExitWithActivity(
		ctx,
		startedCommand.Wait,
		func() {
			if beforeTerminate != nil {
				beforeTerminate()
			}
			terminateStartedCommand(startedCommand, processOwner)
		},
		activity,
		processOwner.ActivitySnapshot,
		stallTimeout,
		commandActivitySamplingInterval,
		commandTerminationWaitTimeout,
	)
}

func waitForCommandExitWithActivity(
	ctx context.Context,
	wait func() error,
	terminate func(),
	activity <-chan struct{},
	sampleActivity func() (commandProcessActivitySnapshot, bool),
	stallTimeout time.Duration,
	samplingInterval time.Duration,
	terminationTimeout time.Duration,
) error {
	if stallTimeout <= 0 || samplingInterval <= 0 || sampleActivity == nil {
		return waitForCommandExitWithTimeout(ctx, wait, terminate, terminationTimeout)
	}
	waitResult := make(chan error, 1)
	go func() {
		waitResult <- wait()
	}()

	previousSnapshot, samplingSupported := sampleActivity()
	if !samplingSupported {
		return waitForCommandExitResult(ctx, waitResult, terminate, terminationTimeout)
	}
	lastActivity := time.Now()
	ticker := time.NewTicker(samplingInterval)
	defer ticker.Stop()

	for {
		select {
		case err := <-waitResult:
			return err
		case <-ctx.Done():
			terminate()
			return waitForTerminatedCommand(waitResult, terminationTimeout, ctx.Err())
		case <-activity:
			lastActivity = time.Now()
		case <-ticker.C:
			currentSnapshot, ok := sampleActivity()
			if !ok {
				return waitForCommandExitResult(ctx, waitResult, terminate, terminationTimeout)
			}
			if currentSnapshot.ProcessorTime != previousSnapshot.ProcessorTime || currentSnapshot.ActiveProcesses != previousSnapshot.ActiveProcesses {
				lastActivity = time.Now()
				previousSnapshot = currentSnapshot
			}
			// A child installer can legitimately be silent for several minutes. The
			// watchdog only terminates WinGet when it is the sole process left in
			// the owned tree and neither it nor its output has changed.
			if currentSnapshot.ActiveProcesses != 1 || time.Since(lastActivity) < stallTimeout {
				continue
			}
			stallErr := &commandStallError{IdleFor: time.Since(lastActivity)}
			terminate()
			if err := waitForTerminatedCommand(waitResult, terminationTimeout, stallErr); err != nil {
				var lifecycleErr *commandLifecycleTimeoutError
				if errors.As(err, &lifecycleErr) {
					return err
				}
			}
			return stallErr
		}
	}
}

func waitForCommandExitResult(ctx context.Context, waitResult <-chan error, terminate func(), timeout time.Duration) error {
	select {
	case err := <-waitResult:
		return err
	case <-ctx.Done():
		terminate()
		return waitForTerminatedCommand(waitResult, timeout, ctx.Err())
	}
}

func waitForTerminatedCommand(waitResult <-chan error, timeout time.Duration, cause error) error {
	select {
	case err := <-waitResult:
		return err
	case <-time.After(timeout):
		return &commandLifecycleTimeoutError{Phase: "process exit after cancellation", Cause: cause}
	}
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
