package main

import (
	"strings"
	"testing"
)

func TestParseStoreSearchAndUpdates(t *testing.T) {
	searchOutput := `
Name             ID              Publisher
------------------------------------------
Microsoft To Do  9NBLGGH5R558    Microsoft
Codex            OpenAI.Codex    OpenAI
`
	search := parseStoreSearch(searchOutput)
	if len(search) != 2 {
		t.Fatalf("expected two store search results, got %#v", search)
	}
	if search[0].Manager != "store" || search[0].ActionBackend != "store-cli" || search[0].ID != "9NBLGGH5R558" {
		t.Fatalf("unexpected store search parse: %#v", search[0])
	}

	updateOutput := `
Name   ID            Current  Available
---------------------------------------
Codex  OpenAI.Codex  1.0.0    1.1.0
`
	updates := parseStoreUpdates(updateOutput)
	if updates["store:openai.codex"] != "1.1.0" {
		t.Fatalf("unexpected store updates parse: %#v", updates)
	}
}

func TestParseStoreSearchBoxTable(t *testing.T) {
	output := `
Searching for "codex"…

── Search Results for "codex" ──────────────────────────────────────────────────

┌────────────────┬────────────────┬────────────────┬───────────────┬───────────┐
│ Name           │ Product ID     │ Publisher      │ Categories    │ Price     │
├────────────────┼────────────────┼────────────────┼───────────────┼───────────┤
│ Codex          │ 9PLM9XGG6VKS   │ OpenAI         │ Entwicklungst │ Kostenlos │
│                │                │                │ ools          │           │
│ Codex (Beta)   │ 9N8CJ4W95TBZ   │ OpenAI         │ Entwicklungst │ Kostenlos │
└────────────────┴────────────────┴────────────────┴───────────────┴───────────┘
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

func TestParseStoreUpdatesBoxTable(t *testing.T) {
	output := `
Checking for updates…

── Updates available (1 found) ─────────────────────────────────────────────────

Store-managed update available
This Store app update can be installed immediately.
┌───────┬───────────┬───────────────┬────────────┐
│ Name  │ Publisher │ Version       │ Date       │
├───────┼───────────┼───────────────┼────────────┤
│ Codex │ OpenAI    │ 26.609.4994.0 │ 2026-06-13 │
└───────┴───────────┴───────────────┴────────────┘
`
	got := parseStoreUpdates(output)
	if got["store:codex"] != "26.609.4994.0" {
		t.Fatalf("expected Codex Store update from box table, got %#v", got)
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
		t.Fatalf("store search banner was parsed as a result: %#v", got[0])
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
