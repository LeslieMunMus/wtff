package duplicatescan

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// build writes a fixture tree and returns its root.
func build(t *testing.T, layout map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, contents := range layout {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("setup: %v", err)
		}
		if err := os.WriteFile(full, []byte(contents), 0o600); err != nil {
			t.Fatalf("setup: %v", err)
		}
	}
	return root
}

// big pads content past the minimum size floor, since anything smaller is
// deliberately ignored.
func big(marker string) string {
	return marker + strings.Repeat("=", 8*1024)
}

func TestIdenticalFilesAreGrouped(t *testing.T) {
	root := build(t, map[string]string{
		"one/report.pdf": big("report"),
		"two/report.pdf": big("report"),
		"three/other.md": big("different"),
	})

	result, err := Find(Options{Root: root})
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if len(result.Groups) != 1 {
		t.Fatalf("expected one group, got %d: %+v", len(result.Groups), result.Groups)
	}
	if len(result.Groups[0].Files) != 2 {
		t.Fatalf("expected two copies, got %d", len(result.Groups[0].Files))
	}
}

// The property everything rests on. Two files of identical size that differ in
// content must never be grouped, because a person acting on a false match
// deletes real data.
func TestSameSizeDifferentContentIsNotAMatch(t *testing.T) {
	same := 8 * 1024
	root := build(t, map[string]string{
		"a.bin": strings.Repeat("a", same),
		"b.bin": strings.Repeat("b", same),
	})

	result, err := Find(Options{Root: root})
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if len(result.Groups) != 0 {
		t.Fatalf("files of equal size but different content were grouped: %+v",
			result.Groups)
	}
}

// The hardest case for a cheap matcher: identical for the first 64KB, then
// different. A prefix only comparison reports these as duplicates. A full
// content hash does not.
func TestFilesSharingALongPrefixAreNotMatched(t *testing.T) {
	shared := strings.Repeat("s", prefixBytes+1024)
	root := build(t, map[string]string{
		"first.bin":  shared + "ending one",
		"second.bin": shared + "ending two",
	})

	result, err := Find(Options{Root: root})
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if len(result.Groups) != 0 {
		t.Fatalf("files sharing a 64KB prefix were wrongly matched: %+v", result.Groups)
	}
}

// Files identical past the prefix boundary must still be found, or the cheap
// pass has become a filter that loses real duplicates.
func TestFilesIdenticalBeyondThePrefixAreMatched(t *testing.T) {
	contents := strings.Repeat("s", prefixBytes+4096) + "tail"
	root := build(t, map[string]string{
		"one/a.bin": contents,
		"two/a.bin": contents,
	})

	result, err := Find(Options{Root: root})
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if len(result.Groups) != 1 {
		t.Fatalf("identical large files were not matched: %+v", result.Groups)
	}
}

// Oldest first is load bearing: a merge keeps the first entry in place, so
// getting the order wrong moves the wrong copy.
func TestGroupsAreOrderedOldestFirst(t *testing.T) {
	root := build(t, map[string]string{
		"newer/file.txt": big("same"),
		"older/file.txt": big("same"),
	})

	older := filepath.Join(root, "older", "file.txt")
	newer := filepath.Join(root, "newer", "file.txt")
	past := time.Now().Add(-72 * time.Hour)
	if err := os.Chtimes(older, past, past); err != nil {
		t.Fatalf("setup: %v", err)
	}

	result, err := Find(Options{Root: root})
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if len(result.Groups) != 1 {
		t.Fatalf("expected one group, got %+v", result.Groups)
	}

	group := result.Groups[0]
	if group.Oldest().Path != older {
		t.Fatalf("oldest is %q, want %q", group.Oldest().Path, older)
	}
	if group.Files[1].Path != newer {
		t.Fatalf("second entry is %q, want %q", group.Files[1].Path, newer)
	}
}

func TestReclaimableCountsEveryCopyButTheFirst(t *testing.T) {
	contents := big("dup")
	root := build(t, map[string]string{
		"a/f.bin": contents,
		"b/f.bin": contents,
		"c/f.bin": contents,
	})

	result, err := Find(Options{Root: root})
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	group := result.Groups[0]
	if want := group.Size * 2; group.Reclaimable() != want {
		t.Fatalf("reclaimable = %d, want %d for three copies",
			group.Reclaimable(), want)
	}
	if result.Reclaimable() != group.Reclaimable() {
		t.Fatal("the total should match the single group's figure")
	}
}

// A lone file is not a duplicate of anything.
func TestSingleCopiesAreNotReported(t *testing.T) {
	root := build(t, map[string]string{
		"a.bin": big("one"),
		"b.bin": big("two"),
		"c.bin": big("three"),
	})

	result, err := Find(Options{Root: root})
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if len(result.Groups) != 0 {
		t.Fatalf("distinct files were grouped: %+v", result.Groups)
	}
}

// Thousands of identical tiny files are normal on any machine, would dominate
// the results, and reclaim nothing worth the reading.
func TestFilesBelowTheSizeFloorAreIgnored(t *testing.T) {
	root := build(t, map[string]string{
		"a/tiny.txt": "same",
		"b/tiny.txt": "same",
	})

	result, err := Find(Options{Root: root})
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if len(result.Groups) != 0 {
		t.Fatalf("tiny files were reported: %+v", result.Groups)
	}

	// The floor is adjustable, so a caller that wants them can ask.
	lowered, err := Find(Options{Root: root, MinSize: 1})
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if len(lowered.Groups) != 1 {
		t.Fatalf("lowering the floor should find them, got %+v", lowered.Groups)
	}
}

// A link is not a duplicate of what it points at, and reporting it as one
// would offer to delete a file by way of its own alias.
//
// This does not prove the explicit symlink skip is what excludes it. Checking
// found that removing that skip, and the regular file check with it, still
// leaves this passing: a symlink's own size is the length of its target path,
// a few dozen bytes, so the minimum size floor excludes it first. The skip is
// kept as defence in depth and because it states the intent, but the honest
// record is that three separate things would each have to fail before a link
// could be matched.
func TestSymlinksAreNeverReportedAsDuplicates(t *testing.T) {
	root := build(t, map[string]string{"real/file.bin": big("content")})
	target := filepath.Join(root, "real", "file.bin")
	if err := os.Symlink(target, filepath.Join(root, "alias.bin")); err != nil {
		t.Fatalf("setup: %v", err)
	}

	result, err := Find(Options{Root: root})
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if len(result.Groups) != 0 {
		t.Fatalf("a symlink was matched against its own target: %+v", result.Groups)
	}
}

// The hazard a symlink actually presents to a walk: a loop that never ends.
// This is the property worth pinning, and unlike the matching case it is
// genuinely load bearing.
func TestSymlinkLoopDoesNotHangTheSearch(t *testing.T) {
	root := build(t, map[string]string{"inner/file.bin": big("content")})
	if err := os.Symlink(root, filepath.Join(root, "inner", "loop")); err != nil {
		t.Fatalf("setup: %v", err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		if _, err := Find(Options{Root: root}); err != nil {
			t.Errorf("find: %v", err)
		}
	}()

	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("a symlink loop did not terminate")
	}
}

func TestUnreadableDirectoryIsReportedNotFatal(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("running as root, which bypasses the permission under test")
	}
	root := build(t, map[string]string{
		"open/a.bin":   big("same"),
		"open/b.bin":   big("same"),
		"locked/x.bin": big("hidden"),
	})
	locked := filepath.Join(root, "locked")
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatalf("setup: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	result, err := Find(Options{Root: root})
	if err != nil {
		t.Fatalf("an unreadable directory should not fail the search: %v", err)
	}
	if len(result.Denied) == 0 {
		t.Fatal("the unreadable directory should be reported")
	}
	// The readable duplicates are still found, which is what makes a partial
	// search useful rather than merely honest.
	if len(result.Groups) != 1 {
		t.Fatalf("readable duplicates were lost: %+v", result.Groups)
	}
}

func TestDeadlineTruncatesTheSearch(t *testing.T) {
	root := build(t, map[string]string{
		"a/1.bin": big("x"), "a/2.bin": big("x"), "b/1.bin": big("y"),
	})

	calls := 0
	start := time.Now()
	result, err := Find(Options{
		Root:     root,
		Deadline: time.Second,
		now: func() time.Time {
			calls++
			if calls > 2 {
				return start.Add(time.Hour)
			}
			return start
		},
	})
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if !result.Truncated {
		t.Fatal("a search past its deadline must report itself truncated")
	}
	if !strings.Contains(result.Reason, "time limit") {
		t.Fatalf("reason = %q, should say it ran out of time", result.Reason)
	}
}

func TestFindRefusesWithoutARoot(t *testing.T) {
	if _, err := Find(Options{}); !errors.Is(err, ErrNoRoot) {
		t.Fatalf("expected ErrNoRoot, got %v", err)
	}
}

func TestFindRefusesAFileAsRoot(t *testing.T) {
	root := build(t, map[string]string{"f.bin": big("x")})
	if _, err := Find(Options{Root: filepath.Join(root, "f.bin")}); err == nil {
		t.Fatal("searching a file rather than a directory should be refused")
	}
}

// Groups are ordered by what acting on them would actually free, since the
// reason someone opened this is to find what is worth doing something about.
func TestGroupsAreOrderedByReclaimableSpace(t *testing.T) {
	small := strings.Repeat("s", 8*1024)
	large := strings.Repeat("L", 64*1024)
	root := build(t, map[string]string{
		"a/small.bin": small, "b/small.bin": small,
		"a/large.bin": large, "b/large.bin": large,
	})

	result, err := Find(Options{Root: root})
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if len(result.Groups) != 2 {
		t.Fatalf("expected two groups, got %+v", result.Groups)
	}
	if result.Groups[0].Reclaimable() < result.Groups[1].Reclaimable() {
		t.Fatal("groups should be ordered by reclaimable space, largest first")
	}
}
