package updater

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	storeScanSnapshotMaxBytes = 16 << 20
	storeScanSnapshotDirName  = "store-scans"
)

type StoreScanFileRepository struct {
	root        string
	maxBytes    int64
	retention   int
	mu          sync.Mutex
	diagnostics []string
}

func openDefaultStoreScanFileRepository() (*StoreScanFileRepository, error) {
	dir, err := stateDir()
	if err != nil {
		return nil, err
	}
	return openStoreScanFileRepository(filepath.Join(dir, storeScanSnapshotDirName))
}

func openStoreScanFileRepository(root string) (*StoreScanFileRepository, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("Store scan file repository root is empty")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	return &StoreScanFileRepository{
		root:      root,
		maxBytes:  storeScanSnapshotMaxBytes,
		retention: storeScanRetentionRunsUser,
	}, nil
}

func (repo *StoreScanFileRepository) PersistCompletedScanSnapshot(ctx context.Context, snapshot StoreScanSnapshot) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if repo == nil {
		return false, errors.New("Store scan file repository is nil")
	}
	if snapshot.SchemaVersion == 0 {
		snapshot.SchemaVersion = storeScanSchemaVersion
	}
	if snapshot.SchemaVersion != storeScanSchemaVersion {
		return false, fmt.Errorf("unsupported Store scan snapshot schema version %d", snapshot.SchemaVersion)
	}
	if err := validateStoreScanSnapshot(snapshot); err != nil {
		return false, err
	}
	snapshot = snapshotForFilePersistence(snapshot)
	userDir := repo.userDir(snapshot.Scan.UserSID)
	if err := os.MkdirAll(userDir, 0o700); err != nil {
		return false, err
	}

	repo.mu.Lock()
	defer repo.mu.Unlock()

	existing, err := repo.validSnapshotsLocked(ctx, snapshot.Scan.UserSID)
	if err != nil {
		return false, err
	}
	for _, candidate := range existing {
		if candidate.Scan.ScanID == snapshot.Scan.ScanID {
			return false, fmt.Errorf("Store scan snapshot already exists for scan ID %s", snapshot.Scan.ScanID)
		}
	}

	published := false
	if snapshot.Published {
		latest, ok := latestPublishedSnapshot(existing)
		published = !ok || snapshotSortsAfter(snapshot, latest)
	}

	data, err := marshalStoreScanSnapshot(snapshot)
	if err != nil {
		return false, err
	}
	if int64(len(data)) > repo.snapshotMaxBytes() {
		return false, fmt.Errorf("Store scan snapshot exceeds size limit: %d bytes", len(data))
	}
	finalPath := repo.snapshotPath(snapshot)
	if _, err := os.Stat(finalPath); err == nil {
		return false, fmt.Errorf("Store scan snapshot already exists for scan ID %s", snapshot.Scan.ScanID)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	if err := repo.writeSnapshotAtomically(userDir, finalPath, data); err != nil {
		return false, err
	}
	if err := repo.pruneLocked(ctx, snapshot.Scan.UserSID); err != nil {
		return false, err
	}
	return published, nil
}

func (repo *StoreScanFileRepository) LoadLatestPublishedSnapshot(ctx context.Context, userSID string) (StoreScanSnapshot, bool, error) {
	if repo == nil {
		return StoreScanSnapshot{}, false, errors.New("Store scan file repository is nil")
	}
	repo.mu.Lock()
	defer repo.mu.Unlock()
	return repo.loadLatestPublishedSnapshotLocked(ctx, userSID)
}

func (repo *StoreScanFileRepository) LoadPreviousSnapshot(ctx context.Context, userSID string, before StoreScanGeneration) (StoreScanSnapshot, bool, error) {
	if repo == nil {
		return StoreScanSnapshot{}, false, errors.New("Store scan file repository is nil")
	}
	if strings.TrimSpace(userSID) == "" || before.StartedAt.IsZero() {
		return StoreScanSnapshot{}, false, nil
	}
	repo.mu.Lock()
	defer repo.mu.Unlock()
	snapshots, err := repo.validSnapshotsLocked(ctx, userSID)
	if err != nil {
		return StoreScanSnapshot{}, false, err
	}
	sort.Slice(snapshots, func(i, j int) bool {
		return snapshotSortsAfter(snapshots[i], snapshots[j])
	})
	for _, snapshot := range snapshots {
		if snapshot.Scan.StartedAt.Before(before.StartedAt) {
			return snapshot, true, nil
		}
	}
	return StoreScanSnapshot{}, false, nil
}

func (repo *StoreScanFileRepository) Close() error {
	return nil
}

func (repo *StoreScanFileRepository) Diagnostics() []string {
	if repo == nil {
		return nil
	}
	repo.mu.Lock()
	defer repo.mu.Unlock()
	out := make([]string, len(repo.diagnostics))
	copy(out, repo.diagnostics)
	return out
}

func (repo *StoreScanFileRepository) loadLatestPublishedSnapshotLocked(ctx context.Context, userSID string) (StoreScanSnapshot, bool, error) {
	snapshots, err := repo.validSnapshotsLocked(ctx, userSID)
	if err != nil {
		return StoreScanSnapshot{}, false, err
	}
	var latest StoreScanSnapshot
	found := false
	for _, snapshot := range snapshots {
		if !snapshot.Published {
			continue
		}
		if !found || snapshotSortsAfter(snapshot, latest) {
			latest = snapshot
			found = true
		}
	}
	return latest, found, nil
}

func (repo *StoreScanFileRepository) validSnapshotsLocked(ctx context.Context, userSID string) ([]StoreScanSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	userSID = strings.TrimSpace(userSID)
	if userSID == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(repo.userDir(userSID))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	snapshots := make([]StoreScanSnapshot, 0, len(entries))
	seenScanIDs := map[string]string{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".json") {
			continue
		}
		path := filepath.Join(repo.userDir(userSID), entry.Name())
		snapshot, err := repo.readSnapshotFile(path, userSID)
		if err != nil {
			repo.recordDiagnostic("Store snapshot rejected %s: %s", entry.Name(), err)
			_ = repo.quarantineSnapshot(path)
			continue
		}
		if previousPath, ok := seenScanIDs[snapshot.Scan.ScanID]; ok {
			repo.recordDiagnostic("Store snapshot duplicate scan ID %s in %s and %s", snapshot.Scan.ScanID, filepath.Base(previousPath), entry.Name())
			continue
		}
		seenScanIDs[snapshot.Scan.ScanID] = path
		snapshots = append(snapshots, snapshot)
	}
	return snapshots, nil
}

func (repo *StoreScanFileRepository) readSnapshotFile(path, expectedUserSID string) (StoreScanSnapshot, error) {
	info, err := os.Stat(path)
	if err != nil {
		return StoreScanSnapshot{}, err
	}
	if info.Size() > repo.snapshotMaxBytes() {
		return StoreScanSnapshot{}, fmt.Errorf("snapshot exceeds size limit: %d bytes", info.Size())
	}
	file, err := os.Open(path)
	if err != nil {
		return StoreScanSnapshot{}, err
	}
	defer file.Close()
	limited := io.LimitReader(file, repo.snapshotMaxBytes()+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return StoreScanSnapshot{}, err
	}
	if int64(len(data)) > repo.snapshotMaxBytes() {
		return StoreScanSnapshot{}, fmt.Errorf("snapshot exceeds size limit: %d bytes", len(data))
	}
	snapshot, err := decodeStoreScanSnapshot(data)
	if err != nil {
		return StoreScanSnapshot{}, err
	}
	if snapshot.Scan.UserSID != expectedUserSID {
		return StoreScanSnapshot{}, fmt.Errorf("snapshot belongs to a different user")
	}
	if err := validateStoreScanSnapshot(snapshot); err != nil {
		return StoreScanSnapshot{}, err
	}
	snapshot.Scan.ProviderHealth = providerHealthMap(snapshot.ProviderRuns)
	snapshot.Scan.ProviderVersions = providerVersionMap(snapshot.ProviderRuns)
	snapshot.Inventory.Scan = snapshot.Scan
	sortStoreScanSnapshot(&snapshot)
	return snapshot, nil
}

func decodeStoreScanSnapshot(data []byte) (StoreScanSnapshot, error) {
	var envelope struct {
		SchemaVersion int `json:"schema_version"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return StoreScanSnapshot{}, err
	}
	if envelope.SchemaVersion == 0 {
		return StoreScanSnapshot{}, errors.New("snapshot is missing schema version")
	}
	if envelope.SchemaVersion > storeScanSchemaVersion {
		return StoreScanSnapshot{}, fmt.Errorf("unsupported future Store scan snapshot schema version %d", envelope.SchemaVersion)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var snapshot StoreScanSnapshot
	if err := decoder.Decode(&snapshot); err != nil {
		return StoreScanSnapshot{}, err
	}
	if snapshot.SchemaVersion < storeScanSchemaVersion {
		migrated, err := migrateStoreScanSnapshot(snapshot)
		if err != nil {
			return StoreScanSnapshot{}, err
		}
		snapshot = migrated
	}
	return snapshot, nil
}

func migrateStoreScanSnapshot(snapshot StoreScanSnapshot) (StoreScanSnapshot, error) {
	switch snapshot.SchemaVersion {
	case 1:
		snapshot.SchemaVersion = storeScanSchemaVersion
		return snapshot, nil
	default:
		return StoreScanSnapshot{}, fmt.Errorf("unsupported Store scan snapshot schema version %d", snapshot.SchemaVersion)
	}
}

func marshalStoreScanSnapshot(snapshot StoreScanSnapshot) ([]byte, error) {
	sortStoreScanSnapshot(&snapshot)
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func snapshotForFilePersistence(snapshot StoreScanSnapshot) StoreScanSnapshot {
	sortStoreScanSnapshot(&snapshot)
	for index := range snapshot.Inventory.Errors {
		snapshot.Inventory.Errors[index] = sanitizeProviderDiagnostic(snapshot.Inventory.Errors[index])
	}
	for runIndex := range snapshot.ProviderRuns {
		run := &snapshot.ProviderRuns[runIndex]
		run.Error = sanitizeProviderDiagnostic(run.Error)
		for mappingIndex := range run.Mappings {
			run.Mappings[mappingIndex].Evidence = sanitizeProviderDiagnostic(run.Mappings[mappingIndex].Evidence)
		}
		for observationIndex := range run.Observations {
			run.Observations[observationIndex].Diagnostics = sanitizeProviderDiagnostic(run.Observations[observationIndex].Diagnostics)
		}
	}
	for assessmentIndex := range snapshot.Assessments {
		assessment := &snapshot.Assessments[assessmentIndex]
		assessment.Reason = sanitizeProviderDiagnostic(assessment.Reason)
		if assessment.Target != nil {
			assessment.StoreProductID = firstNonEmpty(assessment.StoreProductID, assessment.Target.ProductID)
			assessment.UpdateID = firstNonEmpty(assessment.UpdateID, assessment.Target.UpdateID)
		}
	}
	return snapshot
}

func (repo *StoreScanFileRepository) writeSnapshotAtomically(dir, finalPath string, data []byte) error {
	temp, err := os.CreateTemp(dir, ".tmp-store-scan-*.json")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tempPath)
		}
	}()
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if _, err := os.Stat(finalPath); err == nil {
		return fmt.Errorf("Store scan snapshot already exists: %s", filepath.Base(finalPath))
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(tempPath, finalPath); err != nil {
		return err
	}
	cleanup = false
	return nil
}

func (repo *StoreScanFileRepository) pruneLocked(ctx context.Context, userSID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	snapshots, err := repo.validSnapshotsLocked(ctx, userSID)
	if err != nil {
		return err
	}
	if len(snapshots) <= repo.retentionLimit() {
		return nil
	}
	sort.Slice(snapshots, func(i, j int) bool {
		return snapshotSortsAfter(snapshots[i], snapshots[j])
	})
	keep := map[string]bool{}
	for index, snapshot := range snapshots {
		if index < repo.retentionLimit() {
			keep[snapshot.Scan.ScanID] = true
		}
	}
	if latest, found := latestPublishedSnapshot(snapshots); found {
		keep[latest.Scan.ScanID] = true
	}
	for _, snapshot := range snapshots {
		if keep[snapshot.Scan.ScanID] {
			continue
		}
		if err := os.Remove(repo.snapshotPath(snapshot)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func (repo *StoreScanFileRepository) quarantineSnapshot(path string) error {
	if _, err := os.Stat(path); err != nil {
		return err
	}
	target := fmt.Sprintf("%s.corrupt.%s", path, time.Now().UTC().Format("20060102T150405.000000000Z"))
	return os.Rename(path, target)
}

func (repo *StoreScanFileRepository) snapshotPath(snapshot StoreScanSnapshot) string {
	return filepath.Join(repo.userDir(snapshot.Scan.UserSID), snapshotFileName(snapshot))
}

func (repo *StoreScanFileRepository) userDir(userSID string) string {
	return filepath.Join(repo.root, userScopeHash(userSID))
}

func (repo *StoreScanFileRepository) snapshotMaxBytes() int64 {
	if repo != nil && repo.maxBytes > 0 {
		return repo.maxBytes
	}
	return storeScanSnapshotMaxBytes
}

func (repo *StoreScanFileRepository) retentionLimit() int {
	if repo != nil && repo.retention > 0 {
		return repo.retention
	}
	return storeScanRetentionRunsUser
}

func (repo *StoreScanFileRepository) recordDiagnostic(format string, args ...any) {
	if repo == nil {
		return
	}
	message := sanitizeProviderDiagnostic(fmt.Sprintf(format, args...))
	repo.diagnostics = append(repo.diagnostics, message)
	if len(repo.diagnostics) > 100 {
		repo.diagnostics = repo.diagnostics[len(repo.diagnostics)-100:]
	}
	appLog("%s", message)
}

func snapshotFileName(snapshot StoreScanSnapshot) string {
	started := snapshot.Scan.StartedAt.UTC().Format("20060102T150405.000000000Z")
	return started + "-" + shortHash(snapshot.Scan.ScanID) + ".json"
}

func userScopeHash(userSID string) string {
	return "user-" + shortHash(userSID)
}

func shortHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])[:24]
}

func snapshotSortsAfter(left, right StoreScanSnapshot) bool {
	if !left.Scan.StartedAt.Equal(right.Scan.StartedAt) {
		return left.Scan.StartedAt.After(right.Scan.StartedAt)
	}
	return left.Scan.ScanID > right.Scan.ScanID
}

func latestPublishedSnapshot(snapshots []StoreScanSnapshot) (StoreScanSnapshot, bool) {
	var latest StoreScanSnapshot
	found := false
	for _, snapshot := range snapshots {
		if !snapshot.Published {
			continue
		}
		if !found || snapshotSortsAfter(snapshot, latest) {
			latest = snapshot
			found = true
		}
	}
	return latest, found
}
