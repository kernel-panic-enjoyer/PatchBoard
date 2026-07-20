package updater

import (
	"fmt"
	"os"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

func isAdmin() bool {
	shell32 := syscall.NewLazyDLL("shell32.dll")
	proc := shell32.NewProc("IsUserAnAdmin")
	ret, _, _ := proc.Call()
	return ret != 0
}

type shellExecuteInfo struct {
	cbSize       uint32
	fMask        uint32
	hwnd         uintptr
	lpVerb       *uint16
	lpFile       *uint16
	lpParameters *uint16
	lpDirectory  *uint16
	nShow        int32
	hInstApp     uintptr
	lpIDList     uintptr
	lpClass      *uint16
	hkeyClass    uintptr
	dwHotKey     uint32
	hIcon        uintptr
	hProcess     windows.Handle
}

const seeMaskNoCloseProcess = 0x00000040

func shellExecuteRunasProcess(file string, params string) (windows.Handle, error) {
	shell32 := syscall.NewLazyDLL("shell32.dll")
	proc := shell32.NewProc("ShellExecuteExW")
	verb, _ := syscall.UTF16PtrFromString("runas")
	target, _ := syscall.UTF16PtrFromString(file)
	parameters, _ := syscall.UTF16PtrFromString(params)
	dir, _ := syscall.UTF16PtrFromString(appRoot())
	info := shellExecuteInfo{
		cbSize:       uint32(unsafe.Sizeof(shellExecuteInfo{})),
		fMask:        seeMaskNoCloseProcess,
		lpVerb:       verb,
		lpFile:       target,
		lpParameters: parameters,
		lpDirectory:  dir,
		nShow:        0,
	}
	ret, _, err := proc.Call(uintptr(unsafe.Pointer(&info)))
	if ret == 0 {
		if err != syscall.Errno(0) {
			return 0, err
		}
		return 0, fmt.Errorf("ShellExecuteExW failed")
	}
	return info.hProcess, nil
}

func quoteArg(arg string) string {
	return syscall.EscapeArg(arg)
}

func currentUserSID() (string, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return "", err
	}
	if user == nil || user.User.Sid == nil {
		return "", fmt.Errorf("current process token has no user SID")
	}
	return user.User.Sid.String(), nil
}

func currentSessionID() (uint32, error) {
	var sessionID uint32
	if err := windows.ProcessIdToSessionId(windows.GetCurrentProcessId(), &sessionID); err != nil {
		return 0, err
	}
	return sessionID, nil
}

// detectElevatedPackageUpdateBatchCapability determines whether one UAC
// consent prompt can produce a worker token for the same interactive user.
// A standard-user credential prompt may switch SIDs, which is acceptable for
// Chocolatey but must never be used for current-user WinGet operations.
func detectElevatedPackageUpdateBatchCapability() elevatedPackageUpdateBatchCapability {
	currentToken := windows.GetCurrentProcessToken()
	if currentToken.IsElevated() {
		return elevatedPackageUpdateBatchCapability{SameUserElevationAvailable: true}
	}

	capability := elevatedPackageUpdateBatchCapability{RequiresElevation: true}
	linkedToken, err := currentToken.GetLinkedToken()
	if err != nil {
		return capability
	}
	defer linkedToken.Close()

	currentSID, err := tokenUserSID(currentToken)
	if err != nil {
		return capability
	}
	linkedSID, err := tokenUserSID(linkedToken)
	if err != nil || !strings.EqualFold(currentSID, linkedSID) {
		return capability
	}
	currentTokenSession, err := tokenSessionID(currentToken)
	if err != nil {
		return capability
	}
	linkedTokenSession, err := tokenSessionID(linkedToken)
	if err != nil || currentTokenSession != linkedTokenSession {
		return capability
	}
	capability.SameUserElevationAvailable = true
	return capability
}

func tokenUserSID(token windows.Token) (string, error) {
	user, err := token.GetTokenUser()
	if err != nil {
		return "", err
	}
	if user == nil || user.User.Sid == nil {
		return "", fmt.Errorf("token has no user SID")
	}
	return user.User.Sid.String(), nil
}

func tokenSessionID(token windows.Token) (uint32, error) {
	var sessionID uint32
	var returnedLength uint32
	if err := windows.GetTokenInformation(
		token,
		windows.TokenSessionId,
		(*byte)(unsafe.Pointer(&sessionID)),
		uint32(unsafe.Sizeof(sessionID)),
		&returnedLength,
	); err != nil {
		return 0, err
	}
	if returnedLength != uint32(unsafe.Sizeof(sessionID)) {
		return 0, fmt.Errorf("unexpected token session ID length %d", returnedLength)
	}
	return sessionID, nil
}

func launchElevatedWorkerProcess(pipeName, capability, userSID string, sessionID uint32, jobName, cancelEventName string) (elevatedWorkerProcess, error) {
	exe, err := osExecutable()
	if err != nil {
		return elevatedWorkerProcess{}, err
	}
	args := []string{
		"--elevated-worker",
		"--worker-pipe=" + pipeName,
		"--worker-capability=" + capability,
		"--worker-user-sid=" + userSID,
		fmt.Sprintf("--worker-session-id=%d", sessionID),
		"--worker-job=" + jobName,
		"--worker-cancel-event=" + cancelEventName,
	}
	handle, err := shellExecuteRunasProcess(exe, strings.Join(quoteArgs(args), " "))
	if err != nil {
		return elevatedWorkerProcess{}, err
	}
	return elevatedWorkerProcess{handle: handle}, nil
}

func osExecutable() (string, error) {
	return os.Executable()
}

func quoteArgs(args []string) []string {
	quoted := make([]string, 0, len(args))
	for _, arg := range args {
		quoted = append(quoted, quoteArg(arg))
	}
	return quoted
}
