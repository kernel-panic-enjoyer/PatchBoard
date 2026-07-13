package updater

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	patchBoardGitHubOwner      = "kernel-panic-enjoyer"
	patchBoardGitHubRepository = "PatchBoard"

	releaseAssetExecutable = "PatchBoard.exe"
	releaseAssetMetadata   = "PatchBoard.metadata.json"
	releaseAssetSHA256     = "PatchBoard.exe.sha256"

	maxGitHubReleaseResponseBytes = 512 * 1024
	maxGitHubReleaseErrorBytes    = 8 * 1024
	maxSelfUpdateExecutableBytes  = 100 * 1024 * 1024
	maxSelfUpdateChecksumBytes    = 4 * 1024
	maxSelfUpdateMetadataBytes    = 64 * 1024
	appUpdateCheckTimeout         = 8 * time.Second
	selfUpdateApplyTimeout        = 2 * time.Minute
	utf8ByteOrderMark             = "\xef\xbb\xbf"
)

var sha256LinePattern = regexp.MustCompile(`(?i)\b[0-9a-f]{64}\b`)
var gitCommitPattern = regexp.MustCompile(`(?i)^[0-9a-f]{40}$`)

type appUpdateChecker interface {
	Check(context.Context, string) (AppUpdateStatus, error)
}

type AppUpdateStatus struct {
	CurrentVersion     string `json:"current_version"`
	LatestVersion      string `json:"latest_version,omitempty"`
	LatestTag          string `json:"latest_tag,omitempty"`
	Available          bool   `json:"available"`
	CheckedAt          string `json:"checked_at,omitempty"`
	ReleaseURL         string `json:"release_url,omitempty"`
	Error              string `json:"error,omitempty"`
	IncompatibleReason string `json:"incompatible_reason,omitempty"`

	ExecutableURL  string `json:"-"`
	MetadataURL    string `json:"-"`
	SHA256URL      string `json:"-"`
	ExecutableSize int64  `json:"-"`
	// ReleaseTargetCommit is hidden from the public status response but is
	// required during download verification so a release asset cannot claim to
	// be built from a different source revision than the GitHub release target.
	ReleaseTargetCommit string `json:"-"`
}

type GitHubReleaseChecker struct {
	Client               *http.Client
	LatestReleaseURL     string
	LatestReleasePageURL string
}

type githubReleaseResponse struct {
	TagName         string               `json:"tag_name"`
	TargetCommitish string               `json:"target_commitish"`
	Draft           bool                 `json:"draft"`
	Prerelease      bool                 `json:"prerelease"`
	HTMLURL         string               `json:"html_url"`
	Assets          []githubReleaseAsset `json:"assets"`
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

type selfUpdateReleaseMetadata struct {
	Artifact    string `json:"artifact"`
	Commit      string `json:"commit"`
	Dirty       bool   `json:"dirty"`
	GoVersion   string `json:"go_version"`
	GOOS        string `json:"goos"`
	GOARCH      string `json:"goarch"`
	CGOEnabled  string `json:"cgo_enabled"`
	Bytes       int64  `json:"bytes"`
	SHA256      string `json:"sha256"`
	Version     string `json:"version"`
	Stripped    bool   `json:"stripped"`
	Unstripped  bool   `json:"unstripped"`
	License     string `json:"license"`
	Repository  string `json:"repository"`
	LinkerFlags string `json:"linker_flags"`
	GeneratedAt string `json:"generated_at"`
}

func defaultGitHubReleaseChecker() GitHubReleaseChecker {
	return GitHubReleaseChecker{
		Client: http.DefaultClient,
		LatestReleaseURL: fmt.Sprintf(
			"https://api.github.com/repos/%s/%s/releases/latest",
			patchBoardGitHubOwner,
			patchBoardGitHubRepository,
		),
		LatestReleasePageURL: fmt.Sprintf(
			"https://github.com/%s/%s/releases/latest",
			patchBoardGitHubOwner,
			patchBoardGitHubRepository,
		),
	}
}

func (checker GitHubReleaseChecker) Check(ctx context.Context, currentVersion string) (AppUpdateStatus, error) {
	if checker.Client == nil {
		checker.Client = http.DefaultClient
	}
	status := AppUpdateStatus{CurrentVersion: currentVersion}
	latestReleaseURL := strings.TrimSpace(checker.LatestReleaseURL)
	if latestReleaseURL == "" {
		latestReleaseURL = defaultGitHubReleaseChecker().LatestReleaseURL
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, latestReleaseURL, nil)
	if err != nil {
		return status, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "PatchBoard/"+currentAppVersion())
	response, err := checker.Client.Do(request)
	if err != nil {
		return status, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return status, nil
	}
	if response.StatusCode < 200 || response.StatusCode > 299 {
		if isGitHubAPIRateLimited(response) {
			fallbackStatus, fallbackErr := checker.checkGitHubReleasePage(ctx, currentVersion)
			if fallbackErr == nil {
				return fallbackStatus, nil
			}
			return status, fmt.Errorf("GitHub release check is rate limited and release page fallback failed: %w", fallbackErr)
		}
		return status, fmt.Errorf("GitHub release check failed with HTTP %d", response.StatusCode)
	}
	releaseJSON, err := readBounded(response.Body, maxGitHubReleaseResponseBytes, "release response")
	if err != nil {
		return status, err
	}
	return parseGitHubRelease(releaseJSON, currentVersion)
}

func isGitHubAPIRateLimited(response *http.Response) bool {
	if response == nil {
		return false
	}
	if response.StatusCode == http.StatusTooManyRequests {
		return true
	}
	if response.StatusCode != http.StatusForbidden {
		return false
	}
	if strings.TrimSpace(response.Header.Get("X-RateLimit-Remaining")) == "0" {
		return true
	}
	body, err := readBounded(response.Body, maxGitHubReleaseErrorBytes, "GitHub release error response")
	return err == nil && strings.Contains(strings.ToLower(string(body)), "rate limit")
}

// checkGitHubReleasePage avoids GitHub's unauthenticated REST rate limit only
// after the API explicitly reports that limit. The page and asset URLs remain
// constrained to the configured GitHub repository and HTTPS origins.
func (checker GitHubReleaseChecker) checkGitHubReleasePage(ctx context.Context, currentVersion string) (AppUpdateStatus, error) {
	releaseTag, releaseURL, err := checker.resolveLatestReleaseTag(ctx)
	if err != nil {
		return AppUpdateStatus{CurrentVersion: currentVersion}, err
	}
	latestVersion, ok := normalizeAppVersion(releaseTag)
	if !ok {
		return AppUpdateStatus{CurrentVersion: currentVersion}, fmt.Errorf("release tag %q is not a supported semantic version", releaseTag)
	}
	metadataURL, err := releaseAssetURL(releaseURL, releaseAssetMetadata)
	if err != nil {
		return AppUpdateStatus{CurrentVersion: currentVersion}, err
	}
	metadata, _, err := downloadSelfUpdateMetadata(ctx, checker.Client, metadataURL)
	if err != nil {
		return AppUpdateStatus{CurrentVersion: currentVersion}, err
	}
	commit, _, err := validateSelfUpdateMetadataDescriptor(metadata, latestVersion)
	if err != nil {
		return AppUpdateStatus{CurrentVersion: currentVersion}, err
	}
	status := AppUpdateStatus{
		CurrentVersion:      currentVersion,
		LatestVersion:       latestVersion,
		LatestTag:           releaseTag,
		ReleaseURL:          releaseURL.String(),
		ExecutableSize:      metadata.Bytes,
		ReleaseTargetCommit: commit,
	}
	status.ExecutableURL, err = releaseAssetURL(releaseURL, releaseAssetExecutable)
	if err != nil {
		return AppUpdateStatus{CurrentVersion: currentVersion}, err
	}
	status.MetadataURL = metadataURL
	status.SHA256URL, err = releaseAssetURL(releaseURL, releaseAssetSHA256)
	if err != nil {
		return AppUpdateStatus{CurrentVersion: currentVersion}, err
	}
	status.Available = compareAppVersions(latestVersion, currentVersion) > 0
	return status, nil
}

func (checker GitHubReleaseChecker) resolveLatestReleaseTag(ctx context.Context) (string, *url.URL, error) {
	latestReleasePageURL := strings.TrimSpace(checker.LatestReleasePageURL)
	if latestReleasePageURL == "" {
		latestReleasePageURL = defaultGitHubReleaseChecker().LatestReleasePageURL
	}
	if err := validateSelfUpdateDownloadURL(latestReleasePageURL); err != nil {
		return "", nil, err
	}
	client := checker.Client
	if client == nil {
		client = http.DefaultClient
	}
	noRedirectClient := *client
	noRedirectClient.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, latestReleasePageURL, nil)
	if err != nil {
		return "", nil, err
	}
	request.Header.Set("Accept", "text/html")
	request.Header.Set("User-Agent", "PatchBoard/"+currentAppVersion())
	response, err := noRedirectClient.Do(request)
	if err != nil {
		return "", nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusMultipleChoices || response.StatusCode > http.StatusPermanentRedirect {
		return "", nil, fmt.Errorf("GitHub latest release redirect failed with HTTP %d", response.StatusCode)
	}
	location, err := response.Location()
	if err != nil {
		return "", nil, fmt.Errorf("GitHub latest release redirect is missing a location: %w", err)
	}
	if err := validateSelfUpdateURL(location); err != nil {
		return "", nil, err
	}
	pathSegments := strings.Split(strings.Trim(location.Path, "/"), "/")
	if len(pathSegments) < 3 || pathSegments[len(pathSegments)-2] != "tag" || pathSegments[len(pathSegments)-3] != "releases" {
		return "", nil, errors.New("GitHub latest release redirect does not identify a release tag")
	}
	releaseTag := pathSegments[len(pathSegments)-1]
	if releaseTag == "" {
		return "", nil, errors.New("GitHub latest release redirect has an empty release tag")
	}
	return releaseTag, location, nil
}

func releaseAssetURL(releaseURL *url.URL, assetName string) (string, error) {
	if releaseURL == nil || strings.TrimSpace(assetName) == "" {
		return "", errors.New("release asset URL is incomplete")
	}
	pathSegments := strings.Split(strings.Trim(releaseURL.Path, "/"), "/")
	if len(pathSegments) < 3 || pathSegments[len(pathSegments)-2] != "tag" || pathSegments[len(pathSegments)-3] != "releases" {
		return "", errors.New("release URL does not identify a release tag")
	}
	pathSegments[len(pathSegments)-2] = "download"
	pathSegments = append(pathSegments, assetName)
	assetURL := *releaseURL
	assetURL.Path = "/" + strings.Join(pathSegments, "/")
	assetURL.RawPath = ""
	assetURL.RawQuery = ""
	assetURL.Fragment = ""
	return assetURL.String(), nil
}

func parseGitHubRelease(releaseJSON []byte, currentVersion string) (AppUpdateStatus, error) {
	updateStatus := AppUpdateStatus{CurrentVersion: currentVersion}
	var latestRelease githubReleaseResponse
	decoder := json.NewDecoder(bytes.NewReader(releaseJSON))
	if err := decoder.Decode(&latestRelease); err != nil {
		return updateStatus, err
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			err = errors.New("release response contains trailing JSON data")
		}
		return updateStatus, err
	}
	updateStatus.LatestTag = strings.TrimSpace(latestRelease.TagName)
	updateStatus.ReleaseURL = strings.TrimSpace(latestRelease.HTMLURL)
	latestVersion, ok := normalizeAppVersion(updateStatus.LatestTag)
	if !ok {
		if updateStatus.LatestTag == "" {
			return updateStatus, errors.New("release tag is missing")
		}
		return updateStatus, fmt.Errorf("release tag %q is not a supported semantic version", updateStatus.LatestTag)
	}
	updateStatus.LatestVersion = latestVersion
	if latestRelease.Draft || latestRelease.Prerelease || compareAppVersions(latestVersion, currentVersion) <= 0 {
		return updateStatus, nil
	}
	assetsByName := make(map[string]githubReleaseAsset, len(latestRelease.Assets))
	for _, asset := range latestRelease.Assets {
		assetsByName[asset.Name] = asset
	}
	executableAsset := assetsByName[releaseAssetExecutable]
	metadataAsset := assetsByName[releaseAssetMetadata]
	checksumAsset := assetsByName[releaseAssetSHA256]
	if executableAsset.BrowserDownloadURL == "" || metadataAsset.BrowserDownloadURL == "" || checksumAsset.BrowserDownloadURL == "" {
		updateStatus.IncompatibleReason = missingSelfUpdateAssetReason(executableAsset, metadataAsset, checksumAsset)
		return updateStatus, nil
	}
	if executableAsset.Size > maxSelfUpdateExecutableBytes {
		return updateStatus, fmt.Errorf("release executable exceeds %d bytes", maxSelfUpdateExecutableBytes)
	}
	targetCommit, ok := normalizeReleaseTargetCommit(latestRelease.TargetCommitish)
	if !ok {
		updateStatus.IncompatibleReason = "latest release does not identify an exact target commit"
		return updateStatus, nil
	}
	updateStatus.Available = true
	updateStatus.ExecutableURL = executableAsset.BrowserDownloadURL
	updateStatus.MetadataURL = metadataAsset.BrowserDownloadURL
	updateStatus.SHA256URL = checksumAsset.BrowserDownloadURL
	updateStatus.ExecutableSize = executableAsset.Size
	updateStatus.ReleaseTargetCommit = targetCommit
	return updateStatus, nil
}

func normalizeReleaseTargetCommit(targetCommitish string) (string, bool) {
	targetCommitish = strings.ToLower(strings.TrimSpace(targetCommitish))
	if !gitCommitPattern.MatchString(targetCommitish) {
		return "", false
	}
	return targetCommitish, true
}

func missingSelfUpdateAssetReason(executableAsset, metadataAsset, checksumAsset githubReleaseAsset) string {
	var missing []string
	if executableAsset.BrowserDownloadURL == "" {
		missing = append(missing, releaseAssetExecutable)
	}
	if metadataAsset.BrowserDownloadURL == "" {
		missing = append(missing, releaseAssetMetadata)
	}
	if checksumAsset.BrowserDownloadURL == "" {
		missing = append(missing, releaseAssetSHA256)
	}
	if len(missing) == 0 {
		return ""
	}
	return "latest release does not include PatchBoard self-update assets: " + strings.Join(missing, ", ")
}

func downloadSelfUpdateArtifact(ctx context.Context, client *http.Client, updateStatus AppUpdateStatus, downloadDir string) (selfUpdateArtifact, error) {
	if client == nil {
		client = http.DefaultClient
	}
	if !updateStatus.Available {
		return selfUpdateArtifact{}, errors.New("no application update is available")
	}
	if updateStatus.ExecutableURL == "" || updateStatus.MetadataURL == "" || updateStatus.SHA256URL == "" {
		return selfUpdateArtifact{}, errors.New("application update release assets are incomplete")
	}
	if updateStatus.ExecutableSize > maxSelfUpdateExecutableBytes {
		return selfUpdateArtifact{}, fmt.Errorf("release executable exceeds %d bytes", maxSelfUpdateExecutableBytes)
	}
	if err := ensureUserPrivateDir(downloadDir); err != nil {
		return selfUpdateArtifact{}, err
	}
	expectedSHA256, err := downloadExpectedSHA256(ctx, client, updateStatus.SHA256URL)
	if err != nil {
		return selfUpdateArtifact{}, err
	}
	metadata, _, err := downloadSelfUpdateMetadata(ctx, client, updateStatus.MetadataURL)
	if err != nil {
		return selfUpdateArtifact{}, err
	}
	tempFile, err := os.CreateTemp(downloadDir, "PatchBoard-update-*.exe")
	if err != nil {
		return selfUpdateArtifact{}, err
	}
	artifactPath := tempFile.Name()
	removePartialDownload := true
	defer func() {
		if removePartialDownload {
			_ = os.Remove(artifactPath)
		}
	}()
	actualSHA256, actualBytes, err := downloadFileAndHash(ctx, client, updateStatus.ExecutableURL, tempFile, sha256.New())
	closeErr := tempFile.Close()
	if err != nil {
		return selfUpdateArtifact{}, err
	}
	if closeErr != nil {
		return selfUpdateArtifact{}, closeErr
	}
	if !strings.EqualFold(actualSHA256, expectedSHA256) {
		return selfUpdateArtifact{}, fmt.Errorf("self-update checksum mismatch: got %s want %s", actualSHA256, expectedSHA256)
	}
	if err := validateSelfUpdateMetadata(metadata, updateStatus, actualSHA256, actualBytes); err != nil {
		return selfUpdateArtifact{}, err
	}
	if err := validateDownloadedSelfUpdateExecutable(artifactPath, metadata); err != nil {
		return selfUpdateArtifact{}, err
	}
	if err := protectUserPrivateExecutable(artifactPath); err != nil {
		return selfUpdateArtifact{}, err
	}
	removePartialDownload = false
	return selfUpdateArtifact{Path: artifactPath, SHA256: strings.ToLower(actualSHA256), Version: updateStatus.LatestVersion}, nil
}

func downloadSelfUpdateMetadata(ctx context.Context, client *http.Client, metadataURL string) (selfUpdateReleaseMetadata, []byte, error) {
	metadataData, err := httpGetBounded(ctx, client, metadataURL, maxSelfUpdateMetadataBytes, "metadata")
	if err != nil {
		return selfUpdateReleaseMetadata{}, nil, err
	}
	metadata, err := decodeSelfUpdateMetadata(metadataData)
	return metadata, metadataData, err
}

func decodeSelfUpdateMetadata(metadataData []byte) (selfUpdateReleaseMetadata, error) {
	var metadata selfUpdateReleaseMetadata
	// Windows PowerShell's UTF-8 output includes this optional marker. Accept it
	// only at the beginning so CI metadata remains readable without relaxing the
	// strict JSON schema below.
	metadataData = bytes.TrimPrefix(metadataData, []byte(utf8ByteOrderMark))
	decoder := json.NewDecoder(bytes.NewReader(metadataData))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&metadata); err != nil {
		return metadata, fmt.Errorf("invalid self-update metadata: %w", err)
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return metadata, errors.New("invalid self-update metadata: trailing data")
		}
		return metadata, fmt.Errorf("invalid self-update metadata: %w", err)
	}
	return metadata, nil
}

func validateSelfUpdateMetadata(metadata selfUpdateReleaseMetadata, updateStatus AppUpdateStatus, actualSHA256 string, actualBytes int64) error {
	releaseTargetCommit, ok := normalizeReleaseTargetCommit(updateStatus.ReleaseTargetCommit)
	if !ok {
		return errors.New("self-update release target commit is missing or invalid")
	}
	metadataCommit, metadataSHA256, err := validateSelfUpdateMetadataDescriptor(metadata, updateStatus.LatestVersion)
	if err != nil {
		return err
	}
	if metadataCommit != releaseTargetCommit {
		return fmt.Errorf("self-update metadata commit %s does not match release target commit %s", metadataCommit, releaseTargetCommit)
	}
	if !strings.EqualFold(metadataSHA256, actualSHA256) {
		return fmt.Errorf("self-update metadata SHA-256 mismatch: metadata %s executable %s", metadataSHA256, actualSHA256)
	}
	if metadata.Bytes <= 0 || metadata.Bytes != actualBytes {
		return fmt.Errorf("self-update metadata byte count %d does not match executable size %d", metadata.Bytes, actualBytes)
	}
	if updateStatus.ExecutableSize > 0 && metadata.Bytes != updateStatus.ExecutableSize {
		return fmt.Errorf("self-update metadata byte count %d does not match release asset size %d", metadata.Bytes, updateStatus.ExecutableSize)
	}
	return nil
}

func validateSelfUpdateMetadataDescriptor(metadata selfUpdateReleaseMetadata, expectedVersion string) (string, string, error) {
	metadataCommit, ok := normalizeReleaseTargetCommit(metadata.Commit)
	if !ok {
		return "", "", errors.New("self-update metadata commit is missing or invalid")
	}
	metadataSHA256 := strings.ToLower(strings.TrimSpace(metadata.SHA256))
	if metadataSHA256 == "" || !sha256LinePattern.MatchString(metadataSHA256) {
		return "", "", errors.New("self-update metadata has invalid SHA-256")
	}
	metadataVersion, ok := normalizeAppVersion(metadata.Version)
	if !ok || metadataVersion != expectedVersion {
		return "", "", fmt.Errorf("self-update metadata version %q does not match release version %q", metadata.Version, expectedVersion)
	}
	if strings.TrimSpace(metadata.Repository) != appRepositoryURL {
		return "", "", fmt.Errorf("self-update metadata repository %q does not match %q", metadata.Repository, appRepositoryURL)
	}
	if strings.TrimSpace(metadata.License) != appLicenseID {
		return "", "", fmt.Errorf("self-update metadata license %q does not match %q", metadata.License, appLicenseID)
	}
	if metadata.Dirty {
		return "", "", errors.New("self-update metadata reports a dirty release build")
	}
	if metadata.Bytes <= 0 || metadata.Bytes > maxSelfUpdateExecutableBytes {
		return "", "", fmt.Errorf("self-update metadata byte count %d is invalid", metadata.Bytes)
	}
	if !strings.EqualFold(filepath.Base(strings.TrimSpace(metadata.Artifact)), releaseAssetExecutable) {
		return "", "", fmt.Errorf("self-update metadata artifact must describe %s", releaseAssetExecutable)
	}
	return metadataCommit, metadataSHA256, nil
}

func downloadExpectedSHA256(ctx context.Context, client *http.Client, checksumURL string) (string, error) {
	checksumData, err := httpGetBounded(ctx, client, checksumURL, maxSelfUpdateChecksumBytes, "checksum")
	if err != nil {
		return "", err
	}
	digest := sha256LinePattern.FindString(string(checksumData))
	if digest == "" {
		return "", errors.New("release checksum asset does not contain a SHA-256 digest")
	}
	return strings.ToLower(digest), nil
}

func downloadFileAndHash(ctx context.Context, client *http.Client, downloadURL string, destination io.Writer, digest hash.Hash) (string, int64, error) {
	if err := validateSelfUpdateDownloadURL(downloadURL); err != nil {
		return "", 0, err
	}
	client = securedSelfUpdateHTTPClient(client)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return "", 0, err
	}
	request.Header.Set("User-Agent", "PatchBoard/"+currentAppVersion())
	response, err := client.Do(request)
	if err != nil {
		return "", 0, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return "", 0, fmt.Errorf("download failed with HTTP %d", response.StatusCode)
	}
	limitedBody := &io.LimitedReader{R: response.Body, N: maxSelfUpdateExecutableBytes + 1}
	hashingWriter := io.MultiWriter(destination, digest)
	bytesWritten, err := io.Copy(hashingWriter, limitedBody)
	if err != nil {
		return "", 0, err
	}
	if bytesWritten > maxSelfUpdateExecutableBytes || limitedBody.N == 0 {
		return "", 0, fmt.Errorf("downloaded executable exceeds %d bytes", maxSelfUpdateExecutableBytes)
	}
	return hex.EncodeToString(digest.Sum(nil)), bytesWritten, nil
}

func httpGetBounded(ctx context.Context, client *http.Client, downloadURL string, maxBytes int64, resourceLabel string) ([]byte, error) {
	if err := validateSelfUpdateDownloadURL(downloadURL); err != nil {
		return nil, err
	}
	client = securedSelfUpdateHTTPClient(client)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", "PatchBoard/"+currentAppVersion())
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return nil, fmt.Errorf("%s download failed with HTTP %d", resourceLabel, response.StatusCode)
	}
	return readBounded(response.Body, maxBytes, resourceLabel)
}

func securedSelfUpdateHTTPClient(client *http.Client) *http.Client {
	if client == nil {
		client = http.DefaultClient
	}
	cloned := *client
	previousCheckRedirect := client.CheckRedirect
	cloned.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("self-update download exceeded redirect limit")
		}
		if err := validateSelfUpdateURL(request.URL); err != nil {
			return err
		}
		if previousCheckRedirect != nil {
			return previousCheckRedirect(request, via)
		}
		return nil
	}
	return &cloned
}

func validateSelfUpdateDownloadURL(rawURL string) error {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return fmt.Errorf("self-update URL is invalid: %w", err)
	}
	return validateSelfUpdateURL(parsed)
}

func validateSelfUpdateURL(parsed *url.URL) error {
	if parsed == nil || parsed.User != nil {
		return errors.New("self-update URL is invalid")
	}
	hostname := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	if hostname == "" {
		return errors.New("self-update URL host is missing")
	}
	if parsed.Scheme == "http" && currentAppVersion() == "0.0.0-dev" && (hostname == "127.0.0.1" || hostname == "::1" || hostname == "localhost") {
		return nil
	}
	if parsed.Scheme != "https" {
		return errors.New("self-update URL must use HTTPS")
	}
	switch hostname {
	case "api.github.com", "github.com", "objects.githubusercontent.com", "release-assets.githubusercontent.com", "github-releases.githubusercontent.com":
		return nil
	default:
		return fmt.Errorf("self-update URL host %q is not trusted", hostname)
	}
}

func readBounded(source io.Reader, maxBytes int64, resourceLabel string) ([]byte, error) {
	limitedReader := io.LimitReader(source, maxBytes+1)
	data, err := io.ReadAll(limitedReader)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("%s exceeds %d bytes", resourceLabel, maxBytes)
	}
	return data, nil
}

func selfUpdateDownloadDir() (string, error) {
	tempRoot, err := appTempDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(tempRoot, "self-update"), nil
}
