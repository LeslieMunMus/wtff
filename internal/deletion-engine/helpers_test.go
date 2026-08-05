package deletionengine

import (
	"testing"

	pathvalidation "github.com/lesliemusengi/wtff/internal/path-validation"
)

// resolveForTest reports the path the engine will see for a target, which under
// a temporary directory differs from the one the test wrote to because the
// macOS temporary area sits beneath a link. A test that compared against the
// requested path would configure policy for a path the engine never consults.
func resolveForTest(t *testing.T, path string) string {
	t.Helper()
	resolved, err := pathvalidation.Resolve(path)
	if err != nil {
		t.Fatalf("resolving %s for the test: %v", path, err)
	}
	defer resolved.Close()
	return resolved.Path()
}
