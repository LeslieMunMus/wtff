package deletionengine

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	operationlog "github.com/lesliemunmus/wtff/internal/operation-log"
)

// stageOne stages a single file and returns the staging area and its batch.
func stageOne(t *testing.T, contents string) (*StagingArea, *Batch, string) {
	t.Helper()
	home := t.TempDir()
	target := filepath.Join(home, "cache-entry")
	if err := os.WriteFile(target, []byte(contents), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	policy := AllowAll{}
	manifest, err := Plan([]Candidate{{Path: target, RuleID: "r", Reason: "x"}},
		PlanOptions{Command: "test", Policy: policy, Log: operationlog.Discard(), MeasureSizes: true})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}

	staging, err := NewStagingArea(filepath.Join(home, "staging"))
	if err != nil {
		t.Fatalf("staging area: %v", err)
	}
	if _, err := Apply(manifest, ApplyOptions{
		Staging: staging, Policy: policy, Log: operationlog.Discard(),
	}); err != nil {
		t.Fatalf("apply: %v", err)
	}

	batches, err := staging.ListBatches()
	if err != nil || len(batches) != 1 {
		t.Fatalf("expected one batch, got %d, %v", len(batches), err)
	}
	return staging, batches[0], target
}

func TestPurgeBatchRemovesEverythingAndClearsTheBatch(t *testing.T) {
	staging, batch, _ := stageOne(t, "reclaimable")
	batchDir := batch.Dir()

	result, err := staging.PurgeBatch(batch, operationlog.Discard())
	if err != nil {
		t.Fatalf("purge: %v", err)
	}
	if result.PurgedCount != 1 || result.FailedCount != 0 {
		t.Fatalf("purged %d, failed %d", result.PurgedCount, result.FailedCount)
	}
	if result.BytesReclaimed != int64(len("reclaimable")) {
		t.Fatalf("reclaimed %d bytes", result.BytesReclaimed)
	}
	if _, err := os.Stat(batchDir); !os.IsNotExist(err) {
		t.Fatal("an emptied batch directory should be removed")
	}

	remaining, err := staging.ListBatches()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(remaining) != 0 {
		t.Fatalf("staging area still holds %d batch(es)", len(remaining))
	}
}

// A purge must not restore anything on its way out. This pins the property
// that separates it from undo, since both operate on the same batch.
func TestPurgeBatchDoesNotRestoreToTheOriginalLocation(t *testing.T) {
	staging, batch, target := stageOne(t, "reclaimable")

	if _, err := staging.PurgeBatch(batch, operationlog.Discard()); err != nil {
		t.Fatalf("purge: %v", err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatal("purge must not put the item back where it came from")
	}
}

// The containment guard is the whole safety story for this operation, so it
// gets tested against the shape an attack actually takes rather than only the
// happy path.
func TestPurgeBatchRefusesABatchOutsideTheStagingArea(t *testing.T) {
	staging, batch, _ := stageOne(t, "reclaimable")

	// The forged batch is built so containment is the only guard that can
	// refuse it: its directory's base name matches its own identifier, so the
	// identifier cross-check is satisfied, and the name is an ordinary path
	// component, so the component check is satisfied too.
	//
	// An earlier version of this test used a directory whose name did not match
	// the identifier. It passed with the containment check deleted, because the
	// identifier check was refusing it instead, and it would have reported the
	// containment guard as working long after someone removed it.
	outside := t.TempDir()
	forgedDir := filepath.Join(outside, batch.BatchID)
	if err := os.MkdirAll(filepath.Join(forgedDir, "items"), 0o700); err != nil {
		t.Fatalf("setup: %v", err)
	}
	victim := filepath.Join(forgedDir, "items", batch.Items[0].StagedName)
	if err := os.WriteFile(victim, []byte("keep me"), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	forged := &Batch{Version: stagingSchemaVersion, BatchID: batch.BatchID,
		Items: batch.Items, dir: forgedDir}

	_, err := staging.PurgeBatch(forged, operationlog.Discard())
	if !errors.Is(err, ErrBatchOutsideStagingArea) {
		t.Fatalf("expected a containment refusal, got %v", err)
	}
	if _, err := os.Stat(victim); err != nil {
		t.Fatal("the refused purge must not have touched anything")
	}
}

// A record whose identifier disagrees with where it was found is refused
// rather than resolved, since choosing which one to believe while holding a
// delete is not a judgment call worth making.
func TestPurgeBatchRefusesAMismatchedIdentifier(t *testing.T) {
	staging, batch, _ := stageOne(t, "reclaimable")
	batch.BatchID = "some-other-batch"

	_, err := staging.PurgeBatch(batch, operationlog.Discard())
	if !errors.Is(err, ErrBatchOutsideStagingArea) {
		t.Fatalf("expected a refusal, got %v", err)
	}
	if _, statErr := os.Stat(batch.Dir()); statErr != nil {
		t.Fatal("the refused purge must not have removed the batch")
	}
}

// FindBatch is what turns a typed identifier into a target, so it is the
// place a traversal would enter. The path-traversal defect this guards
// against was already found once elsewhere in this project.
func TestFindBatchRefusesTraversalIdentifiers(t *testing.T) {
	staging, batch, _ := stageOne(t, "reclaimable")

	for _, id := range []string{"..", ".", "", "../../etc", "a/b", "/etc"} {
		if _, err := staging.FindBatch(id); err == nil {
			t.Fatalf("FindBatch(%q) should have been refused", id)
		}
	}

	// The cases above would fail anyway on a missing record, so on their own
	// they do not prove the name check is doing anything. This one would
	// succeed without it: a real, loadable batch record placed one level above
	// the staging area, reachable only by a traversal.
	sibling := filepath.Join(filepath.Dir(staging.Root()), "planted-batch")
	if err := os.MkdirAll(filepath.Join(sibling, "items"), 0o700); err != nil {
		t.Fatalf("setup: %v", err)
	}
	planted := &Batch{Version: stagingSchemaVersion, BatchID: "planted-batch",
		Items: batch.Items, dir: sibling}
	if err := planted.writeRecord(); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if _, err := LoadBatch(sibling); err != nil {
		t.Fatalf("setup: the planted record should be loadable, got %v", err)
	}

	if _, err := staging.FindBatch("../planted-batch"); err == nil {
		t.Fatal("FindBatch followed a traversal to a real record outside the staging area")
	}
}

func TestFindBatchLocatesARealBatch(t *testing.T) {
	staging, batch, _ := stageOne(t, "reclaimable")

	found, err := staging.FindBatch(batch.BatchID)
	if err != nil {
		t.Fatalf("FindBatch: %v", err)
	}
	if found.BatchID != batch.BatchID || len(found.Items) != len(batch.Items) {
		t.Fatal("FindBatch returned a different batch")
	}
}

// A symlink inside a staged batch must be unlinked, never followed, or a
// purge could delete something that was never staged.
func TestPurgeBatchDoesNotFollowSymlinksOutOfTheBatch(t *testing.T) {
	staging, batch, _ := stageOne(t, "reclaimable")

	outside := t.TempDir()
	victim := filepath.Join(outside, "keep-me")
	if err := os.WriteFile(victim, []byte("important"), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	// Plant a link inside the batch pointing at data the batch does not own,
	// the way a hostile or corrupted staging area would look.
	link := filepath.Join(batch.Dir(), "items", "0001-link")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatalf("setup: %v", err)
	}
	batch.Items = append(batch.Items, StagedItem{
		Index: 1, OriginalPath: filepath.Join(outside, "phantom"),
		StagedName: "0001-link", IsSymlink: true,
	})

	if _, err := staging.PurgeBatch(batch, operationlog.Discard()); err != nil {
		t.Fatalf("purge: %v", err)
	}
	if _, err := os.Stat(victim); err != nil {
		t.Fatal("purge followed a symlink and destroyed data outside the batch")
	}
}

// If the items directory itself has been replaced by a link, the purge must
// refuse rather than delete whatever the link names. A path based recursive
// remove would follow it, which is the difference this test exists to hold.
func TestPurgeBatchRefusesWhenItemsDirectoryIsALink(t *testing.T) {
	staging, batch, _ := stageOne(t, "reclaimable")

	outside := t.TempDir()
	victim := filepath.Join(outside, "keep-me")
	if err := os.WriteFile(victim, []byte("important"), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	items := filepath.Join(batch.Dir(), "items")
	if err := os.RemoveAll(items); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.Symlink(outside, items); err != nil {
		t.Fatalf("setup: %v", err)
	}

	if _, err := staging.PurgeBatch(batch, operationlog.Discard()); err == nil {
		t.Fatal("purge should refuse to open a linked items directory")
	}
	if _, err := os.Stat(victim); err != nil {
		t.Fatal("purge followed a linked items directory and destroyed outside data")
	}
}

func TestPurgeAllClearsEveryBatch(t *testing.T) {
	staging, _, _ := stageOne(t, "first")

	// A second batch in the same staging area, so PurgeAll has more than one
	// thing to iterate over.
	home := t.TempDir()
	second := filepath.Join(home, "another")
	if err := os.WriteFile(second, []byte("second"), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}
	policy := AllowAll{}
	manifest, err := Plan([]Candidate{{Path: second, RuleID: "r", Reason: "x"}},
		PlanOptions{Command: "test", Policy: policy, Log: operationlog.Discard(), MeasureSizes: true})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if _, err := Apply(manifest, ApplyOptions{
		Staging: staging, Policy: policy, Log: operationlog.Discard(),
	}); err != nil {
		t.Fatalf("apply: %v", err)
	}

	result, err := staging.PurgeAll(operationlog.Discard())
	if err != nil {
		t.Fatalf("purge all: %v", err)
	}
	if result.PurgedCount != 2 {
		t.Fatalf("purged %d, want 2", result.PurgedCount)
	}

	remaining, err := staging.ListBatches()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(remaining) != 0 {
		t.Fatalf("%d batch(es) survived PurgeAll", len(remaining))
	}
}

// Every destroyed item must appear in the operation log under its original
// path, since that record is the only account of the removal that survives it.
func TestPurgeBatchLogsOriginalPaths(t *testing.T) {
	staging, batch, target := stageOne(t, "reclaimable")

	logPath := filepath.Join(t.TempDir(), "operations.log")
	log, err := operationlog.Open(logPath, "purge")
	if err != nil {
		t.Fatalf("open log: %v", err)
	}
	if _, err := staging.PurgeBatch(batch, log); err != nil {
		t.Fatalf("purge: %v", err)
	}
	if err := log.Close(); err != nil {
		t.Fatalf("close log: %v", err)
	}

	written, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if !strings.Contains(string(written), target) {
		t.Fatalf("the log does not name what was destroyed:\n%s", written)
	}
	if !strings.Contains(string(written), "purged") {
		t.Fatalf("the log does not record the purge:\n%s", written)
	}
}
