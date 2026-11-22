//go:build windows

package updater

import (
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"
)

func setTrustedMachineFoldersForTest(t *testing.T, folders machineFolders) {
	t.Helper()
	original := machineFoldersProvider
	machineFoldersProvider = func() machineFolders { return folders }
	t.Cleanup(func() { machineFoldersProvider = original })
}

func setCurrentUserLocalAppDataForTest(t *testing.T, path string) {
	t.Helper()
	original := currentUserLocalAppDataProvider
	currentUserLocalAppDataProvider = func() (string, error) { return path, nil }
	t.Cleanup(func() { currentUserLocalAppDataProvider = original })
}

func setAppExecutionAliasReaderForTest(t *testing.T, aliases map[string]appExecutionAlias) {
	t.Helper()
	original := readAppExecutionAliasForPath
	readAppExecutionAliasForPath = func(path string) (appExecutionAlias, error) {
		if alias, ok := aliases[strings.ToLower(path)]; ok {
			return alias, nil
		}
		return appExecutionAlias{}, errors.New("not an app execution alias")
	}
	t.Cleanup(func() { readAppExecutionAliasForPath = original })
}

func attackerEnvironmentForTest(t *testing.T) string {
	t.Helper()
	attacker := t.TempDir()
	for _, name := range []string{"ProgramFiles", "ProgramFiles(x86)", "ProgramData", "SystemRoot", "windir", "LOCALAPPDATA", "USERPROFILE", "APPDATA"} {
		t.Setenv(name, filepath.Join(attacker, name))
	}
	return attacker
}

func TestSystemMachineFoldersIgnoreEnvironment(t *testing.T) {
	attacker := attackerEnvironmentForTest(t)
	folders := systemMachineFolders()
	for name, path := range map[string]string{
		"ProgramFiles": folders.ProgramFiles, "ProgramFilesX86": folders.ProgramFilesX86,
		"ProgramData": folders.ProgramData, "Windows": folders.Windows,
	} {
		if path == "" || strings.HasPrefix(strings.ToLower(path), strings.ToLower(attacker)) {
			t.Fatalf("%s = %q follows the environment", name, path)
		}
	}
}

func TestTrustedPathsIgnoreEnvironment(t *testing.T) {
	attacker := attackerEnvironmentForTest(t)
	trusted := t.TempDir()
	setTrustedMachineFoldersForTest(t, machineFolders{
		ProgramFiles:    filepath.Join(trusted, "Program Files"),
		ProgramFilesX86: filepath.Join(trusted, "Program Files (x86)"),
		ProgramData:     filepath.Join(trusted, "ProgramData"),
		Windows:         filepath.Join(trusted, "Windows"),
	})
	paths := defaultApplicationInstallPaths()
	for _, path := range []string{paths.InstallDirectory, paths.TargetPath, paths.StartMenuShortcut} {
		if !strings.HasPrefix(path, trusted) {
			t.Fatalf("install path %q is not under the trusted folders", path)
		}
	}
	for _, root := range trustedAutoUpdateTaskRoots() {
		if strings.HasPrefix(strings.ToLower(root), strings.ToLower(attacker)) {
			t.Fatalf("auto-update task root %q follows the environment", root)
		}
	}
	if err := validateAutoUpdateTaskExecutable(filepath.Join(attacker, "ProgramFiles", "PatchBoard", "PatchBoard.exe")); err == nil {
		t.Fatal("auto-update task accepted an executable under an environment-supplied Program Files")
	}
}

func TestHardenedStateDirIgnoresEnvironment(t *testing.T) {
	attacker := attackerEnvironmentForTest(t)
	setProcessExecutionModeForTest(t, processModeElevatedWorker)
	trusted := t.TempDir()
	setCurrentUserLocalAppDataForTest(t, filepath.Join(trusted, "LocalAppData"))
	setTrustedMachineFoldersForTest(t, machineFolders{ProgramData: filepath.Join(trusted, "ProgramData")})
	candidates := stateDirCandidates()
	if len(candidates) == 0 || candidates[0] != filepath.Join(trusted, "LocalAppData", appDirName) {
		t.Fatalf("unexpected hardened state candidates %#v", candidates)
	}
	for _, candidate := range candidates {
		if strings.HasPrefix(strings.ToLower(candidate), strings.ToLower(attacker)) {
			t.Fatalf("hardened state candidate %q follows the environment", candidate)
		}
	}
}

func TestHardenedManagerResolutionAcceptsOnlyGenuineAliases(t *testing.T) {
	attackerEnvironmentForTest(t)
	setProcessExecutionModeForTest(t, processModeElevatedWorker)
	trusted := t.TempDir()
	localAppData := filepath.Join(trusted, "LocalAppData")
	windowsApps := filepath.Join(localAppData, "Microsoft", "WindowsApps")
	if err := os.MkdirAll(windowsApps, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"winget.exe", "store.exe"} {
		if err := os.WriteFile(filepath.Join(windowsApps, name), []byte("fake"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	programFiles := filepath.Join(trusted, "Program Files")
	setTrustedMachineFoldersForTest(t, machineFolders{ProgramFiles: programFiles, Windows: filepath.Join(trusted, "Windows")})
	setCurrentUserLocalAppDataForTest(t, localAppData)
	wingetAlias := filepath.Join(windowsApps, "winget.exe")
	storeAlias := filepath.Join(windowsApps, "store.exe")
	setAppExecutionAliasReaderForTest(t, map[string]appExecutionAlias{
		strings.ToLower(wingetAlias): {
			PackageFamilyName: "Microsoft.DesktopAppInstaller_8wekyb3d8bbwe",
			TargetPath:        filepath.Join(programFiles, "WindowsApps", "Microsoft.DesktopAppInstaller_1.29.380.0_x64__8wekyb3d8bbwe", "winget.exe"),
		},
		// An alias that claims the Store family but points into another package.
		strings.ToLower(storeAlias): {
			PackageFamilyName: "Microsoft.WindowsStore_8wekyb3d8bbwe",
			TargetPath:        filepath.Join(programFiles, "WindowsApps", "Contoso.Evil_1.0.0.0_x64__abcdefghijklm", "store.exe"),
		},
	})
	if got := managerCommand(managerWinget, "--version"); got[0] != wingetAlias {
		t.Fatalf("winget resolved to %#v, want the verified alias", got)
	}
	if got := managerCommand(managerStore, "--version"); !strings.Contains(strings.ToLower(got[0]), "patchboard-store-not-resolved.exe") {
		t.Fatalf("store resolved to %#v despite a foreign alias target", got)
	}
}

func TestValidateManagerAppExecutionAliasRejectsForeignAliases(t *testing.T) {
	folders := machineFolders{ProgramFiles: `C:\Program Files`}
	family := "Microsoft.DesktopAppInstaller_8wekyb3d8bbwe"
	for _, tc := range []struct {
		name  string
		alias appExecutionAlias
		ok    bool
	}{
		{"genuine", appExecutionAlias{family, "", `C:\Program Files\WindowsApps\Microsoft.DesktopAppInstaller_1.29.380.0_x64__8wekyb3d8bbwe\winget.exe`}, true},
		{"other family", appExecutionAlias{"Contoso.App_abcdefghijklm", "", `C:\Program Files\WindowsApps\Microsoft.DesktopAppInstaller_1.29.380.0_x64__8wekyb3d8bbwe\winget.exe`}, false},
		{"target outside WindowsApps", appExecutionAlias{family, "", `C:\Users\Public\winget.exe`}, false},
		{"target escapes WindowsApps", appExecutionAlias{family, "", `C:\Program Files\WindowsApps\..\Evil\winget.exe`}, false},
		{"other package folder", appExecutionAlias{family, "", `C:\Program Files\WindowsApps\Contoso.App_1.0.0.0_x64__abcdefghijklm\winget.exe`}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			original := readAppExecutionAliasForPath
			readAppExecutionAliasForPath = func(string) (appExecutionAlias, error) { return tc.alias, nil }
			defer func() { readAppExecutionAliasForPath = original }()
			err := validateManagerAppExecutionAlias(`C:\alias\winget.exe`, family, folders)
			if (err == nil) != tc.ok {
				t.Fatalf("err = %v, want ok=%t", err, tc.ok)
			}
		})
	}
}

func TestParseAppExecutionAlias(t *testing.T) {
	var payload []byte
	payload = binary.LittleEndian.AppendUint32(payload, 3)
	for _, field := range []string{"Microsoft.DesktopAppInstaller_8wekyb3d8bbwe", "Microsoft.DesktopAppInstaller_8wekyb3d8bbwe!winget", `C:\Program Files\WindowsApps\x\winget.exe`, "0"} {
		for _, unit := range utf16.Encode([]rune(field)) {
			payload = binary.LittleEndian.AppendUint16(payload, unit)
		}
		payload = binary.LittleEndian.AppendUint16(payload, 0)
	}
	var data []byte
	data = binary.LittleEndian.AppendUint32(data, ioReparseTagAppExecLink)
	data = binary.LittleEndian.AppendUint16(data, uint16(len(payload)))
	data = binary.LittleEndian.AppendUint16(data, 0)
	data = append(data, payload...)
	alias, err := parseAppExecutionAlias(data)
	if err != nil {
		t.Fatal(err)
	}
	if alias.PackageFamilyName != "Microsoft.DesktopAppInstaller_8wekyb3d8bbwe" || alias.TargetPath != `C:\Program Files\WindowsApps\x\winget.exe` {
		t.Fatalf("unexpected alias %#v", alias)
	}
	binary.LittleEndian.PutUint32(data[0:4], 0xA000000C) // symbolic link
	if _, err := parseAppExecutionAlias(data); err == nil {
		t.Fatal("accepted a symbolic link as an app execution alias")
	}
	if _, err := parseAppExecutionAlias(data[:6]); err == nil {
		t.Fatal("accepted truncated reparse data")
	}
}

// TestInstalledWingetAliasIsAccepted checks the real alias on machines that
// have App Installer, so a format change in Windows shows up here first.
func TestInstalledWingetAliasIsAccepted(t *testing.T) {
	attacker := attackerEnvironmentForTest(t)
	localAppData, err := currentUserLocalAppDataProvider()
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(strings.ToLower(localAppData), strings.ToLower(attacker)) {
		t.Fatalf("Local AppData %q follows the environment", localAppData)
	}
	for executable, family := range trustedManagerAliasPackageFamilies {
		aliasPath := filepath.Join(localAppData, "Microsoft", "WindowsApps", executable)
		if _, statErr := os.Lstat(aliasPath); statErr != nil {
			t.Logf("%s not installed", aliasPath)
			continue
		}
		if err := validateManagerAppExecutionAlias(aliasPath, family, systemMachineFolders()); err != nil {
			t.Fatalf("installed %s alias rejected: %v", executable, err)
		}
	}
}
