package deletionengine

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	operationlog "github.com/lesliemusengi/wtff/internal/operation-log"
	"golang.org/x/sys/unix"
)

// Filenames a person can create through Finder or a shell, which a tool that
// builds paths by string concatenation somewhere gets wrong.
var awkwardNames = []string{
	"file with spaces",
	"file'with'quotes",
	`file"with"double`,
	"file$with$dollars",
	"file;with;semicolons",
	"file*with*globs",
	"café-unicode-é",
	"emoji-🗑-name",
	"-leading-dash",
	"trailing.dots...",
	"very" + strings.Repeat("long", 40) + "name",
}

// Staging and restoring must survive every name the filesystem accepts. Names
// are passed to renameat as bytes and never through a shell, so this pins that
// nothing along the way starts interpreting them.
func TestAwkwardFilenamesStageAndRestore(t *testing.T) {
	home := t.TempDir()
	source := filepath.Join(home, "cache")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}

	var candidates []Candidate
	var created []string
	for _, name := range awkwardNames {
		path := filepath.Join(source, name)
		if err := os.WriteFile(path, []byte("payload"), 0o600); err != nil {
			// A name this filesystem genuinely cannot represent is not a defect
			// in wtff, so it is skipped rather than failed.
			t.Logf("filesystem refused %q: %v", name, err)
			continue
		}
		created = append(created, path)
		candidates = append(candidates, Candidate{Path: path, RuleID: "r", Reason: "x"})
	}
	if len(candidates) == 0 {
		t.Fatal("setup created nothing")
	}

	manifest, err := Plan(candidates, PlanOptions{
		Command: "test", Policy: AllowAll{}, Log: operationlog.Discard(), MeasureSizes: true,
	})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(manifest.Entries) != len(candidates) {
		t.Fatalf("planned %d of %d awkward names", len(manifest.Entries), len(candidates))
	}

	staging, err := NewStagingArea(filepath.Join(home, "staging"))
	if err != nil {
		t.Fatalf("staging area: %v", err)
	}
	result, err := Apply(manifest, ApplyOptions{
		Staging: staging, Policy: AllowAll{}, Log: operationlog.Discard(),
	})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if result.AppliedCount != len(candidates) {
		t.Fatalf("staged %d of %d", result.AppliedCount, len(candidates))
	}
	for _, path := range created {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("%q was not staged away", filepath.Base(path))
		}
	}

	batches, err := staging.ListBatches()
	if err != nil || len(batches) != 1 {
		t.Fatalf("expected one batch, got %d, %v", len(batches), err)
	}
	restore, err := Undo(batches[0], operationlog.Discard())
	if err != nil {
		t.Fatalf("undo: %v", err)
	}
	if restore.RestoredCount != len(created) {
		t.Fatalf("restored %d of %d", restore.RestoredCount, len(created))
	}
	for _, path := range created {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("%q did not come back: %v", filepath.Base(path), err)
		}
	}
}

// A path carrying control characters is refused, which this test exists to
// document rather than to complain about.
//
// Such a name is legal on macOS and vanishingly rare, and printing one to a
// terminal hands it the ability to emit escape sequences: a filename can move
// the cursor, rewrite the line above it, or hide what a confirmation prompt
// actually says. Refusing costs the disk space of one unusual file. Accepting
// costs the trustworthiness of every path wtff prints, including the ones in
// the prompt that asks whether to delete permanently.
//
// The awkward name list above deliberately excludes these, because they belong
// here as an assertion rather than there as an expected success.
func TestPathsWithControlCharactersAreRefused(t *testing.T) {
	home := t.TempDir()
	source := filepath.Join(home, "cache")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}

	for _, name := range []string{
		"file\twith\ttabs",
		"file\nwith\nnewlines",
		"escape\x1b[31mred",
	} {
		path := filepath.Join(source, name)
		if err := os.WriteFile(path, []byte("payload"), 0o600); err != nil {
			t.Logf("filesystem refused %q: %v", name, err)
			continue
		}

		var skipReason string
		manifest, err := Plan([]Candidate{{Path: path, RuleID: "r", Reason: "x"}},
			PlanOptions{
				Command: "test", Policy: AllowAll{}, Log: operationlog.Discard(),
				SkipSink: func(_, reason string) { skipReason = reason },
			})
		if err != nil {
			t.Fatalf("plan: %v", err)
		}
		if len(manifest.Entries) != 0 {
			t.Errorf("%q was planned, but a path with control characters must be refused", name)
		}
		if !strings.Contains(skipReason, "control characters") {
			t.Errorf("%q was refused for the wrong reason: %s", name, skipReason)
		}
		if _, err := os.Stat(path); err != nil {
			t.Errorf("the refused file should be untouched: %v", err)
		}
	}
}

// A directory whose contents cannot be read must be reported as a failure, not
// silently treated as empty and then removed. Reporting success for a tree
// that was not actually emptied is how a tool loses data it claims to have
// handled.
func TestPurgeReportsAnUnreadableSubdirectory(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("running as root, which bypasses the permissions under test")
	}

	root := t.TempDir()
	target := filepath.Join(root, "tree")
	locked := filepath.Join(target, "locked")
	if err := os.MkdirAll(locked, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(locked, "hidden"), []byte("x"), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatalf("setup: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	parentFD, err := openDirForTest(t, target)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	err = removeTreeAt(parentFD, "locked", 0)
	if err == nil {
		t.Fatal("removing an unreadable directory should be reported, not passed over")
	}
	if !strings.Contains(err.Error(), "locked") {
		t.Fatalf("the failure should name what could not be removed, got: %v", err)
	}
}

// An already absent target is the desired state, not a failure. Cleanup work
// races with the applications being cleaned up, and treating "already gone" as
// an error would make ordinary runs look broken.
func TestRemovingSomethingAlreadyGoneSucceeds(t *testing.T) {
	root := t.TempDir()
	parentFD, err := openDirForTest(t, root)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := removeTreeAt(parentFD, "never-existed", 0); err != nil {
		t.Fatalf("an absent target should be a no-op, got %v", err)
	}
}

// Two runs sharing one staging area must not collide. Batch directories carry
// a random suffix precisely because a timestamp alone repeats inside a second.
func TestConcurrentStagingProducesDistinctBatches(t *testing.T) {
	home := t.TempDir()
	staging, err := NewStagingArea(filepath.Join(home, "staging"))
	if err != nil {
		t.Fatalf("staging area: %v", err)
	}

	const runs = 8
	var wg sync.WaitGroup
	errs := make(chan error, runs)

	for i := 0; i < runs; i++ {
		target := filepath.Join(home, "item", string(rune('a'+i)))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatalf("setup: %v", err)
		}
		if err := os.WriteFile(target, []byte("payload"), 0o600); err != nil {
			t.Fatalf("setup: %v", err)
		}

		wg.Add(1)
		go func(path string) {
			defer wg.Done()
			manifest, err := Plan([]Candidate{{Path: path, RuleID: "r", Reason: "x"}},
				PlanOptions{Command: "test", Policy: AllowAll{}, Log: operationlog.Discard()})
			if err != nil {
				errs <- err
				return
			}
			if _, err := Apply(manifest, ApplyOptions{
				Staging: staging, Policy: AllowAll{}, Log: operationlog.Discard(),
			}); err != nil {
				errs <- err
			}
		}(target)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent staging failed: %v", err)
	}

	batches, err := staging.ListBatches()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(batches) != runs {
		t.Fatalf("expected %d distinct batches, got %d", runs, len(batches))
	}

	seen := make(map[string]bool, runs)
	for _, batch := range batches {
		if seen[batch.BatchID] {
			t.Fatalf("two runs produced the same batch id %q", batch.BatchID)
		}
		seen[batch.BatchID] = true
	}
}

// A batch directory without a readable record is left alone rather than
// treated as a batch, since it may be an interrupted staging run holding
// recoverable data.
func TestUnreadableBatchDirectoryIsNotListed(t *testing.T) {
	home := t.TempDir()
	staging, err := NewStagingArea(filepath.Join(home, "staging"))
	if err != nil {
		t.Fatalf("staging area: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(staging.Root(), "not-a-batch"), 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	// A file where a batch directory would be, which ListBatches must ignore
	// rather than try to read.
	if err := os.WriteFile(filepath.Join(staging.Root(), "stray-file"), []byte("x"), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	batches, err := staging.ListBatches()
	if err != nil {
		t.Fatalf("listing should tolerate junk in the staging area, got %v", err)
	}
	if len(batches) != 0 {
		t.Fatalf("expected no usable batches, got %d", len(batches))
	}
}

// Restoring must never overwrite. Something else occupying the original
// location means the item stays staged and is reported, because replacing
// whatever is there would let undo destroy data it never removed.
func TestUndoRefusesToOverwriteAnOccupiedLocation(t *testing.T) {
	home := t.TempDir()
	target := filepath.Join(home, "cache-entry")
	if err := os.WriteFile(target, []byte("original"), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	manifest, err := Plan([]Candidate{{Path: target, RuleID: "r", Reason: "x"}},
		PlanOptions{Command: "test", Policy: AllowAll{}, Log: operationlog.Discard()})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	staging, err := NewStagingArea(filepath.Join(home, "staging"))
	if err != nil {
		t.Fatalf("staging area: %v", err)
	}
	if _, err := Apply(manifest, ApplyOptions{
		Staging: staging, Policy: AllowAll{}, Log: operationlog.Discard(),
	}); err != nil {
		t.Fatalf("apply: %v", err)
	}

	// Something new takes the old name before the restore.
	if err := os.WriteFile(target, []byte("replacement"), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	batches, _ := staging.ListBatches()
	result, err := Undo(batches[0], operationlog.Discard())
	if err != nil {
		t.Fatalf("undo: %v", err)
	}
	if result.RestoredCount != 0 || result.SkippedCount != 1 {
		t.Fatalf("expected the item to be left staged, got %d restored, %d skipped",
			result.RestoredCount, result.SkippedCount)
	}

	contents, err := os.ReadFile(target)
	if err != nil || string(contents) != "replacement" {
		t.Fatalf("undo overwrote data it never removed: %q, %v", contents, err)
	}

	// Still staged, so a person can deal with it deliberately.
	if remaining, _ := staging.ListBatches(); len(remaining) != 1 {
		t.Fatal("the unrestored item should still be held in staging")
	}
}

// Measuring a tree it cannot fully read reports a floor rather than a total,
// so nothing downstream presents a partial sum as exact.
func TestSizeOfAnUnreadableTreeIsMarkedIncomplete(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("running as root, which bypasses the permissions under test")
	}

	root := t.TempDir()
	readable := filepath.Join(root, "readable")
	locked := filepath.Join(root, "locked")
	if err := os.MkdirAll(readable, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(readable, "f"), []byte("0123456789"), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.MkdirAll(locked, 0o000); err != nil {
		t.Fatalf("setup: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	total, complete := walkSize(root)
	if complete {
		t.Fatal("a tree with an unreadable subdirectory must not report a complete size")
	}
	if total < 10 {
		t.Fatalf("the readable part should still be counted, got %d", total)
	}
}

// openDirForTest opens a directory descriptor and closes it when the test
// ends, so the adversarial cases can drive descriptor relative primitives
// directly.
func openDirForTest(t *testing.T, path string) (int, error) {
	t.Helper()
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return 0, err
	}
	t.Cleanup(func() { _ = unix.Close(fd) })
	return fd, nil
}
