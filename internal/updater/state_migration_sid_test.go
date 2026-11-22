package updater

import (
	"errors"
	"testing"
)

// RULE-273: a Store PFN opt-in survives a start where the user SID cannot be
// read, and is migrated on the next start.
func TestStorePFNOptInKeptWhenUserSIDUnavailable(t *testing.T) {
	legacyKey := packageKey(managerStore, "Microsoft.WindowsTerminal_8wekyb3d8bbwe")
	state := &State{AutoUpdatePackages: map[string]bool{legacyKey: true}}

	migrateAutoUpdatePackageKeysWithUserSID(state, nil, func() (string, error) {
		return "", errors.New("token not ready")
	})
	if !state.AutoUpdatePackages[legacyKey] {
		t.Fatalf("opt-in dropped while SID unavailable: %#v", state.AutoUpdatePackages)
	}
	if len(state.StoreAutoUpdateMigration.Disabled) != 0 {
		t.Fatalf("opt-in reported disabled: %#v", state.StoreAutoUpdateMigration.Disabled)
	}

	migrateAutoUpdatePackageKeysWithUserSID(state, nil, func() (string, error) { return "S-1-5-21-1-2-3-1001", nil })
	want := canonicalStoreAutoUpdateKey("S-1-5-21-1-2-3-1001", "Microsoft.WindowsTerminal_8wekyb3d8bbwe")
	if !state.AutoUpdatePackages[want] || len(state.AutoUpdatePackages) != 1 {
		t.Fatalf("expected migrated key %q, got %#v", want, state.AutoUpdatePackages)
	}
}
