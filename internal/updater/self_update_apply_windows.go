//go:build windows

package updater

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

const (
	selfUpdateHelperShutdownTimeout    = 5 * time.Second
	selfUpdateReplacementRetryTimeout  = 10 * time.Second
	selfUpdateReplacementRetryInterval = 100 * time.Millisecond
	selfUpdateProcessStillActive       = 259
)

type selfUpdateRestartedProcess struct {
	handle           windows.Handle
	representsTarget bool
}

type selfUpdateRestartFunc func(string) (selfUpdateRestartedProcess, error)

func launchSelfUpdateApply(ctx context.Context, artifact selfUpdateArtifact, targetPath string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if artifact.Path == "" || artifact.SHA256 == "" {
		return errors.New("self-update artifact is incomplete")
	}
	if err := validateSelfUpdateLaunchTarget(targetPath); err != nil {
		return err
	}
	userSID, err := currentUserSID()
	if err != nil {
		return err
	}
	sessionID, err := currentSessionID()
	if err != nil {
		return err
	}
	request := selfUpdateApplyRequest{
		SourcePath:      artifact.Path,
		TargetPath:      targetPath,
		ExpectedSHA256:  strings.ToLower(strings.TrimSpace(artifact.SHA256)),
		ParentPID:       os.Getpid(),
		ParentUserSID:   userSID,
		ParentSessionID: sessionID,
		DeadlineUnixMS:  time.Now().Add(selfUpdateApplyTimeout + selfUpdateApplyStartupTimeout).UnixMilli(),
		Restart:         true,
	}
	return launchSelfUpdateApplyHelper(ctx, artifact.Path, request, false)
}

func validateSelfUpdateLaunchTarget(targetPath string) error {
	currentExecutable, err := os.Executable()
	if err != nil {
		return err
	}
	if sameSelfUpdatePath(targetPath, currentExecutable) {
		return nil
	}
	installedTargetPath := applicationInstallPathsProvider().TargetPath
	if installedTargetPath != "" && sameSelfUpdatePath(targetPath, installedTargetPath) {
		return nil
	}
	return errors.New("self-update target must be the running executable or installed PatchBoard executable")
}

// runSelfUpdateApplyFromOptions keeps the legacy argument path solely so an
// older PatchBoard parent can hand off to a newly downloaded helper. Legacy
// authorization is not based on the target argument alone: the target must be
// the exact image currently running as the supplied parent PID.
func runSelfUpdateApplyFromOptions(options cliOptions) error {
	if options.SelfUpdateManifest != "" {
		manifest, err := consumeSelfUpdateApplyManifest(options.SelfUpdateManifest, time.Now())
		if err != nil {
			return err
		}
		return runSelfUpdateApplyFromPipe(manifest.PipeName, manifest.Capability)
	}
	request := selfUpdateApplyRequestFromOptions(options)
	parentHandle, err := authorizeLegacySelfUpdateApplyRequest(&request)
	if err != nil {
		return err
	}
	return runAuthorizedSelfUpdateApply(request, parentHandle)
}

func runSelfUpdateApplyFromPipe(pipeName, capability string) error {
	pipeConn, err := connectElevatedWorkerPipe(pipeName, selfUpdateApplyStartupTimeout)
	if err != nil {
		return fmt.Errorf("connect self-update apply pipe: %w", err)
	}
	var message selfUpdateApplyMessage
	if err := readSelfUpdateApplyMessage(pipeConn, &message); err != nil {
		_ = pipeConn.Close()
		return fmt.Errorf("read self-update apply request: %w", err)
	}
	parentHandle, validationErr := validateSelfUpdateApplyMessageForCurrentProcess(message, capability, time.Now())
	response := selfUpdateApplyResponse{
		ProtocolVersion: selfUpdateApplyProtocolVersion,
		RequestID:       message.RequestID,
		State:           selfUpdateApplyReadyState,
	}
	if validationErr != nil {
		response.State = ""
		response.Error = validationErr.Error()
	}
	if err := writeSelfUpdateApplyMessage(pipeConn, response); err != nil {
		_ = pipeConn.Close()
		if parentHandle != 0 {
			_ = windows.CloseHandle(parentHandle)
		}
		return fmt.Errorf("write self-update apply readiness: %w", err)
	}
	if err := pipeConn.Close(); err != nil && validationErr == nil {
		if parentHandle != 0 {
			_ = windows.CloseHandle(parentHandle)
		}
		return fmt.Errorf("close self-update apply pipe: %w", err)
	}
	if validationErr != nil {
		return validationErr
	}
	err = runAuthorizedSelfUpdateApply(message.Request, parentHandle)
	recordSelfUpdateApplyOutcome(message.Request, err)
	return err
}

func validateSelfUpdateApplyMessageForCurrentProcess(message selfUpdateApplyMessage, capability string, now time.Time) (windows.Handle, error) {
	if err := validateSelfUpdateApplyMessage(message, capability, now); err != nil {
		return 0, err
	}
	request := message.Request
	currentExecutable, err := os.Executable()
	if err != nil {
		return 0, err
	}
	currentExecutable, err = filepath.Abs(currentExecutable)
	if err != nil {
		return 0, err
	}
	sourcePath, err := filepath.Abs(request.SourcePath)
	if err != nil {
		return 0, err
	}
	if !sameSelfUpdatePath(currentExecutable, sourcePath) {
		return 0, errors.New("self-update apply source is not the running helper executable")
	}
	currentSID, err := currentUserSID()
	if err != nil {
		return 0, err
	}
	if !strings.EqualFold(currentSID, request.ParentUserSID) {
		return 0, errors.New("self-update apply user SID does not match the parent user")
	}
	currentSession, err := currentSessionID()
	if err != nil {
		return 0, err
	}
	if currentSession != request.ParentSessionID {
		return 0, errors.New("self-update apply session does not match the parent session")
	}
	expectedParentImage := request.TargetPath
	if request.Delegated {
		expectedParentImage = request.SourcePath
	}
	return openValidatedSelfUpdateParentProcess(request.ParentPID, expectedParentImage, request.ParentUserSID, request.ParentSessionID)
}

func authorizeLegacySelfUpdateApplyRequest(request *selfUpdateApplyRequest) (windows.Handle, error) {
	if request == nil {
		return 0, errors.New("self-update apply request is required")
	}
	if err := validateSelfUpdateApplyRequest(*request); err != nil {
		return 0, err
	}
	userSID, err := currentUserSID()
	if err != nil {
		return 0, err
	}
	sessionID, err := currentSessionID()
	if err != nil {
		return 0, err
	}
	request.ParentUserSID = userSID
	request.ParentSessionID = sessionID
	request.DeadlineUnixMS = time.Now().Add(selfUpdateApplyTimeout).UnixMilli()
	return openValidatedSelfUpdateParentProcess(request.ParentPID, request.TargetPath, userSID, sessionID)
}

func openValidatedSelfUpdateParentProcess(parentPID int, expectedImagePath, expectedUserSID string, expectedSessionID uint32) (windows.Handle, error) {
	if parentPID <= 0 {
		return 0, errors.New("self-update parent PID is required")
	}
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.SYNCHRONIZE, false, uint32(parentPID))
	if err != nil {
		return 0, fmt.Errorf("open self-update parent process: %w", err)
	}
	valid := false
	defer func() {
		if !valid {
			_ = windows.CloseHandle(handle)
		}
	}()

	imagePath, err := processImagePath(handle)
	if err != nil {
		return 0, fmt.Errorf("query self-update parent image: %w", err)
	}
	if !sameSelfUpdatePath(imagePath, expectedImagePath) {
		return 0, fmt.Errorf("self-update parent image %q does not match authorized path", imagePath)
	}
	var token windows.Token
	if err := windows.OpenProcessToken(handle, windows.TOKEN_QUERY, &token); err != nil {
		return 0, fmt.Errorf("open self-update parent token: %w", err)
	}
	defer token.Close()
	tokenUser, err := token.GetTokenUser()
	if err != nil {
		return 0, fmt.Errorf("query self-update parent user: %w", err)
	}
	if tokenUser == nil || tokenUser.User.Sid == nil || !strings.EqualFold(tokenUser.User.Sid.String(), expectedUserSID) {
		return 0, errors.New("self-update parent user SID mismatch")
	}
	var sessionID uint32
	if err := windows.ProcessIdToSessionId(uint32(parentPID), &sessionID); err != nil {
		return 0, fmt.Errorf("query self-update parent session: %w", err)
	}
	if sessionID != expectedSessionID {
		return 0, errors.New("self-update parent session mismatch")
	}
	valid = true
	return handle, nil
}

func processImagePath(process windows.Handle) (string, error) {
	buffer := make([]uint16, 32768)
	size := uint32(len(buffer))
	if err := windows.QueryFullProcessImageName(process, 0, &buffer[0], &size); err != nil {
		return "", err
	}
	if size == 0 || int(size) > len(buffer) {
		return "", errors.New("process image path is empty")
	}
	return windows.UTF16ToString(buffer[:size]), nil
}

func runAuthorizedSelfUpdateApply(request selfUpdateApplyRequest, parentHandle windows.Handle) error {
	if err := validateSelfUpdateApplyRequest(request); err != nil {
		if parentHandle != 0 {
			_ = windows.CloseHandle(parentHandle)
		}
		return err
	}
	if parentHandle != 0 {
		defer windows.CloseHandle(parentHandle)
	}
	if request.DeadlineUnixMS > 0 && !time.Now().Before(time.UnixMilli(request.DeadlineUnixMS)) {
		return errors.New("self-update apply request expired before execution")
	}
	if parentHandle != 0 {
		remaining := selfUpdateApplyTimeout
		if request.DeadlineUnixMS > 0 {
			remaining = time.Until(time.UnixMilli(request.DeadlineUnixMS))
			if remaining <= 0 {
				return errors.New("self-update apply request expired while waiting for parent")
			}
		}
		if err := waitForParentHandleExit(parentHandle, request.ParentPID, remaining); err != nil {
			return err
		}
	}
	return applySelfUpdateReplacementTransaction(request)
}

func applySelfUpdateReplacementTransaction(request selfUpdateApplyRequest) error {
	return applySelfUpdateReplacementTransactionWithRestart(request, restartSelfUpdatedApp)
}

func applySelfUpdateReplacementTransactionWithRestart(request selfUpdateApplyRequest, restart selfUpdateRestartFunc) error {
	if restart == nil {
		return errors.New("self-update restart function is required")
	}
	if err := replaceSelfUpdateExecutableWithRetry(request); err != nil {
		if !request.Elevated && isSelfUpdatePermissionError(err) {
			return relaunchSelfUpdateApplyElevated(request)
		}
		return err
	}
	if !request.Restart {
		return nil
	}
	healthRequest, healthRequestPath, healthAckPath, err := createSelfUpdateStartupHealthRequest(request)
	if err != nil {
		return rollbackSelfUpdateAfterStartupFailure(request, restart, "create startup health request", err)
	}
	defer os.Remove(healthRequestPath)
	defer os.Remove(healthAckPath)
	restartedProcess, err := restart(request.TargetPath)
	if err != nil {
		return rollbackSelfUpdateAfterStartupFailure(request, restart, "restart updated application", err)
	}
	defer restartedProcess.Close()
	if err := waitForSelfUpdateStartupHealth(healthRequest, healthAckPath, restartedProcess); err != nil {
		if restartedProcess.representsTarget {
			terminateSelfUpdateHelper(elevatedWorkerProcess{handle: restartedProcess.handle})
		}
		return rollbackSelfUpdateAfterStartupFailure(request, restart, "verify restarted application", err)
	}
	if err := os.Remove(request.TargetPath + ".bak"); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove verified self-update backup: %w", err)
	}
	return nil
}

func (process selfUpdateRestartedProcess) Close() {
	if process.handle != 0 {
		_ = windows.CloseHandle(process.handle)
	}
}

func waitForSelfUpdateStartupHealth(healthRequest selfUpdateStartupHealthRequest, healthAckPath string, restartedProcess selfUpdateRestartedProcess) error {
	deadline := time.UnixMilli(healthRequest.DeadlineUnixMS)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		if ack, err := readSelfUpdateStartupHealthAck(healthAckPath); err == nil {
			return validateSelfUpdateStartupHealthAck(healthRequest, ack)
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("read self-update startup health acknowledgement: %w", err)
		}
		if restartedProcess.representsTarget && restartedProcess.handle != 0 {
			var exitCode uint32
			if err := windows.GetExitCodeProcess(restartedProcess.handle, &exitCode); err != nil {
				return fmt.Errorf("query restarted application state: %w", err)
			}
			if exitCode != selfUpdateProcessStillActive {
				return fmt.Errorf("restarted application exited with code %d before startup health acknowledgement", exitCode)
			}
		}
		if !time.Now().Before(deadline) {
			return errors.New("timed out waiting for restarted application startup health acknowledgement")
		}
		<-ticker.C
	}
}

func rollbackSelfUpdateAfterStartupFailure(request selfUpdateApplyRequest, restart selfUpdateRestartFunc, phase string, cause error) error {
	if restoreErr := restoreSelfUpdateBackup(request.TargetPath); restoreErr != nil {
		return fmt.Errorf("self-update %s failed: %w; rollback failed: %v", phase, cause, restoreErr)
	}
	if request.Restart {
		if restoredProcess, restartErr := restart(request.TargetPath); restartErr != nil {
			return fmt.Errorf("self-update %s failed: %w; restored previous executable but could not restart it: %v", phase, cause, restartErr)
		} else {
			restoredProcess.Close()
		}
	}
	return fmt.Errorf("self-update %s failed: %w; restored previous executable", phase, cause)
}

func restoreSelfUpdateBackup(targetPath string) error {
	backupPath := targetPath + ".bak"
	backup, err := os.Open(backupPath)
	if err != nil {
		return fmt.Errorf("open self-update backup: %w", err)
	}
	temp, err := os.CreateTemp(filepath.Dir(targetPath), ".PatchBoard-rollback-*.exe")
	if err != nil {
		_ = backup.Close()
		return err
	}
	tempPath := temp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tempPath)
		}
	}()
	if _, err := io.Copy(temp, backup); err != nil {
		_ = backup.Close()
		_ = temp.Close()
		return err
	}
	if err := backup.Close(); err != nil {
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
	if err := replaceFileKeepingBackup(tempPath, targetPath, targetPath+".failed"); err != nil {
		return err
	}
	cleanup = false
	if err := os.Remove(backupPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func replaceSelfUpdateExecutableWithRetry(request selfUpdateApplyRequest) error {
	deadline := time.Now().Add(selfUpdateReplacementRetryTimeout)
	for {
		err := replaceExecutableForSelfUpdate(request)
		if err == nil || !isSelfUpdateSharingViolation(err) || !time.Now().Before(deadline) {
			return err
		}
		time.Sleep(selfUpdateReplacementRetryInterval)
	}
}

func launchSelfUpdateApplyHelper(ctx context.Context, executablePath string, request selfUpdateApplyRequest, elevated bool) error {
	requestID, err := randomToken()
	if err != nil {
		return err
	}
	capability, err := randomToken()
	if err != nil {
		return err
	}
	pipeName := `\\.\pipe\PatchBoard-self-update-` + requestID
	pipeServer, err := newElevatedWorkerPipeServer(pipeName, request.ParentUserSID)
	if err != nil {
		return err
	}
	defer pipeServer.Close()
	manifestPath, err := createSelfUpdateApplyManifest(pipeName, capability, time.Now().Add(selfUpdateApplyStartupTimeout))
	if err != nil {
		return err
	}
	defer os.Remove(manifestPath)

	args := []string{
		flagSelfUpdateApply,
		"--self-update-manifest=" + manifestPath,
	}
	var helperProcess elevatedWorkerProcess
	if elevated {
		processHandle, launchErr := shellExecuteRunasProcess(executablePath, strings.Join(quoteArgs(args), " "))
		if launchErr != nil {
			return launchErr
		}
		helperProcess = elevatedWorkerProcess{handle: processHandle}
	} else {
		helperProcess, err = launchSelfUpdateApplyProcess(executablePath, args)
		if err != nil {
			return err
		}
	}
	defer helperProcess.Close()

	startupCtx, cancelStartup := context.WithTimeout(ctx, selfUpdateApplyStartupTimeout)
	pipeConn, err := acceptElevatedWorkerConnection(startupCtx, pipeServer, helperProcess)
	cancelStartup()
	if err != nil {
		terminateSelfUpdateHelper(helperProcess)
		return fmt.Errorf("self-update apply helper did not connect: %w", err)
	}
	defer pipeConn.Close()
	message := selfUpdateApplyMessage{
		ProtocolVersion: selfUpdateApplyProtocolVersion,
		RequestID:       requestID,
		Capability:      capability,
		Request:         request,
	}
	if err := writeSelfUpdateApplyMessage(pipeConn, message); err != nil {
		terminateSelfUpdateHelper(helperProcess)
		return fmt.Errorf("send self-update apply request: %w", err)
	}
	var response selfUpdateApplyResponse
	if err := readSelfUpdateApplyMessage(pipeConn, &response); err != nil {
		terminateSelfUpdateHelper(helperProcess)
		return fmt.Errorf("read self-update apply readiness: %w", err)
	}
	if err := validateSelfUpdateApplyResponse(response, requestID); err != nil {
		terminateSelfUpdateHelper(helperProcess)
		return err
	}
	return nil
}

func launchSelfUpdateApplyProcess(executablePath string, args []string) (elevatedWorkerProcess, error) {
	command := exec.Command(executablePath, args...)
	command.Env = launchEnv()
	command.SysProcAttr = hiddenSysProcAttr()
	if err := command.Start(); err != nil {
		return elevatedWorkerProcess{}, err
	}
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_TERMINATE, false, uint32(command.Process.Pid))
	if err != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		return elevatedWorkerProcess{}, err
	}
	if err := command.Process.Release(); err != nil {
		_ = windows.TerminateProcess(handle, uint32(commandCancelledCode))
		_ = windows.CloseHandle(handle)
		return elevatedWorkerProcess{}, err
	}
	return elevatedWorkerProcess{handle: handle}, nil
}

func terminateSelfUpdateHelper(process elevatedWorkerProcess) {
	process.Terminate()
	if process.handle == 0 {
		return
	}
	_, _ = windows.WaitForSingleObject(process.handle, uint32(selfUpdateHelperShutdownTimeout/time.Millisecond))
}

func waitForParentHandleExit(handle windows.Handle, pid int, timeout time.Duration) error {
	if timeout <= 0 {
		return errors.New("self-update parent wait deadline has expired")
	}
	deadline := uint32(timeout / time.Millisecond)
	result, err := windows.WaitForSingleObject(handle, deadline)
	if err != nil {
		return err
	}
	if result == uint32(windows.WAIT_TIMEOUT) {
		return fmt.Errorf("timed out waiting for parent process %d", pid)
	}
	return nil
}

// waitForParentExit is used by the uninstall helper, which has no authenticated
// handoff protocol. Self-update uses waitForParentHandleExit after validating
// and retaining the parent handle before it acknowledges readiness.
func waitForParentExit(pid int, timeout time.Duration) error {
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
			return nil
		}
		return err
	}
	defer windows.CloseHandle(handle)
	return waitForParentHandleExit(handle, pid, timeout)
}

func relaunchSelfUpdateApplyElevated(request selfUpdateApplyRequest) error {
	executablePath, err := os.Executable()
	if err != nil {
		return err
	}
	userSID, err := currentUserSID()
	if err != nil {
		return err
	}
	sessionID, err := currentSessionID()
	if err != nil {
		return err
	}
	request.SourcePath = executablePath
	request.ParentPID = os.Getpid()
	request.ParentUserSID = userSID
	request.ParentSessionID = sessionID
	request.DeadlineUnixMS = time.Now().Add(selfUpdateApplyTimeout + selfUpdateApplyStartupTimeout).UnixMilli()
	request.Elevated = true
	request.Delegated = true
	return launchSelfUpdateApplyHelper(context.Background(), executablePath, request, true)
}

func restartSelfUpdatedApp(targetPath string) (selfUpdateRestartedProcess, error) {
	if isAdmin() {
		command := exec.Command("explorer.exe", targetPath)
		command.SysProcAttr = hiddenSysProcAttr()
		if err := command.Start(); err != nil {
			return selfUpdateRestartedProcess{}, err
		}
		if err := command.Process.Release(); err != nil {
			return selfUpdateRestartedProcess{}, err
		}
		return selfUpdateRestartedProcess{}, nil
	}
	process, err := launchSelfUpdateApplyProcess(targetPath, nil)
	if err != nil {
		return selfUpdateRestartedProcess{}, err
	}
	return selfUpdateRestartedProcess{handle: process.handle, representsTarget: true}, nil
}

func isSelfUpdatePermissionError(err error) bool {
	for err != nil {
		if errors.Is(err, os.ErrPermission) {
			return true
		}
		if errno, ok := err.(syscall.Errno); ok && errno == syscall.Errno(windows.ERROR_ACCESS_DENIED) {
			return true
		}
		err = errors.Unwrap(err)
	}
	return false
}

func isSelfUpdateSharingViolation(err error) bool {
	for err != nil {
		if errno, ok := err.(syscall.Errno); ok && errno == syscall.Errno(windows.ERROR_SHARING_VIOLATION) {
			return true
		}
		err = errors.Unwrap(err)
	}
	return false
}

func selfUpdateApplyRequestFromOptions(options cliOptions) selfUpdateApplyRequest {
	return selfUpdateApplyRequest{
		SourcePath:     executablePathOrEmpty(),
		TargetPath:     options.SelfUpdateTarget,
		ExpectedSHA256: strings.ToLower(strings.TrimSpace(options.SelfUpdateSHA256)),
		ParentPID:      options.SelfUpdateParentPID,
		Restart:        options.SelfUpdateRestart,
		Elevated:       options.SelfUpdateElevated,
	}
}

func executablePathOrEmpty() string {
	executablePath, err := os.Executable()
	if err != nil {
		return ""
	}
	return executablePath
}

func parseSelfUpdateParentPID(raw string) (int, error) {
	if strings.TrimSpace(raw) == "" {
		return 0, errors.New("self-update parent PID is required")
	}
	pid, err := strconv.Atoi(raw)
	if err != nil || pid <= 0 {
		return 0, fmt.Errorf("invalid self-update parent PID %q", raw)
	}
	return pid, nil
}
