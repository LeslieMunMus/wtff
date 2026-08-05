package cli

import (
	"testing"
	"time"
)

// detectDarkBackground's own worst case, an unanswered terminal query taking
// up to five seconds, cannot be exercised or bounded from a unit test: it
// happens inside Bubble Tea's package init, before any test in this package
// runs, against the real process stdio, not against anything a test can
// substitute. That was confirmed directly by running the compiled binary
// under a pseudo-terminal that never answers the query and observing the
// full delay, and confirmed again the other way by running it under one
// that answers correctly and seeing the first frame in under 25
// milliseconds. This test only checks the ordinary case: under go test's
// own stdio, which is not a terminal, detection should resolve immediately
// rather than attempting a query at all.
func TestDetectDarkBackgroundReturnsPromptlyUnderGoTest(t *testing.T) {
	start := time.Now()
	detectDarkBackground()
	elapsed := time.Since(start)

	const bound = 500 * time.Millisecond
	if elapsed > bound {
		t.Fatalf("detectDarkBackground took %s under go test's own non-terminal stdio, want under %s",
			elapsed, bound)
	}
}
