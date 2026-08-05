package pathvalidation

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// These cases come from a review pass over the walk rather than from the
// original design. Caller supplied paths reject a parent reference outright,
// which made it easy to assume no parent reference could reach the walk. A link
// target is not caller supplied and is spliced in unchecked, and a parent
// reference is an ordinary directory entry rather than a link, so opening it
// without following links does not stop it.

// The reported path has to describe where the operation will actually land. A
// preview that shows one location while the deletion happens somewhere else is
// the failure this whole package exists to prevent, so an ascent through a link
// must leave the logical path collapsed rather than accumulating.
func TestAscendingLinkTargetReportsCollapsedPath(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, "level-one", "level-two")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	marker := filepath.Join(dir, "marker-file")
	if err := os.WriteFile(marker, []byte("payload"), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}
	climb := filepath.Join(nested, "climb")
	if err := os.Symlink("../..", climb); err != nil {
		t.Fatalf("setup: %v", err)
	}

	resolved, err := Resolve(filepath.Join(climb, "marker-file"))
	if err != nil {
		t.Fatalf("Resolve through an ascending link = %v, want success", err)
	}
	defer resolved.Close()

	if strings.Contains(resolved.Path(), "..") {
		t.Fatalf("reported path %q still contains a parent reference", resolved.Path())
	}

	// The reported path must name the same object the caller reached.
	expected, err := filepath.EvalSymlinks(marker)
	if err != nil {
		t.Fatalf("resolving expected path: %v", err)
	}
	if resolved.Path() != expected {
		t.Fatalf("reported path = %q, want %q", resolved.Path(), expected)
	}
	if err := resolved.Verify(); err != nil {
		t.Fatalf("Verify = %v, want nil", err)
	}
}

// The same mechanism used offensively: climb far enough to escape the temporary
// directory entirely, then descend into a denied tree. The requested path is
// textually innocent and a prefix comparison accepts it.
func TestAscendingLinkCannotReachDeniedTree(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, "one", "two", "three")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	escape := filepath.Join(nested, "escape-hatch")
	// Enough ascents to reach the volume root from anywhere a temporary
	// directory might sit. Climbing past the root stays at the root, so
	// overshooting is safe and counting exactly is not required. An earlier
	// version of this test used ten, which landed on an intermediate directory
	// and passed for the wrong reason: the walk failed to find a component
	// rather than refusing a denied one.
	if err := os.Symlink(strings.Repeat("../", 40), escape); err != nil {
		t.Fatalf("setup: %v", err)
	}

	resolveExpectingError(t, filepath.Join(escape, "System", "Library"), ErrDenied)
}

// Climbing past the volume root must stay at the root rather than running off
// the end of the logical path or the descriptor chain.
func TestAscendingBeyondRootIsBounded(t *testing.T) {
	dir := t.TempDir()
	climb := filepath.Join(dir, "climb-past-root")
	if err := os.Symlink(strings.Repeat("../", 64), climb); err != nil {
		t.Fatalf("setup: %v", err)
	}

	// Landing on the volume root and asking for it as a target is denied by the
	// floor. The specific error matters less than not crashing or escaping.
	resolved, err := Resolve(filepath.Join(climb, "Applications"))
	if resolved != nil {
		resolved.Close()
		t.Fatal("expected the floor to reject /Applications reached by climbing")
	}
	if err == nil {
		t.Fatal("expected an error")
	}
}

// A link whose entire target is a parent reference is still just a link when it
// is the final component, so it resolves to the link itself and is not followed.
func TestLinkWhoseTargetIsOnlyAnAscentResolvesAsTheLink(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, "level-one")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	climb := filepath.Join(nested, "climb")
	if err := os.Symlink("..", climb); err != nil {
		t.Fatalf("setup: %v", err)
	}

	resolved, err := Resolve(climb)
	if err != nil {
		t.Fatalf("Resolve(%q) = %v, want the link itself", climb, err)
	}
	defer resolved.Close()

	if !resolved.IsSymlink() {
		t.Fatal("target should be the link, not the directory it points at")
	}
	if resolved.LeafName() != "climb" {
		t.Fatalf("leaf name = %q, want climb", resolved.LeafName())
	}
}

// A parent reference written by the caller is rejected before the walk begins,
// regardless of where in the path it appears. This is the reason a spliced
// parent reference can only ever originate from a link target.
func TestCallerSuppliedAscentIsRejectedBeforeWalking(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, "level-one")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}

	// Built by concatenation rather than filepath.Join, which would normalize
	// the parent reference away before Resolve ever saw it. An earlier version
	// of this test used Join and silently tested nothing.
	resolveExpectingError(t, nested+"/..", ErrTraversal)
	resolveExpectingError(t, nested+"/../..", ErrTraversal)
	resolveExpectingError(t, dir+"/../"+filepath.Base(dir), ErrTraversal)
}
