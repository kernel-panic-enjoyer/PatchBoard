package updater

import (
	"context"
	"encoding/xml"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const (
	taskAutoUpdate            = "PatchBoard-AutoUpdate"
	defaultAutoUpdateTime     = "03:00"
	startupRunRegistryCommand = "startup run entry"
)

var (
	startupRunRegistryPath  = `Software\Microsoft\Windows\CurrentVersion\Run`
	startupRunRegistryValue = "PatchBoard"
)

type scheduledTaskXML struct {
	Actions scheduledTaskActions `xml:"Actions"`
}

type scheduledTaskActions struct {
	Execs []scheduledTaskExec `xml:"Exec"`
}

type scheduledTaskExec struct {
	Command   string `xml:"Command"`
	Arguments string `xml:"Arguments"`
}

func taskExists(name string) bool {
	return taskExistsContext(context.Background(), name)
}

func taskExistsContext(ctx context.Context, name string) bool {
	return runCommandContext(ctx, 30*time.Second, "schtasks.exe", "/Query", "/TN", name, "/FO", "LIST").OK
}

func startupTaskEnabledContext(ctx context.Context) bool {
	return startupRunEntryMatchesCurrentExecutableContext(ctx)
}

func autoUpdateTaskEnabledContext(ctx context.Context) bool {
	return taskMatchesCurrentExecutableContext(ctx, taskAutoUpdate, []string{"--task", "auto-update"})
}

func autoUpdateTaskSupportStatus() (bool, string) {
	exe, err := osExecutable()
	if err != nil {
		return false, err.Error()
	}
	return autoUpdateTaskSupportStatusForExecutable(exe)
}

func autoUpdateTaskSupportStatusForExecutable(exe string) (bool, string) {
	if err := validateAutoUpdateTaskExecutable(exe); err != nil {
		return false, err.Error()
	}
	return true, ""
}

func taskMatchesCurrentExecutableContext(ctx context.Context, name string, expectedArgs []string) bool {
	exe, err := osExecutable()
	if err != nil {
		return false
	}
	taskQuery := runCommandContext(ctx, 30*time.Second, "schtasks.exe", "/Query", "/TN", name, "/XML")
	if !taskQuery.OK {
		return false
	}
	return scheduledTaskXMLMatchesAction(taskQuery.Stdout, exe, expectedArgs)
}

func scheduledTaskXMLMatchesAction(rawXML, expectedExecutable string, expectedArgs []string) bool {
	var task scheduledTaskXML
	if err := xml.Unmarshal([]byte(rawXML), &task); err != nil {
		return false
	}
	for _, execAction := range task.Actions.Execs {
		if scheduledTaskExecMatchesAction(execAction, expectedExecutable, expectedArgs) {
			return true
		}
	}
	return false
}

func scheduledTaskExecMatchesAction(execAction scheduledTaskExec, expectedExecutable string, expectedArgs []string) bool {
	command := strings.Trim(strings.TrimSpace(execAction.Command), `"`)
	if sameWindowsPath(command, expectedExecutable) && stringSlicesEqual(splitWindowsCommandLine(execAction.Arguments), expectedArgs) {
		return true
	}
	fullCommandLine := strings.TrimSpace(execAction.Command + " " + execAction.Arguments)
	fullArgs := splitWindowsCommandLine(fullCommandLine)
	return len(fullArgs) > 0 && sameWindowsPath(fullArgs[0], expectedExecutable) && stringSlicesEqual(fullArgs[1:], expectedArgs)
}

func splitWindowsCommandLine(commandLine string) []string {
	args, err := windows.DecomposeCommandLine(strings.TrimSpace(commandLine))
	if err != nil {
		return nil
	}
	return args
}

func sameWindowsPath(left, right string) bool {
	left = strings.Trim(strings.TrimSpace(left), `"`)
	right = strings.Trim(strings.TrimSpace(right), `"`)
	if left == "" || right == "" {
		return false
	}
	if absoluteLeft, err := filepath.Abs(left); err == nil {
		left = absoluteLeft
	}
	if absoluteRight, err := filepath.Abs(right); err == nil {
		right = absoluteRight
	}
	return strings.EqualFold(filepath.Clean(left), filepath.Clean(right))
}

func stringSlicesEqual(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func startupRunEntryMatchesCurrentExecutableContext(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return false
	default:
	}
	exe, err := osExecutable()
	if err != nil {
		return false
	}
	value, err := startupRunEntryCommandLine()
	if err != nil {
		return false
	}
	return startupRunEntryMatchesAction(value, exe)
}

func startupRunEntryCommandLine() (string, error) {
	key, err := registry.OpenKey(registry.CURRENT_USER, startupRunRegistryPath, registry.QUERY_VALUE)
	if err != nil {
		return "", err
	}
	defer key.Close()
	value, _, err := key.GetStringValue(startupRunRegistryValue)
	if err != nil {
		return "", err
	}
	return value, nil
}

func startupRunEntryMatchesAction(value, expectedExecutable string) bool {
	args := splitWindowsCommandLine(value)
	return len(args) == 2 && sameWindowsPath(args[0], expectedExecutable) && args[1] == "--no-browser"
}

func startupRunEntryCommandLineForExecutable(exe string) string {
	return windows.ComposeCommandLine([]string{exe, "--no-browser"})
}

func setStartupRunEntryDirect(enabled bool) CommandResult {
	if enabled {
		return createStartupRunEntryDirect()
	}
	return deleteStartupRunEntryDirect()
}

func createStartupRunEntryDirect() CommandResult {
	exe, err := osExecutable()
	if err != nil {
		return validationCommandResult(startupRunRegistryCommand, err)
	}
	action := startupRunEntryCommandLineForExecutable(exe)
	key, _, err := registry.CreateKey(registry.CURRENT_USER, startupRunRegistryPath, registry.SET_VALUE)
	if err != nil {
		return validationCommandResult(startupRunRegistryCommand, err)
	}
	defer key.Close()
	if err := key.SetStringValue(startupRunRegistryValue, action); err != nil {
		return validationCommandResult(startupRunRegistryCommand, err)
	}
	return CommandResult{OK: true, Command: startupRunRegistryCommand, Stdout: "Start with Windows enabled for the current user."}
}

func deleteStartupRunEntryDirect() CommandResult {
	key, err := registry.OpenKey(registry.CURRENT_USER, startupRunRegistryPath, registry.SET_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return CommandResult{OK: true, Command: startupRunRegistryCommand, Stdout: "Start with Windows was not enabled."}
	}
	if err != nil {
		return validationCommandResult(startupRunRegistryCommand, err)
	}
	defer key.Close()
	if err := key.DeleteValue(startupRunRegistryValue); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return validationCommandResult(startupRunRegistryCommand, err)
	}
	return CommandResult{OK: true, Command: startupRunRegistryCommand, Stdout: "Start with Windows disabled for the current user."}
}

func createAutoUpdateTask() CommandResult {
	if !isAdmin() {
		return runElevatedWorkerOperation(context.Background(), elevatedWorkerInvocation{
			Operation: workerOperationAutoUpdateTask,
			Payload:   elevatedWorkerTaskPayload{Enabled: true},
		})
	}
	return createAutoUpdateTaskDirect()
}

func createAutoUpdateTaskDirect() CommandResult {
	exe, err := osExecutable()
	if err != nil {
		return validationCommandResult("auto-update task", err)
	}
	if err := validateAutoUpdateTaskExecutable(exe); err != nil {
		return validationCommandResult("auto-update task", err)
	}
	action := windows.ComposeCommandLine([]string{exe, "--task", "auto-update"})
	return runCommand(60*time.Second, "schtasks.exe", "/Create", "/TN", taskAutoUpdate, "/TR", action, "/SC", "DAILY", "/ST", defaultAutoUpdateTime, "/RL", "HIGHEST", "/F")
}

func validateAutoUpdateTaskExecutable(exe string) error {
	if pathWithinAnyRoot(exe, trustedAutoUpdateTaskRoots()) {
		return nil
	}
	return errors.New("auto-update task requires the executable to be installed under Program Files or Windows")
}

func trustedAutoUpdateTaskRoots() []string {
	return []string{
		os.Getenv("ProgramFiles"),
		os.Getenv("ProgramFiles(x86)"),
		os.Getenv("SystemRoot"),
	}
}

func pathWithinAnyRoot(path string, roots []string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	candidate, err := filepath.Abs(path)
	if err != nil {
		candidate = path
	}
	candidate = filepath.Clean(candidate)
	for _, root := range roots {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		rootPath, err := filepath.Abs(root)
		if err != nil {
			rootPath = root
		}
		rootPath = filepath.Clean(rootPath)
		rel, err := filepath.Rel(rootPath, candidate)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
			continue
		}
		return true
	}
	return false
}

func deleteTask(name string) CommandResult {
	if name == taskAutoUpdate && !isAdmin() {
		return runElevatedWorkerOperation(context.Background(), elevatedWorkerInvocation{
			Operation: workerOperationAutoUpdateTask,
			Payload:   elevatedWorkerTaskPayload{Enabled: false},
		})
	}
	return deleteTaskDirect(name)
}

func deleteTaskDirect(name string) CommandResult {
	if !taskExists(name) {
		return CommandResult{OK: true, Command: "delete " + name, Stdout: "Task did not exist."}
	}
	return runCommand(60*time.Second, "schtasks.exe", "/Delete", "/TN", name, "/F")
}

func setStartupTaskDirect(enabled bool) CommandResult {
	return setStartupRunEntryDirect(enabled)
}

func setAutoUpdateTaskDirect(enabled bool) CommandResult {
	if enabled {
		return createAutoUpdateTaskDirect()
	}
	return deleteTaskDirect(taskAutoUpdate)
}

var startupRunEntryRunner = setStartupRunEntryDirect
var createAutoUpdateTaskRunner = createAutoUpdateTask
var deleteTaskRunner = deleteTask

func setStartup(enabled bool) CommandResult {
	appLog("Startup setting update started: enabled=%t.", enabled)
	result := startupRunEntryRunner(enabled)
	appLog("Startup setting update finished with code %d.", result.Code)
	return result
}
