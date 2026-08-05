package pathvalidation

import "errors"

// Validation failures. Callers are expected to distinguish these with
// errors.Is rather than by inspecting message text, since the messages carry
// path detail that varies per call.
var (
	// ErrEmptyPath is returned for an empty target.
	ErrEmptyPath = errors.New("path is empty")

	// ErrNotAbsolute is returned for a relative target. There is no notion of
	// "relative to the current directory" for a deletion candidate.
	ErrNotAbsolute = errors.New("path is not absolute")

	// ErrTraversal is returned when a dot-dot appears as a complete path
	// component. A filename that merely contains two dots is not a traversal
	// and is allowed.
	ErrTraversal = errors.New("path contains a traversal component")

	// ErrControlCharacter is returned for control characters or newlines, which
	// have no legitimate place in a target and break downstream logging.
	ErrControlCharacter = errors.New("path contains control characters")

	// ErrDenied is returned when the path, or an ancestor of it, is on the
	// structural denylist. This is the floor beneath policy: it holds even if
	// the protection rule file is empty, missing, or wrong.
	ErrDenied = errors.New("path is denied by the structural floor")

	// ErrSymlinkDepth is returned when link resolution exceeds the hop budget,
	// which indicates a loop or a deliberately pathological chain.
	ErrSymlinkDepth = errors.New("symbolic link resolution exceeded maximum depth")

	// ErrNotDirectory is returned when an intermediate component exists but is
	// not a directory, so the walk cannot continue through it.
	ErrNotDirectory = errors.New("intermediate path component is not a directory")

	// ErrNotFound is returned when the final component does not exist. This is
	// frequently benign: a cleanup candidate that is already gone.
	ErrNotFound = errors.New("path does not exist")

	// ErrResolution is returned when the walk fails for a reason that is not
	// one of the above, such as a permission failure part way down. It is
	// always a denial. There is no path on which an inconclusive result is
	// treated as safe.
	ErrResolution = errors.New("path could not be resolved")
)
