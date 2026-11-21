package updater

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestGoSafeGroupRecoversPanicAndPublishesOnPanicBeforeDone(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("UPDATER_STATE_DIR", stateDir)
	var group sync.WaitGroup
	var reported error
	goSafeGroup(&group, "status fixture", func() {
		panic("status check exploded")
	}, func(err error) {
		reported = err
	})
	group.Wait()
	if reported == nil || !strings.Contains(reported.Error(), "status fixture failed: internal job panic: status check exploded") {
		t.Fatalf("onPanic error = %v", reported)
	}
	assertRecoveredPanicReport(t, stateDir, "Recovered panic in status fixture")
}

func TestGoSafeRecoversPanicAndUnblocksWaiter(t *testing.T) {
	t.Setenv("UPDATER_STATE_DIR", t.TempDir())
	result := make(chan error, 1)
	goSafe("waiter fixture", func() {
		panic("waiter fixture exploded")
	}, func(err error) { result <- err })
	select {
	case err := <-result:
		if err == nil || !strings.Contains(err.Error(), "waiter fixture failed: internal job panic:") {
			t.Fatalf("unexpected error %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("waiter was not unblocked after the goroutine panicked")
	}
}

func TestGoSafeWithoutPanicDoesNotCallOnPanic(t *testing.T) {
	var group sync.WaitGroup
	called := false
	ran := false
	goSafeGroup(&group, "quiet fixture", func() { ran = true }, func(error) { called = true })
	group.Wait()
	if !ran || called {
		t.Fatalf("ran=%t onPanicCalled=%t", ran, called)
	}
}

func TestOperationJobPanicKeepsStackInCrashReport(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("UPDATER_STATE_DIR", stateDir)
	app := testSessionApp(t)
	job := app.startOperationJob(jobTypeScan, "", 1, nil, func(ctx context.Context, job *OperationJob) {
		panic("job fixture panic")
	})
	status, ok := waitForOperationJobState(app, job.JobID, 5*time.Second)
	if !ok || status.State != jobStateFailed {
		t.Fatalf("job did not fail: %#v", status)
	}
	assertRecoveredPanicReport(t, stateDir, "Recovered panic in job "+job.JobID)
}

func assertRecoveredPanicReport(t *testing.T, stateDir, wantHeader string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(stateDir, diagnosticLogDirectoryName, crashLogFilePrefix+"*"+diagnosticLogFileSuffix))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range matches {
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatal(readErr)
		}
		report := string(data)
		if strings.Contains(report, wantHeader) {
			if !strings.Contains(report, "goroutine") {
				t.Fatalf("crash report %s has no stack:\n%s", path, report)
			}
			return
		}
	}
	t.Fatalf("no crash report containing %q among %v", wantHeader, matches)
}

// TestProductionGoroutinesAreSupervised fails when production code starts a
// goroutine with a bare go statement. An unrecovered panic on any goroutine
// ends the process; use goSafe or goSafeGroup instead.
func TestProductionGoroutinesAreSupervised(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fileSet := token.NewFileSet()
	var violations []string
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") || name == "goroutine_supervision.go" {
			continue
		}
		file, parseErr := parser.ParseFile(fileSet, name, nil, parser.SkipObjectResolution)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			if statement, ok := node.(*ast.GoStmt); ok {
				violations = append(violations, fileSet.Position(statement.Pos()).String())
			}
			return true
		})
	}
	if len(violations) > 0 {
		t.Fatalf("bare go statements in production code (use goSafe or goSafeGroup):\n%s", strings.Join(violations, "\n"))
	}
}

func TestFailedBackgroundCommandResultIsAFailure(t *testing.T) {
	result := failedBackgroundCommandResult("winget list", errors.New("winget list failed: internal job panic: x"))
	if result.OK || result.Code == 0 || !strings.Contains(result.Stderr, "internal job panic") || result.Command != "winget list" {
		t.Fatalf("unexpected result %#v", result)
	}
}
