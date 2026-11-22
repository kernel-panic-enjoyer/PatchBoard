//go:build crashintegration

package updater

// These tests relaunch the test binary as a hidden child that deliberately
// crashes. Like the processintegration and selfupdateintegration suites they
// are excluded from default test binaries because that pattern can trigger
// endpoint-security heuristics. Run with dev\scripts\Run-Tests.ps1 -CrashIntegration.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCrashOutputFileCapturesUnrecoveredPanic(t *testing.T) {
	directory := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run", "^TestCrashOutputFileHelperProcess$")
	cmd.Env = append(os.Environ(), "UPDATER_CRASH_OUTPUT_HELPER_DIR="+directory)
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("crash helper exited successfully; output=%s", output)
	}
	names := diagnosticLogFilesWithPrefix(t, directory, crashLogFilePrefix)
	if len(names) != 1 {
		t.Fatalf("expected one crash file, got %v; helper output=%s", names, output)
	}
	data, err := os.ReadFile(filepath.Join(directory, names[0]))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "panic: diagnostic crash helper") || !strings.Contains(string(data), "goroutine") {
		t.Fatalf("crash file did not capture the panic and stack: %q", data)
	}
}

func TestCrashOutputFileHelperProcess(t *testing.T) {
	directory := os.Getenv("UPDATER_CRASH_OUTPUT_HELPER_DIR")
	if directory == "" {
		t.Skip("helper process only")
	}
	startCrashOutputFile(directory, time.Now(), os.Getpid())
	done := make(chan struct{})
	go func() {
		defer close(done)
		panic("diagnostic crash helper")
	}()
	<-done
}
