//go:build windows

package updater

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf16"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const (
	applicationInstallCommand       = "application install"
	applicationInstallFolderName    = "PatchBoard"
	applicationInstallExecutable    = "PatchBoard.exe"
	applicationStartMenuShortcut    = "PatchBoard.lnk"
	applicationUninstallRegistryKey = `Software\Microsoft\Windows\CurrentVersion\Uninstall\PatchBoard`
)

type ApplicationInstallStatus struct {
	Installed                bool   `json:"installed"`
	TargetPath               string `json:"target_path"`
	InstallDirectory         string `json:"install_directory"`
	RunningFromInstalledPath bool   `json:"running_from_installed_path"`
	ExecutableMatches        bool   `json:"executable_matches"`
	StartMenuShortcutExists  bool   `json:"start_menu_shortcut_exists"`
	InstalledAppsEntryExists bool   `json:"installed_apps_entry_exists"`
	RepairRequired           bool   `json:"repair_required"`
	RepairReason             string `json:"repair_reason,omitempty"`
	Version                  string `json:"version"`
}

type applicationInstallPaths struct {
	InstallDirectory  string
	TargetPath        string
	StartMenuShortcut string
}

var (
	applicationInstallCurrentExecutable    = osExecutable
	applicationInstalledAppsEntryExists    = applicationInstalledAppsEntryExistsDirect
	writeApplicationInstalledAppsEntry     = writeApplicationInstalledAppsEntryDirect
	deleteApplicationInstalledAppsEntry    = deleteApplicationInstalledAppsEntryDirect
	createApplicationStartMenuShortcut     = createApplicationStartMenuShortcutDirect
	launchInstalledApplicationExecutable   = launchInstalledApplicationExecutableDirect
	applicationInstallPathsProvider        = defaultApplicationInstallPaths
	scheduleApplicationUninstallSelfDelete = scheduleSelfDelete
)

func defaultApplicationInstallPaths() applicationInstallPaths {
	programFiles := strings.TrimSpace(os.Getenv("ProgramFiles"))
	if programFiles == "" {
		programFiles = `C:\Program Files`
	}
	programData := strings.TrimSpace(os.Getenv("ProgramData"))
	if programData == "" {
		programData = `C:\ProgramData`
	}
	installDirectory := filepath.Join(programFiles, applicationInstallFolderName)
	return applicationInstallPaths{
		InstallDirectory:  installDirectory,
		TargetPath:        filepath.Join(installDirectory, applicationInstallExecutable),
		StartMenuShortcut: filepath.Join(programData, "Microsoft", "Windows", "Start Menu", "Programs", applicationStartMenuShortcut),
	}
}

func currentApplicationInstallStatus() ApplicationInstallStatus {
	paths := applicationInstallPathsProvider()
	status := ApplicationInstallStatus{
		TargetPath:       paths.TargetPath,
		InstallDirectory: paths.InstallDirectory,
		Version:          currentAppVersion(),
	}
	currentExecutable, currentExecutableErr := applicationInstallCurrentExecutable()
	if currentExecutableErr == nil {
		status.RunningFromInstalledPath = sameWindowsPath(currentExecutable, paths.TargetPath)
	}
	status.StartMenuShortcutExists = fileExists(paths.StartMenuShortcut)
	status.InstalledAppsEntryExists = applicationInstalledAppsEntryExists(paths)

	targetExists := fileExists(paths.TargetPath)
	if targetExists && currentExecutableErr == nil {
		status.ExecutableMatches = filesHaveSameSHA256(currentExecutable, paths.TargetPath)
	}
	if targetExists && currentExecutableErr != nil {
		status.RepairRequired = true
		status.RepairReason = "Could not compare the running executable with the installed copy."
		return status
	}
	if !targetExists {
		return status
	}
	missingParts := applicationInstallRepairReasons(status)
	status.RepairRequired = len(missingParts) > 0
	status.RepairReason = strings.Join(missingParts, "; ")
	status.Installed = !status.RepairRequired
	return status
}

func applicationInstallRepairReasons(status ApplicationInstallStatus) []string {
	var reasons []string
	if !status.ExecutableMatches {
		reasons = append(reasons, "installed executable differs from the running executable")
	}
	if !status.StartMenuShortcutExists {
		reasons = append(reasons, "Start Menu shortcut is missing")
	}
	if !status.InstalledAppsEntryExists {
		reasons = append(reasons, "Installed Apps registration is missing")
	}
	return reasons
}

func fileExists(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func filesHaveSameSHA256(left, right string) bool {
	leftHash, err := fileSHA256(left)
	if err != nil {
		return false
	}
	rightHash, err := fileSHA256(right)
	return err == nil && strings.EqualFold(leftHash, rightHash)
}

func currentApplicationInstallPayload() (elevatedWorkerApplicationInstallPayload, error) {
	source, err := applicationInstallCurrentExecutable()
	if err != nil {
		return elevatedWorkerApplicationInstallPayload{}, err
	}
	paths := applicationInstallPathsProvider()
	payload := elevatedWorkerApplicationInstallPayload{
		SourceExe: source,
		TargetExe: paths.TargetPath,
	}
	if err := validateApplicationInstallPayload(payload); err != nil {
		return elevatedWorkerApplicationInstallPayload{}, err
	}
	return payload, nil
}

func validateApplicationInstallPayload(payload elevatedWorkerApplicationInstallPayload) error {
	source := strings.TrimSpace(payload.SourceExe)
	target := strings.TrimSpace(payload.TargetExe)
	if source == "" {
		return errors.New("application install source executable is required")
	}
	if target == "" {
		return errors.New("application install target executable is required")
	}
	currentExecutable, err := applicationInstallCurrentExecutable()
	if err != nil {
		return fmt.Errorf("resolve current executable: %w", err)
	}
	if !sameWindowsPath(source, currentExecutable) {
		return errors.New("application install source must be the current PatchBoard executable")
	}
	paths := applicationInstallPathsProvider()
	if !sameWindowsPath(target, paths.TargetPath) {
		return fmt.Errorf("application install target must be %s", paths.TargetPath)
	}
	if !strings.EqualFold(filepath.Base(target), applicationInstallExecutable) {
		return fmt.Errorf("application install target must be %s", applicationInstallExecutable)
	}
	if _, err := os.Stat(source); err != nil {
		return fmt.Errorf("application install source is not readable: %w", err)
	}
	return nil
}

func installApplicationContext(ctx context.Context) CommandResult {
	payload, err := currentApplicationInstallPayload()
	if err != nil {
		return validationCommandResult(applicationInstallCommand, err)
	}
	if !isAdmin() {
		return runElevatedWorkerOperation(ctx, elevatedWorkerInvocation{
			Operation: workerOperationApplicationInstall,
			Payload:   payload,
		})
	}
	return installApplicationDirectContext(ctx, payload.SourceExe, payload.TargetExe)
}

func installApplicationDirectContext(ctx context.Context, sourcePath, targetPath string) CommandResult {
	payload := elevatedWorkerApplicationInstallPayload{SourceExe: sourcePath, TargetExe: targetPath}
	if err := validateApplicationInstallPayload(payload); err != nil {
		return validationCommandResult(applicationInstallCommand, err)
	}
	if err := ctx.Err(); err != nil {
		return validationCommandResult(applicationInstallCommand, err)
	}
	paths := applicationInstallPathsProvider()
	if err := os.MkdirAll(paths.InstallDirectory, 0o755); err != nil {
		return validationCommandResult(applicationInstallCommand, fmt.Errorf("create install directory: %w", err))
	}
	if err := copyApplicationExecutableIfNeeded(sourcePath, targetPath); err != nil {
		return validationCommandResult(applicationInstallCommand, err)
	}
	if err := createApplicationStartMenuShortcut(ctx, paths); err != nil {
		return validationCommandResult(applicationInstallCommand, fmt.Errorf("create Start Menu shortcut: %w", err))
	}
	if err := writeApplicationInstalledAppsEntry(paths); err != nil {
		return validationCommandResult(applicationInstallCommand, fmt.Errorf("register Installed Apps entry: %w", err))
	}
	return CommandResult{OK: true, Command: applicationInstallCommand, Stdout: "PatchBoard installed to Program Files. Restart from the installed copy when ready."}
}

func copyApplicationExecutableIfNeeded(sourcePath, targetPath string) error {
	if sameWindowsPath(sourcePath, targetPath) {
		return nil
	}
	sourceHash, err := fileSHA256(sourcePath)
	if err != nil {
		return fmt.Errorf("hash source executable: %w", err)
	}
	if fileExists(targetPath) {
		targetHash, err := fileSHA256(targetPath)
		if err == nil && strings.EqualFold(sourceHash, targetHash) {
			return nil
		}
	}
	temp, err := os.CreateTemp(filepath.Dir(targetPath), ".PatchBoard-install-*.exe")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	cleanupTemp := true
	defer func() {
		if cleanupTemp {
			_ = os.Remove(tempPath)
		}
	}()
	if err := copyFileContents(temp, sourcePath); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tempPath, 0o755); err != nil {
		return err
	}
	tempHash, err := fileSHA256(tempPath)
	if err != nil {
		return fmt.Errorf("hash copied executable: %w", err)
	}
	if !strings.EqualFold(sourceHash, tempHash) {
		return errors.New("copied executable checksum mismatch")
	}
	backupPath := targetPath + ".bak"
	if err := replaceFileKeepingBackup(tempPath, targetPath, backupPath); err != nil {
		return fmt.Errorf("replace installed executable: %w", err)
	}
	cleanupTemp = false
	_ = os.Remove(backupPath)
	targetHash, err := fileSHA256(targetPath)
	if err != nil {
		return fmt.Errorf("hash installed executable: %w", err)
	}
	if !strings.EqualFold(sourceHash, targetHash) {
		return errors.New("installed executable checksum mismatch")
	}
	return nil
}

func createApplicationStartMenuShortcutDirect(ctx context.Context, paths applicationInstallPaths) error {
	if err := os.MkdirAll(filepath.Dir(paths.StartMenuShortcut), 0o755); err != nil {
		return err
	}
	script := strings.Join([]string{
		"$shell = New-Object -ComObject WScript.Shell",
		"$shortcut = $shell.CreateShortcut(" + powershellSingleQuoted(paths.StartMenuShortcut) + ")",
		"$shortcut.TargetPath = " + powershellSingleQuoted(paths.TargetPath),
		"$shortcut.WorkingDirectory = " + powershellSingleQuoted(paths.InstallDirectory),
		"$shortcut.Description = " + powershellSingleQuoted("PatchBoard"),
		"$shortcut.IconLocation = " + powershellSingleQuoted(paths.TargetPath+",0"),
		"$shortcut.Save()",
	}, "\n")
	result := runCommandContext(ctx, 30*time.Second, "powershell.exe", "-NoProfile", "-ExecutionPolicy", "Bypass", "-EncodedCommand", powershellEncodedCommand(script))
	if !result.OK {
		return fmt.Errorf("PowerShell shortcut creation failed with code %d: %s", result.Code, strings.TrimSpace(result.Stderr))
	}
	return nil
}

func powershellSingleQuoted(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func powershellEncodedCommand(script string) string {
	encodedRunes := utf16.Encode([]rune(script))
	bytes := make([]byte, 0, len(encodedRunes)*2)
	for _, r := range encodedRunes {
		bytes = append(bytes, byte(r), byte(r>>8))
	}
	return base64.StdEncoding.EncodeToString(bytes)
}

func applicationInstalledAppsEntryExistsDirect(paths applicationInstallPaths) bool {
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, applicationUninstallRegistryKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer key.Close()
	displayName, _, err := key.GetStringValue("DisplayName")
	if err != nil || displayName != appName {
		return false
	}
	installLocation, _, err := key.GetStringValue("InstallLocation")
	if err != nil || !sameWindowsPath(installLocation, paths.InstallDirectory) {
		return false
	}
	uninstallString, _, err := key.GetStringValue("UninstallString")
	return err == nil && strings.Contains(uninstallString, flagUninstall) && strings.Contains(uninstallString, applicationInstallExecutable)
}

func writeApplicationInstalledAppsEntryDirect(paths applicationInstallPaths) error {
	key, _, err := registry.CreateKey(registry.LOCAL_MACHINE, applicationUninstallRegistryKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()
	if err := key.SetStringValue("DisplayName", appName); err != nil {
		return err
	}
	if err := key.SetStringValue("DisplayVersion", currentAppVersion()); err != nil {
		return err
	}
	if err := key.SetStringValue("Publisher", "kernel-panic-enjoyer"); err != nil {
		return err
	}
	if err := key.SetStringValue("InstallLocation", paths.InstallDirectory); err != nil {
		return err
	}
	if err := key.SetStringValue("DisplayIcon", paths.TargetPath+",0"); err != nil {
		return err
	}
	if err := key.SetStringValue("URLInfoAbout", appRepositoryURL); err != nil {
		return err
	}
	if err := key.SetStringValue("UninstallString", windows.ComposeCommandLine([]string{paths.TargetPath, flagUninstall})); err != nil {
		return err
	}
	if err := key.SetDWordValue("NoModify", 1); err != nil {
		return err
	}
	if err := key.SetDWordValue("NoRepair", 1); err != nil {
		return err
	}
	if info, err := os.Stat(paths.TargetPath); err == nil {
		estimatedSizeKB := uint32((info.Size() + 1023) / 1024)
		if err := key.SetDWordValue("EstimatedSize", estimatedSizeKB); err != nil {
			return err
		}
	}
	return nil
}

func deleteApplicationInstalledAppsEntryDirect() error {
	err := registry.DeleteKey(registry.LOCAL_MACHINE, applicationUninstallRegistryKey)
	if errors.Is(err, registry.ErrNotExist) {
		return nil
	}
	return err
}

func restartInstalledApplicationContext(ctx context.Context) CommandResult {
	paths := applicationInstallPathsProvider()
	status := currentApplicationInstallStatus()
	if !status.Installed {
		return validationCommandResult("restart installed PatchBoard", errors.New("PatchBoard is not installed or the installation needs repair"))
	}
	if err := ctx.Err(); err != nil {
		return validationCommandResult("restart installed PatchBoard", err)
	}
	if err := launchInstalledApplicationExecutable(paths.TargetPath); err != nil {
		return validationCommandResult("restart installed PatchBoard", err)
	}
	return CommandResult{OK: true, Command: "restart installed PatchBoard", Stdout: "Started installed PatchBoard."}
}

func launchInstalledApplicationExecutableDirect(targetPath string) error {
	if isAdmin() {
		cmd := exec.Command("explorer.exe", targetPath)
		cmd.SysProcAttr = hiddenSysProcAttr()
		cmd.Env = launchEnv()
		return cmd.Start()
	}
	cmd := exec.Command(targetPath)
	cmd.SysProcAttr = hiddenSysProcAttr()
	cmd.Env = launchEnv()
	return cmd.Start()
}

func runApplicationUninstall() error {
	paths := applicationInstallPathsProvider()
	exe, err := applicationInstallCurrentExecutable()
	if err != nil {
		return err
	}
	if !sameWindowsPath(exe, paths.TargetPath) {
		return fmt.Errorf("uninstall must be run from %s", paths.TargetPath)
	}
	if !isAdmin() {
		process, err := shellExecuteRunasProcess(exe, strings.Join(quoteArgs([]string{flagUninstall}), " "))
		if err != nil {
			return err
		}
		if process != 0 {
			_ = windows.CloseHandle(process)
		}
		return nil
	}
	return launchApplicationUninstallHelper(paths)
}

func launchApplicationUninstallHelper(paths applicationInstallPaths) error {
	exe, err := applicationInstallCurrentExecutable()
	if err != nil {
		return err
	}
	helperPath := filepath.Join(os.TempDir(), fmt.Sprintf("PatchBoard-uninstall-%d.exe", os.Getpid()))
	helper, err := os.Create(helperPath)
	if err != nil {
		return err
	}
	if err := copyFileContents(helper, exe); err != nil {
		_ = helper.Close()
		return err
	}
	if err := helper.Close(); err != nil {
		return err
	}
	if !filesHaveSameSHA256(exe, helperPath) {
		return errors.New("uninstall helper checksum mismatch")
	}
	args := []string{
		flagUninstallApply,
		"--uninstall-target=" + paths.InstallDirectory,
		fmt.Sprintf("--uninstall-parent-pid=%d", os.Getpid()),
	}
	cmd := exec.Command(helperPath, args...)
	cmd.SysProcAttr = hiddenSysProcAttr()
	cmd.Env = launchEnv()
	return cmd.Start()
}

func runApplicationUninstallApply(targetDirectory string, parentPID int) error {
	if parentPID > 0 {
		if err := waitForParentExit(parentPID, selfUpdateApplyTimeout); err != nil {
			return err
		}
	}
	if err := validateApplicationUninstallTarget(targetDirectory); err != nil {
		return err
	}
	paths := applicationInstallPathsProvider()
	_ = os.Remove(paths.StartMenuShortcut)
	if err := deleteApplicationInstalledAppsEntry(); err != nil {
		return err
	}
	if err := os.RemoveAll(paths.InstallDirectory); err != nil {
		return err
	}
	scheduleApplicationUninstallSelfDelete()
	return nil
}

func validateApplicationUninstallTarget(targetDirectory string) error {
	paths := applicationInstallPathsProvider()
	if strings.TrimSpace(targetDirectory) == "" {
		return errors.New("uninstall target is required")
	}
	if !sameWindowsPath(targetDirectory, paths.InstallDirectory) {
		return fmt.Errorf("uninstall target must be %s", paths.InstallDirectory)
	}
	return nil
}

func scheduleSelfDelete() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	command := "ping 127.0.0.1 -n 2 >NUL & del /f /q " + syscallEscapeForCmd(exe)
	cmd := exec.Command("cmd.exe", "/d", "/c", command)
	cmd.SysProcAttr = hiddenSysProcAttr()
	_ = cmd.Start()
}

func syscallEscapeForCmd(path string) string {
	return `"` + strings.ReplaceAll(path, `"`, `\"`) + `"`
}
