package updater

import (
	"fmt"
	"runtime/debug"
	"sync"
	"time"
)

// An unrecovered panic on any goroutine ends the whole PatchBoard process,
// including running installers and the web UI. Every goroutine in production
// code therefore starts through goSafe or goSafeGroup (enforced by
// TestProductionGoroutinesAreSupervised). A recovered panic is logged with its
// stack, written to a crash file, and handed to onPanic so the caller can fail
// its job or unblock whoever waits for the goroutine's result. onPanic must
// not be nil when someone waits on a channel the goroutine would have written.
func goSafe(name string, fn func(), onPanic func(error)) {
	// goroutine:supervised (this is the supervisor)
	go func() {
		defer recoverBackgroundPanic(name, onPanic)
		fn()
	}()
}

// goSafeGroup is goSafe for goroutines a WaitGroup waits on. It adds to the
// group itself and calls Done only after onPanic has run, so values onPanic
// writes are visible once Wait returns.
func goSafeGroup(group *sync.WaitGroup, name string, fn func(), onPanic func(error)) {
	group.Add(1)
	// goroutine:supervised (this is the supervisor)
	go func() {
		defer group.Done()
		defer recoverBackgroundPanic(name, onPanic)
		fn()
	}()
}

// recoverBackgroundPanic must be deferred directly; recover only works there.
func recoverBackgroundPanic(name string, onPanic func(error)) {
	panicValue := recover()
	if panicValue == nil {
		return
	}
	reportRecoveredPanic(name, panicValue, debug.Stack())
	if onPanic != nil {
		onPanic(backgroundPanicError(name, panicValue))
	}
}

func reportRecoveredPanic(source string, panicValue any, stack []byte) {
	appLog("%s panicked: %v\n%s", source, panicValue, stack)
	writeRecoveredPanicReport(source, panicValue, stack)
}

func backgroundPanicError(name string, panicValue any) error {
	return fmt.Errorf("%s failed: %s", name, sanitizedPanicDiagnostic(panicValue))
}

// failedBackgroundCommandResult stands in for the result a panicked
// background task never produced, so callers see a failure, not "nothing".
func failedBackgroundCommandResult(command string, err error) CommandResult {
	return CommandResult{Command: command, Code: 1, Stderr: err.Error()}
}

// requestShutdownSoon gives the current HTTP response or notice time to reach
// the browser before the process stops.
func (app *App) requestShutdownSoon(requestSource string) {
	goSafe("shutdown request", func() {
		time.Sleep(200 * time.Millisecond)
		app.requestShutdown(requestSource)
	}, nil)
}
