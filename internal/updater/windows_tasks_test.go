package updater

import (
	"context"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

func TestAutoUpdateTaskExecutableRequiresTrustedRoot(t *testing.T) {
	roots := []string{`C:\Program Files`, `C:\Program Files (x86)`, `C:\Windows`}
	if !pathWithinAnyRoot(`C:\Program Files\PatchBoard\PatchBoard.exe`, roots) {
		t.Fatal("expected Program Files executable to be trusted")
	}
	for _, exe := range []string{
		`C:\Users\User\Downloads\PatchBoard.exe`,
		`C:\Program Files Evil\PatchBoard.exe`,
		`C:\Users\User\AppData\Local\Temp\PatchBoard.exe`,
	} {
		if pathWithinAnyRoot(exe, roots) {
			t.Fatalf("expected user-writable executable path to be rejected: %s", exe)
		}
	}
}

func TestAutoUpdateTaskSupportStatusExplainsUntrustedInstallLocation(t *testing.T) {
	supported, reason := autoUpdateTaskSupportStatusForExecutable(`C:\Users\User\Documents\Updater\dist\PatchBoard.exe`)
	if supported {
		t.Fatal("expected daily auto-update to reject user-writable install location")
	}
	if !strings.Contains(reason, "Program Files") {
		t.Fatalf("expected install-location guidance, got %q", reason)
	}

	supported, reason = autoUpdateTaskSupportStatusForExecutable(`C:\Program Files\PatchBoard\PatchBoard.exe`)
	if !supported || reason != "" {
		t.Fatalf("expected Program Files executable to support daily auto-update, supported=%v reason=%q", supported, reason)
	}
}

func TestScheduledTaskXMLMatchesExpectedExecutableAndArguments(t *testing.T) {
	exe := `C:\Program Files\PatchBoard\PatchBoard.exe`
	taskXML := `<Task>
  <Actions Context="Author">
    <Exec>
      <Command>C:\Program Files\PatchBoard\PatchBoard.exe</Command>
      <Arguments>--task auto-update</Arguments>
    </Exec>
  </Actions>
</Task>`

	if !scheduledTaskXMLMatchesAction(taskXML, exe, []string{"--task", "auto-update"}) {
		t.Fatal("expected task XML to match the current auto-update action")
	}
	if scheduledTaskXMLMatchesAction(taskXML, exe, []string{"--no-browser"}) {
		t.Fatal("startup arguments should not match auto-update task XML")
	}
	if scheduledTaskXMLMatchesAction(taskXML, `C:\Other\PatchBoard.exe`, []string{"--task", "auto-update"}) {
		t.Fatal("task XML for another executable should not match")
	}
}

func TestScheduledTaskXMLMatchesCombinedCommandAction(t *testing.T) {
	exe := `C:\Program Files\PatchBoard\PatchBoard.exe`
	taskXML := `<Task>
  <Actions>
    <Exec>
      <Command>"C:\Program Files\PatchBoard\PatchBoard.exe" --no-browser</Command>
    </Exec>
  </Actions>
</Task>`

	if !scheduledTaskXMLMatchesAction(taskXML, exe, []string{"--no-browser"}) {
		t.Fatal("expected combined command XML to match startup action")
	}
}

func TestScheduledTaskActionComposerKeepsExecutableAndArgumentsSeparate(t *testing.T) {
	exe := `C:\Program Files\PatchBoard\PatchBoard.exe`
	action := windows.ComposeCommandLine([]string{exe, "--task", "auto-update"})
	args, err := windows.DecomposeCommandLine(action)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(args, "|") != strings.Join([]string{exe, "--task", "auto-update"}, "|") {
		t.Fatalf("scheduled task action did not round-trip cleanly: %#v from %q", args, action)
	}
}

func TestStartupRunEntryUsesCurrentUserRegistryInsteadOfScheduledTask(t *testing.T) {
	withTemporaryStartupRunRegistry(t)

	result := setStartupRunEntryDirect(true)
	if !result.OK {
		t.Fatalf("expected startup registry write to succeed, got %#v", result)
	}
	if strings.Contains(strings.ToLower(result.Command), "schtasks") {
		t.Fatalf("startup should not use schtasks because normal users can receive access denied: %#v", result)
	}
	if !startupTaskEnabledContext(context.Background()) {
		t.Fatal("expected startup status to be enabled after writing current-user Run entry")
	}
	value, err := startupRunEntryCommandLine()
	if err != nil {
		t.Fatal(err)
	}
	exe, err := osExecutable()
	if err != nil {
		t.Fatal(err)
	}
	if !startupRunEntryMatchesAction(value, exe) {
		t.Fatalf("startup Run entry did not target current executable with --no-browser: %q", value)
	}

	result = setStartupRunEntryDirect(false)
	if !result.OK {
		t.Fatalf("expected startup registry delete to succeed, got %#v", result)
	}
	if startupTaskEnabledContext(context.Background()) {
		t.Fatal("expected startup status to be disabled after deleting current-user Run entry")
	}
}

func TestSetStartupUsesRunEntryForEnableAndDisable(t *testing.T) {
	oldRunner := startupRunEntryRunner
	var calls []bool
	startupRunEntryRunner = func(ctx context.Context, enabled bool) CommandResult {
		calls = append(calls, enabled)
		return CommandResult{OK: true, Command: startupRunRegistryCommand}
	}
	t.Cleanup(func() { startupRunEntryRunner = oldRunner })

	if result := setStartup(true); !result.OK {
		t.Fatalf("enable startup result=%#v", result)
	}
	if result := setStartup(false); !result.OK {
		t.Fatalf("disable startup result=%#v", result)
	}
	if strings.Join(boolCalls(calls), "|") != "true|false" {
		t.Fatalf("expected startup Run entry runner for enable and disable, got %#v", calls)
	}
}

func TestSetStartupContextPassesCallerContextToRunner(t *testing.T) {
	oldRunner := startupRunEntryRunner
	contextKey := struct{}{}
	startupRunEntryRunner = func(ctx context.Context, enabled bool) CommandResult {
		if got := ctx.Value(contextKey); got != "request-context" {
			t.Fatalf("startup runner received context value %v, want request-context", got)
		}
		return CommandResult{OK: true, Command: startupRunRegistryCommand}
	}
	t.Cleanup(func() { startupRunEntryRunner = oldRunner })

	ctx := context.WithValue(context.Background(), contextKey, "request-context")
	if result := setStartupContext(ctx, true); !result.OK {
		t.Fatalf("startup result=%#v", result)
	}
}

func TestStartupRunEntryRequiresExactExecutableAndArgument(t *testing.T) {
	exe := `C:\Program Files\PatchBoard\PatchBoard.exe`
	valid := startupRunEntryCommandLineForExecutable(exe)
	if !startupRunEntryMatchesAction(valid, exe) {
		t.Fatalf("expected exact startup Run command to match: %q", valid)
	}
	for _, value := range []string{
		windows.ComposeCommandLine([]string{exe}),
		windows.ComposeCommandLine([]string{exe, "--task", "auto-update"}),
		windows.ComposeCommandLine([]string{`C:\Other\PatchBoard.exe`, "--no-browser"}),
		`schtasks.exe /Run /TN PatchBoard-Startup`,
	} {
		if startupRunEntryMatchesAction(value, exe) {
			t.Fatalf("unexpected startup Run match for %q", value)
		}
	}
}

func boolCalls(values []bool) []string {
	converted := make([]string, 0, len(values))
	for _, value := range values {
		if value {
			converted = append(converted, "true")
		} else {
			converted = append(converted, "false")
		}
	}
	return converted
}

func withTemporaryStartupRunRegistry(t *testing.T) {
	t.Helper()
	oldPath := startupRunRegistryPath
	oldValue := startupRunRegistryValue
	safeName := strings.NewReplacer("\\", "_", "/", "_", " ", "_").Replace(t.Name())
	testPath := `Software\PatchBoard-Test-` + safeName
	testValue := "Startup"
	startupRunRegistryPath = testPath
	startupRunRegistryValue = testValue
	t.Cleanup(func() {
		key, err := registry.OpenKey(registry.CURRENT_USER, testPath, registry.SET_VALUE)
		if err == nil {
			_ = key.DeleteValue(testValue)
			_ = key.Close()
		}
		_ = registry.DeleteKey(registry.CURRENT_USER, testPath)
		startupRunRegistryPath = oldPath
		startupRunRegistryValue = oldValue
	})
}
