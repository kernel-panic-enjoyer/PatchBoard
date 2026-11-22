//go:build windows

package updater

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

// storeWorkerLaunch describes one run of a same-binary Store worker: the
// Store inventory worker or the Store update discovery worker. Each worker
// keeps its own timeout, request and response limit; the process handling
// is shared.
type storeWorkerLaunch struct {
	// Name is used in diagnostics, such as "Store inventory worker".
	Name          string
	Executable    string
	Args          []string
	Env           []string
	Request       []byte
	ResponseLimit int
}

// storeWorkerOutput is what a worker process produced. WaitErr and ExitCode
// are set when the process exited with an error; the caller still decodes
// Stdout, because a worker can report a structured failure and exit nonzero.
type storeWorkerOutput struct {
	Result   CommandResult
	Stdout   []byte
	WaitErr  error
	ExitCode int
}

// storeWorkerContext applies the worker's timeout (fallback when not set)
// and returns the deadline to send in the request.
func storeWorkerContext(ctx context.Context, timeout, fallback time.Duration) (context.Context, context.CancelFunc, time.Time) {
	if timeout <= 0 {
		timeout = fallback
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(timeout)
	}
	return ctx, cancel, deadline.UTC()
}

// storeWorkerExecutable is the worker binary: the given one, or this one.
func storeWorkerExecutable(executable string) (string, error) {
	if executable != "" {
		return executable, nil
	}
	return os.Executable()
}

// runStoreWorkerProcess starts the worker hidden in a kill-on-close Job
// Object, writes the request to its stdin, and reads at most ResponseLimit
// bytes of stdout and a bounded stderr tail. On error the returned output's
// Result holds the code and diagnostics to report; the worker has been
// terminated.
func runStoreWorkerProcess(ctx context.Context, launch storeWorkerLaunch) (storeWorkerOutput, error) {
	out := storeWorkerOutput{Result: CommandResult{Command: strings.Join(append([]string{launch.Executable}, launch.Args...), " ")}}
	fail := func(code int, err error) (storeWorkerOutput, error) {
		out.Result.Code = code
		out.Result.Stderr = err.Error()
		return out, err
	}
	cmd := exec.Command(launch.Executable, launch.Args...)
	cmd.Env = launchEnv()
	if len(launch.Env) > 0 {
		cmd.Env = append(cmd.Env, launch.Env...)
	}
	cmd.SysProcAttr = hiddenSysProcAttrWithFlags(true)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fail(127, err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fail(127, err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fail(127, err)
	}
	owner, err := newCommandProcessOwner(true)
	if err != nil {
		return fail(127, err)
	}
	defer owner.Close()
	if err := startCommandInJobObject(cmd, owner); err != nil {
		return fail(127, err)
	}

	writeDone := make(chan error, 1)
	goSafe("Store worker request writer", func() {
		_, writeErr := stdin.Write(launch.Request)
		closeErr := stdin.Close()
		if writeErr != nil {
			writeDone <- writeErr
			return
		}
		writeDone <- closeErr
	}, func(err error) { writeDone <- err })
	stdoutDone := make(chan []byte, 1)
	stderrTail := newBoundedOutputTail(commandResultStreamLimitBytes)
	stderrDone := make(chan struct{})
	goSafe("Store worker response reader", func() {
		stdoutDone <- readBoundedPipe(stdout, launch.ResponseLimit)
	}, func(error) { stdoutDone <- nil })
	goSafe("Store worker stderr reader", func() {
		_, _ = io.Copy(stderrTail, stderr)
		close(stderrDone)
	}, func(error) { close(stderrDone) })

	type workerIOResult struct {
		writeErr    error
		stdoutBytes []byte
	}
	workerIODone := make(chan workerIOResult, 1)
	goSafe("Store worker I/O collector", func() {
		workerIO := workerIOResult{
			writeErr:    <-writeDone,
			stdoutBytes: <-stdoutDone,
		}
		<-stderrDone
		workerIODone <- workerIO
	}, func(err error) { workerIODone <- workerIOResult{writeErr: err} })

	waitErr := waitForStartedCommand(ctx, cmd, owner)
	workerIO, workerIOErr := waitForCommandChannel(workerIODone, func() {
		_ = stdin.Close()
		_ = stdout.Close()
		_ = stderr.Close()
	}, ctx.Err(), commandOutputDrainTimeout)
	out.Result.Stderr = stderrTail.String()
	stop := func(code int, err error) (storeWorkerOutput, error) {
		out.Result.Code = code
		out.Result.Stderr = appendDiagnostic(out.Result.Stderr, err.Error())
		return out, err
	}
	switch {
	case ctx.Err() == context.DeadlineExceeded:
		return stop(124, errors.New(launch.Name+" timed out and was terminated"))
	case ctx.Err() == context.Canceled:
		return stop(commandCancelledCode, errors.New(launch.Name+" was cancelled and terminated"))
	case workerIOErr != nil:
		return stop(commandLaunchFailureCode, workerIOErr)
	case workerIO.writeErr != nil:
		return stop(commandLaunchFailureCode, workerIO.writeErr)
	}
	out.Stdout = workerIO.stdoutBytes
	if waitErr != nil {
		out.WaitErr = waitErr
		out.ExitCode = commandExitCode(waitErr)
		out.Result.Code = out.ExitCode
		out.Result.Stderr = appendDiagnostic(out.Result.Stderr, waitErr.Error())
	}
	return out, nil
}
