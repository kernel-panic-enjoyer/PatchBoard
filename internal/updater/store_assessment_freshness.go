package updater

import (
	"fmt"
	"strings"
	"time"
)

// storeAssessmentFreshnessWindow is the maximum age of published Store evidence
// that may authorize an update. Store offers and installed AppX package
// versions can change outside this process; two hours is long enough for a
// normal WebUI session while preventing day-old or recovered evidence from
// becoming executable.
const storeAssessmentFreshnessWindow = 2 * time.Hour

type storeAssessmentFreshnessStatus struct {
	Fresh  bool
	Reason string
}

func evaluatePublishedStoreAssessmentFreshness(snapshot StoreScanSnapshot, assessment StorePublishedAssessment, currentInstalledVersion string, now time.Time) storeAssessmentFreshnessStatus {
	now = now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if !snapshot.Published {
		return staleStoreAssessmentStatus("snapshot was not published")
	}
	if snapshot.RecoveredFromFallback {
		return staleStoreAssessmentStatus("snapshot was recovered after a newer snapshot could not be decoded")
	}
	if assessment.Stale {
		return staleStoreAssessmentStatus("assessment was retained from an earlier scan")
	}
	if staleReason := storeEvidenceTimeStale("snapshot", snapshot.Scan.CompletedAt, now); staleReason != "" {
		return staleStoreAssessmentStatus(staleReason)
	}
	if staleReason := storeEvidenceTimeStale("assessment", assessment.ObservedAt, now); staleReason != "" {
		return staleStoreAssessmentStatus(staleReason)
	}
	currentInstalledVersion = strings.TrimSpace(currentInstalledVersion)
	assessedInstalledVersion := strings.TrimSpace(assessment.InstalledVersion)
	if !storeAssessmentVersionKnown(currentInstalledVersion) {
		return staleStoreAssessmentStatus("current installed version is unavailable")
	}
	if !storeAssessmentVersionKnown(assessedInstalledVersion) {
		return staleStoreAssessmentStatus("assessed installed version is unavailable")
	}
	if !strings.EqualFold(currentInstalledVersion, assessedInstalledVersion) {
		return staleStoreAssessmentStatus("installed version no longer matches the assessed version")
	}
	return storeAssessmentFreshnessStatus{Fresh: true}
}

func storeAssessmentVersionKnown(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && !strings.EqualFold(value, "unknown")
}

func storeEvidenceTimeStale(label string, value time.Time, now time.Time) string {
	if value.IsZero() {
		return fmt.Sprintf("%s time is unavailable", label)
	}
	age := now.Sub(value.UTC())
	if age < 0 {
		age = 0
	}
	if age > storeAssessmentFreshnessWindow {
		return fmt.Sprintf("%s evidence is older than %s", label, storeAssessmentFreshnessWindow)
	}
	return ""
}

func staleStoreAssessmentStatus(reason string) storeAssessmentFreshnessStatus {
	return storeAssessmentFreshnessStatus{Reason: sanitizeProviderDiagnostic(reason)}
}

func staleStoreAssessmentProjection(assessment StorePublishedAssessment, reason string) StorePublishedAssessment {
	assessment.Stale = true
	assessment.ExactActionTargetAvailable = false
	if reason != "" {
		assessment.Reason = firstNonEmpty(assessment.Reason, "stale Store update evidence")
		assessment.Reason = sanitizeProviderDiagnostic(assessment.Reason + ": " + reason)
	}
	return assessment
}
