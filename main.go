package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

const (
	appName               = "Windows Updater WebUI"
	appDirName            = "WindowsUpdaterWebUI"
	defaultHost           = "127.0.0.1"
	defaultPort           = 4183
	taskStartup           = "WindowsUpdaterWebUI-Startup"
	taskAutoUpdate        = "WindowsUpdaterWebUI-AutoUpdate"
	defaultAutoUpdateTime = "03:00"
)

type CommandResult struct {
	OK      bool   `json:"ok"`
	Code    int    `json:"code"`
	Stdout  string `json:"stdout"`
	Stderr  string `json:"stderr"`
	Command string `json:"command"`
}

type ManagerStatus struct {
	Available bool   `json:"available"`
	Version   string `json:"version,omitempty"`
	Path      string `json:"path,omitempty"`
	Error     string `json:"error,omitempty"`
}

type Package struct {
	Key              string `json:"key"`
	Manager          string `json:"manager"`
	ID               string `json:"id"`
	Name             string `json:"name"`
	Version          string `json:"version"`
	AvailableVersion string `json:"available_version"`
	UpdateAvailable  bool   `json:"update_available"`
	Installed        bool   `json:"installed"`
	AutoUpdate       bool   `json:"auto_update"`
	Source           string `json:"source,omitempty"`
	Match            string `json:"match,omitempty"`
}

type ScannedApp struct {
	Key             string `json:"key"`
	Name            string `json:"name"`
	Version         string `json:"version"`
	Publisher       string `json:"publisher"`
	InstallLocation string `json:"install_location"`
	Source          string `json:"source"`
	RegistryHive    string `json:"registry_hive,omitempty"`
	Manager         string `json:"manager,omitempty"`
	PackageID       string `json:"package_id,omitempty"`
	FirstSeen       string `json:"first_seen"`
}

type State struct {
	CreatedAt             string                `json:"created_at"`
	UpdatedAt             string                `json:"updated_at"`
	AutoUpdateGlobal      bool                  `json:"auto_update_global"`
	AutoUpdatePackages    map[string]bool       `json:"auto_update_packages"`
	RegistryApps          map[string]ScannedApp `json:"registry_apps"`
	WingetApps            map[string]ScannedApp `json:"winget_apps"`
	LastScanAt            string                `json:"last_scan_at"`
	LastAutoUpdateAt      string                `json:"last_auto_update_at"`
	LastAutoUpdateResults []UpdateResult        `json:"last_auto_update_results"`
	Theme                 string                `json:"theme"`
}

type Inventory struct {
	Packages       []Package                `json:"packages"`
	Managers       map[string]ManagerStatus `json:"managers"`
	CommandResults map[string]CommandResult `json:"command_results"`
	Scan           map[string]any           `json:"scan"`
}

type ScanResult struct {
	LastScanAt      string              `json:"last_scan_at"`
	Baseline        bool                `json:"baseline"`
	Baselines       map[string]bool     `json:"baselines"`
	NewApps         []ScannedApp        `json:"new_apps"`
	RemovedApps     []ScannedApp        `json:"removed_apps"`
	TrackedCount    int                 `json:"tracked_count"`
	SourceCounts    map[string]int      `json:"source_counts"`
	WingetAvailable bool                `json:"winget_available"`
	WingetResult    *CommandResult      `json:"winget_result,omitempty"`
	Errors          []map[string]string `json:"errors"`
}

type UpdateResult struct {
	Key    string        `json:"key"`
	Result CommandResult `json:"result"`
}

type PageData struct {
	Token           string
	URLToken        string
	Admin           bool
	StateDir        string
	Managers        map[string]ManagerStatus
	Packages        []Package
	SearchQuery     string
	SearchResults   []Package
	Scan            *ScanResult
	Message         string
	CommandResult   *CommandResult
	ActionResults   []UpdateResult
	StartupEnabled  bool
	AutoTaskEnabled bool
	Settings        State
	Theme           string
}

func utcNow() string {
	return time.Now().UTC().Truncate(time.Second).Format(time.RFC3339)
}

func appRoot() string {
	exe, err := os.Executable()
	if err != nil {
		wd, _ := os.Getwd()
		return wd
	}
	return filepath.Dir(exe)
}

func stateDir() (string, error) {
	if override := os.Getenv("UPDATER_STATE_DIR"); override != "" {
		if err := os.MkdirAll(override, 0o755); err != nil {
			return "", err
		}
		return override, nil
	}

	var candidates []string
	for _, env := range []string{"LOCALAPPDATA", "APPDATA", "USERPROFILE", "ProgramData"} {
		if value := os.Getenv(env); value != "" {
			candidates = append(candidates, filepath.Join(value, appDirName))
		}
	}
	candidates = append(candidates, filepath.Join(appRoot(), ".state"))

	for _, candidate := range candidates {
		if err := os.MkdirAll(candidate, 0o755); err == nil {
			return candidate, nil
		}
	}
	return "", errors.New("could not create a state directory")
}

func defaultState() State {
	now := utcNow()
	return State{
		CreatedAt:          now,
		UpdatedAt:          now,
		AutoUpdatePackages: map[string]bool{},
		RegistryApps:       map[string]ScannedApp{},
		WingetApps:         map[string]ScannedApp{},
		Theme:              "dark",
	}
}

func loadState() State {
	state := defaultState()
	dir, err := stateDir()
	if err != nil {
		return state
	}
	data, err := os.ReadFile(filepath.Join(dir, "state.json"))
	if err != nil {
		return state
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return defaultState()
	}
	if state.AutoUpdatePackages == nil {
		state.AutoUpdatePackages = map[string]bool{}
	}
	if state.RegistryApps == nil {
		state.RegistryApps = map[string]ScannedApp{}
	}
	if state.WingetApps == nil {
		state.WingetApps = map[string]ScannedApp{}
	}
	if state.Theme == "" {
		state.Theme = "dark"
	}
	return state
}

func saveState(state State) error {
	state.UpdatedAt = utcNow()
	dir, err := stateDir()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(dir, "state.tmp")
	path := filepath.Join(dir, "state.json")
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func isAdmin() bool {
	shell32 := syscall.NewLazyDLL("shell32.dll")
	proc := shell32.NewProc("IsUserAnAdmin")
	ret, _, _ := proc.Call()
	return ret != 0
}

func shellExecuteRunas(file string, params string) error {
	shell32 := syscall.NewLazyDLL("shell32.dll")
	proc := shell32.NewProc("ShellExecuteW")
	verb, _ := syscall.UTF16PtrFromString("runas")
	target, _ := syscall.UTF16PtrFromString(file)
	parameters, _ := syscall.UTF16PtrFromString(params)
	dir, _ := syscall.UTF16PtrFromString(appRoot())
	ret, _, err := proc.Call(0, uintptr(unsafe.Pointer(verb)), uintptr(unsafe.Pointer(target)), uintptr(unsafe.Pointer(parameters)), uintptr(unsafe.Pointer(dir)), 0)
	if ret <= 32 {
		return err
	}
	return nil
}

func quoteArg(arg string) string {
	return syscall.EscapeArg(arg)
}

func runCommand(timeout time.Duration, args ...string) CommandResult {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	result := CommandResult{Command: strings.Join(args, " ")}
	if len(args) == 0 {
		result.Stderr = "empty command"
		result.Code = 127
		return result
	}

	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Env = launchEnv()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	result.Stdout = stdout.String()
	result.Stderr = stderr.String()
	if ctx.Err() == context.DeadlineExceeded {
		result.Code = 124
		result.Stderr += "\nTimed out."
		return result
	}
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.Code = exitErr.ExitCode()
		} else {
			result.Code = 127
			if result.Stderr == "" {
				result.Stderr = err.Error()
			}
		}
		return result
	}
	result.OK = true
	return result
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
	if strings.EqualFold(name, "winget") {
		var candidates []string
		if root := os.Getenv("SystemRoot"); root != "" {
			candidates = append(candidates, filepath.Join(root, "System32", "winget.exe"), filepath.Join(root, "Sysnative", "winget.exe"))
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
				filepath.Join(base, "Microsoft", "WindowsApps", "winget.exe"),
				filepath.Join(base, "Microsoft", "WinGet", "Links", "winget.exe"),
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
	if manager == "winget" {
		return append([]string{"cmd.exe", "/d", "/c", "winget"}, args...)
	}
	return append([]string{manager}, args...)
}

func detectManager(manager string) ManagerStatus {
	result := runCommand(20*time.Second, managerCommand(manager, "--version")...)
	output := strings.TrimSpace(result.Stdout)
	if output == "" {
		output = strings.TrimSpace(result.Stderr)
	}
	status := ManagerStatus{Available: result.OK}
	if result.OK {
		lines := strings.Split(output, "\n")
		status.Version = strings.TrimSpace(lines[0])
		status.Path = resolveExecutable(manager)
	} else {
		status.Error = strings.TrimSpace(result.Stderr + result.Stdout)
	}
	return status
}

func detectManagers() map[string]ManagerStatus {
	return map[string]ManagerStatus{
		"winget": detectManager("winget"),
		"choco":  detectManager("choco"),
	}
}

func parseChocoList(output string) []Package {
	var packages []Package
	for _, raw := range strings.Split(output, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || !strings.Contains(line, "|") {
			continue
		}
		parts := strings.Split(line, "|")
		if len(parts) < 2 {
			continue
		}
		id := strings.TrimSpace(parts[0])
		version := strings.TrimSpace(parts[1])
		if id == "" || version == "" || strings.Contains(id, " ") || strings.HasPrefix(strings.ToLower(id), "this is try") {
			continue
		}
		packages = append(packages, Package{ID: id, Name: id, Version: version, Manager: "choco"})
	}
	return packages
}

func parseChocoOutdated(output string) map[string]string {
	updates := map[string]string{}
	for _, raw := range strings.Split(output, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || !strings.Contains(line, "|") {
			continue
		}
		parts := strings.Split(line, "|")
		if len(parts) >= 3 {
			id := strings.ToLower(strings.TrimSpace(parts[0]))
			available := strings.TrimSpace(parts[2])
			if id != "" && available != "" {
				updates[id] = available
			}
		}
	}
	return updates
}

func splitColumns(line string) []string {
	return regexp.MustCompile(`\s{2,}`).Split(strings.TrimSpace(line), -1)
}

func isSourceToken(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	return value == "winget" || value == "msstore"
}

func parseWingetTable(output string) []Package {
	lines := strings.Split(output, "\n")
	headerSeen := false
	var packages []Package
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		if !headerSeen {
			if strings.Contains(lower, "name") && strings.Contains(lower, "id") && strings.Contains(lower, "version") {
				headerSeen = true
			}
			continue
		}
		if strings.Trim(line, "-") == "" || strings.HasPrefix(line, "-") {
			continue
		}
		cols := splitColumns(line)
		if len(cols) < 3 {
			continue
		}
		pkg := Package{Name: cols[0], ID: cols[1], Version: cols[2], Manager: "winget"}
		rest := cols[3:]
		if len(rest) > 0 {
			if isSourceToken(rest[len(rest)-1]) {
				pkg.Source = strings.ToLower(rest[len(rest)-1])
				rest = rest[:len(rest)-1]
			}
		}
		if len(rest) > 0 {
			pkg.AvailableVersion = rest[0]
			if strings.HasPrefix(strings.ToLower(rest[0]), "tag:") {
				pkg.AvailableVersion = ""
				pkg.Match = rest[0]
			}
		}
		if len(rest) > 1 {
			pkg.Match = rest[1]
		}
		packages = append(packages, pkg)
	}
	return packages
}

func parseWingetExport(output string) []Package {
	var decoded struct {
		Sources []struct {
			Packages []struct {
				PackageIdentifier string
				Version           string
			}
			SourceDetails struct {
				Name string
			}
		}
	}
	if err := json.Unmarshal([]byte(output), &decoded); err != nil {
		return nil
	}
	var packages []Package
	for _, source := range decoded.Sources {
		sourceName := source.SourceDetails.Name
		if sourceName == "" {
			sourceName = "winget"
		}
		for _, item := range source.Packages {
			id := strings.TrimSpace(item.PackageIdentifier)
			if id == "" {
				continue
			}
			packages = append(packages, Package{ID: id, Name: id, Version: item.Version, Manager: "winget", Source: sourceName})
		}
	}
	return packages
}

func isTruncatedID(id string) bool {
	return strings.Contains(id, "…") || strings.Contains(id, "...")
}

func wingetIDMatches(fullID, tableID string) bool {
	full := strings.ToLower(fullID)
	table := strings.ToLower(tableID)
	if full == table {
		return true
	}
	if strings.Contains(table, "…") {
		return strings.HasPrefix(full, strings.Split(table, "…")[0])
	}
	if strings.Contains(table, "...") {
		return strings.HasPrefix(full, strings.Split(table, "...")[0])
	}
	return false
}

func mergeWingetExportWithTable(exported, table []Package) []Package {
	used := map[int]bool{}
	var merged []Package
	for _, pkg := range exported {
		match := -1
		for i, tablePkg := range table {
			if used[i] || !wingetIDMatches(pkg.ID, tablePkg.ID) {
				continue
			}
			if pkg.Version != "" && tablePkg.Version != "" && pkg.Version != tablePkg.Version {
				continue
			}
			match = i
			break
		}
		if match >= 0 {
			used[match] = true
			tablePkg := table[match]
			pkg.Name = tablePkg.Name
			pkg.AvailableVersion = tablePkg.AvailableVersion
			if tablePkg.Source != "" {
				pkg.Source = tablePkg.Source
			}
		}
		merged = append(merged, pkg)
	}
	exportedIDs := map[string]bool{}
	for _, pkg := range exported {
		exportedIDs[strings.ToLower(pkg.ID)] = true
	}
	for i, pkg := range table {
		if used[i] || isTruncatedID(pkg.ID) || exportedIDs[strings.ToLower(pkg.ID)] {
			continue
		}
		if pkg.Source == "winget" || pkg.Source == "msstore" {
			merged = append(merged, pkg)
		}
	}
	sort.Slice(merged, func(i, j int) bool {
		return strings.ToLower(merged[i].Name) < strings.ToLower(merged[j].Name)
	})
	return merged
}

func chocoInstalled() ([]Package, CommandResult) {
	result := runCommand(90*time.Second, managerCommand("choco", "list", "--local-only", "--limit-output", "--no-color")...)
	return parseChocoList(result.Stdout + "\n" + result.Stderr), result
}

func chocoUpdates() (map[string]string, CommandResult) {
	result := runCommand(120*time.Second, managerCommand("choco", "outdated", "--limit-output", "--no-color")...)
	return parseChocoOutdated(result.Stdout + "\n" + result.Stderr), result
}

func wingetInstalled() ([]Package, CommandResult) {
	listResult := runCommand(120*time.Second, managerCommand("winget", "list", "--accept-source-agreements", "--disable-interactivity")...)
	tablePackages := parseWingetTable(listResult.Stdout + "\n" + listResult.Stderr)

	exportPath := filepath.Join(os.TempDir(), fmt.Sprintf("windows-updater-winget-%d.json", os.Getpid()))
	defer os.Remove(exportPath)
	exportResult := runCommand(180*time.Second, managerCommand("winget", "export", "-o", exportPath, "--include-versions", "--accept-source-agreements", "--disable-interactivity")...)
	exportOutput, _ := os.ReadFile(exportPath)
	exported := parseWingetExport(string(exportOutput))

	listResult.Stderr += exportResult.Stderr
	if len(exported) > 0 {
		return mergeWingetExportWithTable(exported, tablePackages), listResult
	}
	return tablePackages, listResult
}

func wingetUpdates() (map[string]string, CommandResult) {
	result := runCommand(120*time.Second, managerCommand("winget", "upgrade", "--accept-source-agreements", "--disable-interactivity")...)
	updates := map[string]string{}
	for _, pkg := range parseWingetTable(result.Stdout + "\n" + result.Stderr) {
		if pkg.AvailableVersion != "" && !isTruncatedID(pkg.ID) {
			updates[strings.ToLower(pkg.ID)] = pkg.AvailableVersion
		}
	}
	return updates, result
}

func packageKey(manager, id string) string {
	return manager + ":" + id
}

func splitPackageKey(key string) (string, string, error) {
	parts := strings.SplitN(key, ":", 2)
	if len(parts) != 2 || parts[1] == "" || (parts[0] != "winget" && parts[0] != "choco") {
		return "", "", errors.New("package key must be manager:id")
	}
	return parts[0], parts[1], nil
}

func getInventory() Inventory {
	state := loadState()
	managers := detectManagers()
	commandResults := map[string]CommandResult{}
	var packages []Package

	if managers["winget"].Available {
		installed, result := wingetInstalled()
		commandResults["winget_list"] = result
		updates, updateResult := wingetUpdates()
		commandResults["winget_upgrade"] = updateResult
		for _, pkg := range installed {
			key := packageKey("winget", pkg.ID)
			available := updates[strings.ToLower(pkg.ID)]
			if available == "" {
				available = pkg.AvailableVersion
			}
			pkg.Key = key
			pkg.Manager = "winget"
			pkg.AvailableVersion = available
			pkg.UpdateAvailable = available != ""
			pkg.Installed = true
			pkg.AutoUpdate = state.AutoUpdatePackages[key]
			packages = append(packages, pkg)
		}
	}

	if managers["choco"].Available {
		installed, result := chocoInstalled()
		commandResults["choco_list"] = result
		updates, updateResult := chocoUpdates()
		commandResults["choco_outdated"] = updateResult
		for _, pkg := range installed {
			key := packageKey("choco", pkg.ID)
			available := updates[strings.ToLower(pkg.ID)]
			pkg.Key = key
			pkg.Manager = "choco"
			pkg.AvailableVersion = available
			pkg.UpdateAvailable = available != ""
			pkg.Installed = true
			pkg.AutoUpdate = state.AutoUpdatePackages[key]
			packages = append(packages, pkg)
		}
	}

	sort.Slice(packages, func(i, j int) bool {
		if strings.EqualFold(packages[i].Name, packages[j].Name) {
			return packages[i].Manager < packages[j].Manager
		}
		return strings.ToLower(packages[i].Name) < strings.ToLower(packages[j].Name)
	})

	return Inventory{
		Packages:       packages,
		Managers:       managers,
		CommandResults: commandResults,
		Scan: map[string]any{
			"last_scan_at":   state.LastScanAt,
			"tracked_count":  len(state.RegistryApps) + len(state.WingetApps),
			"registry_count": len(state.RegistryApps),
			"winget_count":   len(state.WingetApps),
		},
	}
}

func searchPackages(query string) ([]Package, map[string]ManagerStatus, map[string]CommandResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil, nil, errors.New("search query cannot be empty")
	}
	managers := detectManagers()
	results := []Package{}
	commandResults := map[string]CommandResult{}

	if managers["winget"].Available {
		result := runCommand(90*time.Second, managerCommand("winget", "search", query, "--accept-source-agreements", "--disable-interactivity")...)
		commandResults["winget"] = result
		for _, pkg := range parseWingetTable(result.Stdout + "\n" + result.Stderr) {
			if !isTruncatedID(pkg.ID) {
				pkg.Key = packageKey("winget", pkg.ID)
				pkg.Manager = "winget"
				results = append(results, pkg)
			}
		}
	}

	if managers["choco"].Available {
		result := runCommand(90*time.Second, managerCommand("choco", "search", query, "--limit-output", "--no-color")...)
		commandResults["choco"] = result
		for _, pkg := range parseChocoList(result.Stdout + "\n" + result.Stderr) {
			pkg.Key = packageKey("choco", pkg.ID)
			results = append(results, pkg)
		}
	}

	seen := map[string]bool{}
	deduped := []Package{}
	for _, pkg := range results {
		key := strings.ToLower(packageKey(pkg.Manager, pkg.ID))
		if seen[key] {
			continue
		}
		seen[key] = true
		deduped = append(deduped, pkg)
	}
	sort.Slice(deduped, func(i, j int) bool {
		if deduped[i].Manager == deduped[j].Manager {
			return strings.ToLower(deduped[i].Name) < strings.ToLower(deduped[j].Name)
		}
		return deduped[i].Manager == "winget"
	})
	return deduped, managers, commandResults, nil
}

var packageIDPattern = regexp.MustCompile(`^[A-Za-z0-9_.+\-:]+$`)

func validateManagerAndID(manager, id string) error {
	if manager != "winget" && manager != "choco" {
		return errors.New("manager must be winget or choco")
	}
	if id == "" || !packageIDPattern.MatchString(id) {
		return errors.New("package id contains unsupported characters")
	}
	return nil
}

func installPackage(manager, id string) CommandResult {
	if err := validateManagerAndID(manager, id); err != nil {
		return CommandResult{Code: 2, Stderr: err.Error(), Command: "install"}
	}
	if manager == "winget" {
		return runCommand(3600*time.Second, managerCommand("winget", "install", "--id", id, "--exact", "--accept-package-agreements", "--accept-source-agreements", "--disable-interactivity", "--silent")...)
	}
	return runCommand(3600*time.Second, managerCommand("choco", "install", id, "-y", "--no-progress", "--no-color")...)
}

func updatePackage(manager, id string) CommandResult {
	if err := validateManagerAndID(manager, id); err != nil {
		return CommandResult{Code: 2, Stderr: err.Error(), Command: "update"}
	}
	if manager == "winget" {
		return runCommand(3600*time.Second, managerCommand("winget", "upgrade", "--id", id, "--exact", "--accept-package-agreements", "--accept-source-agreements", "--disable-interactivity", "--silent")...)
	}
	return runCommand(3600*time.Second, managerCommand("choco", "upgrade", id, "-y", "--no-progress", "--no-color")...)
}

func updateAll(packageKeys []string) []UpdateResult {
	results := []UpdateResult{}
	if len(packageKeys) > 0 {
		for _, key := range packageKeys {
			manager, id, err := splitPackageKey(key)
			if err != nil {
				results = append(results, UpdateResult{Key: key, Result: CommandResult{Code: 2, Stderr: err.Error()}})
				continue
			}
			results = append(results, UpdateResult{Key: key, Result: updatePackage(manager, id)})
		}
		return results
	}

	managers := detectManagers()
	if managers["winget"].Available {
		results = append(results, UpdateResult{Key: "winget:*", Result: runCommand(7200*time.Second, managerCommand("winget", "upgrade", "--all", "--accept-package-agreements", "--accept-source-agreements", "--disable-interactivity", "--silent")...)})
	}
	if managers["choco"].Available {
		results = append(results, UpdateResult{Key: "choco:*", Result: runCommand(7200*time.Second, managerCommand("choco", "upgrade", "all", "-y", "--no-progress", "--no-color")...)})
	}
	return results
}

func installManager(manager string) CommandResult {
	switch manager {
	case "winget":
		err := openURL("ms-appinstaller:?source=https://aka.ms/getwinget")
		if err != nil {
			return CommandResult{Code: 1, Stderr: err.Error(), Command: "open winget installer"}
		}
		return CommandResult{OK: true, Command: "open winget installer", Stdout: "Opened Microsoft App Installer for winget."}
	case "choco":
		if detectManager("winget").Available {
			return installPackage("winget", "Chocolatey.Chocolatey")
		}
		err := openURL("https://chocolatey.org/install")
		if err != nil {
			return CommandResult{Code: 1, Stderr: err.Error(), Command: "open chocolatey install page"}
		}
		return CommandResult{OK: true, Command: "open chocolatey install page", Stdout: "Opened Chocolatey install page because winget is unavailable."}
	default:
		return CommandResult{Code: 2, Stderr: "unknown manager", Command: "manager install"}
	}
}

func normalizeText(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func normalizeRegistryKey(name, publisher, location string) string {
	base := strings.ToLower(name + "|" + publisher + "|" + location)
	re := regexp.MustCompile(`[^a-z0-9]+`)
	key := strings.Trim(re.ReplaceAllString(base, "-"), "-")
	if key == "" {
		key = strings.Trim(re.ReplaceAllString(strings.ToLower(name), "-"), "-")
	}
	return key
}

func parseRegQuery(output, hive string) []ScannedApp {
	valuePattern := regexp.MustCompile(`^\s+([^\s]+)\s+REG_\S+\s*(.*)$`)
	apps := map[string]map[string]string{}
	current := ""
	for _, raw := range strings.Split(output, "\n") {
		line := strings.TrimRight(raw, "\r")
		if strings.HasPrefix(strings.TrimSpace(line), "HKEY_") {
			current = strings.TrimSpace(line)
			if apps[current] == nil {
				apps[current] = map[string]string{}
			}
			continue
		}
		if current == "" {
			continue
		}
		match := valuePattern.FindStringSubmatch(line)
		if len(match) == 3 {
			apps[current][match[1]] = normalizeText(match[2])
		}
	}

	var scanned []ScannedApp
	for _, values := range apps {
		name := values["DisplayName"]
		if name == "" {
			continue
		}
		if values["SystemComponent"] == "0x1" {
			continue
		}
		releaseType := strings.ToLower(values["ReleaseType"])
		if releaseType == "hotfix" || releaseType == "security update" || releaseType == "update rollup" {
			continue
		}
		publisher := values["Publisher"]
		location := values["InstallLocation"]
		key := normalizeRegistryKey(name, publisher, location)
		scanned = append(scanned, ScannedApp{
			Key:             key,
			Name:            name,
			Version:         values["DisplayVersion"],
			Publisher:       publisher,
			InstallLocation: location,
			Source:          "registry",
			RegistryHive:    hive,
		})
	}
	sort.Slice(scanned, func(i, j int) bool { return strings.ToLower(scanned[i].Name) < strings.ToLower(scanned[j].Name) })
	return scanned
}

func readRegistryApps() ([]ScannedApp, error) {
	queries := []struct {
		key  string
		hive string
	}{
		{`HKLM\Software\Microsoft\Windows\CurrentVersion\Uninstall`, "HKLM"},
		{`HKLM\Software\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall`, "HKLM32"},
		{`HKCU\Software\Microsoft\Windows\CurrentVersion\Uninstall`, "HKCU"},
	}
	appMap := map[string]ScannedApp{}
	var errs []string
	for _, query := range queries {
		result := runCommand(60*time.Second, "reg.exe", "query", query.key, "/s")
		if !result.OK && result.Stdout == "" {
			errs = append(errs, result.Stderr)
			continue
		}
		for _, app := range parseRegQuery(result.Stdout, query.hive) {
			appMap[app.Key] = app
		}
	}
	var apps []ScannedApp
	for _, app := range appMap {
		apps = append(apps, app)
	}
	sort.Slice(apps, func(i, j int) bool { return strings.ToLower(apps[i].Name) < strings.ToLower(apps[j].Name) })
	if len(apps) == 0 && len(errs) > 0 {
		return apps, errors.New(strings.Join(errs, "\n"))
	}
	return apps, nil
}

func readWingetApps() ([]ScannedApp, *CommandResult, error) {
	if !detectManager("winget").Available {
		return nil, nil, nil
	}
	packages, result := wingetInstalled()
	apps := []ScannedApp{}
	for _, pkg := range packages {
		if pkg.ID == "" {
			continue
		}
		apps = append(apps, ScannedApp{
			Key:             "winget:" + strings.ToLower(pkg.ID),
			Name:            pkg.Name,
			Version:         pkg.Version,
			InstallLocation: pkg.ID,
			Source:          "winget",
			Manager:         "winget",
			PackageID:       pkg.ID,
		})
	}
	return apps, &result, nil
}

func diffSnapshot(current []ScannedApp, previous map[string]ScannedApp) (map[string]ScannedApp, []ScannedApp, []ScannedApp, bool) {
	now := utcNow()
	currentMap := map[string]ScannedApp{}
	var newApps []ScannedApp
	for _, app := range current {
		prev, ok := previous[app.Key]
		if ok {
			app.FirstSeen = prev.FirstSeen
		} else {
			app.FirstSeen = now
			if len(previous) > 0 {
				newApps = append(newApps, app)
			}
		}
		currentMap[app.Key] = app
	}
	var removed []ScannedApp
	for key, app := range previous {
		if _, ok := currentMap[key]; !ok {
			removed = append(removed, app)
		}
	}
	sort.Slice(newApps, func(i, j int) bool { return strings.ToLower(newApps[i].Name) < strings.ToLower(newApps[j].Name) })
	sort.Slice(removed, func(i, j int) bool { return strings.ToLower(removed[i].Name) < strings.ToLower(removed[j].Name) })
	return currentMap, newApps, removed, len(previous) == 0
}

func scanAppsAndStore() ScanResult {
	state := loadState()
	var errorsOut []map[string]string

	registryApps, err := readRegistryApps()
	if err != nil {
		errorsOut = append(errorsOut, map[string]string{"source": "registry", "error": err.Error()})
	}
	wingetApps, wingetResult, err := readWingetApps()
	if err != nil {
		errorsOut = append(errorsOut, map[string]string{"source": "winget", "error": err.Error()})
	}

	registryMap, registryNew, registryRemoved, registryBaseline := diffSnapshot(registryApps, state.RegistryApps)
	wingetMap, wingetNew, wingetRemoved, wingetBaseline := diffSnapshot(wingetApps, state.WingetApps)
	state.RegistryApps = registryMap
	state.WingetApps = wingetMap
	state.LastScanAt = utcNow()
	_ = saveState(state)

	newApps := append(registryNew, wingetNew...)
	removedApps := append(registryRemoved, wingetRemoved...)
	sort.Slice(newApps, func(i, j int) bool {
		return strings.ToLower(newApps[i].Source+newApps[i].Name) < strings.ToLower(newApps[j].Source+newApps[j].Name)
	})

	return ScanResult{
		LastScanAt:      state.LastScanAt,
		Baseline:        registryBaseline && wingetBaseline,
		Baselines:       map[string]bool{"registry": registryBaseline, "winget": wingetBaseline},
		NewApps:         newApps,
		RemovedApps:     removedApps,
		TrackedCount:    len(registryMap) + len(wingetMap),
		SourceCounts:    map[string]int{"registry": len(registryMap), "winget": len(wingetMap)},
		WingetAvailable: wingetResult != nil,
		WingetResult:    wingetResult,
		Errors:          errorsOut,
	}
}

func taskExists(name string) bool {
	return runCommand(30*time.Second, "schtasks.exe", "/Query", "/TN", name, "/FO", "LIST").OK
}

func createStartupTask() CommandResult {
	exe, _ := os.Executable()
	action := quoteArg(exe) + " --no-browser"
	return runCommand(60*time.Second, "schtasks.exe", "/Create", "/TN", taskStartup, "/TR", action, "/SC", "ONLOGON", "/RL", "HIGHEST", "/F")
}

func createAutoUpdateTask() CommandResult {
	exe, _ := os.Executable()
	action := quoteArg(exe) + " --task auto-update"
	return runCommand(60*time.Second, "schtasks.exe", "/Create", "/TN", taskAutoUpdate, "/TR", action, "/SC", "DAILY", "/ST", defaultAutoUpdateTime, "/RL", "HIGHEST", "/F")
}

func deleteTask(name string) CommandResult {
	if !taskExists(name) {
		return CommandResult{OK: true, Command: "delete " + name, Stdout: "Task did not exist."}
	}
	return runCommand(60*time.Second, "schtasks.exe", "/Delete", "/TN", name, "/F")
}

func setStartup(enabled bool) CommandResult {
	if enabled {
		return createStartupTask()
	}
	return deleteTask(taskStartup)
}

func setAutoUpdate(global *bool, packageKeys []string, packageEnabled *bool) (State, CommandResult) {
	state := loadState()
	if state.AutoUpdatePackages == nil {
		state.AutoUpdatePackages = map[string]bool{}
	}
	if global != nil {
		state.AutoUpdateGlobal = *global
	}
	if packageEnabled != nil {
		for _, key := range packageKeys {
			if _, _, err := splitPackageKey(key); err == nil {
				state.AutoUpdatePackages[key] = *packageEnabled
			}
		}
	}
	_ = saveState(state)
	if state.AutoUpdateGlobal {
		return state, createAutoUpdateTask()
	}
	return state, deleteTask(taskAutoUpdate)
}

func runAutoUpdate() []UpdateResult {
	state := loadState()
	if !state.AutoUpdateGlobal {
		return nil
	}
	var selected []string
	for key, enabled := range state.AutoUpdatePackages {
		if enabled {
			selected = append(selected, key)
		}
	}
	results := updateAll(selected)
	state.LastAutoUpdateAt = utcNow()
	state.LastAutoUpdateResults = results
	_ = saveState(state)
	return results
}

func openURL(url string) error {
	return exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", url).Start()
}

func randomToken() string {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

func freePort(start int) int {
	for port := start; port < start+50; port++ {
		listener, err := net.Listen("tcp", fmt.Sprintf("%s:%d", defaultHost, port))
		if err == nil {
			_ = listener.Close()
			return port
		}
	}
	return start
}

type App struct {
	token  string
	server *http.Server
}

func (app *App) tokenOK(r *http.Request) bool {
	token := r.URL.Query().Get("token")
	if token == "" {
		_ = r.ParseForm()
		token = r.Form.Get("token")
	}
	if token == "" {
		token = r.Header.Get("X-Updater-Token")
	}
	return token == app.token
}

func (app *App) serveHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/shutdown" && app.tokenOK(r) {
		_, _ = io.WriteString(w, "Stopping")
		go func() {
			time.Sleep(200 * time.Millisecond)
			_ = app.server.Shutdown(context.Background())
		}()
		return
	}
	if !app.tokenOK(r) {
		http.Error(w, "Unauthorized. Start the app and use the tokenized URL.", http.StatusUnauthorized)
		return
	}

	switch r.URL.Path {
	case "/":
		app.render(w, r, PageData{})
	case "/search":
		app.render(w, r, PageData{SearchQuery: r.URL.Query().Get("q")})
	case "/scan":
		scan := scanAppsAndStore()
		app.render(w, r, PageData{Scan: &scan, Message: "Application scan completed."})
	case "/install":
		_ = r.ParseForm()
		result := installPackage(r.Form.Get("manager"), r.Form.Get("package_id"))
		app.render(w, r, PageData{CommandResult: &result, Message: "Install command completed."})
	case "/manager/install":
		_ = r.ParseForm()
		result := installManager(r.Form.Get("manager"))
		app.render(w, r, PageData{CommandResult: &result, Message: "Package manager install action completed."})
	case "/update":
		_ = r.ParseForm()
		result := updatePackage(r.Form.Get("manager"), r.Form.Get("package_id"))
		app.render(w, r, PageData{CommandResult: &result, Message: "Update command completed."})
	case "/update-selected":
		_ = r.ParseForm()
		results := updateAll(r.Form["package_key"])
		app.render(w, r, PageData{ActionResults: results, Message: "Selected update command completed."})
	case "/update-all":
		results := updateAll(nil)
		app.render(w, r, PageData{ActionResults: results, Message: "Update all command completed."})
	case "/settings/startup":
		_ = r.ParseForm()
		result := setStartup(r.Form.Get("enabled") == "true")
		app.render(w, r, PageData{CommandResult: &result, Message: "Startup setting updated."})
	case "/settings/auto":
		_ = r.ParseForm()
		var global *bool
		if r.Form.Has("global") {
			value := r.Form.Get("global") == "true"
			global = &value
		}
		var packageEnabled *bool
		if r.Form.Has("package_enabled") {
			value := r.Form.Get("package_enabled") == "true"
			packageEnabled = &value
		}
		_, result := setAutoUpdate(global, r.Form["package_key"], packageEnabled)
		app.render(w, r, PageData{CommandResult: &result, Message: "Auto-update setting updated."})
	case "/settings/theme":
		_ = r.ParseForm()
		state := loadState()
		if r.Form.Get("theme") == "light" {
			state.Theme = "light"
		} else {
			state.Theme = "dark"
		}
		_ = saveState(state)
		app.render(w, r, PageData{Message: "Theme updated."})
	default:
		http.NotFound(w, r)
	}
}

func (app *App) render(w http.ResponseWriter, r *http.Request, data PageData) {
	state := loadState()
	inventory := getInventory()
	data.Token = app.token
	data.URLToken = template.URLQueryEscaper(app.token)
	data.Admin = isAdmin()
	data.StateDir, _ = stateDir()
	data.Managers = inventory.Managers
	data.Packages = inventory.Packages
	data.StartupEnabled = taskExists(taskStartup)
	data.AutoTaskEnabled = taskExists(taskAutoUpdate)
	data.Settings = state
	data.Theme = state.Theme

	if data.SearchQuery != "" {
		results, _, _, err := searchPackages(data.SearchQuery)
		if err != nil {
			data.Message = err.Error()
		} else {
			data.SearchResults = results
		}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := pageTemplate.Execute(w, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func runServer(noBrowser bool) error {
	token := randomToken()
	port := freePort(defaultPort)
	app := &App{token: token}
	mux := http.NewServeMux()
	mux.HandleFunc("/", app.serveHTTP)
	server := &http.Server{Addr: fmt.Sprintf("%s:%d", defaultHost, port), Handler: mux}
	app.server = server

	url := fmt.Sprintf("http://%s:%d/?token=%s", defaultHost, port, token)
	if !noBrowser {
		_ = openURL(url)
	}
	return server.ListenAndServe()
}

func hasArg(name string) bool {
	for _, arg := range os.Args[1:] {
		if arg == name {
			return true
		}
	}
	return false
}

func main() {
	if hasArg("--task") {
		for i, arg := range os.Args {
			if arg == "--task" && i+1 < len(os.Args) && os.Args[i+1] == "auto-update" {
				results := runAutoUpdate()
				data, _ := json.MarshalIndent(results, "", "  ")
				fmt.Println(string(data))
				return
			}
		}
	}

	if !hasArg("--no-elevate") && !isAdmin() {
		exe, _ := os.Executable()
		var params []string
		for _, arg := range os.Args[1:] {
			if arg != "--no-elevate" {
				params = append(params, quoteArg(arg))
			}
		}
		if err := shellExecuteRunas(exe, strings.Join(params, " ")); err == nil {
			return
		}
	}

	if err := runServer(hasArg("--no-browser")); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fmt.Fprintln(os.Stderr, err)
	}
}

var pageTemplate = template.Must(template.New("page").Funcs(template.FuncMap{
	"not": func(v bool) bool { return !v },
}).Parse(`<!doctype html>
<html lang="en" data-theme="{{.Theme}}">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Windows Updater WebUI</title>
  <style>` + pageCSS + `</style>
</head>
<body>
  <header class="app-header">
    <div>
      <h1>Windows Updater WebUI</h1>
      <p>{{if .Admin}}Running elevated{{else}}Not elevated{{end}} · State: {{.StateDir}}</p>
    </div>
    <div class="header-actions">
      <form method="post" action="/settings/theme"><input type="hidden" name="token" value="{{.Token}}"><input type="hidden" name="theme" value="{{if eq .Theme "dark"}}light{{else}}dark{{end}}"><button type="submit">{{if eq .Theme "dark"}}Light{{else}}Dark{{end}} Mode</button></form>
      <form method="post" action="/scan"><input type="hidden" name="token" value="{{.Token}}"><button type="submit">Scan Apps</button></form>
      <form method="post" action="/shutdown"><input type="hidden" name="token" value="{{.Token}}"><button class="secondary" type="submit">Stop</button></form>
    </div>
  </header>
  <main>
    {{if .Message}}<section class="notice">{{.Message}}</section>{{end}}
    {{if .CommandResult}}<section class="log"><h2>Command Result</h2><pre>{{.CommandResult.Command}}
OK={{.CommandResult.OK}} Code={{.CommandResult.Code}}
{{.CommandResult.Stdout}}{{.CommandResult.Stderr}}</pre></section>{{end}}
    {{if .ActionResults}}<section class="log"><h2>Update Results</h2>{{range .ActionResults}}<pre>{{.Key}} · OK={{.Result.OK}} Code={{.Result.Code}}
{{.Result.Stdout}}{{.Result.Stderr}}</pre>{{end}}</section>{{end}}

    <section class="status-grid">
      <div class="panel"><h2>Package Managers</h2>{{range $name, $manager := .Managers}}<div class="manager"><strong>{{$name}}</strong>{{if $manager.Available}}<span class="badge ok">Available {{$manager.Version}}</span><span class="muted">{{$manager.Path}}</span>{{else}}<span class="badge error">Missing</span><span class="muted">{{$manager.Error}}</span><form method="post" action="/manager/install"><input type="hidden" name="token" value="{{$.Token}}"><input type="hidden" name="manager" value="{{$name}}"><button type="submit">Install {{$name}}</button></form>{{end}}</div>{{end}}</div>
      <div class="panel"><h2>Automation</h2><div class="stack">
        <form method="post" action="/settings/startup"><input type="hidden" name="token" value="{{.Token}}"><input type="hidden" name="enabled" value="{{if .StartupEnabled}}false{{else}}true{{end}}"><button type="submit">{{if .StartupEnabled}}Disable{{else}}Enable{{end}} Start With Windows</button></form>
        <form method="post" action="/settings/auto"><input type="hidden" name="token" value="{{.Token}}"><input type="hidden" name="global" value="{{if .Settings.AutoUpdateGlobal}}false{{else}}true{{end}}"><button type="submit">{{if .Settings.AutoUpdateGlobal}}Disable{{else}}Enable{{end}} Daily Auto-Update</button></form>
        <form method="post" action="/settings/auto"><input type="hidden" name="token" value="{{.Token}}"><input type="hidden" name="global" value="true"><input type="hidden" name="package_enabled" value="true">{{range .Packages}}<input type="hidden" name="package_key" value="{{.Key}}">{{end}}<button type="submit">Auto All</button></form>
        <form method="post" action="/settings/auto"><input type="hidden" name="token" value="{{.Token}}"><input type="hidden" name="global" value="false"><input type="hidden" name="package_enabled" value="false">{{range .Packages}}<input type="hidden" name="package_key" value="{{.Key}}">{{end}}<button type="submit">Auto None</button></form>
        <p class="muted">Startup task: {{.StartupEnabled}} · Auto task: {{.AutoTaskEnabled}}</p>
      </div></div>
      <div class="panel"><h2>Search</h2><form class="search" method="get" action="/search"><input type="hidden" name="token" value="{{.Token}}"><input name="q" value="{{.SearchQuery}}" placeholder="Search packages"><button type="submit">Search</button></form></div>
    </section>

    {{if .Scan}}<section class="panel"><h2>Scan Results</h2><p class="muted">Tracked {{.Scan.TrackedCount}} apps · Registry {{index .Scan.SourceCounts "registry"}} · Winget {{index .Scan.SourceCounts "winget"}}</p>{{if .Scan.Errors}}<pre>{{range .Scan.Errors}}{{.source}}: {{.error}}
{{end}}</pre>{{end}}<table><thead><tr><th>Source</th><th>Name</th><th>Version</th><th>Publisher</th><th>Location</th></tr></thead><tbody>{{range .Scan.NewApps}}<tr><td>{{.Source}}</td><td>{{.Name}}</td><td>{{.Version}}</td><td>{{.Publisher}}</td><td>{{.InstallLocation}}</td></tr>{{else}}<tr><td colspan="5">No newly detected applications.</td></tr>{{end}}</tbody></table></section>{{end}}

    {{if .SearchQuery}}<section class="panel"><h2>Search Results</h2><table><thead><tr><th>Name</th><th>Manager</th><th>ID</th><th>Version</th><th>Action</th></tr></thead><tbody>{{range .SearchResults}}<tr><td>{{.Name}}</td><td>{{.Manager}}</td><td>{{.ID}}</td><td>{{.Version}}</td><td><form method="post" action="/install"><input type="hidden" name="token" value="{{$.Token}}"><input type="hidden" name="manager" value="{{.Manager}}"><input type="hidden" name="package_id" value="{{.ID}}"><button type="submit">Install</button></form></td></tr>{{else}}<tr><td colspan="5">No installable results.</td></tr>{{end}}</tbody></table></section>{{end}}

	<section class="panel">
	  <div class="section-heading"><h2>Installed Packages</h2><form method="post" action="/update-all"><input type="hidden" name="token" value="{{.Token}}"><button type="submit">Update All</button></form></div>
	  <form id="update-selected-form" method="post" action="/update-selected"><input type="hidden" name="token" value="{{.Token}}"></form>
	  <table><thead><tr><th></th><th>Name</th><th>Manager</th><th>Installed</th><th>Available</th><th>Status</th><th>Auto</th><th>Action</th></tr></thead><tbody>{{range .Packages}}<tr>
		<td>{{if .UpdateAvailable}}<input form="update-selected-form" type="checkbox" name="package_key" value="{{.Key}}">{{end}}</td>
		<td><strong>{{.Name}}</strong><br><span class="muted">{{.ID}}</span></td>
		<td><span class="badge">{{.Manager}}</span></td>
		<td>{{.Version}}</td>
        <td>{{.AvailableVersion}}</td>
        <td>{{if .UpdateAvailable}}<span class="badge warn">Update</span>{{else}}<span class="badge ok">Current</span>{{end}}</td>
		<td><form method="post" action="/settings/auto"><input type="hidden" name="token" value="{{$.Token}}"><input type="hidden" name="package_key" value="{{.Key}}"><input type="hidden" name="package_enabled" value="{{if .AutoUpdate}}false{{else}}true{{end}}"><button type="submit">{{if .AutoUpdate}}On{{else}}Off{{end}}</button></form></td>
		<td>{{if .UpdateAvailable}}<form method="post" action="/update"><input type="hidden" name="token" value="{{$.Token}}"><input type="hidden" name="manager" value="{{.Manager}}"><input type="hidden" name="package_id" value="{{.ID}}"><button type="submit">Update</button></form>{{end}}</td>
	  </tr>{{else}}<tr><td colspan="8">No managed packages found.</td></tr>{{end}}</tbody></table>
	  <button form="update-selected-form" type="submit">Update Selected</button>
	</section>
  </main>
</body>
</html>`))

const pageCSS = `
:root{color-scheme:light dark;--bg:#f6f7f9;--surface:#fff;--line:#d8dee8;--text:#18202b;--muted:#5d6979;--header:#172033;--header-text:#fff;--blue:#1d5fd1;--green:#18784f;--amber:#946200;--red:#b42318;--input:#fff;--input-text:#18202b}
[data-theme=dark]{--bg:#101419;--surface:#171d24;--line:#2c3542;--text:#ecf1f7;--muted:#a8b3c2;--header:#0c1117;--header-text:#f5f8fb;--blue:#5d9bff;--green:#71d29d;--amber:#f1c65b;--red:#ff8b7f;--input:#111820;--input-text:#ecf1f7}
*{box-sizing:border-box}body{margin:0;background:var(--bg);color:var(--text);font:14px/1.45 "Segoe UI",system-ui,sans-serif}.app-header{display:flex;justify-content:space-between;gap:16px;align-items:center;background:var(--header);color:var(--header-text);padding:18px 24px;border-bottom:4px solid #2c9a78}.app-header h1{margin:0;font-size:24px}.app-header p{margin:4px 0 0;color:#dce6f4}.header-actions,.section-heading,.search{display:flex;gap:10px;align-items:center;flex-wrap:wrap}main{width:min(1480px,100%);margin:auto;padding:20px 24px}.status-grid{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:14px}.panel,.notice,.log{background:var(--surface);border:1px solid var(--line);margin-bottom:16px;padding:14px;box-shadow:0 8px 24px rgba(18,32,51,.08)}.notice{border-left:4px solid var(--blue)}.manager{display:grid;gap:6px;margin-top:10px}.stack{display:grid;gap:10px}.muted{color:var(--muted)}button{min-height:34px;border:1px solid var(--blue);background:var(--blue);color:#fff;padding:6px 10px;font:inherit;font-weight:600;cursor:pointer}button.secondary{background:transparent;border-color:rgba(255,255,255,.35)}input{min-height:34px;border:1px solid var(--line);background:var(--input);color:var(--input-text);padding:6px 9px;font:inherit}table{width:100%;border-collapse:collapse;table-layout:fixed}th,td{border-bottom:1px solid var(--line);padding:9px 10px;text-align:left;vertical-align:middle;overflow-wrap:anywhere}th{color:var(--muted);text-transform:uppercase;font-size:12px}.badge{display:inline-flex;min-height:22px;align-items:center;border:1px solid var(--line);padding:1px 7px;background:rgba(127,127,127,.1);font-size:12px;font-weight:650}.badge.ok{color:var(--green)}.badge.warn{color:var(--amber)}.badge.error{color:var(--red)}pre{white-space:pre-wrap;overflow:auto;background:#0a0e13;color:#d8f3dc;padding:10px}@media(max-width:900px){.app-header,.status-grid{display:block}.header-actions{margin-top:12px}main{padding:12px}table{min-width:900px}.panel{overflow-x:auto}}`
