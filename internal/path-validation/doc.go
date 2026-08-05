// Package pathvalidation answers one question: is it structurally safe to touch
// this path.
//
// It does not decide policy. A path can pass every check in this package and
// still be something that must never be deleted, such as a user's keychain.
// That decision belongs to the protection-rules package, which the deletion
// engine consults separately. Keeping the two apart means a defect in the
// structural walker cannot silently disable policy, and a defect in policy
// cannot silently disable the walker.
//
// The central idea is that validating a path string and then handing that same
// string to a delete call leaves a gap: the filesystem can change between the
// check and the use. This package closes the gap by resolving a path one
// component at a time and returning a descriptor to the containing directory
// rather than a string. Every subsequent operation is descriptor relative, so
// the directory that was validated is the directory that gets operated on,
// because the kernel is holding it open, not because a second check hoped it
// still matched.
//
// Symbolic links in intermediate components are followed, not refused. Refusing
// them is not an option on macOS, where /var, /tmp, and /etc are themselves
// links into /private and a great many legitimate cleanup targets live beneath
// them. They are followed deliberately: each hop is counted, bounded, and
// re-checked against the structural denylist, so a link cannot be used to
// smuggle a target into a denied tree.
//
// A symbolic link in the final component is never followed. Deleting a link and
// deleting what it points at are different operations, and this package resolves
// to the link itself so that callers unlink the link.
package pathvalidation
