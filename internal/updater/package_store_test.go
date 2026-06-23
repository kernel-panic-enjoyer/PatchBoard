package updater

import (
	"strings"
	"testing"
)

func TestParseStoreSearch(t *testing.T) {
	output := `
Name             ID              Publisher
------------------------------------------
Microsoft To Do  9NBLGGH5R558    Microsoft
Codex            OpenAI.Codex    OpenAI
`
	search := parseStoreSearch(output)
	if len(search) != 2 {
		t.Fatalf("expected two Store search results, got %#v", search)
	}
	if search[0].Manager != managerStore || search[0].ActionBackend != backendStoreCLI || search[0].ID != "9NBLGGH5R558" {
		t.Fatalf("unexpected Store search parse: %#v", search[0])
	}
}

func TestStoreUpdatesCommandIsNonApplying(t *testing.T) {
	command := strings.Join(storeUpdatesCommand(), " ")
	for _, expected := range []string{"store", "updates", "--apply", "false"} {
		if !strings.Contains(command, expected) {
			t.Fatalf("Store update discovery command missing %q: %s", expected, command)
		}
	}
}

func TestParseStoreSearchPipeTable(t *testing.T) {
	output := `
| Name         | Product ID   | Publisher | Categories        | Price |
|--------------|--------------|-----------|-------------------|-------|
| Codex        | 9PLM9XGG6VKS | OpenAI    | Developer tools   | Free  |
| Codex (Beta) | 9N8CJ4W95TBZ | OpenAI    | Developer tools   | Free  |
`
	got := parseStoreSearch(output)
	if len(got) != 2 {
		t.Fatalf("expected two parsed Store rows, got %#v", got)
	}
	if got[0].Name != "Codex" || got[0].ID != "9PLM9XGG6VKS" || got[0].ActionBackend != backendStoreCLI {
		t.Fatalf("unexpected first Store row: %#v", got[0])
	}
	if got[1].Name != "Codex (Beta)" || got[1].ID != "9N8CJ4W95TBZ" {
		t.Fatalf("unexpected second Store row: %#v", got[1])
	}
}

func TestParseStoreSearchSkipsBannerLines(t *testing.T) {
	output := `
Application Compatibility Enhancements
-- Search Results for
"Application Compatibility Enhancements"
--------------------------------------
Name                                    ID                                     Version
------------------------------------------------------------------------------------
Application Compatibility Enhancements  Microsoft.ApplicationCompatibility     1.2511.9.0
`
	got := parseStoreSearch(output)
	if len(got) != 1 {
		t.Fatalf("expected one parsed search result, got %#v", got)
	}
	if got[0].ID != "Microsoft.ApplicationCompatibility" || strings.Contains(got[0].ID, "Search Results") {
		t.Fatalf("Store search banner was parsed as a result: %#v", got[0])
	}
}

func TestParseStoreHelpVersionIgnoresUsageBanner(t *testing.T) {
	output := `Usage: store <command> [options]

Commands:
  install
  search
`
	if got := parseStoreHelpVersion(output); got != "" {
		t.Fatalf("usage banner should not be treated as a version, got %q", got)
	}
	if got := parseStoreHelpVersion("Store CLI version 1.2.3"); got != "Store CLI version 1.2.3" {
		t.Fatalf("expected version-like line to be preserved, got %q", got)
	}
}
