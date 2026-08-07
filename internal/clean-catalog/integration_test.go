package cleancatalog

import (
	"os"
	"path/filepath"
	"testing"

	deletionengine "github.com/lesliemunmus/wtff/internal/deletion-engine"
	operationlog "github.com/lesliemunmus/wtff/internal/operation-log"
	protectionrules "github.com/lesliemunmus/wtff/internal/protection-rules"
)

// This is the same guarantee the deletion engine's own integration test
// checks for protection rules, exercised here for discovery: a candidate this
// package proposes is not trusted, it is still fully validated by the real
// engine and the real rule set before anything happens to it.
//
// The fixture deliberately plants both a third party cache, which should be
// removed, and a directory shaped like one of the Apple service state
// directories found during this package's own design, which must survive
// despite being proposed as a candidate by the broad container scan.
func TestDiscoveredCandidatesRespectRealProtectionThroughPlanAndApply(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cachesDir := filepath.Join(home, "Library", "Caches")
	thirdParty := filepath.Join(cachesDir, "SomeThirdPartyApp")
	if err := os.MkdirAll(thirdParty, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(thirdParty, "blob.bin"), []byte("disposable"), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	familyCircle := filepath.Join(cachesDir, "FamilyCircle")
	if err := os.MkdirAll(familyCircle, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(familyCircle, "state.plist"), []byte("account state"), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	catalog, err := LoadBuiltin()
	if err != nil {
		t.Fatalf("LoadBuiltin: %v", err)
	}
	candidates, _ := Discover(catalog.Entries(), home)

	rules, err := protectionrules.LoadBuiltinForHome(home)
	if err != nil {
		t.Fatalf("loading rules: %v", err)
	}

	manifest, err := deletionengine.Plan(candidates, deletionengine.PlanOptions{
		Command: "clean", Policy: rules, Log: operationlog.Discard(), MeasureSizes: true,
	})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	planned := make(map[string]bool)
	for _, entry := range manifest.Entries {
		planned[filepath.Base(entry.ResolvedPath)] = true
	}
	if !planned["SomeThirdPartyApp"] {
		t.Error("the third party cache was not planned for removal")
	}
	if planned["FamilyCircle"] {
		t.Fatal("FamilyCircle was planned for removal despite the protection rule")
	}

	staging, err := deletionengine.NewStagingArea(filepath.Join(home, "staging-area"))
	if err != nil {
		t.Fatalf("staging setup: %v", err)
	}
	result, err := deletionengine.Apply(manifest, deletionengine.ApplyOptions{
		Staging: staging, Policy: rules, Log: operationlog.Discard(),
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if result.AppliedCount != 1 {
		t.Fatalf("applied %d, want 1", result.AppliedCount)
	}

	if _, err := os.Stat(thirdParty); err == nil {
		t.Fatal("the third party cache was not actually removed")
	}
	if _, err := os.Stat(familyCircle); err != nil {
		t.Fatalf("the protected directory was removed: %v", err)
	}
}
