package updater

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const selfUpdateTestReleaseCommit = "0123456789abcdef0123456789abcdef01234567"
const selfUpdateTestSigningKeyID = "test-key-2026"

var selfUpdateTestSigningPrivateKey ed25519.PrivateKey

func init() {
	publicKey, privateKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		panic(err)
	}
	selfUpdateTestSigningPrivateKey = privateKey
	appUpdateSigningKeyID = selfUpdateTestSigningKeyID
	appUpdateTrustedSigningKeys = selfUpdateTestSigningKeyID + "=" + base64.StdEncoding.EncodeToString(publicKey)
}

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
		"target_commitish": "0123456789abcdef0123456789abcdef01234567",
		"draft": false,
		"prerelease": false,
		"html_url": "https://github.example/release",
		"assets": [
			{"name":"PatchBoard.exe","browser_download_url":"https://github.example/app.exe","size":1234},
			{"name":"PatchBoard.metadata.json","browser_download_url":"https://github.example/app.metadata.json","size":321},
			{"name":"PatchBoard.exe.sha256","browser_download_url":"https://github.example/app.exe.sha256","size":64},
			{"name":"PatchBoard.update-signature.json","browser_download_url":"https://github.example/app.signature.json","size":512}
		]
	}`), "0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if !status.Available || status.LatestVersion != "0.0.2" || status.ExecutableURL == "" || status.SHA256URL == "" || status.ReleaseTargetCommit != selfUpdateTestReleaseCommit {
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

func TestParseGitHubReleaseRequiresExactTargetCommit(t *testing.T) {
	for _, targetCommitish := range []string{"", "main", "0123456"} {
		releaseJSON := fmt.Sprintf(`{
			"tag_name": "v0.0.2",
			"target_commitish": %q,
			"draft": false,
			"prerelease": false,
			"assets": [
				{"name":"PatchBoard.exe","browser_download_url":"https://github.example/app.exe","size":1234},
				{"name":"PatchBoard.metadata.json","browser_download_url":"https://github.example/app.metadata.json","size":321},
				{"name":"PatchBoard.exe.sha256","browser_download_url":"https://github.example/app.exe.sha256","size":64},
				{"name":"PatchBoard.update-signature.json","browser_download_url":"https://github.example/app.signature.json","size":512}
			]
		}`, targetCommitish)
		status, err := parseGitHubRelease([]byte(releaseJSON), "0.0.1")
		if err != nil {
			t.Fatal(err)
		}
		if status.Available || status.ReleaseTargetCommit != "" {
			t.Fatalf("release without exact target commit must not be available: %#v", status)
		}
		if status.IncompatibleReason == "" || !strings.Contains(status.IncompatibleReason, "exact target commit") {
			t.Fatalf("expected target-commit incompatible reason, got %#v", status)
		}
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
	currentExecutable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(currentExecutable)
	if err != nil {
		t.Fatal(err)
	}
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
		case "/app.signature.json":
			_, _ = w.Write([]byte(selfUpdateSignatureFixture(metadataText, payload)))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	dir := t.TempDir()
	artifact, err := downloadSelfUpdateArtifact(context.Background(), server.Client(), AppUpdateStatus{
		Available:           true,
		LatestVersion:       "0.0.2",
		ReleaseTargetCommit: selfUpdateTestReleaseCommit,
		ExecutableURL:       server.URL + "/app.exe",
		MetadataURL:         server.URL + "/app.metadata.json",
		SHA256URL:           server.URL + "/app.exe.sha256",
		SignatureURL:        server.URL + "/app.signature.json",
		ExecutableSize:      int64(len(payload)),
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
		Available:           true,
		LatestVersion:       "0.0.2",
		ReleaseTargetCommit: selfUpdateTestReleaseCommit,
		ExecutableURL:       server.URL + "/app.exe",
		MetadataURL:         server.URL + "/app.metadata.json",
		SHA256URL:           server.URL + "/app.exe.sha256",
		SignatureURL:        server.URL + "/app.signature.json",
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
		{
			name:     "wrong release commit",
			metadata: selfUpdateMetadataFixture(payload, "0.0.2", map[string]string{"commit": "fedcba9876543210fedcba9876543210fedcba98"}),
			want:     "metadata commit",
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
				Available:           true,
				LatestVersion:       "0.0.2",
				ReleaseTargetCommit: selfUpdateTestReleaseCommit,
				ExecutableURL:       server.URL + "/app.exe",
				MetadataURL:         server.URL + "/app.metadata.json",
				SHA256URL:           server.URL + "/app.exe.sha256",
				SignatureURL:        server.URL + "/app.signature.json",
				ExecutableSize:      int64(len(payload)),
			}, t.TempDir())
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected metadata rejection containing %q, got %v", tc.want, err)
			}
		})
	}
}

func TestDownloadSelfUpdateRequiresReleaseTargetCommit(t *testing.T) {
	payload := []byte("new executable")
	sum := sha256.Sum256(payload)
	shaText := hex.EncodeToString(sum[:]) + "  PatchBoard.exe\n"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/app.exe":
			_, _ = w.Write(payload)
		case "/app.metadata.json":
			_, _ = w.Write([]byte(selfUpdateMetadataFixture(payload, "0.0.2", nil)))
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
		SignatureURL:   server.URL + "/app.signature.json",
		ExecutableSize: int64(len(payload)),
	}, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "release target commit") {
		t.Fatalf("expected release target commit rejection, got %v", err)
	}
}

func TestSelfUpdateSignatureRejectsUntrustedAndTamperedArtifacts(t *testing.T) {
	payload := []byte("new executable")
	metadata := selfUpdateMetadataFixture(payload, "0.0.2", nil)
	signature, err := decodeSelfUpdateSignature([]byte(selfUpdateSignatureFixture(metadata, payload)))
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(payload)
	artifactSHA256 := hex.EncodeToString(digest[:])
	if err := validateSelfUpdateSignature(signature, []byte(metadata), artifactSHA256); err != nil {
		t.Fatal(err)
	}
	signature.ArtifactSHA256 = strings.Repeat("0", 64)
	if err := validateSelfUpdateSignature(signature, []byte(metadata), artifactSHA256); err == nil || !strings.Contains(err.Error(), "artifact digest") {
		t.Fatalf("expected artifact signature rejection, got %v", err)
	}
	originalKeys := appUpdateTrustedSigningKeys
	originalKeyID := appUpdateSigningKeyID
	t.Cleanup(func() {
		appUpdateTrustedSigningKeys = originalKeys
		appUpdateSigningKeyID = originalKeyID
	})
	appUpdateTrustedSigningKeys = ""
	if _, err := trustedSelfUpdateSigningKeys(); err == nil || !strings.Contains(err.Error(), "no trusted") {
		t.Fatalf("expected missing trust configuration rejection, got %v", err)
	}
}

func TestSelfUpdateDownloadURLRejectsUntrustedAndInsecureOrigins(t *testing.T) {
	for _, rawURL := range []string{
		"http://github.com/kernel-panic-enjoyer/PatchBoard/releases/download/v1/PatchBoard.exe",
		"https://example.invalid/PatchBoard.exe",
		"https://trusted@example.invalid/PatchBoard.exe",
	} {
		if err := validateSelfUpdateDownloadURL(rawURL); err == nil {
			t.Fatalf("self-update URL %q was accepted", rawURL)
		}
	}
	if err := validateSelfUpdateDownloadURL("https://github-releases.githubusercontent.com/PatchBoard.exe"); err != nil {
		t.Fatalf("trusted GitHub release origin was rejected: %v", err)
	}
}

func TestApplySelfUpdateCopiesExecutableAndKeepsBackup(t *testing.T) {
	setProcessExecutionModeForTest(t, processModeInteractive)
	tempRoot := t.TempDir()
	t.Setenv("UPDATER_TEMP_DIR", tempRoot)
	stagingDir := filepath.Join(tempRoot, "self-update")
	if err := os.MkdirAll(stagingDir, 0o700); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(stagingDir, "PatchBoard-new.exe")
	target := filepath.Join(t.TempDir(), "PatchBoard.exe")
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

func TestCopyAndVerifySelfUpdateSourceUsesAlreadyOpenedHandle(t *testing.T) {
	directory := t.TempDir()
	sourcePath := filepath.Join(directory, "PatchBoard-update.exe")
	trustedPayload := []byte("trusted update")
	replacementPayload := []byte("substituted update")
	if err := os.WriteFile(sourcePath, trustedPayload, 0o600); err != nil {
		t.Fatal(err)
	}
	openedSource, err := os.Open(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	defer openedSource.Close()
	if err := os.WriteFile(sourcePath, replacementPayload, 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(trustedPayload)
	var copied bytes.Buffer
	err = copyAndVerifySelfUpdateSourceHandle(&copied, openedSource, hex.EncodeToString(digest[:]))
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("source mutation after open must fail closed, got %v with bytes %q", err, copied.Bytes())
	}
}

func selfUpdateMetadataFixture(payload []byte, version string, overrides map[string]string) string {
	sum := sha256.Sum256(payload)
	values := map[string]string{
		"artifact":       `"C:\\Program Files\\PatchBoard\\PatchBoard.exe"`,
		"commit":         fmt.Sprintf("%q", selfUpdateTestReleaseCommit),
		"sha256":         fmt.Sprintf("%q", hex.EncodeToString(sum[:])),
		"version":        fmt.Sprintf("%q", version),
		"bytes":          fmt.Sprintf("%d", len(payload)),
		"dirty":          "false",
		"license":        fmt.Sprintf("%q", appLicenseID),
		"repository":     fmt.Sprintf("%q", appRepositoryURL),
		"signing_key_id": fmt.Sprintf("%q", selfUpdateTestSigningKeyID),
		"go_version":     fmt.Sprintf("%q", runtime.Version()),
		"goos":           fmt.Sprintf("%q", runtime.GOOS),
		"goarch":         fmt.Sprintf("%q", runtime.GOARCH),
		"stripped":       "true",
		"unstripped":     "false",
	}
	for key, value := range overrides {
		switch key {
		case "artifact", "commit", "sha256", "version", "license", "repository", "signing_key_id", "go_version", "goos", "goarch":
			values[key] = fmt.Sprintf("%q", value)
		default:
			values[key] = value
		}
	}
	return fmt.Sprintf(`{
		"artifact": %s,
		"commit": %s,
		"sha256": %s,
		"version": %s,
		"bytes": %s,
		"dirty": %s,
		"license": %s,
		"repository": %s,
		"signing_key_id": %s,
		"go_version": %s,
		"goos": %s,
		"goarch": %s,
		"stripped": %s,
		"unstripped": %s
	}`, values["artifact"], values["commit"], values["sha256"], values["version"], values["bytes"], values["dirty"], values["license"], values["repository"], values["signing_key_id"], values["go_version"], values["goos"], values["goarch"], values["stripped"], values["unstripped"])
}

func selfUpdateSignatureFixture(metadata string, payload []byte) string {
	metadataDigest := sha256.Sum256([]byte(metadata))
	artifactDigest := sha256.Sum256(payload)
	metadataSHA256 := hex.EncodeToString(metadataDigest[:])
	artifactSHA256 := hex.EncodeToString(artifactDigest[:])
	signature := ed25519.Sign(selfUpdateTestSigningPrivateKey, selfUpdateSignatureMessage(metadataSHA256, artifactSHA256))
	data, err := json.Marshal(selfUpdateSignature{
		ProtocolVersion: selfUpdateSignatureProtocolVersion,
		KeyID:           selfUpdateTestSigningKeyID,
		MetadataSHA256:  metadataSHA256,
		ArtifactSHA256:  artifactSHA256,
		Signature:       base64.StdEncoding.EncodeToString(signature),
	})
	if err != nil {
		panic(err)
	}
	return string(data)
}
