//go:build windows

package updater

import (
	"encoding/binary"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"unicode/utf16"

	"golang.org/x/sys/windows"
)

// machineFolders are the machine-wide folders that admin-rights code may
// trust. They come from the OS (the known-folder API, which reads them from
// HKLM, and GetSystemWindowsDirectory), never from environment variables: the
// user who starts an elevated run controls its environment.
type machineFolders struct {
	ProgramFiles    string
	ProgramFilesX86 string
	ProgramData     string
	Windows         string
}

// machineFoldersProvider is replaced in tests.
var machineFoldersProvider = sync.OnceValue(systemMachineFolders)

func trustedMachineFolders() machineFolders {
	return machineFoldersProvider()
}

func systemMachineFolders() machineFolders {
	folders := machineFolders{
		ProgramFiles:    knownFolderOr(windows.FOLDERID_ProgramFiles, `C:\Program Files`),
		ProgramFilesX86: knownFolderOr(windows.FOLDERID_ProgramFilesX86, `C:\Program Files (x86)`),
		ProgramData:     knownFolderOr(windows.FOLDERID_ProgramData, `C:\ProgramData`),
		Windows:         `C:\Windows`,
	}
	if directory, err := windows.GetSystemWindowsDirectory(); err == nil && strings.TrimSpace(directory) != "" {
		folders.Windows = filepath.Clean(directory)
	}
	return folders
}

func knownFolderOr(id *windows.KNOWNFOLDERID, fallback string) string {
	path, err := windows.KnownFolderPath(id, windows.KF_FLAG_DEFAULT)
	if err != nil || strings.TrimSpace(path) == "" {
		return fallback
	}
	return filepath.Clean(path)
}

// currentUserLocalAppDataProvider returns the running token's Local AppData.
// The profile folder comes from GetUserProfileDirectory, which reads the
// machine-wide ProfileList. The shell's FOLDERID_LocalAppData is not used: it
// expands %USERPROFILE% from the environment and caches the result.
// Replaced in tests.
var currentUserLocalAppDataProvider = func() (string, error) {
	// A pseudo-handle with TOKEN_QUERY access; it needs no Close.
	profile, err := windows.GetCurrentProcessToken().GetUserProfileDirectory()
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(profile) == "" {
		return "", errors.New("the user has no profile directory")
	}
	return filepath.Join(filepath.Clean(profile), "AppData", "Local"), nil
}

// Package families whose App Execution Alias PatchBoard runs in hardened mode.
// store.exe only works through its alias: started directly from its package
// folder it fails with "the process has no package identity" (0x80073D54).
var trustedManagerAliasPackageFamilies = map[string]string{
	"winget.exe": "Microsoft.DesktopAppInstaller_8wekyb3d8bbwe",
	"store.exe":  "Microsoft.WindowsStore_8wekyb3d8bbwe",
}

// hardenedAliasManagerExecutablePath returns the user's WindowsApps alias for
// winget or store only when it is a genuine App Execution Alias of the
// expected Microsoft package whose target lies in Program Files\WindowsApps.
// Anyone with the user's rights can drop a winget.exe into that folder; a
// plain file or an alias for another package is rejected.
func hardenedAliasManagerExecutablePath(executableFileName string) string {
	packageFamily, ok := trustedManagerAliasPackageFamilies[strings.ToLower(executableFileName)]
	if !ok {
		return ""
	}
	localAppData, err := currentUserLocalAppDataProvider()
	if err != nil || strings.TrimSpace(localAppData) == "" {
		return ""
	}
	aliasPath := filepath.Join(localAppData, "Microsoft", "WindowsApps", executableFileName)
	if err := validateManagerAppExecutionAlias(aliasPath, packageFamily, trustedMachineFolders()); err != nil {
		appLog("Ignoring %s: %v", aliasPath, err)
		return ""
	}
	return aliasPath
}

// readAppExecutionAliasForPath is replaced in tests.
var readAppExecutionAliasForPath = readAppExecutionAlias

func validateManagerAppExecutionAlias(aliasPath, packageFamily string, folders machineFolders) error {
	alias, err := readAppExecutionAliasForPath(aliasPath)
	if err != nil {
		return err
	}
	if !strings.EqualFold(alias.PackageFamilyName, packageFamily) {
		return fmt.Errorf("alias belongs to package family %q, expected %q", alias.PackageFamilyName, packageFamily)
	}
	windowsApps := filepath.Join(folders.ProgramFiles, "WindowsApps")
	if !pathWithinAnyRoot(alias.TargetPath, []string{windowsApps}) {
		return fmt.Errorf("alias target %q is outside %s", alias.TargetPath, windowsApps)
	}
	relative, err := filepath.Rel(filepath.Clean(windowsApps), filepath.Clean(alias.TargetPath))
	if err != nil {
		return err
	}
	packageFolder := strings.SplitN(relative, string(filepath.Separator), 2)[0]
	name, publisherID, ok := strings.Cut(packageFamily, "_")
	if !ok {
		return fmt.Errorf("invalid package family %q", packageFamily)
	}
	lowerFolder := strings.ToLower(packageFolder)
	if !strings.HasPrefix(lowerFolder, strings.ToLower(name)+"_") || !strings.HasSuffix(lowerFolder, "__"+strings.ToLower(publisherID)) {
		return fmt.Errorf("alias target folder %q does not belong to %s", packageFolder, packageFamily)
	}
	return nil
}

const ioReparseTagAppExecLink = 0x8000001B

type appExecutionAlias struct {
	PackageFamilyName string
	AppUserModelID    string
	TargetPath        string
}

func readAppExecutionAlias(path string) (appExecutionAlias, error) {
	pathPointer, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return appExecutionAlias{}, err
	}
	handle, err := windows.CreateFile(pathPointer, 0,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING,
		windows.FILE_FLAG_OPEN_REPARSE_POINT|windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return appExecutionAlias{}, err
	}
	defer windows.CloseHandle(handle)
	buffer := make([]byte, windows.MAXIMUM_REPARSE_DATA_BUFFER_SIZE)
	var returned uint32
	if err := windows.DeviceIoControl(handle, windows.FSCTL_GET_REPARSE_POINT, nil, 0, &buffer[0], uint32(len(buffer)), &returned, nil); err != nil {
		return appExecutionAlias{}, fmt.Errorf("not an app execution alias: %w", err)
	}
	return parseAppExecutionAlias(buffer[:returned])
}

// parseAppExecutionAlias decodes an IO_REPARSE_TAG_APPEXECLINK buffer: the
// reparse header, a version (3), then NUL-terminated UTF-16 strings for the
// package family, the app user model ID and the target executable.
func parseAppExecutionAlias(data []byte) (appExecutionAlias, error) {
	if len(data) < 12 {
		return appExecutionAlias{}, errors.New("reparse data too short")
	}
	if tag := binary.LittleEndian.Uint32(data[0:4]); tag != ioReparseTagAppExecLink {
		return appExecutionAlias{}, fmt.Errorf("reparse tag %#x is not an app execution alias", tag)
	}
	payload := data[8:]
	if length := int(binary.LittleEndian.Uint16(data[4:6])); length < len(payload) {
		payload = payload[:length]
	}
	if len(payload) < 4 || binary.LittleEndian.Uint32(payload[0:4]) != 3 {
		return appExecutionAlias{}, errors.New("unsupported app execution alias version")
	}
	units := make([]uint16, (len(payload)-4)/2)
	for index := range units {
		units[index] = binary.LittleEndian.Uint16(payload[4+2*index:])
	}
	var fields []string
	start := 0
	for index, unit := range units {
		if unit == 0 {
			fields = append(fields, string(utf16.Decode(units[start:index])))
			start = index + 1
		}
	}
	if len(fields) < 3 || fields[0] == "" || fields[2] == "" {
		return appExecutionAlias{}, errors.New("app execution alias is incomplete")
	}
	return appExecutionAlias{PackageFamilyName: fields[0], AppUserModelID: fields[1], TargetPath: fields[2]}, nil
}
