package deletionengine

import (
	"os"
	"path/filepath"
	"testing"

	operationlog "github.com/lesliemusengi/wtff/internal/operation-log"
	protectionrules "github.com/lesliemusengi/wtff/internal/protection-rules"
)

// The engine is written against an interface so it can be tested without a real
// rule set. That leaves one thing unproven until the two meet: whether the real
// rule set actually satisfies the interface and behaves as the engine expects.
// A compile time assertion catches the shape, and the test below catches the
// behavior.
var _ PolicyChecker = (*protectionrules.Set)(nil)

// A protected path must survive an end to end plan and apply against the real
// shipped rules, not a stub. This is the first point where the two halves of the
// safety story are exercised together.
func TestRealRuleSetProtectsThroughPlanAndApply(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	rules, err := protectionrules.LoadBuiltinForHome(home)
	if err != nil {
		t.Fatalf("loading the shipped rules: %v", err)
	}

	// One path the rules protect, and one they do not, written side by side so
	// the test fails if the rule set stops discriminating between them.
	keychainDir := filepath.Join(home, "Library", "Keychains")
	if err := os.MkdirAll(keychainDir, 0o700); err != nil {
		t.Fatalf("setup: %v", err)
	}
	keychain := filepath.Join(keychainDir, "login.keychain-db")
	if err := os.WriteFile(keychain, []byte("credential material"), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	cacheDir := filepath.Join(home, "Library", "Caches", "com.example.app")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	cacheFile := filepath.Join(cacheDir, "rebuildable.bin")
	if err := os.WriteFile(cacheFile, []byte("disposable"), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	manifest, err := Plan(candidatesFor(keychain, cacheFile), PlanOptions{
		Command: "clean",
		Policy:  rules,
		Log:     operationlog.Discard(),
	})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	if len(manifest.Entries) != 1 {
		for _, entry := range manifest.Entries {
			t.Logf("planned: %s", entry.ResolvedPath)
		}
		t.Fatalf("planned %d entries, expected only the cache file", len(manifest.Entries))
	}
	if filepath.Base(manifest.Entries[0].ResolvedPath) != "rebuildable.bin" {
		t.Fatalf("planned the wrong entry: %s", manifest.Entries[0].ResolvedPath)
	}

	staging, err := NewStagingArea(filepath.Join(home, "staging-area"))
	if err != nil {
		t.Fatalf("staging setup: %v", err)
	}
	result, err := Apply(manifest, ApplyOptions{
		Staging: staging, Policy: rules, Log: operationlog.Discard(),
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if result.AppliedCount != 1 {
		t.Fatalf("applied %d, want 1", result.AppliedCount)
	}

	if _, err := os.Stat(keychain); err != nil {
		t.Fatalf("the protected keychain was removed: %v", err)
	}
	if _, err := os.Stat(cacheFile); err == nil {
		t.Fatal("the reclaimable cache file was not staged")
	}
}

// Even if a caller hands the engine a manifest naming a protected path, apply
// re-checks policy and refuses. This covers the case of a manifest planned
// before a rule existed and applied after.
func TestApplyRefusesAProtectedPathEvenWhenPlanned(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	sensitiveDir := filepath.Join(home, "Documents")
	if err := os.MkdirAll(sensitiveDir, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	sensitive := filepath.Join(sensitiveDir, "thesis.txt")
	if err := os.WriteFile(sensitive, []byte("years of work"), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	// Planned with no policy at all, which is how a manifest predating a rule
	// would look.
	manifest, err := Plan(candidatesFor(sensitive), PlanOptions{
		Command: "clean", Policy: AllowAll{}, Log: operationlog.Discard(),
	})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(manifest.Entries) != 1 {
		t.Fatalf("expected the permissive plan to include the entry")
	}

	rules, err := protectionrules.LoadBuiltinForHome(home)
	if err != nil {
		t.Fatalf("loading rules: %v", err)
	}
	staging, err := NewStagingArea(filepath.Join(home, "staging-area"))
	if err != nil {
		t.Fatalf("staging setup: %v", err)
	}

	result, err := Apply(manifest, ApplyOptions{
		Staging: staging, Policy: rules, Log: operationlog.Discard(),
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if result.AppliedCount != 0 || result.SkippedCount != 1 {
		t.Fatalf("applied %d skipped %d, want the entry refused at apply time",
			result.AppliedCount, result.SkippedCount)
	}
	if _, err := os.Stat(sensitive); err != nil {
		t.Fatalf("a protected file was removed despite the apply time check: %v", err)
	}
}
