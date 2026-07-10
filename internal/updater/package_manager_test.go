package updater

import "testing"

func TestStorePackageKeysAreValid(t *testing.T) {
	for rank, manager := range managedPackageManagers {
		if !isManagedPackageManager(manager) {
			t.Fatalf("%q should be accepted by manager validation", manager)
		}
		if managerSortRank(manager) != rank {
			t.Fatalf("%q sort rank should be %d, got %d", manager, rank, managerSortRank(manager))
		}
	}
	if isManagedPackageManager("npm") {
		t.Fatal("unexpected manager accepted")
	}
	if managerValidationError().Error() != managerValidationMessage {
		t.Fatalf("unexpected manager validation message: %q", managerValidationError().Error())
	}

	manager, id, err := splitPackageKey("store:9NBLGGH5R558")
	if err != nil {
		t.Fatal(err)
	}
	if manager != "store" || id != "9NBLGGH5R558" {
		t.Fatalf("unexpected store package key split: %q %q", manager, id)
	}
	if err := validateManagerAndID("store", "9NBLGGH5R558"); err != nil {
		t.Fatalf("store manager should validate: %v", err)
	}
	if wingetSourceArg("store") != "msstore" {
		t.Fatal("store manager should use the msstore winget source")
	}
	if err := validateManagerAndID("store", "Microsoft To Do"); err != nil {
		t.Fatalf("store queries with spaces should validate: %v", err)
	}
	if err := validateManagerAndID("store", "bad&query"); err == nil {
		t.Fatal("store queries with shell metacharacters should be rejected")
	}
	if err := validateManagerAndID("store", "bad%query"); err == nil {
		t.Fatal("store queries with cmd expansion characters should be rejected")
	}
	if err := validateManagerAndID("store", "--manifest=C:\\malicious.yaml"); err == nil {
		t.Fatal("store option-shaped targets should be rejected")
	}
	if err := validateManagerAndID("winget", "Long Desktop App"); err != nil {
		t.Fatalf("winget positional package targets should validate: %v", err)
	}
	if err := validateManagerAndID("winget", "bad&target"); err == nil {
		t.Fatal("winget targets with shell metacharacters should be rejected")
	}
	if err := validateManagerAndID("winget", "--manifest=C:\\malicious.yaml"); err == nil {
		t.Fatal("winget option-shaped targets should be rejected")
	}
	if err := validateManagerAndID("choco", "--version"); err == nil {
		t.Fatal("chocolatey option-shaped package ids should be rejected")
	}
}

func TestPackageAutoUpdateEnabledUsesCanonicalStoreKey(t *testing.T) {
	userSID, err := currentUserSID()
	if err != nil {
		t.Fatal(err)
	}
	state := State{
		AutoUpdatePackages: map[string]bool{
			canonicalStoreAutoUpdateKey(userSID, "OpenAI.Codex_abc123"): true,
		},
	}
	pkg := Package{
		Key:                        "store:OpenAI.Codex_abc123",
		Manager:                    "store",
		ID:                         "OpenAI.Codex_abc123",
		InstalledPackageFamilyName: "OpenAI.Codex_abc123",
	}
	if !packageAutoUpdateEnabled(state, pkg) {
		t.Fatalf("expected canonical Store auto-update key to be honored")
	}
	state.AutoUpdatePackages = map[string]bool{
		"store:OpenAI.Codex_1.0.0.0_x64__abc123": true,
	}
	if packageAutoUpdateEnabled(state, pkg) {
		t.Fatalf("versioned Store full-name key must not be treated as equivalent")
	}
}

func TestNumericVersionLooksNewer(t *testing.T) {
	cases := []struct {
		candidate string
		current   string
		want      bool
	}{
		{"1.1.0", "1.0.9", true},
		{"1.10", "1.2", true},
		{"2026.11050.1001.0", "2026.11050.1001.0", false},
		{"2024.12", "2024.9", true},
		{"1.0", "1.0.1", false},
		{"v2.0.0", "1.9.9", true},
		{"1.0.0-beta", "1.0.0", false},
		{"latest", "1.0.0", false},
		{"unbekannt", "1.0.0", false},
	}
	for _, tc := range cases {
		if got := numericVersionLooksNewer(tc.candidate, tc.current); got != tc.want {
			t.Fatalf("numericVersionLooksNewer(%q, %q) = %t, want %t", tc.candidate, tc.current, got, tc.want)
		}
	}
}

func TestPackageIdentityNormalizationAndKeySplitting(t *testing.T) {
	cases := map[string]string{
		" OpenAI.Codex_2p2nqsd0c76g0 ":              "openaicodex2p2nqsd0c76g0",
		"Microsoft.WindowsCalculator_8wekyb3d8bbwe": "microsoftwindowscalculator",
		"Capture-Picker! 2026":                      "capturepicker2026",
	}
	for input, want := range cases {
		if got := normalizePackageIdentity(input); got != want {
			t.Fatalf("normalizePackageIdentity(%q) = %q, want %q", input, got, want)
		}
	}
	if _, _, err := splitPackageKey("winget:"); err == nil {
		t.Fatal("empty package id should be rejected")
	}
	if _, _, err := splitPackageKey("npm:leftpad"); err == nil {
		t.Fatal("unmanaged package manager should be rejected")
	}
	if manager, id, err := splitPackageKey("choco:git.install"); err != nil || manager != managerChoco || id != "git.install" {
		t.Fatalf("unexpected split result manager=%q id=%q err=%v", manager, id, err)
	}
}
