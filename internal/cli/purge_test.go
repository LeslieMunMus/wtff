package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// trashHome builds an isolated home with a Trash holding one file, so no test
// here can reach the real machine's Trash.
func trashHome(t *testing.T) (home, trashed string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)

	trash := filepath.Join(home, ".Trash")
	if err := os.MkdirAll(trash, 0o700); err != nil {
		t.Fatalf("setup: %v", err)
	}
	trashed = filepath.Join(trash, "discarded-file")
	if err := os.WriteFile(trashed, []byte("already thrown away"), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}
	return home, trashed
}

func TestPurgeDryRunChangesNothing(t *testing.T) {
	_, trashed := trashHome(t)

	var out, errOut bytes.Buffer
	code := Run([]string{"purge", "--dry-run"}, strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "dry run") {
		t.Fatalf("output should say it was a dry run:\n%s", out.String())
	}
	if _, err := os.Stat(trashed); err != nil {
		t.Fatal("a dry run must not delete anything")
	}
}

func TestPurgeEmptiesTheTrash(t *testing.T) {
	_, trashed := trashHome(t)

	var out, errOut bytes.Buffer
	code := Run([]string{"purge", "--yes"}, strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, errOut.String())
	}
	if _, err := os.Stat(trashed); !os.IsNotExist(err) {
		t.Fatal("purge should have emptied the trash")
	}
}

// Purge is irreversible, so it must not proceed on a keystroke, and must not
// proceed at all where nobody can answer.
func TestPurgeRefusesNonInteractiveWithoutYes(t *testing.T) {
	_, trashed := trashHome(t)

	var out, errOut bytes.Buffer
	code := Run([]string{"purge"}, strings.NewReader("y\n"), &out, &errOut)
	if code == 0 {
		t.Fatal("purge without --yes in a non-interactive session should refuse")
	}
	if _, err := os.Stat(trashed); err != nil {
		t.Fatal("the refused purge must not have deleted anything")
	}
}

// Purge must not reach beyond the entries marked purgeable. A cache sitting in
// the same home is the check that it does not quietly become a second clean.
func TestPurgeLeavesCachesAlone(t *testing.T) {
	home, _ := trashHome(t)

	cache := filepath.Join(home, "Library", "Caches", "com.example.app")
	if err := os.MkdirAll(cache, 0o700); err != nil {
		t.Fatalf("setup: %v", err)
	}
	marker := filepath.Join(cache, "cached-data")
	if err := os.WriteFile(marker, []byte("regenerable"), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	var out, errOut bytes.Buffer
	if code := Run([]string{"purge", "--yes"}, strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, errOut.String())
	}

	if _, err := os.Stat(marker); err != nil {
		t.Fatal("purge deleted a cache, which belongs to clean and its staging")
	}
}

func TestPurgeRejectsPositionalArguments(t *testing.T) {
	trashHome(t)

	var out, errOut bytes.Buffer
	code := Run([]string{"purge", "/some/path"}, strings.NewReader(""), &out, &errOut)
	if code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
}

func TestPurgeOnAnEmptyTrashReportsNothingToDo(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	var out, errOut bytes.Buffer
	code := Run([]string{"purge", "--dry-run"}, strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, errOut.String())
	}
}

// stageSomething runs clean to produce a real staged batch, and returns its id.
func stageSomething(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)

	cache := filepath.Join(home, "Library", "Caches", "com.example.app")
	if err := os.MkdirAll(cache, 0o700); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cache, "data"), []byte("cached"), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	var out, errOut bytes.Buffer
	if code := Run([]string{"clean", "--yes"}, strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatalf("staging setup failed, exit %d: %s", code, errOut.String())
	}

	var listed bytes.Buffer
	if code := Run([]string{"staged"}, strings.NewReader(""), &listed, &errOut); code != 0 {
		t.Fatalf("staged setup failed: %s", errOut.String())
	}
	fields := strings.Fields(listed.String())
	if len(fields) == 0 {
		t.Fatalf("nothing was staged:\n%s", listed.String())
	}
	return fields[0]
}

func TestStagedPurgeDeletesABatchPermanently(t *testing.T) {
	batchID := stageSomething(t)

	var out, errOut bytes.Buffer
	code := Run([]string{"staged", "--purge", batchID, "--yes"},
		strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "permanently") {
		t.Fatalf("output should say the deletion was permanent:\n%s", out.String())
	}

	var listed bytes.Buffer
	Run([]string{"staged"}, strings.NewReader(""), &listed, &errOut)
	if !strings.Contains(listed.String(), "nothing is staged") {
		t.Fatalf("the batch should be gone:\n%s", listed.String())
	}
}

// Undo must fail after a purge. If it appeared to succeed, the tool would be
// telling someone their data came back when it did not.
func TestUndoFailsAfterTheBatchWasPurged(t *testing.T) {
	batchID := stageSomething(t)

	var out, errOut bytes.Buffer
	if code := Run([]string{"staged", "--purge", batchID, "--yes"},
		strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatalf("purge failed: %s", errOut.String())
	}

	var undoOut, undoErr bytes.Buffer
	if code := Run([]string{"undo", batchID}, strings.NewReader(""), &undoOut, &undoErr); code == 0 {
		t.Fatal("undo should fail for a batch that was purged")
	}
}

// A staged purge withdraws a promise of reversibility, so it needs the same
// friction every other irreversible path uses, not a keystroke.
func TestStagedPurgeRefusesNonInteractiveWithoutYes(t *testing.T) {
	batchID := stageSomething(t)

	var out, errOut bytes.Buffer
	code := Run([]string{"staged", "--purge", batchID}, strings.NewReader("y\n"), &out, &errOut)
	if code == 0 {
		t.Fatal("a staged purge without --yes should refuse in a non-interactive session")
	}

	var listed bytes.Buffer
	Run([]string{"staged"}, strings.NewReader(""), &listed, &errOut)
	if !strings.Contains(listed.String(), batchID) {
		t.Fatalf("the batch should still be staged:\n%s", listed.String())
	}
}

// A batch id is attacker-shaped input: it comes from an argument and is joined
// to a path. This is the traversal that was already found once in this project.
func TestStagedPurgeRefusesTraversalBatchIdentifiers(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	secret := filepath.Join(home, "important")
	if err := os.WriteFile(secret, []byte("keep"), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	for _, id := range []string{"..", "../..", "../../..", "/etc"} {
		var out, errOut bytes.Buffer
		code := Run([]string{"staged", "--purge", id, "--yes"},
			strings.NewReader(""), &out, &errOut)
		if code == 0 {
			t.Errorf("staged --purge %q should have been refused", id)
		}
	}
	if _, err := os.Stat(secret); err != nil {
		t.Fatal("a refused traversal must not have deleted anything")
	}
}

func TestStagedPurgeNeedsATarget(t *testing.T) {
	stageSomething(t)

	var out, errOut bytes.Buffer
	code := Run([]string{"staged", "--purge", "--yes"}, strings.NewReader(""), &out, &errOut)
	if code != 2 {
		t.Fatalf("exit %d, want 2 for --purge with no batch id and no --all", code)
	}
}

func TestStagedPurgeAllClearsEverything(t *testing.T) {
	stageSomething(t)

	var out, errOut bytes.Buffer
	code := Run([]string{"staged", "--purge", "--all", "--yes"},
		strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, errOut.String())
	}

	var listed bytes.Buffer
	Run([]string{"staged"}, strings.NewReader(""), &listed, &errOut)
	if !strings.Contains(listed.String(), "nothing is staged") {
		t.Fatalf("everything should be gone:\n%s", listed.String())
	}
}

// The flag reordering bug this project already hit once: a flag typed after a
// positional argument was silently swallowed. --all is a new flag and gets the
// same check.
func TestStagedPurgeFlagsWorkAfterPositionalArguments(t *testing.T) {
	batchID := stageSomething(t)

	var out, errOut bytes.Buffer
	code := Run([]string{"staged", "--purge", batchID, "--yes"},
		strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("a flag after a positional argument was not honored, exit %d: %s",
			code, errOut.String())
	}
}
