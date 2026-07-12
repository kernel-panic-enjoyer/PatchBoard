package updater

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"
)

const (
	commandTimeoutCode       = 124
	commandCancelledCode     = 130
	commandSkippedCode       = 204
	commandLaunchFailureCode = 127
)

type commandOperation uint8

const (
	// commandOperationAuto preserves compatibility for call sites that have not
	// yet declared their command policy. The command classifier remains the
	// source of truth until those call sites migrate.
	commandOperationAuto commandOperation = iota
	commandOperationReadOnly
	commandOperationPackageMutation
)

// CommandSpec makes timeout and package-operation policy explicit at command
// call sites. A read-only declaration is validated against the command
// classifier so a provider cannot accidentally bypass mutation ownership.
type CommandSpec struct {
	Arguments []string
	Timeout   time.Duration
	Operation commandOperation
}

type CommandResult struct {
	OK      bool   `json:"ok"`
	Code    int    `json:"code"`
	Stdout  string `json:"stdout"`
	Stderr  string `json:"stderr"`
	Command string `json:"command"`
}

func validationCommandResult(command string, err error) CommandResult {
	return CommandResult{Code: 2, Stderr: err.Error(), Command: command}
}

func runCommand(timeout time.Duration, args ...string) CommandResult {
	return runCommandContext(context.Background(), timeout, args...)
}

func runCommandContext(parentCtx context.Context, timeout time.Duration, args ...string) CommandResult {
	return runCommandSpec(parentCtx, CommandSpec{
		Arguments: args,
		Timeout:   timeout,
		Operation: commandOperationAuto,
	})
}

func runReadOnlyCommand(ctx context.Context, timeout time.Duration, args ...string) CommandResult {
	return runCommandSpec(ctx, CommandSpec{
		Arguments: args,
		Timeout:   timeout,
		Operation: commandOperationReadOnly,
	})
}

func runCommandSpec(parentCtx context.Context, spec CommandSpec) CommandResult {
	args := spec.Arguments
	timeout := spec.Timeout
	result := CommandResult{Command: strings.Join(args, " ")}
	packageMutation := isPackageManagerMutationCommand(args)
	switch spec.Operation {
	case commandOperationAuto:
	case commandOperationReadOnly:
		if packageMutation {
			return validationCommandResult(result.Command, fmt.Errorf("read-only command spec cannot run package mutation: %s", result.Command))
		}
	case commandOperationPackageMutation:
		if !packageMutation {
			return validationCommandResult(result.Command, fmt.Errorf("package-mutation command spec requires a recognized package mutation: %s", result.Command))
		}
	default:
		return validationCommandResult(result.Command, fmt.Errorf("unsupported command operation %d", spec.Operation))
	}
	logCategories := logCategoriesForCommand(args)
	commandLogCtx := withLogMetadata(parentCtx, logMetadata{CommandID: nextCommandLogID()})
	logCommand := func(stream, message string) {
		sessionLogs.AppendContext(commandLogCtx, stream, message, logCategories)
	}
	// launchFailureResult records an internal launch failure consistently: the
	// process never produced its own exit code, so we synthesize one and log it.
	launchFailureResult := func(message string) CommandResult {
		result.Code = commandLaunchFailureCode
		result.Stderr = message
		logCommand("stderr", message)
		logCommand("exit", fmt.Sprintf("%s exited with code %d", result.Command, commandLaunchFailureCode))
		return result
	}
	if len(args) == 0 {
		result.Stderr = "empty command"
		result.Code = commandLaunchFailureCode
		logCommand("command", "<empty command>")
		logCommand("stderr", result.Stderr)
		logCommand("exit", fmt.Sprintf("empty command exited with code %d", commandLaunchFailureCode))
		return result
	}
	commandCtx, cancel := context.WithTimeout(commandLogCtx, timeout)
	defer cancel()

	startedAt := time.Now()
	logCommand("command", result.Command)
	if packageMutation {
		releasePackageOperation, err := defaultPackageMutationCoordinator.Acquire(commandCtx, func() {
			logCommand("app", "Waiting for another package operation before running "+result.Command)
		})
		if err != nil {
			return packageMutationLockFailureResult(commandCtx, result.Command, logCategories, err)
		}
		defer releasePackageOperation()
	}
	if isWingetCommand(args) && packageMutation {
		if !lockMutexContextWithWait(commandCtx, &wingetCommandMu, func() {
			logCommand("app", "Waiting for another winget mutation to finish before running "+result.Command)
		}) {
			return commandContextDoneResult(commandCtx, result.Command, "while waiting for winget lock", logCategories)
		}
		defer wingetCommandMu.Unlock()
	}

	processOwner, err := newCommandProcessOwner(packageMutation)
	if err != nil {
		return launchFailureResult(err.Error())
	}
	if processOwner != nil {
		defer processOwner.Close()
	}

	commandProcess := exec.Command(args[0], args[1:]...)
	commandProcess.Env = launchEnv()
	commandProcess.SysProcAttr = hiddenSysProcAttrWithFlags(processOwner != nil)
	stdoutTail := newBoundedOutputTail(commandResultStreamLimitBytes)
	stderrTail := newBoundedOutputTail(commandResultStreamLimitBytes)
	stdoutPipe, err := commandProcess.StdoutPipe()
	if err != nil {
		return launchFailureResult(err.Error())
	}
	stderrPipe, err := commandProcess.StderrPipe()
	if err != nil {
		return launchFailureResult(err.Error())
	}

	if err := startCommandInJobObject(commandProcess, processOwner); err != nil {
		if commandCtx.Err() == context.DeadlineExceeded {
			result.Code = commandTimeoutCode
			result.Stderr = "Timed out."
			logCommand("stderr", result.Stderr)
			logCommand("exit", fmt.Sprintf("%s timed out before start", result.Command))
			return result
		}
		if commandCtx.Err() == context.Canceled {
			result.Code = commandCancelledCode
			result.Stderr = "Cancelled."
			logCommand("stderr", result.Stderr)
			logCommand("exit", fmt.Sprintf("%s cancelled before start", result.Command))
			return result
		}
		return launchFailureResult(err.Error())
	}
	var outputReaders sync.WaitGroup
	emitStdoutToSessionLog := !suppressCommandStdoutInSessionLog(args)
	outputReaders.Add(2)
	go streamCommandOutputContext(commandCtx, stdoutPipe, "stdout", stdoutTail, &outputReaders, logCategories, emitStdoutToSessionLog)
	go streamCommandOutputContext(commandCtx, stderrPipe, "stderr", stderrTail, &outputReaders, logCategories, true)
	commandExitErr := waitForStartedCommand(commandCtx, commandProcess, processOwner)
	outputDrainErr := waitForCommandOutputReaders(&outputReaders, func() {
		_ = stdoutPipe.Close()
		_ = stderrPipe.Close()
	}, commandCtx.Err())

	result.Stdout = stdoutTail.String()
	result.Stderr = stderrTail.String()
	logDetectionSummaryIfStdoutSuppressed := func() {
		if emitStdoutToSessionLog {
			return
		}
		logStoreDetectionCommandSummary(commandCtx, args, result, logCategories, time.Since(startedAt))
	}
	if commandCtx.Err() == context.DeadlineExceeded {
		result.Code = commandTimeoutCode
		result.Stderr += "\nTimed out."
		for _, err := range []error{commandExitErr, outputDrainErr} {
			if err != nil {
				result.Stderr = appendDiagnostic(result.Stderr, err.Error())
			}
		}
		logCommand("stderr", "Timed out.")
		logDetectionSummaryIfStdoutSuppressed()
		logCommand("exit", fmt.Sprintf("%s exited with code %d", result.Command, commandTimeoutCode))
		return result
	}
	if commandCtx.Err() == context.Canceled {
		result.Code = commandCancelledCode
		result.Stderr += "\nCancelled."
		for _, err := range []error{commandExitErr, outputDrainErr} {
			if err != nil {
				result.Stderr = appendDiagnostic(result.Stderr, err.Error())
			}
		}
		logCommand("stderr", "Cancelled.")
		logDetectionSummaryIfStdoutSuppressed()
		logCommand("exit", fmt.Sprintf("%s cancelled with code %d", result.Command, result.Code))
		return result
	}
	if commandExitErr != nil || outputDrainErr != nil {
		if exitErr, ok := commandExitErr.(*exec.ExitError); ok {
			result.Code = exitErr.ExitCode()
		} else if commandExitErr != nil {
			result.Code = commandLaunchFailureCode
			if result.Stderr == "" {
				result.Stderr = commandExitErr.Error()
			}
		}
		if outputDrainErr != nil {
			if commandExitErr == nil {
				result.Code = commandLaunchFailureCode
			}
			result.Stderr = appendDiagnostic(result.Stderr, outputDrainErr.Error())
		}
		logDetectionSummaryIfStdoutSuppressed()
		logCommand("exit", fmt.Sprintf("%s exited with code %d", result.Command, result.Code))
		return result
	}
	result.OK = true
	logDetectionSummaryIfStdoutSuppressed()
	logCommand("exit", fmt.Sprintf("%s exited with code 0", result.Command))
	return result
}
