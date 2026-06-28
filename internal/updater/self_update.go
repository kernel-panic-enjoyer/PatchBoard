package updater

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	windowsUpdateUtilityOwner      = "kernel-panic-enjoyer"
	windowsUpdateUtilityRepository = "WindowsUpdateUtility"

	releaseAssetExecutable = "WindowsUpdaterWebUI.exe"
	releaseAssetMetadata   = "WindowsUpdaterWebUI.metadata.json"
	releaseAssetSHA256     = "WindowsUpdaterWebUI.exe.sha256"

	maxGitHubReleaseResponseBytes = 512 * 1024
	maxSelfUpdateExecutableBytes  = 100 * 1024 * 1024
	maxSelfUpdateChecksumBytes    = 4 * 1024
	appUpdateCheckTimeout         = 8 * time.Second
	appUpdateCacheTTL             = 30 * time.Minute
	selfUpdateApplyTimeout        = 2 * time.Minute
)

var sha256LinePattern = regexp.MustCompile(`(?i)\b[0-9a-f]{64}\b`)

type appUpdateChecker interface {
	Check(context.Context, string) (AppUpdateStatus, error)
}

type AppUpdateStatus struct {
	CurrentVersion string `json:"current_version"`
	LatestVersion  string `json:"latest_version,omitempty"`
	LatestTag      string `json:"latest_tag,omitempty"`
	Available      bool   `json:"available"`
	CheckedAt      string `json:"checked_at,omitempty"`
	ReleaseURL     string `json:"release_url,omitempty"`
	Error          string `json:"error,omitempty"`

	ExecutableURL  string `json:"-"`
	MetadataURL    string `json:"-"`
	SHA256URL      string `json:"-"`
	ExecutableSize int64  `json:"-"`
}

type GitHubReleaseChecker struct {
	Client           *http.Client
	LatestReleaseURL string
}

type githubReleaseResponse struct {
	TagName    string               `json:"tag_name"`
	Draft      bool                 `json:"draft"`
	Prerelease bool                 `json:"prerelease"`
	HTMLURL    string               `json:"html_url"`
	Assets     []githubReleaseAsset `json:"assets"`
}

type githubReleaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

type selfUpdateArtifact struct {
	Path    string
	SHA256  string
	Version string
}

func defaultGitHubReleaseChecker() GitHubReleaseChecker {
	return GitHubReleaseChecker{
		Client: http.DefaultClient,
		LatestReleaseURL: fmt.Sprintf(
			"https://api.github.com/repos/%s/%s/releases/latest",
			windowsUpdateUtilityOwner,
			windowsUpdateUtilityRepository,
		),
	}
}

func (checker GitHubReleaseChecker) Check(ctx context.Context, currentVersion string) (AppUpdateStatus, error) {
	if checker.Client == nil {
		checker.Client = http.DefaultClient
	}
	url := strings.TrimSpace(checker.LatestReleaseURL)
	if url == "" {
		url = defaultGitHubReleaseChecker().LatestReleaseURL
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return AppUpdateStatus{CurrentVersion: currentVersion}, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "WindowsUpdaterWebUI/"+currentAppVersion())
	response, err := checker.Client.Do(request)
	if err != nil {
		return AppUpdateStatus{CurrentVersion: currentVersion}, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return AppUpdateStatus{CurrentVersion: currentVersion}, nil
	}
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return AppUpdateStatus{CurrentVersion: currentVersion}, fmt.Errorf("GitHub release check failed with HTTP %d", response.StatusCode)
	}
	data, err := readBounded(response.Body, maxGitHubReleaseResponseBytes, "release response")
	if err != nil {
		return AppUpdateStatus{CurrentVersion: currentVersion}, err
	}
	return parseGitHubRelease(data, currentVersion)
}

func parseGitHubRelease(data []byte, currentVersion string) (AppUpdateStatus, error) {
	status := AppUpdateStatus{CurrentVersion: currentVersion}
	var release githubReleaseResponse
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	if err := decoder.Decode(&release); err != nil {
		return status, err
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			err = errors.New("release response contains trailing JSON data")
		}
		return status, err
	}
	status.LatestTag = strings.TrimSpace(release.TagName)
	status.ReleaseURL = strings.TrimSpace(release.HTMLURL)
	latestVersion, ok := normalizeAppVersion(status.LatestTag)
	if !ok {
		if status.LatestTag == "" {
			return status, errors.New("release tag is missing")
		}
		return status, fmt.Errorf("release tag %q is not a supported semantic version", status.LatestTag)
	}
	status.LatestVersion = latestVersion
	if release.Draft || release.Prerelease || compareAppVersions(latestVersion, currentVersion) <= 0 {
		return status, nil
	}
	assets := map[string]githubReleaseAsset{}
	for _, asset := range release.Assets {
		assets[asset.Name] = asset
	}
	exe := assets[releaseAssetExecutable]
	metadata := assets[releaseAssetMetadata]
	checksum := assets[releaseAssetSHA256]
	if exe.BrowserDownloadURL == "" || metadata.BrowserDownloadURL == "" || checksum.BrowserDownloadURL == "" {
		return status, errors.New("newer release is missing required release assets")
	}
	if exe.Size > maxSelfUpdateExecutableBytes {
		return status, fmt.Errorf("release executable exceeds %d bytes", maxSelfUpdateExecutableBytes)
	}
	status.Available = true
	status.ExecutableURL = exe.BrowserDownloadURL
	status.MetadataURL = metadata.BrowserDownloadURL
	status.SHA256URL = checksum.BrowserDownloadURL
	status.ExecutableSize = exe.Size
	return status, nil
}

func downloadSelfUpdateArtifact(ctx context.Context, client *http.Client, status AppUpdateStatus, dir string) (selfUpdateArtifact, error) {
	if client == nil {
		client = http.DefaultClient
	}
	if !status.Available {
		return selfUpdateArtifact{}, errors.New("no application update is available")
	}
	if status.ExecutableURL == "" || status.SHA256URL == "" {
		return selfUpdateArtifact{}, errors.New("application update release assets are incomplete")
	}
	if status.ExecutableSize > maxSelfUpdateExecutableBytes {
		return selfUpdateArtifact{}, fmt.Errorf("release executable exceeds %d bytes", maxSelfUpdateExecutableBytes)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return selfUpdateArtifact{}, err
	}
	expected, err := downloadExpectedSHA256(ctx, client, status.SHA256URL)
	if err != nil {
		return selfUpdateArtifact{}, err
	}
	temp, err := os.CreateTemp(dir, "WindowsUpdaterWebUI-update-*.exe")
	if err != nil {
		return selfUpdateArtifact{}, err
	}
	path := temp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(path)
		}
	}()
	actual, err := downloadFileAndHash(ctx, client, status.ExecutableURL, temp, sha256.New())
	closeErr := temp.Close()
	if err != nil {
		return selfUpdateArtifact{}, err
	}
	if closeErr != nil {
		return selfUpdateArtifact{}, closeErr
	}
	if !strings.EqualFold(actual, expected) {
		return selfUpdateArtifact{}, fmt.Errorf("self-update checksum mismatch: got %s want %s", actual, expected)
	}
	_ = os.Chmod(path, 0o755)
	cleanup = false
	return selfUpdateArtifact{Path: path, SHA256: strings.ToLower(actual), Version: status.LatestVersion}, nil
}

func downloadExpectedSHA256(ctx context.Context, client *http.Client, url string) (string, error) {
	data, err := httpGetBounded(ctx, client, url, maxSelfUpdateChecksumBytes, "checksum")
	if err != nil {
		return "", err
	}
	match := sha256LinePattern.FindString(string(data))
	if match == "" {
		return "", errors.New("release checksum asset does not contain a SHA-256 digest")
	}
	return strings.ToLower(match), nil
}

func downloadFileAndHash(ctx context.Context, client *http.Client, url string, writer io.Writer, hash hash.Hash) (string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("User-Agent", "WindowsUpdaterWebUI/"+currentAppVersion())
	response, err := client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return "", fmt.Errorf("download failed with HTTP %d", response.StatusCode)
	}
	limited := &io.LimitedReader{R: response.Body, N: maxSelfUpdateExecutableBytes + 1}
	multi := io.MultiWriter(writer, hash)
	written, err := io.Copy(multi, limited)
	if err != nil {
		return "", err
	}
	if written > maxSelfUpdateExecutableBytes || limited.N == 0 {
		return "", fmt.Errorf("downloaded executable exceeds %d bytes", maxSelfUpdateExecutableBytes)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func httpGetBounded(ctx context.Context, client *http.Client, url string, limit int64, label string) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", "WindowsUpdaterWebUI/"+currentAppVersion())
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return nil, fmt.Errorf("%s download failed with HTTP %d", label, response.StatusCode)
	}
	return readBounded(response.Body, limit, label)
}

func readBounded(reader io.Reader, limit int64, label string) ([]byte, error) {
	limited := io.LimitReader(reader, limit+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%s exceeds %d bytes", label, limit)
	}
	return data, nil
}

func selfUpdateDownloadDir() (string, error) {
	dir, err := appTempDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "self-update"), nil
}
