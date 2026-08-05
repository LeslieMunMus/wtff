package deletionengine

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	operationlog "github.com/lesliemusengi/wtff/internal/operation-log"
)

// blockList is a policy checker that protects an explicit set of paths.
type blockList struct{ blocked map[string]string }

func (b blockList) Protected(path string) (string, string, bool) {
	if reason, found := b.blocked[path]; found {
		return "test-rule", reason, true
	}
	return "", "", false
}

type fixture struct {
	t       *testing.T
	root    string
	staging *StagingArea
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	root := t.TempDir()
	// The staging area shares a volume with the work area so that renames
	// succeed, which is the arrangement on a real machine as well.
	staging, err := NewStagingArea(filepath.Join(root, "staging-area"))
	if err != nil {
		t.Fatalf("staging setup: %v", err)
	}
	return &fixture{t: t, root: root, staging: staging}
}

func (f *fixture) writeFile(name, contents string) string {
	f.t.Helper()
	path := filepath.Join(f.root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		f.t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		f.t.Fatalf("setup: %v", err)
	}
	return path
}

func (f *fixture) writeTree(name string, files map[string]string) string {
	f.t.Helper()
	root := filepath.Join(f.root, name)
	for rel, contents := range files {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			f.t.Fatalf("setup: %v", err)
		}
		if err := os.WriteFile(full, []byte(contents), 0o600); err != nil {
			f.t.Fatalf("setup: %v", err)
		}
	}
	return root
}

func candidatesFor(paths ...string) []Candidate {
	out := make([]Candidate, 0, len(paths))
	for _, path := range paths {
		out = append(out, Candidate{Path: path, RuleID: "test-rule", Reason: "test candidate"})
	}
	return out
}

func mustPlan(t *testing.T, candidates []Candidate, opts PlanOptions) *Manifest {
	t.Helper()
	if opts.Policy == nil {
		opts.Policy = AllowAll{}
	}
	if opts.Command == "" {
		opts.Command = "clean"
	}
	manifest, err := Plan(candidates, opts)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	return manifest
}

// Planning without a policy checker is refused rather than defaulted. A default
// would mean the mistake of forgetting to pass protection rules produces a plan
// with no protection, silently.
func TestPlanRequiresAnExplicitPolicy(t *testing.T) {
	_, err := Plan(candidatesFor("/tmp"), PlanOptions{Command: "clean"})
	if !errors.Is(err, ErrNoPolicy) {
		t.Fatalf("Plan without a policy = %v, want ErrNoPolicy", err)
	}
}

// The zero value of Action must be the reversible one, so that a caller who
// forgets to set it gets staging rather than permanent removal.
func TestUnsetActionDefaultsToStaging(t *testing.T) {
	f := newFixture(t)
	target := f.writeFile("cache-entry", "payload")

	manifest := mustPlan(t, candidatesFor(target), PlanOptions{})
	if manifest.Action != ActionStage {
		t.Fatalf("unset action produced %q, want %q", manifest.Action, ActionStage)
	}
}

func TestPlanSkipsUnusableCandidates(t *testing.T) {
	f := newFixture(t)
	present := f.writeFile("present", "payload")
	protectedPath := f.writeFile("protected", "payload")

	policy := blockList{blocked: map[string]string{}}
	// Policy is consulted with the resolved path, which differs from the
	// requested one under a temporary directory, so the fixture resolves it the
	// same way the engine will.
	resolvedProtected := resolveForTest(t, protectedPath)
	policy.blocked[resolvedProtected] = "test protection"

	candidates := []Candidate{
		{Path: present, RuleID: "r", Reason: "keeper"},
		{Path: filepath.Join(f.root, "absent"), RuleID: "r", Reason: "missing"},
		{Path: protectedPath, RuleID: "r", Reason: "protected"},
		{Path: "/System", RuleID: "r", Reason: "floor"},
		{Path: present, RuleID: "", Reason: ""},
	}

	manifest := mustPlan(t, candidates, PlanOptions{Policy: policy})
	if len(manifest.Entries) != 1 {
		for _, entry := range manifest.Entries {
			t.Logf("planned: %s", entry.ResolvedPath)
		}
		t.Fatalf("planned %d entries, want only the usable one", len(manifest.Entries))
	}
	if !strings.HasSuffix(manifest.Entries[0].ResolvedPath, "/present") {
		t.Fatalf("planned the wrong entry: %s", manifest.Entries[0].ResolvedPath)
	}
}

// Two paths can name one object. Planning it twice would have apply report a
// skip on the second, which reads as a problem and is not.
func TestPlanDeduplicatesTheSameObject(t *testing.T) {
	f := newFixture(t)
	target := f.writeFile("real-entry", "payload")
	link := filepath.Join(f.root, "link-directory")
	if err := os.Symlink(f.root, link); err != nil {
		t.Fatalf("setup: %v", err)
	}
	viaLink := filepath.Join(link, "real-entry")

	manifest := mustPlan(t, candidatesFor(target, viaLink), PlanOptions{})
	if len(manifest.Entries) != 1 {
		t.Fatalf("planned %d entries for one object, want 1", len(manifest.Entries))
	}
}

// The home directory is deliberately reached through a link here. Self
// protection compares against a resolved path, so roots recorded only in their
// declared form silently stop matching the moment any component of the home
// directory is a link. That failure mode produced the one outcome this guard
// exists to prevent, and it was found by this test rather than by review.
func TestSelfProtectionHoldsWhenHomeIsReachedThroughALink(t *testing.T) {
	base := t.TempDir()
	actualHome := filepath.Join(base, "actual-home")
	if err := os.MkdirAll(actualHome, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	linkedHome := filepath.Join(base, "linked-home")
	if err := os.Symlink(actualHome, linkedHome); err != nil {
		t.Fatalf("setup: %v", err)
	}
	t.Setenv("HOME", linkedHome)

	ownState := filepath.Join(linkedHome, "Library", "Application Support", "wtff", "staging")
	if err := os.MkdirAll(ownState, 0o700); err != nil {
		t.Fatalf("setup: %v", err)
	}

	manifest := mustPlan(t, candidatesFor(ownState), PlanOptions{})
	if len(manifest.Entries) != 0 {
		t.Fatalf("planned %d entries against wtff's own state reached through a link, want 0",
			len(manifest.Entries))
	}
}

// wtff must never stage its own state, because staging the staging area
// destroys the record undo depends on, making the act itself unrecoverable.
func TestPlanRefusesWtffOwnState(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	ownState := filepath.Join(home, "Library", "Application Support", "wtff", "staging")
	if err := os.MkdirAll(ownState, 0o700); err != nil {
		t.Fatalf("setup: %v", err)
	}
	logDir := filepath.Join(home, "Library", "Logs", "wtff")
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		t.Fatalf("setup: %v", err)
	}

	manifest := mustPlan(t, candidatesFor(ownState, logDir), PlanOptions{})
	if len(manifest.Entries) != 0 {
		t.Fatalf("planned %d entries against wtff's own state, want 0", len(manifest.Entries))
	}
}

func TestApplyStagesAndUndoRestores(t *testing.T) {
	f := newFixture(t)
	file := f.writeFile("cache-file", "payload")
	tree := f.writeTree("cache-tree", map[string]string{
		"one.txt":        "aaa",
		"nested/two.txt": "bbbb",
	})

	manifest := mustPlan(t, candidatesFor(file, tree), PlanOptions{MeasureSizes: true})
	if len(manifest.Entries) != 2 {
		t.Fatalf("planned %d entries, want 2", len(manifest.Entries))
	}
	if manifest.TotalBytes != 14 {
		t.Fatalf("total bytes = %d, want 14", manifest.TotalBytes)
	}

	result, err := Apply(manifest, ApplyOptions{
		Staging: f.staging, Policy: AllowAll{}, Log: operationlog.Discard(),
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if result.AppliedCount != 2 {
		t.Fatalf("applied %d, want 2 (skipped %d, failed %d)",
			result.AppliedCount, result.SkippedCount, result.FailedCount)
	}

	for _, path := range []string{file, tree} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s still present after staging", path)
		}
	}

	// The batch must be readable from disk by a separate process, which is what
	// undo will be.
	reloaded, err := LoadBatch(result.Batch.Dir())
	if err != nil {
		t.Fatalf("LoadBatch: %v", err)
	}
	if len(reloaded.Items) != 2 {
		t.Fatalf("batch holds %d items, want 2", len(reloaded.Items))
	}

	restore, err := Undo(reloaded, operationlog.Discard())
	if err != nil {
		t.Fatalf("Undo: %v", err)
	}
	if restore.RestoredCount != 2 {
		t.Fatalf("restored %d, want 2", restore.RestoredCount)
	}

	for _, path := range []string{file, tree} {
		if _, err := os.Lstat(path); err != nil {
			t.Fatalf("%s was not restored: %v", path, err)
		}
	}
	contents, err := os.ReadFile(filepath.Join(tree, "nested", "two.txt"))
	if err != nil || string(contents) != "bbbb" {
		t.Fatalf("restored tree contents wrong: %q, %v", contents, err)
	}

	// A fully restored batch leaves nothing behind.
	if _, err := os.Stat(reloaded.Dir()); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("batch directory should be gone after a complete restore")
	}
}

// The central guarantee across the plan and apply boundary. Descriptors do not
// survive a persisted manifest, so identity is what ties the approved plan to
// the object finally acted on.
func TestApplyRefusesAnObjectThatChangedSinceThePlan(t *testing.T) {
	f := newFixture(t)
	target := f.writeFile("swappable", "original")

	manifest := mustPlan(t, candidatesFor(target), PlanOptions{})
	if len(manifest.Entries) != 1 {
		t.Fatalf("expected one planned entry")
	}

	// Replace the object while keeping the name, which is what a race between
	// review and execution looks like.
	if err := os.Remove(target); err != nil {
		t.Fatalf("swap setup: %v", err)
	}
	if err := os.WriteFile(target, []byte("substituted"), 0o600); err != nil {
		t.Fatalf("swap setup: %v", err)
	}

	result, err := Apply(manifest, ApplyOptions{
		Staging: f.staging, Policy: AllowAll{}, Log: operationlog.Discard(),
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if result.AppliedCount != 0 {
		t.Fatal("apply removed an object that was not the one planned")
	}
	if result.SkippedCount != 1 {
		t.Fatalf("skipped %d, want 1", result.SkippedCount)
	}
	if !strings.Contains(result.Outcomes[0].Reason, "changed since the plan") {
		t.Fatalf("skip reason = %q, want it to name the change", result.Outcomes[0].Reason)
	}

	contents, err := os.ReadFile(target)
	if err != nil || string(contents) != "substituted" {
		t.Fatalf("substituted file should be untouched, got %q, %v", contents, err)
	}
}

func TestApplyRefusesATamperedManifest(t *testing.T) {
	f := newFixture(t)
	keep := f.writeFile("should-survive", "payload")

	manifest := mustPlan(t, candidatesFor(keep), PlanOptions{})
	// Redirect the plan after it was sealed, exactly as an edited plan file
	// would.
	manifest.Entries[0].ResolvedPath = filepath.Join(f.root, "elsewhere")

	_, err := Apply(manifest, ApplyOptions{
		Staging: f.staging, Policy: AllowAll{}, Log: operationlog.Discard(),
	})
	if !errors.Is(err, ErrDigestMismatch) {
		t.Fatalf("Apply on an edited manifest = %v, want ErrDigestMismatch", err)
	}
	if _, statErr := os.Stat(keep); statErr != nil {
		t.Fatal("nothing should have been touched")
	}
}

// Policy is consulted again at apply time, not only during planning, because a
// manifest can be applied long after it was made.
func TestApplyRechecksPolicy(t *testing.T) {
	f := newFixture(t)
	target := f.writeFile("newly-protected", "payload")

	manifest := mustPlan(t, candidatesFor(target), PlanOptions{})

	policy := blockList{blocked: map[string]string{
		manifest.Entries[0].ResolvedPath: "protected after planning",
	}}
	result, err := Apply(manifest, ApplyOptions{
		Staging: f.staging, Policy: policy, Log: operationlog.Discard(),
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if result.AppliedCount != 0 || result.SkippedCount != 1 {
		t.Fatalf("applied %d skipped %d, want 0 applied and 1 skipped",
			result.AppliedCount, result.SkippedCount)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatal("a newly protected target should still be present")
	}
}

// Undo must never overwrite. Replacing whatever occupies the original location
// would let undo destroy data that was never part of the operation.
func TestUndoWillNotOverwriteAnOccupiedLocation(t *testing.T) {
	f := newFixture(t)
	target := f.writeFile("contested", "original")

	manifest := mustPlan(t, candidatesFor(target), PlanOptions{})
	result, err := Apply(manifest, ApplyOptions{
		Staging: f.staging, Policy: AllowAll{}, Log: operationlog.Discard(),
	})
	if err != nil || result.AppliedCount != 1 {
		t.Fatalf("Apply: %v, applied %d", err, result.AppliedCount)
	}

	// Something else takes the name while the item sits in staging.
	if err := os.WriteFile(target, []byte("newer content"), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	restore, err := Undo(result.Batch, operationlog.Discard())
	if err != nil {
		t.Fatalf("Undo: %v", err)
	}
	if restore.RestoredCount != 0 || restore.SkippedCount != 1 {
		t.Fatalf("restored %d skipped %d, want 0 restored and 1 skipped",
			restore.RestoredCount, restore.SkippedCount)
	}

	contents, err := os.ReadFile(target)
	if err != nil || string(contents) != "newer content" {
		t.Fatalf("undo overwrote the occupying file: %q, %v", contents, err)
	}

	// The unrestored item stays in staging rather than being lost.
	if len(result.Batch.Items) != 1 {
		t.Fatalf("batch holds %d items after a skipped restore, want 1", len(result.Batch.Items))
	}
}

func TestPurgeRemovesIrreversibly(t *testing.T) {
	f := newFixture(t)
	file := f.writeFile("doomed-file", "payload")
	tree := f.writeTree("doomed-tree", map[string]string{"inner.txt": "x"})

	manifest := mustPlan(t, candidatesFor(file, tree), PlanOptions{Action: ActionPurge})
	result, err := Apply(manifest, ApplyOptions{Policy: AllowAll{}, Log: operationlog.Discard()})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if result.AppliedCount != 2 {
		t.Fatalf("applied %d, want 2", result.AppliedCount)
	}
	if result.Batch != nil {
		t.Fatal("a purge run should not create a staging batch")
	}
	for _, path := range []string{file, tree} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s survived a purge", path)
		}
	}
}

func TestStagingActionRequiresAStagingArea(t *testing.T) {
	f := newFixture(t)
	target := f.writeFile("entry", "payload")
	manifest := mustPlan(t, candidatesFor(target), PlanOptions{})

	_, err := Apply(manifest, ApplyOptions{Policy: AllowAll{}, Log: operationlog.Discard()})
	if !errors.Is(err, ErrNoStagingArea) {
		t.Fatalf("Apply without a staging area = %v, want ErrNoStagingArea", err)
	}
}

// A run in which nothing could be applied must not leave an empty batch behind,
// which a later list would report as a recoverable batch holding nothing.
func TestEmptyBatchIsNotLeftBehind(t *testing.T) {
	f := newFixture(t)
	target := f.writeFile("vanishing", "payload")
	manifest := mustPlan(t, candidatesFor(target), PlanOptions{})

	if err := os.Remove(target); err != nil {
		t.Fatalf("setup: %v", err)
	}

	result, err := Apply(manifest, ApplyOptions{
		Staging: f.staging, Policy: AllowAll{}, Log: operationlog.Discard(),
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if result.Batch != nil {
		t.Fatal("expected no batch when nothing was staged")
	}

	batches, err := f.staging.ListBatches()
	if err != nil {
		t.Fatalf("ListBatches: %v", err)
	}
	if len(batches) != 0 {
		t.Fatalf("staging area holds %d batches, want 0", len(batches))
	}
}

// Two items with the same base name from different directories must not
// collide in staging, or one would overwrite the other and be lost.
func TestStagedNamesDoNotCollide(t *testing.T) {
	f := newFixture(t)
	first := f.writeFile("dir-one/Cache", "first")
	second := f.writeFile("dir-two/Cache", "second")

	manifest := mustPlan(t, candidatesFor(first, second), PlanOptions{})
	result, err := Apply(manifest, ApplyOptions{
		Staging: f.staging, Policy: AllowAll{}, Log: operationlog.Discard(),
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if result.AppliedCount != 2 {
		t.Fatalf("applied %d, want 2", result.AppliedCount)
	}

	restore, err := Undo(result.Batch, operationlog.Discard())
	if err != nil {
		t.Fatalf("Undo: %v", err)
	}
	if restore.RestoredCount != 2 {
		t.Fatalf("restored %d, want 2", restore.RestoredCount)
	}

	firstContents, _ := os.ReadFile(first)
	secondContents, _ := os.ReadFile(second)
	if string(firstContents) != "first" || string(secondContents) != "second" {
		t.Fatalf("staged items were confused: %q and %q", firstContents, secondContents)
	}
}
