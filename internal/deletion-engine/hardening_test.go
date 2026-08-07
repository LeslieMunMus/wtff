package deletionengine

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	operationlog "github.com/lesliemunmus/wtff/internal/operation-log"
)

// These cases come from a review pass over the engine rather than from the
// original design. Each one pins a defect that was found by attacking the
// implementation after it was written and passing its own tests.

// A link inside a purged tree is removed as a link, and whatever it points at
// survives.
//
// Stated precisely, because the first version of this comment claimed more than
// the test shows: the standard library's recursive remove does not follow links
// either, so this case passes against the implementation that preceded the
// descriptor based walk. It is kept because the property is worth pinning
// against a future rewrite, not because it caught a defect.
//
// The actual reason the walk moved to descriptors is that a path based
// recursive remove re-resolves the target name after it was verified. That gap
// is a race, and no deterministic test demonstrates it. It is recorded as
// reasoned rather than tested.
func TestPurgeRemovesLinksWithoutTouchingTheirDestinations(t *testing.T) {
	f := newFixture(t)

	outsideDir := filepath.Join(f.root, "outside")
	if err := os.MkdirAll(outsideDir, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	bystander := filepath.Join(outsideDir, "must-survive.txt")
	if err := os.WriteFile(bystander, []byte("untouched"), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	tree := f.writeTree("doomed", map[string]string{"inner/file.txt": "x"})
	if err := os.Symlink(outsideDir, filepath.Join(tree, "escape-link")); err != nil {
		t.Fatalf("setup: %v", err)
	}

	manifest := mustPlan(t, candidatesFor(tree), PlanOptions{Action: ActionPurge})
	result, err := Apply(manifest, ApplyOptions{Policy: AllowAll{}, Log: operationlog.Discard()})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if result.AppliedCount != 1 {
		t.Fatalf("applied %d, want 1 (failed %d)", result.AppliedCount, result.FailedCount)
	}

	if _, err := os.Lstat(tree); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("the target tree should be gone")
	}
	if _, err := os.Stat(bystander); err != nil {
		t.Fatalf("purge followed a link out of the tree and destroyed %s: %v", bystander, err)
	}
	if _, err := os.Stat(outsideDir); err != nil {
		t.Fatalf("purge removed a directory outside the target tree: %v", err)
	}
}

func TestPurgeRemovesDeeplyNestedTrees(t *testing.T) {
	f := newFixture(t)
	deep := filepath.Join(f.root, "deep")
	current := deep
	for i := 0; i < 40; i++ {
		current = filepath.Join(current, "level")
	}
	if err := os.MkdirAll(current, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(current, "leaf.txt"), []byte("x"), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	manifest := mustPlan(t, candidatesFor(deep), PlanOptions{Action: ActionPurge})
	result, err := Apply(manifest, ApplyOptions{Policy: AllowAll{}, Log: operationlog.Discard()})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if result.AppliedCount != 1 {
		t.Fatalf("applied %d, want 1", result.AppliedCount)
	}
	if _, err := os.Lstat(deep); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("deep tree survived")
	}
}

// Restoring is a write. If the parent directory an item came from has been
// replaced by a link, opening it without care would have undo write the user's
// restored data wherever that link points. A misdirected write leaks rather
// than destroys, which makes it quieter and no less serious.
func TestUndoWillNotRestoreThroughARedirectedParent(t *testing.T) {
	f := newFixture(t)
	originalParent := filepath.Join(f.root, "original-parent")
	if err := os.MkdirAll(originalParent, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	target := filepath.Join(originalParent, "secret-data")
	if err := os.WriteFile(target, []byte("sensitive"), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	manifest := mustPlan(t, candidatesFor(target), PlanOptions{})
	result, err := Apply(manifest, ApplyOptions{
		Staging: f.staging, Policy: AllowAll{}, Log: operationlog.Discard(),
	})
	if err != nil || result.AppliedCount != 1 {
		t.Fatalf("Apply: %v, applied %d", err, result.AppliedCount)
	}

	// While the item sits in staging, the parent it came from is replaced by a
	// link pointing somewhere the attacker controls.
	attackerDir := filepath.Join(f.root, "attacker-controlled")
	if err := os.MkdirAll(attackerDir, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.RemoveAll(originalParent); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.Symlink(attackerDir, originalParent); err != nil {
		t.Fatalf("setup: %v", err)
	}

	restore, err := Undo(result.Batch, operationlog.Discard())
	if err != nil {
		t.Fatalf("Undo: %v", err)
	}
	if restore.RestoredCount != 0 {
		t.Fatal("undo restored through a parent directory that had become a link")
	}
	if _, err := os.Stat(filepath.Join(attackerDir, "secret-data")); err == nil {
		t.Fatal("restored data was written into the attacker controlled directory")
	}
}

// A staged name is built from the original base name, which the caller does not
// control the length of. A name the filesystem refuses would turn a removal
// that already happened into one that cannot be recorded or undone.
func TestVeryLongNamesCanStillBeStagedAndRestored(t *testing.T) {
	f := newFixture(t)
	longName := ""
	for len(longName) < 240 {
		longName += "extremely-long-cache-directory-name-"
	}
	// A single path component has its own length limit, so the name is trimmed
	// to what a filesystem will accept before it is created.
	longName = longName[:240]

	target := f.writeFile(longName, "payload")
	manifest := mustPlan(t, candidatesFor(target), PlanOptions{})
	result, err := Apply(manifest, ApplyOptions{
		Staging: f.staging, Policy: AllowAll{}, Log: operationlog.Discard(),
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if result.AppliedCount != 1 {
		t.Fatalf("applied %d, want 1 (failed %d)", result.AppliedCount, result.FailedCount)
	}

	restore, err := Undo(result.Batch, operationlog.Discard())
	if err != nil {
		t.Fatalf("Undo: %v", err)
	}
	if restore.RestoredCount != 1 {
		t.Fatalf("restored %d, want 1", restore.RestoredCount)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("long named item was not restored: %v", err)
	}
}

// Staging an ancestor of the staging area would mean renaming a directory into
// its own subtree. The kernel refuses that, and the engine must surface it as a
// failure rather than losing the directory.
func TestStagingAnAncestorOfTheStagingAreaFailsSafely(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	staging, err := NewStagingArea(filepath.Join(workspace, "staging-area"))
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	manifest := mustPlan(t, candidatesFor(workspace), PlanOptions{})
	result, err := Apply(manifest, ApplyOptions{
		Staging: staging, Policy: AllowAll{}, Log: operationlog.Discard(),
	})
	if err != nil {
		t.Fatalf("Apply returned a run level error: %v", err)
	}
	if result.AppliedCount != 0 {
		t.Fatal("a directory containing the staging area was reported as staged")
	}
	if _, err := os.Stat(workspace); err != nil {
		t.Fatalf("the workspace was damaged: %v", err)
	}
}
