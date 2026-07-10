package updater

import "testing"

func FuzzParseWingetTable(f *testing.F) {
	for _, seed := range []string{
		"Name ID Version Available Source\n--------------------------------\nGit Git.Git 2.53.0 2.54.0 winget",
		"Name ID Version Verfuegbar Quelle\n------------------------------------\nMystery Vendor.Mystery Unknown 1.2.0 winget",
		"Name ID Version Match Source\n----------------------------\nZed ZedIndustries.Zed 1.6.3 Moniker: zed winget",
		"(TM) S E Deve lopmen t Kit 17.0.1 ... Oracle.JDK.17 17.0.10 17.0.12 winget",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, output string) {
		for _, pkg := range parseWingetTable(output) {
			if pkg.ID == "" {
				t.Fatalf("parsed winget package has empty id: %#v", pkg)
			}
			if !isManagedPackageManager(pkg.Manager) {
				t.Fatalf("parsed winget package has unmanaged manager: %#v", pkg)
			}
			if pkg.Source != "" && pkg.Source != sourceWinget && pkg.Source != sourceMSStore {
				t.Fatalf("parsed winget package has unexpected source: %#v", pkg)
			}
		}
	})
}

func FuzzParseStorePackageRows(f *testing.F) {
	for _, seed := range []string{
		"Name ID Publisher\n-----------------\nCodex 9PLM9XGG6VKS OpenAI",
		"| Name | Product ID | Publisher | Version |\n|------|------------|-----------|---------|\n| Codex | 9PLM9XGG6VKS | OpenAI | 1.0.0 |",
		"┌──────┬──────────────┬───────────┐\n│ Name │ Product ID   │ Publisher │\n├──────┼──────────────┼───────────┤\n│ App  │ 9WZDNCRFHVN5 │ Microsoft │\n└──────┴──────────────┴───────────┘",
		"Updates available (1 found)\nChecking for updates...\nName ID Version\n----------------\nApp 9WZDNCRFHVN5 1.2.3",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, output string) {
		for _, pkg := range parseStorePackageRows(output) {
			if pkg.Name == "" || pkg.ID == "" {
				t.Fatalf("parsed Store package is missing name/id: %#v", pkg)
			}
			if pkg.Manager != managerStore || pkg.Source != sourceStoreCLI || pkg.ActionBackend != backendStoreCLI {
				t.Fatalf("parsed Store package has unexpected provenance: %#v", pkg)
			}
		}
	})
}

func FuzzNormalizePackageIdentity(f *testing.F) {
	for _, seed := range []string{
		" OpenAI.Codex_2p2nqsd0c76g0 ",
		"Microsoft.WindowsCalculator_8wekyb3d8bbwe",
		"Capture-Picker! 2026",
		"ÄÖÜ.package-name",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, value string) {
		normalized := normalizePackageIdentity(value)
		if normalizePackageIdentity(normalized) != normalized {
			t.Fatalf("normalization is not idempotent for %q -> %q", value, normalized)
		}
		for _, r := range normalized {
			if (r < 'a' || r > 'z') && (r < '0' || r > '9') {
				t.Fatalf("normalized identity contains unexpected rune %q: %q", r, normalized)
			}
		}
	})
}

func FuzzSplitPackageKey(f *testing.F) {
	for _, seed := range []string{
		"winget:Git.Git",
		"choco:git.install",
		"store:9WZDNCRFHVN5",
		"winget:",
		"npm:leftpad",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, key string) {
		manager, id, err := splitPackageKey(key)
		if err != nil {
			return
		}
		if !isManagedPackageManager(manager) || id == "" {
			t.Fatalf("accepted invalid package key %q as manager=%q id=%q", key, manager, id)
		}
	})
}

func FuzzNumericVersionLooksNewer(f *testing.F) {
	for _, seed := range [][2]string{
		{"1.10", "1.2"},
		{"1.0.0-beta", "1.0.0"},
		{"2024.12", "2024.9"},
		{"unbekannt", "1.0.0"},
	} {
		f.Add(seed[0], seed[1])
	}
	f.Fuzz(func(t *testing.T, candidate, current string) {
		_ = numericVersionLooksNewer(candidate, current)
		_ = numericVersionLooksNewer(current, candidate)
	})
}
