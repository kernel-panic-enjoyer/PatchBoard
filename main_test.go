package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseChocoListIgnoresWarnings(t *testing.T) {
	output := `
This is try 1/3. Retrying after 300 milliseconds.
Error converted to warning:
git|2.54.0
python314|3.14.5
`
	got := parseChocoList(output)
	if len(got) != 2 {
		t.Fatalf("expected 2 packages, got %d: %#v", len(got), got)
	}
	if got[0].ID != "git" || got[1].ID != "python314" {
		t.Fatalf("unexpected packages: %#v", got)
	}
}

func TestParseChocoOutdated(t *testing.T) {
	got := parseChocoOutdated("git|2.53.0|2.54.0|false\npython314|3.14.4|3.14.5|false\n")
	if got["git"] != "2.54.0" || got["python314"] != "3.14.5" {
		t.Fatalf("unexpected outdated map: %#v", got)
	}
}

func TestParseLocalizedWingetTable(t *testing.T) {
	output := `
Name      ID              Version  Verfügbar Quelle
---------------------------------------------------
Git       Git.Git         2.53.0   2.54.0    winget
Zed       Zed.Zed         0.233.10           winget
`
	got := parseWingetTable(output)
	if len(got) != 2 {
		t.Fatalf("expected 2 packages, got %d: %#v", len(got), got)
	}
	if got[0].ID != "Git.Git" || got[0].AvailableVersion != "2.54.0" {
		t.Fatalf("unexpected first package: %#v", got[0])
	}
	if got[1].Source != "winget" {
		t.Fatalf("expected source winget, got %#v", got[1])
	}
}

func TestParseWingetSearchTableWithMatchColumn(t *testing.T) {
	output := `
Name                         ID                                 Version   Übereinstimmung Quelle
-----------------------------------------------------------------------------------------------
DragonframeLicenseManager    DZEDSystems.DragonframeLicenseMa… 3.0.3                    winget
Zed                          ZedIndustries.Zed                  1.6.3     Tag: zed       winget
`
	got := parseWingetTable(output)
	if len(got) != 2 {
		t.Fatalf("expected 2 packages, got %d: %#v", len(got), got)
	}
	if !isTruncatedID(got[0].ID) {
		t.Fatalf("expected truncated id: %#v", got[0])
	}
	if got[1].Source != "winget" {
		t.Fatalf("expected resilient source parsing, got %#v", got[1])
	}
}

func TestParseWingetExport(t *testing.T) {
	output := `{
  "Sources": [{
    "Packages": [{"PackageIdentifier": "ZedIndustries.Zed", "Version": "1.5.4"}],
    "SourceDetails": {"Name": "winget"}
  }]
}`
	got := parseWingetExport(output)
	if len(got) != 1 || got[0].ID != "ZedIndustries.Zed" || got[0].Version != "1.5.4" || got[0].Source != "winget" {
		t.Fatalf("unexpected export parse: %#v", got)
	}
}

func TestMergeWingetExportWithTruncatedTableIDs(t *testing.T) {
	exported := []Package{
		{ID: "Microsoft.VCRedist.2015+.x64", Name: "Microsoft.VCRedist.2015+.x64", Version: "14.51.36231.0", Manager: "winget", Source: "winget"},
		{ID: "ZedIndustries.Zed", Name: "ZedIndustries.Zed", Version: "1.5.4", Manager: "winget", Source: "winget"},
	}
	table := []Package{
		{ID: "Microsoft.VCRedist.2015+.x…", Name: "Microsoft Visual C++ 2015-2026 Redistributable", Version: "14.51.36231.0", AvailableVersion: "14.51.36247.0", Manager: "winget", Source: "winget"},
		{ID: "ZedIndustries.Zed", Name: "Zed", Version: "1.5.4", AvailableVersion: "1.6.3", Manager: "winget", Source: "winget"},
	}
	got := mergeWingetExportWithTable(exported, table)
	byID := map[string]Package{}
	for _, pkg := range got {
		byID[pkg.ID] = pkg
	}
	if byID["Microsoft.VCRedist.2015+.x64"].AvailableVersion != "14.51.36247.0" {
		t.Fatalf("truncated id did not merge: %#v", byID["Microsoft.VCRedist.2015+.x64"])
	}
	if byID["ZedIndustries.Zed"].Name != "Zed" {
		t.Fatalf("display name did not merge: %#v", byID["ZedIndustries.Zed"])
	}
}

func TestParseRegQuery(t *testing.T) {
	output := `
HKEY_LOCAL_MACHINE\Software\Microsoft\Windows\CurrentVersion\Uninstall\Git_is1
    DisplayName    REG_SZ    Git
    DisplayVersion    REG_SZ    2.54.0
    Publisher    REG_SZ    The Git Development Community
    InstallLocation    REG_SZ    C:\Program Files\Git
`
	got := parseRegQuery(output, "HKLM")
	if len(got) != 1 {
		t.Fatalf("expected one app, got %#v", got)
	}
	if got[0].Name != "Git" || got[0].RegistryHive != "HKLM" || got[0].Source != "registry" {
		t.Fatalf("unexpected registry app: %#v", got[0])
	}
}

func TestDiffSnapshot(t *testing.T) {
	previous := map[string]ScannedApp{
		"winget:git.git": {Key: "winget:git.git", Name: "Git", FirstSeen: "old"},
	}
	current := []ScannedApp{
		{Key: "winget:git.git", Name: "Git"},
		{Key: "winget:zed.zed", Name: "Zed"},
	}
	currentMap, newApps, removed, baseline := diffSnapshot(current, previous)
	if baseline {
		t.Fatal("expected non-baseline diff")
	}
	if len(newApps) != 1 || newApps[0].Key != "winget:zed.zed" {
		t.Fatalf("unexpected new apps: %#v", newApps)
	}
	if len(removed) != 0 {
		t.Fatalf("unexpected removed apps: %#v", removed)
	}
	if currentMap["winget:git.git"].FirstSeen != "old" {
		t.Fatalf("first_seen was not preserved: %#v", currentMap["winget:git.git"])
	}
}

func TestStateDirOverride(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("UPDATER_STATE_DIR", dir)
	got, err := stateDir()
	if err != nil {
		t.Fatal(err)
	}
	if got != dir {
		t.Fatalf("expected override %s, got %s", dir, got)
	}
}

func TestManagerCommandOverride(t *testing.T) {
	t.Setenv("UPDATER_WINGET_PATH", filepath.Join("C:", "Tools", "winget.exe"))
	got := managerCommand("winget", "--version")
	if len(got) != 2 || got[0] != filepath.Join("C:", "Tools", "winget.exe") || got[1] != "--version" {
		t.Fatalf("unexpected manager command: %#v", got)
	}
}

func TestIntegrationInventoryAndScan(t *testing.T) {
	if os.Getenv("UPDATER_INTEGRATION") != "1" {
		t.Skip("set UPDATER_INTEGRATION=1 to run real winget/choco integration test")
	}
	inventory := getInventory()
	if !inventory.Managers["winget"].Available {
		t.Fatalf("winget unavailable: %#v", inventory.Managers["winget"])
	}
	if !inventory.Managers["choco"].Available {
		t.Fatalf("choco unavailable: %#v", inventory.Managers["choco"])
	}
	var wingetCount, chocoCount, updateCount int
	for _, pkg := range inventory.Packages {
		switch pkg.Manager {
		case "winget":
			wingetCount++
			if isTruncatedID(pkg.ID) {
				t.Fatalf("inventory contained truncated winget id: %#v", pkg)
			}
		case "choco":
			chocoCount++
		}
		if pkg.UpdateAvailable {
			updateCount++
		}
	}
	if wingetCount == 0 || chocoCount == 0 {
		t.Fatalf("expected both managers to list packages, winget=%d choco=%d", wingetCount, chocoCount)
	}
	if updateCount == 0 {
		t.Fatalf("expected at least one available update in this environment")
	}
	scan := scanAppsAndStore()
	if len(scan.Errors) > 0 {
		t.Fatalf("scan errors: %#v", scan.Errors)
	}
	if scan.SourceCounts["registry"] == 0 || scan.SourceCounts["winget"] == 0 {
		t.Fatalf("expected registry and winget scan counts, got %#v", scan.SourceCounts)
	}
}
