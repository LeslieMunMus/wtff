package deletionengine

import (
	"os"
	"path/filepath"
	"strings"
)

// PolicyChecker decides whether a structurally valid path is nonetheless
// something that must not be removed.
//
// This is an interface rather than a direct dependency on the protection rules
// package for two reasons. It keeps the engine testable without loading a real
// rule set, and it keeps the boundary honest: the engine is not allowed to
// reach into policy data and make its own judgments about it.
type PolicyChecker interface {
	// Protected reports whether a resolved path is protected, and if so, the
	// identifier and human readable reason of the rule responsible. A protected
	// entry that cannot say which rule protected it is not actionable feedback,
	// so both are required.
	Protected(resolvedPath string) (ruleID string, reason string, protected bool)
}

// AllowAll is a checker that protects nothing. It exists for tests and for the
// period before the protection rules package is written.
//
// It is deliberately named for what it does rather than something neutral like
// "default", so that a call site relying on it is obvious in review.
type AllowAll struct{}

// Protected always reports false.
func (AllowAll) Protected(string) (string, string, bool) { return "", "", false }

// selfProtection refuses any path belonging to wtff itself.
//
// This is enforced in the engine rather than left to the rule file. A rule file
// can be edited, replaced, or fail to load, and the failure mode here is
// specific and bad: staging the staging area destroys the record undo depends
// on, so the very act of removing it makes it unrecoverable. The engine holds
// this one itself.
type selfProtection struct {
	roots []string
}

func newSelfProtection() selfProtection {
	home, err := os.UserHomeDir()
	if err != nil {
		return selfProtection{}
	}

	declared := []string{
		filepath.Join(home, "Library", "Application Support", "wtff"),
		filepath.Join(home, "Library", "Logs", "wtff"),
		filepath.Join(home, ".config", "wtff"),
	}

	// Each root is held in both its declared and its fully resolved form.
	//
	// The comparison runs against a resolved path, so a root recorded only as
	// declared fails to match whenever any component of the home directory is a
	// link. That failure is silent and produces the one outcome this guard
	// exists to prevent: wtff staging its own staging area, which destroys the
	// record undo depends on and so cannot be undone. Resolving costs a few
	// calls once per run and removes the dependency on the home directory
	// happening not to contain a link.
	roots := make([]string, 0, len(declared)*2)
	for _, root := range declared {
		roots = append(roots, root)
		resolved, resolveErr := filepath.EvalSymlinks(root)
		// A root that does not exist yet cannot be resolved, which is normal on
		// a first run and is not a problem: there is nothing there to protect
		// until wtff creates it, and the declared form still matches once it is
		// created beneath a link free home.
		if resolveErr == nil && resolved != root {
			roots = append(roots, resolved)
		}
	}
	return selfProtection{roots: roots}
}

// covers reports whether a resolved path is, or lies beneath, wtff's own state.
func (s selfProtection) covers(resolvedPath string) (string, bool) {
	for _, root := range s.roots {
		if resolvedPath == root || strings.HasPrefix(resolvedPath, root+string(os.PathSeparator)) {
			return root, true
		}
		// Compare case insensitively as well, since Apple filesystems are
		// normally case insensitive and a differently cased path names the
		// same directory.
		if strings.EqualFold(resolvedPath, root) ||
			hasPrefixFold(resolvedPath, root+string(os.PathSeparator)) {
			return root, true
		}
	}
	return "", false
}

func hasPrefixFold(value, prefix string) bool {
	return len(value) >= len(prefix) && strings.EqualFold(value[:len(prefix)], prefix)
}
