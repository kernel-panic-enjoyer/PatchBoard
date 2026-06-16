package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

const createNoWindow = 0x08000000

func hiddenSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
}

func launchEnv() []string {
	env := os.Environ()
	path := os.Getenv("PATH")
	additions := []string{}
	if local := os.Getenv("LOCALAPPDATA"); local != "" {
		additions = append(additions,
			filepath.Join(local, "Microsoft", "WindowsApps"),
			filepath.Join(local, "Microsoft", "WinGet", "Links"),
		)
	}
	for _, addition := range additions {
		if _, err := os.Stat(addition); err == nil && !strings.Contains(strings.ToLower(path), strings.ToLower(addition)) {
			path = addition + string(os.PathListSeparator) + path
		}
	}
	env = append(env, "PATH="+path)
	return env
}

func resolveExecutable(name string) string {
	if override := os.Getenv("UPDATER_" + strings.ToUpper(name) + "_PATH"); override != "" {
		return override
	}
	if found, err := exec.LookPath(name); err == nil {
		return found
	}
	if strings.EqualFold(name, "winget") || strings.EqualFold(name, "store") {
		exeName := name
		if !strings.HasSuffix(strings.ToLower(exeName), ".exe") {
			exeName += ".exe"
		}
		var candidates []string
		if root := os.Getenv("SystemRoot"); root != "" {
			candidates = append(candidates, filepath.Join(root, "System32", exeName), filepath.Join(root, "Sysnative", exeName))
		}
		for _, env := range []string{"LOCALAPPDATA", "USERPROFILE"} {
			value := os.Getenv(env)
			if value == "" {
				continue
			}
			base := value
			if env == "USERPROFILE" {
				base = filepath.Join(value, "AppData", "Local")
			}
			candidates = append(candidates,
				filepath.Join(base, "Microsoft", "WindowsApps", exeName),
				filepath.Join(base, "Microsoft", "WinGet", "Links", exeName),
			)
		}
		for _, candidate := range candidates {
			if _, err := os.Stat(candidate); err == nil {
				return candidate
			}
		}
	}
	return name
}

func managerCommand(manager string, args ...string) []string {
	resolved := resolveExecutable(manager)
	if resolved != manager {
		return append([]string{resolved}, args...)
	}
	if manager == "winget" || manager == "store" {
		return append([]string{"cmd.exe", "/d", "/c", manager}, args...)
	}
	return append([]string{manager}, args...)
}
