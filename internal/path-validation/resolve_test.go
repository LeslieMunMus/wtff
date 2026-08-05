package pathvalidation

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// resolveExpectingError asserts that a target is rejected with a specific
// error, and that nothing is handed back for the caller to accidentally use.
func resolveExpectingError(t *testing.T, target string, want error) {
	t.Helper()
	resolved, err := Resolve(target)
	if resolved != nil {
		resolved.Close()
		t.Fatalf("Resolve(%q) returned a handle, expected rejection with %v", target, want)
	}
	if !errors.Is(err, want) {
		t.Fatalf("Resolve(%q) error = %v, want %v", target, err, want)
	}
}

func TestRejectsMalformedTargets(t *testing.T) {
	cases := []struct {
		name   string
		target string
		want   error
	}{
		{"empty", "", ErrEmptyPath},
		{"relative", "Library/Caches/example", ErrNotAbsolute},
		{"relative dot", "./example", ErrNotAbsolute},
		{"traversal middle", "/Users/example/../../etc/passwd", ErrTraversal},
		{"traversal leading", "/../etc", ErrTraversal},
		{"traversal trailing", "/Users/example/..", ErrTraversal},
		{"newline", "/tmp/example\nrm -rf", ErrControlCharacter},
		{"null byte", "/tmp/example\x00", ErrControlCharacter},
		{"escape sequence", "/tmp/example\x1b[2J", ErrControlCharacter},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resolveExpectingError(t, tc.target, tc.want)
		})
	}
}

// A filename containing two dots is not a traversal. Real cache directories
// contain names like this, and rejecting them would quietly skip legitimate
// cleanup targets.
func TestAllowsDoubleDotsInsideAComponent(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "index..data")
	if err := os.WriteFile(target, []byte("payload"), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	resolved, err := Resolve(target)
	if err != nil {
		t.Fatalf("Resolve(%q) = %v, want success", target, err)
	}
	defer resolved.Close()

	if got := filepath.Base(resolved.Path()); got != "index..data" {
		t.Fatalf("leaf name = %q, want index..data", got)
	}
}

func TestRejectsStructuralFloorPaths(t *testing.T) {
	targets := []string{
		"/",
		"/System",
		"/System/Library",
		"/usr",
		"/usr/bin",
		"/bin",
		"/sbin",
		"/dev",
		"/Library",
		"/Library/Keychains",
		"/Applications",
		"/Users",
		"/Volumes",
		"/private",
		"/private/var",
		"/private/etc",
		"/opt",
	}
	for _, target := range targets {
		t.Run(target, func(t *testing.T) {
			resolveExpectingError(t, target, ErrDenied)
		})
	}
}

// A home directory root is denied even though the account name is not known
// ahead of time. This is the shape a bug takes when a target is built by
// joining a home directory with an empty variable.
func TestRejectsHomeDirectoryRoot(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("cannot determine home directory: %v", err)
	}
	if !strings.HasPrefix(home, "/Users/") {
		t.Skipf("home directory %q is not under /Users", home)
	}
	resolveExpectingError(t, home, ErrDenied)
	resolveExpectingError(t, home+"/", ErrDenied)
	resolveExpectingError(t, home+"/.", ErrDenied)
}

// Apple filesystems are normally case insensitive, so a denied path reached
// through different casing is the same directory and must be denied too. The
// text comparison alone would miss this if the case folding were removed, and
// the identity comparison catches it regardless.
func TestRejectsFloorPathsReachedByDifferentCasing(t *testing.T) {
	var st unix.Stat_t
	if err := unix.Stat("/SYSTEM", &st); err != nil {
		t.Skip("filesystem is case sensitive, alias does not exist")
	}
	resolveExpectingError(t, "/SYSTEM/Library", ErrDenied)
}

// The central claim of this package: a link in an ancestor component cannot be
// used to reach a denied tree. The text of the requested path is entirely
// innocent, and only following it reveals where it lands.
func TestRejectsAncestorSymlinkRedirectingIntoDeniedTree(t *testing.T) {
	dir := t.TempDir()
	hop := filepath.Join(dir, "innocent-looking-cache")
	if err := os.Symlink("/System", hop); err != nil {
		t.Fatalf("setup: %v", err)
	}

	target := filepath.Join(hop, "Library")
	resolveExpectingError(t, target, ErrDenied)
}

// The same attack one level further out: the link is not the last component
// before the leaf, it is buried in the middle of an otherwise ordinary path.
func TestRejectsDeeplyBuriedSymlinkRedirect(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, "level-one", "level-two")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	hop := filepath.Join(nested, "level-three")
	if err := os.Symlink("/usr", hop); err != nil {
		t.Fatalf("setup: %v", err)
	}

	resolveExpectingError(t, filepath.Join(hop, "bin"), ErrDenied)
}

// A link in the final component is the target. Following it would mean a
// caller asking to remove a link instead removes whatever it points at, which
// is a different and far more destructive operation.
func TestDoesNotFollowSymlinkInFinalComponent(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real-file")
	if err := os.WriteFile(real, []byte("payload"), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}
	link := filepath.Join(dir, "link-to-real")
	if err := os.Symlink(real, link); err != nil {
		t.Fatalf("setup: %v", err)
	}

	resolved, err := Resolve(link)
	if err != nil {
		t.Fatalf("Resolve(%q) = %v, want success", link, err)
	}
	defer resolved.Close()

	if !resolved.IsSymlink() {
		t.Fatal("resolved target should be the link itself, not its destination")
	}

	var linkStat, realStat unix.Stat_t
	if err := unix.Lstat(link, &linkStat); err != nil {
		t.Fatalf("lstat link: %v", err)
	}
	if err := unix.Stat(real, &realStat); err != nil {
		t.Fatalf("stat destination: %v", err)
	}
	if resolved.Identity().Inode != linkStat.Ino {
		t.Fatal("captured identity is not the link's own identity")
	}
	if resolved.Identity().Inode == realStat.Ino {
		t.Fatal("captured identity is the destination's, the link was followed")
	}
}

// A link pointing into a denied tree, as the final component, is still
// resolvable: unlinking a link does not touch its destination. This documents
// a deliberate decision rather than an oversight.
func TestAllowsFinalSymlinkPointingAtDeniedTree(t *testing.T) {
	dir := t.TempDir()
	link := filepath.Join(dir, "dangling-system-link")
	if err := os.Symlink("/System/Library", link); err != nil {
		t.Fatalf("setup: %v", err)
	}

	resolved, err := Resolve(link)
	if err != nil {
		t.Fatalf("Resolve(%q) = %v, want success: removing a link does not touch its destination", link, err)
	}
	defer resolved.Close()

	if !resolved.IsSymlink() {
		t.Fatal("target should be reported as a link")
	}
}

func TestRejectsSymlinkLoop(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "loop-one")
	second := filepath.Join(dir, "loop-two")
	if err := os.Symlink("loop-two", first); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.Symlink("loop-one", second); err != nil {
		t.Fatalf("setup: %v", err)
	}

	resolveExpectingError(t, filepath.Join(first, "anything"), ErrSymlinkDepth)
}

func TestReportsMissingTarget(t *testing.T) {
	dir := t.TempDir()
	resolveExpectingError(t, filepath.Join(dir, "never-created"), ErrNotFound)
}

func TestRejectsWalkThroughNonDirectory(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "regular-file")
	if err := os.WriteFile(file, []byte("payload"), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}
	resolveExpectingError(t, filepath.Join(file, "child"), ErrNotDirectory)
}

func TestResolvesOrdinaryTargetWithCorrectIdentity(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "cache-entry")
	if err := os.WriteFile(target, []byte("payload"), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	resolved, err := Resolve(target)
	if err != nil {
		t.Fatalf("Resolve(%q) = %v, want success", target, err)
	}
	defer resolved.Close()

	var st unix.Stat_t
	if err := unix.Lstat(target, &st); err != nil {
		t.Fatalf("lstat: %v", err)
	}
	if resolved.Identity().Inode != st.Ino {
		t.Fatalf("identity inode = %d, want %d", resolved.Identity().Inode, st.Ino)
	}
	if resolved.LeafName() != "cache-entry" {
		t.Fatalf("leaf name = %q, want cache-entry", resolved.LeafName())
	}
	if resolved.ParentFD() < 0 {
		t.Fatal("parent descriptor should be open")
	}
	if err := resolved.Verify(); err != nil {
		t.Fatalf("Verify on an untouched target = %v, want nil", err)
	}
}

// The path a caller asks for and the path an operation lands on can differ,
// because temporary directories on macOS sit beneath a link. Both are kept so
// an audit trail can show the difference.
func TestRecordsBothRequestedAndResolvedPaths(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "entry")
	if err := os.WriteFile(target, []byte("payload"), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	resolved, err := Resolve(target)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	defer resolved.Close()

	if resolved.RequestedPath() != target {
		t.Fatalf("requested path = %q, want %q", resolved.RequestedPath(), target)
	}
	if !strings.HasSuffix(resolved.Path(), "/entry") {
		t.Fatalf("resolved path %q does not end at the requested leaf", resolved.Path())
	}
}

// Verify is the last gate before a destructive act. If the leaf is swapped for
// a different object after validation, the captured identity no longer matches
// and the operation must not proceed.
func TestVerifyDetectsLeafSubstitution(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "swappable")
	if err := os.WriteFile(target, []byte("original"), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	resolved, err := Resolve(target)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	defer resolved.Close()

	// Replace the object at the same name with a different one, which is what
	// an attacker racing the window between planning and execution would do.
	if err := os.Remove(target); err != nil {
		t.Fatalf("substitution setup: %v", err)
	}
	if err := os.WriteFile(target, []byte("substituted"), 0o600); err != nil {
		t.Fatalf("substitution setup: %v", err)
	}

	if err := resolved.Verify(); err == nil {
		t.Fatal("Verify accepted a substituted object, expected refusal")
	}
}

func TestVerifyReportsRemovedTarget(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "vanishing")
	if err := os.WriteFile(target, []byte("payload"), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	resolved, err := Resolve(target)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	defer resolved.Close()

	if err := os.Remove(target); err != nil {
		t.Fatalf("removal setup: %v", err)
	}
	if err := resolved.Verify(); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Verify after removal = %v, want ErrNotFound", err)
	}
}

// The parent descriptor is what pins the validated directory. Once it is
// released, the handle must not keep reporting success, since there is nothing
// holding the directory open any more.
func TestVerifyFailsAfterClose(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "entry")
	if err := os.WriteFile(target, []byte("payload"), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	resolved, err := Resolve(target)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if err := resolved.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := resolved.Close(); err != nil {
		t.Fatalf("second Close should be a no-op, got %v", err)
	}
	if err := resolved.Verify(); err == nil {
		t.Fatal("Verify on a closed handle should fail")
	}
}

// A rejected target must not leave a descriptor behind. Repeatedly failing to
// resolve would otherwise exhaust the process descriptor budget, which is the
// kind of defect that only appears under sustained real use.
func TestRejectedResolutionDoesNotLeakDescriptors(t *testing.T) {
	dir := t.TempDir()
	hop := filepath.Join(dir, "redirect")
	if err := os.Symlink("/System", hop); err != nil {
		t.Fatalf("setup: %v", err)
	}
	denied := filepath.Join(hop, "Library")

	before := openDescriptorCount(t)
	for i := 0; i < 200; i++ {
		if _, err := Resolve(denied); err == nil {
			t.Fatal("expected denial")
		}
		if _, err := Resolve(filepath.Join(dir, "does-not-exist")); err == nil {
			t.Fatal("expected not found")
		}
	}
	after := openDescriptorCount(t)

	// A small amount of movement is normal from test framework activity. A leak
	// on this loop would show up as roughly four hundred.
	if after-before > 16 {
		t.Fatalf("descriptor count grew from %d to %d, indicating a leak", before, after)
	}
}

// openDescriptorCount probes how many descriptors this process currently holds
// by counting which low numbered descriptors are live.
func openDescriptorCount(t *testing.T) int {
	t.Helper()
	count := 0
	for fd := 0; fd < 1024; fd++ {
		var st unix.Stat_t
		if err := unix.Fstat(fd, &st); err == nil {
			count++
		}
	}
	return count
}
