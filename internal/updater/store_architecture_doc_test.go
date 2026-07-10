package updater

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStoreArchitectureDocumentCapturesSafetyInvariants(t *testing.T) {
	documentPath := filepath.Join("..", "..", "docs", "architecture", "0001-store-update-detection-architecture.md")
	contents, err := os.ReadFile(documentPath)
	if err != nil {
		t.Fatalf("read Store architecture ADR: %v", err)
	}

	document := strings.Join(strings.Fields(string(contents)), " ")
	requiredPhrases := []string{
		"Installed Store identity is `(user SID, package family name)`.",
		"Display names, localized names, fuzzy matches, normalized punctuation-free strings, and search result rank are never Store identity.",
		"Unknown never becomes Current because a provider failed",
		"Published Store assessments are overlays",
		"Recovered, stale, or incomplete evidence is retained for diagnostics",
		"same-binary worker",
		"kill-on-close",
		"PackageCatalog events do not prove",
		"post-action verification",
	}
	for _, phrase := range requiredPhrases {
		if !strings.Contains(document, phrase) {
			t.Fatalf("Store architecture ADR is missing invariant phrase %q", phrase)
		}
	}
}
