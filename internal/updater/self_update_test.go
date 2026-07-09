package updater

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompareAppVersions(t *testing.T) {
	tests := []struct {
		name  string
		left  string
		right string
		want  int
	}{
		{name: "newer", left: "0.0.2", right: "0.0.1", want: 1},
		{name: "same with v prefix", left: "v0.0.1", right: "0.0.1", want: 0},
		{name: "older", left: "0.0.1", right: "0.1.0", want: -1},
		{name: "dev version is older", left: "0.0.0-dev", right: "0.0.1", want: -1},
		{name: "malformed is rejected low", left: "not-a-version", right: "0.0.1", want: -1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := compareAppVersions(test.left, test.right)
			if got != test.want {
				t.Fatalf("compareAppVersions(%q, %q)=%d, want %d", test.left, test.right, got, test.want)
			}
		})
	}
}

func TestParseGitHubReleaseRequiresStableNewerAssets(t *testing.T) {
	status, err := parseGitHubRelease([]byte(`{
		"tag_name": "v0.0.2",
		"draft": false,
		"prerelease": false,
		"html_url": "https://github.example/release",
		"assets": [
			{"name":"PatchBoard.exe","browser_download_url":"https://github.example/app.exe","size":1234},
			{"name":"PatchBoard.metadata.json","browser_download_url":"https://github.example/app.metadata.json","size":321},
			{"name":"PatchBoard.exe.sha256","browser_download_url":"https://github.example/app.exe.sha256","size":64}
		]
	}`), "0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if !status.Available || status.LatestVersion != "0.0.2" || status.ExecutableURL == "" || status.SHA256URL == "" {
		t.Fatalf("release was not parsed as available with required assets: %#v", status)
	}
}

func TestParseGitHubReleaseIgnoresPrereleaseAndSameVersion(t *testing.T) {
	for _, body := range []string{
		`{"tag_name":"v0.0.2","prerelease":true,"assets":[]}`,
		`{"tag_name":"v0.0.1","draft":false,"prerelease":false,"assets":[]}`,
	} {
		status, err := parseGitHubRelease([]byte(body), "0.0.1")
		if err != nil {
			t.Fatal(err)
		}
		if status.Available {
			t.Fatalf("release should not be available: %#v", status)
		}
	}
}

func TestParseGitHubReleaseTreatsMissingAssetsAsIncompatible(t *testing.T) {
	status, err := parseGitHubRelease([]byte(`{
		"tag_name": "v0.0.2",
		"assets": [{"name":"PatchBoard.exe","browser_download_url":"https://github.example/app.exe","size":1234}]
	}`), "0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if status.Available {
		t.Fatalf("missing release assets must not become available: %#v", status)
	}
	if status.IncompatibleReason == "" || !strings.Contains(status.IncompatibleReason, "PatchBoard.metadata.json") || !strings.Contains(status.IncompatibleReason, "PatchBoard.exe.sha256") {
		t.Fatalf("expected incompatible missing-asset reason, got %#v", status)
	}
}

func TestParseGitHubReleaseTreatsLegacyRenamedAssetsAsIncompatible(t *testing.T) {
	status, err := parseGitHubRelease([]byte(`{
		"tag_name": "v0.1.7",
		"draft": false,
		"prerelease": false,
		"assets": [
			{"name":"WindowsUpdaterWebUI.exe","browser_download_url":"https://github.example/old.exe","size":1234},
			{"name":"WindowsUpdaterWebUI.metadata.json","browser_download_url":"https://github.example/old.metadata.json","size":321},
			{"name":"WindowsUpdaterWebUI.exe.sha256","browser_download_url":"https://github.example/old.exe.sha256","size":64}
		]
	}`), "0.0.0-dev")
	if err != nil {
		t.Fatal(err)
	}
	if status.Available || status.ExecutableURL != "" || status.SHA256URL != "" {
		t.Fatalf("legacy release assets must not be actionable for PatchBoard: %#v", status)
	}
	if status.IncompatibleReason == "" || !strings.Contains(status.IncompatibleReason, "PatchBoard.exe") {
		t.Fatalf("expected incompatible legacy-asset reason, got %#v", status)
	}
}

func TestGitHubReleaseCheckerRejectsOversizedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", maxGitHubReleaseResponseBytes+1)))
	}))
	defer server.Close()

	checker := GitHubReleaseChecker{Client: server.Client(), LatestReleaseURL: server.URL}
	status, err := checker.Check(context.Background(), "0.0.1")
	if err == nil || !strings.Contains(err.Error(), "release response exceeds") {
		t.Fatalf("expected oversized response error, got status=%#v err=%v", status, err)
	}
}

func TestDownloadSelfUpdateVerifiesChecksum(t *testing.T) {
	payload := []byte("new executable")
	sum := sha256.Sum256(payload)
	shaText := hex.EncodeToString(sum[:]) + "  PatchBoard.exe\n"
	metadataText := selfUpdateMetadataFixture(payload, "0.0.2", nil)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/app.exe":
			_, _ = w.Write(payload)
		case "/app.metadata.json":
			_, _ = w.Write([]byte(metadataText))
		case "/app.exe.sha256":
			_, _ = w.Write([]byte(shaText))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	dir := t.TempDir()
	artifact, err := downloadSelfUpdateArtifact(context.Background(), server.Client(), AppUpdateStatus{
		Available:      true,
		LatestVersion:  "0.0.2",
		ExecutableURL:  server.URL + "/app.exe",
		MetadataURL:    server.URL + "/app.metadata.json",
		SHA256URL:      server.URL + "/app.exe.sha256",
		ExecutableSize: int64(len(payload)),
	}, dir)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(artifact.Path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(payload) || artifact.SHA256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("downloaded artifact mismatch: %#v data=%q", artifact, data)
	}
}

func TestDownloadSelfUpdateRejectsChecksumMismatch(t *testing.T) {
	payload := []byte("new executable")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/app.exe":
			_, _ = w.Write(payload)
		case "/app.metadata.json":
			_, _ = w.Write([]byte(selfUpdateMetadataFixture(payload, "0.0.2", nil)))
		case "/app.exe.sha256":
			_, _ = w.Write([]byte(strings.Repeat("0", 64)))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	_, err := downloadSelfUpdateArtifact(context.Background(), server.Client(), AppUpdateStatus{
		Available:     true,
		LatestVersion: "0.0.2",
		ExecutableURL: server.URL + "/app.exe",
		MetadataURL:   server.URL + "/app.metadata.json",
		SHA256URL:     server.URL + "/app.exe.sha256",
	}, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("expected checksum mismatch, got %v", err)
	}
}

func TestDownloadSelfUpdateRejectsMetadataMismatch(t *testing.T) {
	payload := []byte("new executable")
	sum := sha256.Sum256(payload)
	shaText := hex.EncodeToString(sum[:]) + "  PatchBoard.exe\n"
	cases := []struct {
		name     string
		metadata string
		want     string
	}{
		{
			name:     "sha mismatch",
			metadata: selfUpdateMetadataFixture(payload, "0.0.2", map[string]string{"sha256": strings.Repeat("1", 64)}),
			want:     "metadata SHA-256 mismatch",
		},
		{
			name:     "dirty build",
			metadata: selfUpdateMetadataFixture(payload, "0.0.2", map[string]string{"dirty": "true"}),
			want:     "dirty release build",
		},
		{
			name:     "wrong repository",
			metadata: selfUpdateMetadataFixture(payload, "0.0.2", map[string]string{"repository": "https://github.example/other/repo"}),
			want:     "metadata repository",
		},
		{
			name:     "wrong version",
			metadata: selfUpdateMetadataFixture(payload, "9.9.9", nil),
			want:     "metadata version",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/app.exe":
					_, _ = w.Write(payload)
				case "/app.metadata.json":
					_, _ = w.Write([]byte(tc.metadata))
				case "/app.exe.sha256":
					_, _ = w.Write([]byte(shaText))
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()

			_, err := downloadSelfUpdateArtifact(context.Background(), server.Client(), AppUpdateStatus{
				Available:      true,
				LatestVersion:  "0.0.2",
				ExecutableURL:  server.URL + "/app.exe",
				MetadataURL:    server.URL + "/app.metadata.json",
				SHA256URL:      server.URL + "/app.exe.sha256",
				ExecutableSize: int64(len(payload)),
			}, t.TempDir())
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected metadata rejection containing %q, got %v", tc.want, err)
			}
		})
	}
}

func TestApplySelfUpdateCopiesExecutableAndKeepsBackup(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "PatchBoard-new.exe")
	target := filepath.Join(dir, "PatchBoard.exe")
	if err := os.WriteFile(source, []byte("new"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("new"))
	err := replaceExecutableForSelfUpdate(selfUpdateApplyRequest{
		SourcePath:     source,
		TargetPath:     target,
		ExpectedSHA256: hex.EncodeToString(sum[:]),
	})
	if err != nil {
		t.Fatal(err)
	}
	targetData, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	backupData, err := os.ReadFile(target + ".bak")
	if err != nil {
		t.Fatal(err)
	}
	if string(targetData) != "new" || string(backupData) != "old" {
		t.Fatalf("unexpected replacement target=%q backup=%q", targetData, backupData)
	}
}

func selfUpdateMetadataFixture(payload []byte, version string, overrides map[string]string) string {
	sum := sha256.Sum256(payload)
	values := map[string]string{
		"artifact":   `"C:\\Program Files\\PatchBoard\\PatchBoard.exe"`,
		"sha256":     fmt.Sprintf("%q", hex.EncodeToString(sum[:])),
		"version":    fmt.Sprintf("%q", version),
		"bytes":      fmt.Sprintf("%d", len(payload)),
		"dirty":      "false",
		"license":    fmt.Sprintf("%q", appLicenseID),
		"repository": fmt.Sprintf("%q", appRepositoryURL),
	}
	for key, value := range overrides {
		switch key {
		case "artifact", "sha256", "version", "license", "repository":
			values[key] = fmt.Sprintf("%q", value)
		default:
			values[key] = value
		}
	}
	return fmt.Sprintf(`{
		"artifact": %s,
		"sha256": %s,
		"version": %s,
		"bytes": %s,
		"dirty": %s,
		"license": %s,
		"repository": %s
	}`, values["artifact"], values["sha256"], values["version"], values["bytes"], values["dirty"], values["license"], values["repository"])
}
