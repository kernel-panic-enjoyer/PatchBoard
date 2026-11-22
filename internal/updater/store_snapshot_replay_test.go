package updater

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// TestReplayStoreScanSnapshots re-runs the Store decision code over saved
// scan snapshots from real machines. It is opt-in because the snapshots hold
// the list of installed apps and stay out of the repository:
//
//	PATCHBOARD_STORE_SNAPSHOT_DIR  folder of redacted snapshot JSON files
//	PATCHBOARD_STORE_REPLAY_OUT    file the decisions are written to
//
// Diff the output of two code versions to see every decision that changed.
func TestReplayStoreScanSnapshots(t *testing.T) {
	dir := os.Getenv("PATCHBOARD_STORE_SNAPSHOT_DIR")
	if dir == "" {
		t.Skip("PATCHBOARD_STORE_SNAPSHOT_DIR not set")
	}
	paths, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("no snapshots in %s: %v", dir, err)
	}
	sort.Strings(paths)

	type decision struct {
		State      StoreUpdateState `json:"state"`
		Reason     string           `json:"reason"`
		Stale      bool             `json:"stale,omitempty"`
		Actionable bool             `json:"actionable,omitempty"`
	}
	replay := map[string]map[string]decision{}
	var previous StoreScanSnapshot
	reproduced, total := 0, 0
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var snapshot StoreScanSnapshot
		if err := json.Unmarshal(data, &snapshot); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		recorded := map[StoreInstalledIdentity]StoreUpdateState{}
		for _, assessment := range snapshot.Assessments {
			recorded[assessment.Identity] = assessment.State
		}
		assessments := reconcileStoreScanAssessments(snapshot.Scan, snapshot.Inventory.Families, snapshot.ProviderRuns, previousAssessmentsFromSnapshot(previous))
		decisions := map[string]decision{}
		for _, assessment := range assessments {
			decisions[assessment.Identity.PackageFamilyName] = decision{
				State:      assessment.State,
				Reason:     assessment.Reason,
				Stale:      assessment.Stale,
				Actionable: assessment.State == StoreUpdateAvailable && !assessment.Stale && assessment.ExactActionTargetAvailable,
			}
			total++
			if recorded[assessment.Identity] == assessment.State {
				reproduced++
			}
		}
		replay[filepath.Base(path)] = decisions
		if snapshot.Published {
			previous = snapshot
		}
	}
	t.Logf("replayed %d snapshots; %d of %d decisions match the recorded state", len(paths), reproduced, total)
	if out := os.Getenv("PATCHBOARD_STORE_REPLAY_OUT"); out != "" {
		data, err := json.MarshalIndent(replay, "", " ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(out, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
