package spacescan

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// tree builds a fixture directory from a map of relative path to contents.
func tree(t *testing.T, layout map[string]string) string {
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

func childNamed(node *Node, name string) *Node {
	for _, child := range node.Children {
		if child.Name == name {
			return child
		}
	}
	return nil
}

func TestSizesAggregateUpTheTree(t *testing.T) {
	root := tree(t, map[string]string{
		"a/one.txt":      strings.Repeat("x", 100),
		"a/two.txt":      strings.Repeat("x", 200),
		"a/deeper/three": strings.Repeat("x", 400),
		"b/small.txt":    strings.Repeat("x", 50),
	})

	result, err := Scan(Options{Root: root})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}

	if result.Root.Size != 750 {
		t.Fatalf("root total = %d, want 750", result.Root.Size)
	}
	a := childNamed(result.Root, "a")
	if a == nil || a.Size != 700 {
		t.Fatalf("a = %+v, want size 700", a)
	}
	deeper := childNamed(a, "deeper")
	if deeper == nil || deeper.Size != 400 {
		t.Fatalf("a/deeper = %+v, want size 400", deeper)
	}
	if !result.Root.Complete {
		t.Error("a fully readable tree should report a complete total")
	}
}

// Largest first is the entire point of the feature. A tree sorted any other
// way makes a person hunt for what they came to find.
func TestChildrenAreSortedLargestFirst(t *testing.T) {
	root := tree(t, map[string]string{
		"small.txt":  strings.Repeat("x", 10),
		"huge.txt":   strings.Repeat("x", 5000),
		"medium.txt": strings.Repeat("x", 500),
	})

	result, err := Scan(Options{Root: root})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}

	var sizes []int64
	for _, child := range result.Root.Children {
		sizes = append(sizes, child.Size)
	}
	for i := 1; i < len(sizes); i++ {
		if sizes[i] > sizes[i-1] {
			t.Fatalf("children are not sorted largest first: %v", sizes)
		}
	}
	if result.Root.Children[0].Name != "huge.txt" {
		t.Fatalf("largest child is %q, want huge.txt", result.Root.Children[0].Name)
	}
}

// Path is reconstructed by walking up rather than stored, so this checks the
// reconstruction actually produces the real path.
func TestPathReconstructsFromTheTree(t *testing.T) {
	root := tree(t, map[string]string{"a/deeper/file.txt": "x"})

	result, err := Scan(Options{Root: root})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}

	deeper := childNamed(childNamed(result.Root, "a"), "deeper")
	file := childNamed(deeper, "file.txt")
	want := filepath.Join(root, "a", "deeper", "file.txt")
	if got := file.Path(); got != want {
		t.Fatalf("Path() = %q, want %q", got, want)
	}
	if file.Parent() != deeper {
		t.Error("Parent() should return the containing directory")
	}
}

// A symlink is counted as the small entry it is and never followed. Following
// would let a loop become an unbounded walk, and would let a link pointing out
// of the scan root silently widen what the scan covers.
func TestSymlinksAreCountedButNeverFollowed(t *testing.T) {
	root := tree(t, map[string]string{"real/big.txt": strings.Repeat("x", 10000)})

	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "elsewhere.txt"),
		[]byte(strings.Repeat("y", 50000)), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatalf("setup: %v", err)
	}

	result, err := Scan(Options{Root: root})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}

	// Only the real 10000 bytes. The 50000 outside must not be counted.
	if result.Root.Size != 10000 {
		t.Fatalf("root total = %d, want 10000, the link's target was followed",
			result.Root.Size)
	}
	escape := childNamed(result.Root, "escape")
	if escape == nil {
		t.Fatal("the link should still appear as an entry")
	}
	if escape.IsDir {
		t.Error("a link must not be treated as a directory")
	}
	if len(escape.Children) != 0 {
		t.Error("a link must not be descended into")
	}
}

// A symlink loop must not hang the scan.
func TestSymlinkLoopTerminates(t *testing.T) {
	root := t.TempDir()
	inner := filepath.Join(root, "inner")
	if err := os.MkdirAll(inner, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.Symlink(root, filepath.Join(inner, "loop")); err != nil {
		t.Fatalf("setup: %v", err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		if _, err := Scan(Options{Root: root}); err != nil {
			t.Errorf("scan: %v", err)
		}
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("a symlink loop did not terminate")
	}
}

// A directory that cannot be read makes every total above it a floor. Saying
// otherwise would present a partial sum as exact to someone deciding what to
// delete.
func TestUnreadableDirectoryIsReportedAndMakesTotalsAFloor(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("running as root, which bypasses the permission under test")
	}

	root := tree(t, map[string]string{
		"readable/file.txt": strings.Repeat("x", 100),
		"locked/hidden.txt": strings.Repeat("x", 9999),
	})
	locked := filepath.Join(root, "locked")
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatalf("setup: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	result, err := Scan(Options{Root: root})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}

	if result.Root.Complete {
		t.Error("a tree with an unreadable subdirectory must not claim a complete total")
	}
	var found bool
	for _, denied := range result.Denied {
		if denied == locked {
			found = true
		}
	}
	if !found {
		t.Fatalf("the unreadable directory was not reported: %v", result.Denied)
	}
	// The readable part is still counted, which is what makes a partial scan
	// useful rather than merely honest.
	if result.Root.Size < 100 {
		t.Fatalf("readable content was lost, total = %d", result.Root.Size)
	}
}

// The deadline bounds the walk. Without it a home directory on a slow or
// stalled disk has no upper bound, which is the defect this project already
// fixed once in the deletion engine.
func TestDeadlineTruncatesTheWalk(t *testing.T) {
	root := tree(t, map[string]string{
		"a/1.txt": "x", "a/2.txt": "x", "b/1.txt": "x", "c/1.txt": "x",
	})

	// A clock that jumps past the deadline as soon as it is consulted.
	calls := 0
	start := time.Now()
	result, err := Scan(Options{
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
		t.Fatalf("scan: %v", err)
	}

	if !result.Truncated {
		t.Fatal("a walk past its deadline must report itself truncated")
	}
	if !strings.Contains(result.Reason, "time limit") {
		t.Fatalf("reason = %q, should say it ran out of time", result.Reason)
	}
	if result.Root.Complete {
		t.Error("a truncated walk must not claim a complete total")
	}
}

func TestScanRefusesWithoutARoot(t *testing.T) {
	if _, err := Scan(Options{}); !errors.Is(err, ErrNoRoot) {
		t.Fatalf("expected ErrNoRoot, got %v", err)
	}
}

func TestScanRefusesAFileAsRoot(t *testing.T) {
	root := tree(t, map[string]string{"file.txt": "x"})

	if _, err := Scan(Options{Root: filepath.Join(root, "file.txt")}); err == nil {
		t.Fatal("scanning a file rather than a directory should be refused")
	}
}

func TestScanRefusesAMissingRoot(t *testing.T) {
	if _, err := Scan(Options{Root: filepath.Join(t.TempDir(), "nope")}); err == nil {
		t.Fatal("scanning a missing directory should be refused")
	}
}

func TestEmptyDirectoryScansToZero(t *testing.T) {
	result, err := Scan(Options{Root: t.TempDir()})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if result.Root.Size != 0 || len(result.Root.Children) != 0 {
		t.Fatalf("an empty directory should be zero and childless, got %+v", result.Root)
	}
	if !result.Root.Complete {
		t.Error("an empty directory is completely measured")
	}
}

// Progress has to move, or the shell has nothing to show during a long scan.
func TestProgressIsReported(t *testing.T) {
	layout := map[string]string{}
	for i := 0; i < 5000; i++ {
		layout[filepath.Join("dir", "file"+strings.Repeat("0", i%3)+string(rune('a'+i%26))+
			strings.Repeat("z", i%7)+itoa(i))] = "x"
	}
	root := tree(t, layout)

	var last int
	result, err := Scan(Options{
		Root:     root,
		Progress: func(scanned int) { last = scanned },
	})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if last == 0 {
		t.Fatal("progress was never reported during a scan of thousands of entries")
	}
	if last > result.Scanned {
		t.Fatalf("progress reported %d, more than the %d actually scanned", last, result.Scanned)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
