// Package houserules holds tests that enforce this project's own standing
// rules against its source tree.
//
// These live as Go tests rather than as shell in the Makefile deliberately.
// The first version of the em dash check was a grep invocation using a flag
// BSD grep does not have, so on macOS it failed to run, the shell negation
// turned that failure into a pass, and the check reported clean while
// examining nothing. A check that cannot fail is worse than no check, because
// it is trusted. Written as a test, the scanner itself can be tested.
package houserules
