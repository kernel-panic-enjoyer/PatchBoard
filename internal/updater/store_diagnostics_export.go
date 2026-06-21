package updater

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

type StoreDiagnosticsExport struct {
	GeneratedAt         string                         `json:"generated_at"`
	SchemaVersion       int                            `json:"schema_version"`
	DetectorMode        string                         `json:"detector_mode"`
	UserScopeHash       string                         `json:"user_scope_hash,omitempty"`
	Scan                StoreDiagnosticsScan           `json:"scan"`
	Providers           []StorePackageProviderSummary  `json:"providers,omitempty"`
	Packages            []StoreDiagnosticsPackage      `json:"packages,omitempty"`
	Observations        []StoreDiagnosticsObservation  `json:"observations,omitempty"`
	Assessments         []StoreDiagnosticsAssessment   `json:"assessments,omitempty"`
	AutoUpdateMigration StoreAutoUpdateMigrationReport `json:"auto_update_migration,omitempty"`
	Errors              []string                       `json:"errors,omitempty"`
}

type StoreDiagnosticsScan struct {
	ScanID         string `json:"scan_id,omitempty"`
	StartedAt      string `json:"started_at,omitempty"`
	CompletedAt    string `json:"completed_at,omitempty"`
	WindowsVersion string `json:"windows_version,omitempty"`
	WindowsBuild   string `json:"windows_build,omitempty"`
	Architecture   string `json:"architecture,omitempty"`
	Status         string `json:"status,omitempty"`
}

type StoreDiagnosticsPackage struct {
	PackageFamilyName string `json:"package_family_name"`
	DisplayName       string `json:"display_name,omitempty"`
	ProductLike       bool   `json:"product_like"`
}

type StoreDiagnosticsObservation struct {
	Provider          string `json:"provider"`
	PackageFamilyName string `json:"package_family_name"`
	Kind              string `json:"kind"`
	Health            string `json:"health"`
	ObservedAt        string `json:"observed_at"`
	InstalledVersion  string `json:"installed_version,omitempty"`
	AvailableVersion  string `json:"available_version,omitempty"`
	CatalogVersion    string `json:"catalog_version,omitempty"`
	ProductID         string `json:"product_id,omitempty"`
	UpdateID          string `json:"update_id,omitempty"`
	TargetVerified    bool   `json:"target_verified"`
	Diagnostics       string `json:"diagnostics,omitempty"`
}

type StoreDiagnosticsAssessment struct {
	PackageFamilyName          string `json:"package_family_name"`
	State                      string `json:"state"`
	Reason                     string `json:"reason,omitempty"`
	InstalledVersion           string `json:"installed_version,omitempty"`
	AvailableVersion           string `json:"available_version,omitempty"`
	Stale                      bool   `json:"stale"`
	ProductID                  string `json:"product_id,omitempty"`
	UpdateID                   string `json:"update_id,omitempty"`
	ExactActionTargetAvailable bool   `json:"exact_action_target_available"`
	Applicability              string `json:"applicability,omitempty"`
	ObservedAt                 string `json:"observed_at"`
}

func buildStoreDiagnosticsExport(ctx context.Context, state State) ([]byte, error) {
	export := StoreDiagnosticsExport{
		GeneratedAt:         formatAssessmentTime(time.Now().UTC()),
		SchemaVersion:       storeScanSchemaVersion,
		DetectorMode:        "new",
		AutoUpdateMigration: sanitizeStoreAutoUpdateMigration(state.StoreAutoUpdateMigration),
	}
	if storeLegacyDetectorRollbackEnabled() {
		export.DetectorMode = "legacy-rollback"
	}
	userSID, sidErr := currentUserSID()
	if sidErr != nil {
		export.Errors = append(export.Errors, sanitizeProviderDiagnostic(sidErr.Error()))
	} else {
		export.UserScopeHash = hashSensitiveID(userSID)
	}
	store, err := openDefaultStoreScanStore()
	if err != nil {
		export.Errors = append(export.Errors, sanitizeProviderDiagnostic(err.Error()))
		return json.MarshalIndent(export, "", "  ")
	}
	defer store.Close()
	if userSID == "" {
		return json.MarshalIndent(export, "", "  ")
	}
	scan, ok, err := store.LatestPublishedScan(ctx, userSID)
	if err != nil {
		return nil, err
	}
	if !ok {
		export.Errors = append(export.Errors, "no published Store scan is available")
		return json.MarshalIndent(export, "", "  ")
	}
	export.Scan = StoreDiagnosticsScan{
		ScanID:         scan.ScanID,
		StartedAt:      formatStoreScanTime(scan.StartedAt),
		CompletedAt:    formatStoreScanTime(scan.CompletedAt),
		WindowsVersion: scan.WindowsVersion,
		WindowsBuild:   scan.WindowsBuild,
		Architecture:   scan.Architecture,
		Status:         string(scan.CompletionStatus),
	}
	providers, providerErr := store.LatestPublishedProviderSummaries(ctx, userSID)
	if providerErr == nil {
		export.Providers = providers
	} else {
		export.Errors = append(export.Errors, sanitizeProviderDiagnostic(providerErr.Error()))
	}
	if err := store.populateDiagnostics(ctx, scan.ScanID, userSID, &export); err != nil {
		return nil, err
	}
	return json.MarshalIndent(export, "", "  ")
}

func (store *StoreScanStore) populateDiagnostics(ctx context.Context, scanID, userSID string, export *StoreDiagnosticsExport) error {
	if store == nil || store.db == nil || export == nil {
		return errors.New("Store diagnostics store is unavailable")
	}
	familyRows, err := store.db.QueryContext(ctx, `SELECT package_family_name, COALESCE(display_name,''), product_like FROM installed_package_families WHERE scan_id = ? AND user_sid = ? ORDER BY package_family_name`, scanID, userSID)
	if err != nil {
		return err
	}
	defer familyRows.Close()
	for familyRows.Next() {
		var item StoreDiagnosticsPackage
		var productLike int
		if err := familyRows.Scan(&item.PackageFamilyName, &item.DisplayName, &productLike); err != nil {
			return err
		}
		item.ProductLike = productLike != 0
		export.Packages = append(export.Packages, item)
	}
	if err := familyRows.Err(); err != nil {
		return err
	}

	observationRows, err := store.db.QueryContext(ctx, `SELECT provider_id, package_family_name, kind, health, observed_at, COALESCE(installed_version,''), COALESCE(available_version,''), COALESCE(catalog_version,''), COALESCE(product_id,''), COALESCE(update_id,''), target_verified, COALESCE(diagnostics,'') FROM provider_observations WHERE scan_id = ? AND user_sid = ? ORDER BY package_family_name, provider_id, id`, scanID, userSID)
	if err != nil {
		return err
	}
	defer observationRows.Close()
	for observationRows.Next() {
		var item StoreDiagnosticsObservation
		var verified int
		if err := observationRows.Scan(&item.Provider, &item.PackageFamilyName, &item.Kind, &item.Health, &item.ObservedAt, &item.InstalledVersion, &item.AvailableVersion, &item.CatalogVersion, &item.ProductID, &item.UpdateID, &verified, &item.Diagnostics); err != nil {
			return err
		}
		item.TargetVerified = verified != 0
		item.Diagnostics = sanitizeProviderDiagnostic(item.Diagnostics)
		export.Observations = append(export.Observations, item)
	}
	if err := observationRows.Err(); err != nil {
		return err
	}

	assessmentRows, err := store.db.QueryContext(ctx, `SELECT package_family_name, state, COALESCE(reason,''), COALESCE(installed_version,''), COALESCE(available_version,''), stale, COALESCE(product_id,''), COALESCE(update_id,''), exact_action_target_available, COALESCE(applicability,''), observed_at FROM update_assessments WHERE scan_id = ? AND user_sid = ? ORDER BY package_family_name`, scanID, userSID)
	if err != nil {
		return err
	}
	defer assessmentRows.Close()
	for assessmentRows.Next() {
		var item StoreDiagnosticsAssessment
		var stale, exact int
		if err := assessmentRows.Scan(&item.PackageFamilyName, &item.State, &item.Reason, &item.InstalledVersion, &item.AvailableVersion, &stale, &item.ProductID, &item.UpdateID, &exact, &item.Applicability, &item.ObservedAt); err != nil {
			return err
		}
		item.Stale = stale != 0
		item.ExactActionTargetAvailable = exact != 0
		item.Reason = sanitizeProviderDiagnostic(item.Reason)
		export.Assessments = append(export.Assessments, item)
	}
	return assessmentRows.Err()
}

func sanitizeStoreAutoUpdateMigration(report StoreAutoUpdateMigrationReport) StoreAutoUpdateMigrationReport {
	report.Migrated = sanitizeStoreAutoUpdateMigrationEntries(report.Migrated)
	report.Disabled = sanitizeStoreAutoUpdateMigrationEntries(report.Disabled)
	return report
}

func sanitizeStoreAutoUpdateMigrationEntries(entries []StoreAutoUpdateMigrationEntry) []StoreAutoUpdateMigrationEntry {
	sanitized := make([]StoreAutoUpdateMigrationEntry, 0, len(entries))
	for _, entry := range entries {
		if userSID, pfn, ok := splitCanonicalStoreAutoUpdateKey(entry.CanonicalKey); ok {
			entry.CanonicalKey = packageKey(managerStore, hashSensitiveID(userSID)+storeAutoUpdateKeySeparator+strings.ToLower(pfn))
		}
		entry.LegacyKey = sanitizeLegacyStorePreferenceKey(entry.LegacyKey)
		sanitized = append(sanitized, entry)
	}
	return sanitized
}

func sanitizeLegacyStorePreferenceKey(key string) string {
	manager, id, err := splitPackageKey(key)
	if err != nil || manager != managerStore {
		return key
	}
	if userSID, pfn, ok := splitCanonicalStoreAutoUpdateKey(key); ok {
		return packageKey(managerStore, hashSensitiveID(userSID)+storeAutoUpdateKeySeparator+strings.ToLower(pfn))
	}
	return packageKey(managerStore, id)
}

func hashSensitiveID(value string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(value)))
	return hex.EncodeToString(sum[:])[:16]
}
