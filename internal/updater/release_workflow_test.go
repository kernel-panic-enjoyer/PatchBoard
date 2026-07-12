package updater

import (
	"os"
	"path/filepath"
	"regexp"
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
		"-Strip",
		"PatchBoard.exe.sha256",
		"PatchBoard.metadata.json",
		"gh release create",
		"v${{ inputs.version }}",
	} {
		if !strings.Contains(workflow, expected) {
			t.Fatalf("release workflow missing %q", expected)
		}
	}
}

func TestWorkflowsPinActionsToCommitSHAs(t *testing.T) {
	workflowPaths, err := filepath.Glob("../../.github/workflows/*.yml")
	if err != nil {
		t.Fatal(err)
	}
	commitSHA := regexp.MustCompile(`^[0-9a-f]{40}$`)
	for _, workflowPath := range workflowPaths {
		workflowData, readErr := os.ReadFile(workflowPath)
		if readErr != nil {
			t.Fatal(readErr)
		}
		for _, line := range strings.Split(string(workflowData), "\n") {
			line = strings.TrimSpace(line)
			if !strings.HasPrefix(line, "uses: ") {
				continue
			}
			actionRef := strings.TrimSpace(strings.TrimPrefix(line, "uses: "))
			_, revision, found := strings.Cut(actionRef, "@")
			if !found {
				t.Fatalf("workflow action is missing a revision: %s", line)
			}
			revision = strings.TrimSpace(strings.SplitN(revision, "#", 2)[0])
			if !commitSHA.MatchString(revision) {
				t.Fatalf("workflow action must use a 40-character commit SHA, got %q in %s", revision, workflowPath)
			}
		}
	}
}

func TestBuildWorkspaceSupportsReleaseStrippingMetadata(t *testing.T) {
	data, err := os.ReadFile("../../dev/scripts/Build-Workspace.ps1")
	if err != nil {
		t.Fatal(err)
	}
	script := string(data)
	for _, expected := range []string{
		"[switch]$Strip",
		"'-s'",
		"'-w'",
		"stripped",
		"license",
		"GPL-3.0-only",
		"repository",
		"https://github.com/kernel-panic-enjoyer/PatchBoard",
		"Get-Command node -ErrorAction SilentlyContinue",
	} {
		if !strings.Contains(script, expected) {
			t.Fatalf("build script missing %q", expected)
		}
	}
	if strings.Contains(script, "$_ -eq 'node'") {
		t.Fatal("build script should not select a literal node command without checking PATH")
	}
}
