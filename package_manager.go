package main

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
)

type ManagerStatus struct {
	Available          bool   `json:"available"`
	Version            string `json:"version,omitempty"`
	Path               string `json:"path,omitempty"`
	Error              string `json:"error,omitempty"`
	InventoryAvailable bool   `json:"inventory_available,omitempty"`
	InventoryBackend   string `json:"inventory_backend,omitempty"`
	ActionBackend      string `json:"action_backend,omitempty"`
}

type Package struct {
	Key              string `json:"key"`
	Manager          string `json:"manager"`
	ID               string `json:"id"`
	Name             string `json:"name"`
	Version          string `json:"version"`
	AvailableVersion string `json:"available_version"`
	UpdateAvailable  bool   `json:"update_available"`
	UpdateSupported  bool   `json:"update_supported"`
	UnknownVersion   bool   `json:"unknown_version,omitempty"`
	Installed        bool   `json:"installed"`
	AutoUpdate       bool   `json:"auto_update"`
	Source           string `json:"source,omitempty"`
	Match            string `json:"match,omitempty"`
	ActionBackend    string `json:"action_backend,omitempty"`

	AllowUnknownVersionUpdate bool `json:"-"`
	AllowPinnedUpdate         bool `json:"-"`
}

type PackageLookup struct {
	Packages       []Package                `json:"packages"`
	Managers       map[string]ManagerStatus `json:"managers"`
	CommandResults map[string]CommandResult `json:"command_results"`
}

type Inventory struct {
	PackageLookup
	Scan InventoryScanSummary `json:"scan"`
}

type UpdateResult struct {
	Key    string        `json:"key"`
	Result CommandResult `json:"result"`
}

type InventoryScanSummary struct {
	LastScanAt    string `json:"last_scan_at,omitempty"`
	TrackedCount  int    `json:"tracked_count"`
	RegistryCount int    `json:"registry_count"`
	WingetCount   int    `json:"winget_count"`
	StoreCount    int    `json:"store_count"`
}

const (
	managerWinget = "winget"
	managerStore  = "store"
	managerChoco  = "choco"

	sourceWinget   = "winget"
	sourceMSStore  = "msstore"
	sourceStoreCLI = "store-cli"
	sourceAppX     = "appx"

	backendStoreCLI              = "store-cli"
	backendAppXInventory         = "appx-inventory"
	backendStoreCLIResolved      = "store-cli-resolved"
	backendWingetMSStoreFallback = "winget-msstore-fallback"
	inventoryBackendAppX         = "AppX"
)

var managedPackageManagers = []string{managerWinget, managerStore, managerChoco}

const managerValidationMessage = "manager must be winget, store, or choco"
const storeActionUnavailableMessage = "native Store CLI is unavailable and winget msstore fallback is unavailable"

func isManagedPackageManager(manager string) bool {
	for _, supported := range managedPackageManagers {
		if manager == supported {
			return true
		}
	}
	return false
}

func managerValidationError() error {
	return errors.New(managerValidationMessage)
}

func wingetSourceManager(source string) string {
	if strings.EqualFold(strings.TrimSpace(source), sourceMSStore) {
		return managerStore
	}
	return managerWinget
}

func managerSortRank(manager string) int {
	for index, supported := range managedPackageManagers {
		if manager == supported {
			return index
		}
	}
	return len(managedPackageManagers)
}

func versionGreater(candidate, current string) bool {
	candidateParts := versionParts(candidate)
	currentParts := versionParts(current)
	if len(candidateParts) == 0 || len(currentParts) == 0 {
		return false
	}
	maxParts := len(candidateParts)
	if len(currentParts) > maxParts {
		maxParts = len(currentParts)
	}
	for i := 0; i < maxParts; i++ {
		candidatePart := 0
		currentPart := 0
		if i < len(candidateParts) {
			candidatePart = candidateParts[i]
		}
		if i < len(currentParts) {
			currentPart = currentParts[i]
		}
		if candidatePart > currentPart {
			return true
		}
		if candidatePart < currentPart {
			return false
		}
	}
	return false
}

func versionParts(value string) []int {
	fields := regexp.MustCompile(`\D+`).Split(strings.TrimSpace(value), -1)
	parts := []int{}
	for _, field := range fields {
		if field == "" {
			continue
		}
		part, err := strconv.Atoi(field)
		if err != nil {
			return nil
		}
		parts = append(parts, part)
	}
	return parts
}

func normalizePackageIdentity(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.TrimSuffix(value, "_8wekyb3d8bbwe")
	return regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(value, "")
}

func packageKey(manager, id string) string {
	return manager + ":" + id
}

func packageAutoUpdateEnabled(state State, pkg Package) bool {
	if state.AutoUpdatePackages[pkg.Key] {
		return true
	}
	normalizedKey := normalizeAutoUpdatePackageKey(pkg.Key)
	if state.AutoUpdatePackages[normalizedKey] {
		return true
	}
	for key, enabled := range state.AutoUpdatePackages {
		if enabled && equivalentPackageKeys(pkg.Key, key) {
			return true
		}
	}
	return false
}

func equivalentPackageKeys(left, right string) bool {
	leftManager, leftID, leftErr := splitPackageKey(left)
	rightManager, rightID, rightErr := splitPackageKey(right)
	if leftErr != nil || rightErr != nil || leftManager != rightManager {
		return false
	}
	if leftManager == managerStore {
		return strings.EqualFold(stableStoreActionID(leftID), stableStoreActionID(rightID))
	}
	return leftID == rightID
}

func splitPackageKey(key string) (string, string, error) {
	parts := strings.SplitN(key, ":", 2)
	if len(parts) != 2 || parts[1] == "" || !isManagedPackageManager(parts[0]) {
		return "", "", errors.New("package key must be manager:id")
	}
	return parts[0], parts[1], nil
}
