package updater

import (
	"os"
	"strings"
	"testing"
)

func TestReleaseWorkflowBuildsAndPublishesWindowsExecutable(t *testing.T) {
	data, err := os.ReadFile("../../.github/workflows/release.yml")
	if err != nil {
		t.Fatal(err)
	}
	workflow := string(data)
	for _, expected := range []string{
		"workflow_dispatch:",
		"contents: write",
		"windows-latest",
		"-Version",
		"WindowsUpdaterWebUI.exe.sha256",
		"WindowsUpdaterWebUI.metadata.json",
		"gh release create",
		"v${{ inputs.version }}",
	} {
		if !strings.Contains(workflow, expected) {
			t.Fatalf("release workflow missing %q", expected)
		}
	}
}
