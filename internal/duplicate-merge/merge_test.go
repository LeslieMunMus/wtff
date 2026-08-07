package duplicatemerge

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	duplicatescan "github.com/lesliemunmus/wtff/internal/duplicate-scan"
	operationlog "github.com/lesliemunmus/wtff/internal/operation-log"
)

// groupOf builds a scan group from paths, oldest first, matching what the
// detector produces.
func groupOf(t *testing.T, paths ...string) duplicatescan.Group {
	t.Helper()
	group := duplicatescan.Group{Size: 1024, Digest: "test"}
	base := time.Now().Add(-time.Duration(len(paths)) * time.Hour)
	for i, path := range paths {
		group.Files = append(group.Files, duplicatescan.File{
			Path:    path,
			Size:    1024,
			ModTime: base.Add(time.Duration(i) * time.Hour),
		})
	}
	return group
}

func write(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}
}

func TestOldestStaysAndOthersGatherAroundIt(t *testing.T) {
	root := t.TempDir()
	oldest := filepath.Join(root, "Documents", "report.pdf")
	newer := filepath.Join(root, "Downloads", "report.pdf")
	write(t, oldest, "same")
	write(t, newer, "same")

	plan, err := PlanMerge(groupOf(t, oldest, newer))
	if err != nil {
		t.Fatalf("plan: %v", err)
	}

	if plan.Destination != filepath.Join(root, "Documents") {
		t.Fatalf("destination = %q, want the oldest copy's directory", plan.Destination)
	}
	if plan.Keeper != oldest {
		t.Fatalf("keeper = %q, want the oldest copy", plan.Keeper)
	}
	if len(plan.Moves) != 1 {
		t.Fatalf("expected one move, got %+v", plan.Moves)
	}

	// The name says it is a copy, and the extension stays last so whatever
	// opened it before still opens it.
	want := filepath.Join(root, "Documents", "report copy.pdf")
	if plan.Moves[0].To != want {
		t.Fatalf("move target = %q, want %q", plan.Moves[0].To, want)
	}
}

// The rule everything else defers to. A merge moves files into a directory
// that already holds files, and overwriting one would destroy data the person
// never chose to touch.
func TestMergeNeverOverwritesAnExistingFile(t *testing.T) {
	root := t.TempDir()
	documents := filepath.Join(root, "Documents")
	oldest := filepath.Join(documents, "report.pdf")
	newer := filepath.Join(root, "Downloads", "report.pdf")
	write(t, oldest, "the duplicate")
	write(t, newer, "the duplicate")

	// An unrelated file already holding the name the merge would reach for.
	occupied := filepath.Join(documents, "report copy.pdf")
	write(t, occupied, "SOMETHING ELSE ENTIRELY")

	plan, err := PlanMerge(groupOf(t, oldest, newer))
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if plan.Moves[0].To == occupied {
		t.Fatal("the plan targets a name that is already taken")
	}

	if _, err := Apply(plan, operationlog.Discard()); err != nil {
		t.Fatalf("apply: %v", err)
	}

	contents, err := os.ReadFile(occupied)
	if err != nil {
		t.Fatalf("the occupying file is gone: %v", err)
	}
	if string(contents) != "SOMETHING ELSE ENTIRELY" {
		t.Fatalf("the occupying file was overwritten, it now holds %q", contents)
	}
}

// Names are allocated across the whole plan, not per move, so two copies
// merging at once cannot both be promised the same destination.
func TestTwoCopiesDoNotCollideWithEachOther(t *testing.T) {
	root := t.TempDir()
	oldest := filepath.Join(root, "Documents", "photo.jpg")
	second := filepath.Join(root, "Downloads", "photo.jpg")
	third := filepath.Join(root, "Desktop", "photo.jpg")
	for _, path := range []string{oldest, second, third} {
		write(t, path, "same")
	}

	plan, err := PlanMerge(groupOf(t, oldest, second, third))
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(plan.Moves) != 2 {
		t.Fatalf("expected two moves, got %+v", plan.Moves)
	}
	if plan.Moves[0].To == plan.Moves[1].To {
		t.Fatalf("both moves target %q", plan.Moves[0].To)
	}

	result, err := Apply(plan, operationlog.Discard())
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if result.MovedCount != 2 || result.FailedCount != 0 {
		t.Fatalf("moved %d, failed %d", result.MovedCount, result.FailedCount)
	}

	// All three survive, which is the entire point of merging rather than
	// deleting.
	for _, name := range []string{"photo.jpg", "photo copy.jpg", "photo copy 2.jpg"} {
		if _, err := os.Stat(filepath.Join(root, "Documents", name)); err != nil {
			t.Errorf("%s is missing after the merge: %v", name, err)
		}
	}
}

// Every copy survives a merge. Nothing is deleted, which is what makes this
// different from staging the extras.
func TestNothingIsRemovedByAMerge(t *testing.T) {
	root := t.TempDir()
	oldest := filepath.Join(root, "a", "file.txt")
	newer := filepath.Join(root, "b", "file.txt")
	write(t, oldest, "contents")
	write(t, newer, "contents")

	plan, _ := PlanMerge(groupOf(t, oldest, newer))
	if _, err := Apply(plan, operationlog.Discard()); err != nil {
		t.Fatalf("apply: %v", err)
	}

	entries, err := os.ReadDir(filepath.Join(root, "a"))
	if err != nil {
		t.Fatalf("reading destination: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected both copies in the destination, found %d", len(entries))
	}
	if _, err := os.Stat(newer); !os.IsNotExist(err) {
		t.Fatal("the moved copy should no longer be at its old path")
	}
}

// A copy already sitting in the destination is left alone. Renaming it for the
// sake of consistency would achieve nothing and lose the name its owner chose.
func TestACopyAlreadyInTheDestinationIsNotMoved(t *testing.T) {
	root := t.TempDir()
	documents := filepath.Join(root, "Documents")
	oldest := filepath.Join(documents, "notes.md")
	sibling := filepath.Join(documents, "notes-backup.md")
	write(t, oldest, "same")
	write(t, sibling, "same")

	plan, err := PlanMerge(groupOf(t, oldest, sibling))
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(plan.Moves) != 0 {
		t.Fatalf("a copy already in the destination should not move, got %+v", plan.Moves)
	}

	if _, err := os.Stat(sibling); err != nil {
		t.Fatal("the sibling should be untouched, under its original name")
	}
}

// The extension has to stay last or the file stops opening in whatever opened
// it before.
func TestTheSuffixGoesBeforeTheExtension(t *testing.T) {
	root := t.TempDir()
	oldest := filepath.Join(root, "a", "archive.tar.gz")
	newer := filepath.Join(root, "b", "archive.tar.gz")
	write(t, oldest, "same")
	write(t, newer, "same")

	plan, _ := PlanMerge(groupOf(t, oldest, newer))
	got := filepath.Base(plan.Moves[0].To)
	if !strings.HasSuffix(got, ".gz") {
		t.Fatalf("name = %q, the extension must stay last", got)
	}
	if got != "archive.tar copy.gz" {
		t.Fatalf("name = %q, want archive.tar copy.gz", got)
	}
}

// A name taken by a broken symlink is still taken. Renaming onto it would
// destroy the link.
func TestABrokenSymlinkStillOccupiesAName(t *testing.T) {
	root := t.TempDir()
	documents := filepath.Join(root, "Documents")
	oldest := filepath.Join(documents, "file.txt")
	newer := filepath.Join(root, "Downloads", "file.txt")
	write(t, oldest, "same")
	write(t, newer, "same")

	link := filepath.Join(documents, "file copy.txt")
	if err := os.Symlink(filepath.Join(root, "gone"), link); err != nil {
		t.Fatalf("setup: %v", err)
	}

	plan, err := PlanMerge(groupOf(t, oldest, newer))
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if plan.Moves[0].To == link {
		t.Fatal("the plan targets a name held by a broken symlink")
	}

	if _, err := Apply(plan, operationlog.Discard()); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if _, err := os.Lstat(link); err != nil {
		t.Fatal("the broken symlink was destroyed by the merge")
	}
}

// A name taken between planning and applying must not be overwritten, since
// os.Rename would do so without complaint.
func TestANameTakenAfterPlanningIsRefused(t *testing.T) {
	root := t.TempDir()
	documents := filepath.Join(root, "Documents")
	oldest := filepath.Join(documents, "file.txt")
	newer := filepath.Join(root, "Downloads", "file.txt")
	write(t, oldest, "same")
	write(t, newer, "same")

	plan, err := PlanMerge(groupOf(t, oldest, newer))
	if err != nil {
		t.Fatalf("plan: %v", err)
	}

	// Something claims the name in the window between plan and apply.
	write(t, plan.Moves[0].To, "ARRIVED IN BETWEEN")

	result, err := Apply(plan, operationlog.Discard())
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if result.FailedCount != 1 {
		t.Fatalf("the move should have been refused, got %+v", result)
	}
	contents, _ := os.ReadFile(plan.Moves[0].To)
	if string(contents) != "ARRIVED IN BETWEEN" {
		t.Fatalf("the newly arrived file was overwritten, it holds %q", contents)
	}
	if _, err := os.Stat(newer); err != nil {
		t.Fatal("a refused move must leave its source where it was")
	}
}

// One failure must not abandon the rest, or a person is left with a half
// finished merge and no way to know how far it got.
func TestOneFailureDoesNotStopTheRest(t *testing.T) {
	root := t.TempDir()
	documents := filepath.Join(root, "Documents")
	oldest := filepath.Join(documents, "file.txt")
	second := filepath.Join(root, "b", "file.txt")
	third := filepath.Join(root, "c", "file.txt")
	for _, path := range []string{oldest, second, third} {
		write(t, path, "same")
	}

	plan, err := PlanMerge(groupOf(t, oldest, second, third))
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	// Block only the first move.
	write(t, plan.Moves[0].To, "in the way")

	result, err := Apply(plan, operationlog.Discard())
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if result.FailedCount != 1 {
		t.Fatalf("expected one failure, got %d", result.FailedCount)
	}
	if result.MovedCount != 1 {
		t.Fatalf("the second move should still have run, moved %d", result.MovedCount)
	}
}

// Both paths are logged. Knowing something moved without knowing where is not
// a record anyone can act on.
func TestTheLogRecordsWhereEachCopyWent(t *testing.T) {
	root := t.TempDir()
	oldest := filepath.Join(root, "a", "file.txt")
	newer := filepath.Join(root, "b", "file.txt")
	write(t, oldest, "same")
	write(t, newer, "same")

	logPath := filepath.Join(t.TempDir(), "operations.log")
	writer, err := operationlog.Open(logPath, "duplicates")
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	plan, _ := PlanMerge(groupOf(t, oldest, newer))
	if _, err := Apply(plan, writer); err != nil {
		t.Fatalf("apply: %v", err)
	}
	writer.Close()

	recorded, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("reading log: %v", err)
	}
	if !strings.Contains(string(recorded), newer) {
		t.Fatalf("the log does not say what moved:\n%s", recorded)
	}
	if !strings.Contains(string(recorded), plan.Moves[0].To) {
		t.Fatalf("the log does not say where it went:\n%s", recorded)
	}
}

func TestAGroupWithOneCopyIsRefused(t *testing.T) {
	group := groupOf(t, "/tmp/only.txt")
	if _, err := PlanMerge(group); !errors.Is(err, ErrNothingToMerge) {
		t.Fatalf("expected ErrNothingToMerge, got %v", err)
	}
}
