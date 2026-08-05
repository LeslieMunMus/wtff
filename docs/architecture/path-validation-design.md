# Path validation: design

Status: design done, implementation not started.

## What this package is responsible for

One question only: is it structurally safe to touch this path, independent of whether policy
allows touching it. Policy is `internal/protection-rules`. This document is scoped to
structural safety alone.

## The core problem with string-path validation

A validator that checks a path string and then hands that same string to a separate delete
call has a gap between the two: anything can change on disk in between. A parent directory can
be replaced with a symlink after the check runs and before the delete call resolves the path
again from scratch. Every shell-based tool that validates a string and then calls `rm` on that
string has to defend against this as a bolt-on second pass, typically by walking the ancestor
chain a second time immediately before the delete and re-running the same checks against the
resolved result.

wtff avoids the gap structurally instead of patching around it. The approach:

1. Resolve the path one component at a time, starting from a fixed, trusted root file
   descriptor (the volume root), using `openat` with `O_NOFOLLOW` on every intermediate
   component. If any intermediate component turns out to be a symlink, the open fails instead
   of silently following it. This means a symlink swap mid-resolution is caught as a hard
   error, not something a second pass has to notice after the fact.
2. The result of that walk is a file descriptor, not a path string. Every later operation
   (stat, delete, move) is a descriptor-relative syscall (`fstatat`, `unlinkat`, `renameat`)
   against that descriptor, not a fresh string-based resolution. The thing that was validated
   is the thing that gets operated on, by construction, rather than by a second check hoping
   the two still agree.
3. The descriptor's identity, device number and inode number, is captured at validation time
   and carried forward. The deletion engine re-checks this identity immediately before
   executing, and refuses if it no longer matches. This is the last line of defense for the
   narrow window between planning and execution, not the primary defense; the primary defense
   is that the descriptor was never a re-resolvable string to begin with.

This is a genuinely different mechanism from a string-based validator with an ancestor-symlink
scan bolted on, not just a more thorough version of the same idea. It removes an entire class
of race condition by construction rather than by checking for it more carefully.

## Structural checks, in order

1. **Absolute path required.** A relative path is rejected outright; there is no notion of
   "relative to the current directory" in a deletion target.
2. **Traversal rejection.** A `..` component is rejected only when it appears as a complete
   path component (`/../`, a leading `../`, or a trailing `/..`), not as a substring. This
   allows legitimate filenames that happen to contain two dots, without allowing an actual
   traversal attempt.
3. **Control character rejection.** No control characters, no embedded newlines.
4. **Component-by-component descriptor resolution**, as described above, starting from the
   volume root and refusing to follow a symlink at any intermediate step.
5. **Structural denylist**, checked before the walk completes: a small, fixed set of top-level
   and near-top-level paths that must never be a deletion target regardless of any rule file,
   because passing one of them as a target indicates a bug or an attack, not a legitimate
   cleanup candidate. This list is intentionally tiny and is not where policy lives; policy
   (protecting a keychain directory, an active database, a vendor's license file) belongs in
   `internal/protection-rules`, not here. This list is the structural floor underneath policy:
   the paths on it stay denied even if the protection-rules file were empty, missing, or wrong.
6. **Fail closed on any resolution error.** A permission failure, an unreadable metadata call,
   or an unexpected file type appearing mid-walk all result in denial. There is no fallback
   path that treats an inconclusive result as safe.

## What this package deliberately does not do

It does not decide whether a structurally valid path should actually be deleted. A path can
pass every check in this document and still be something that must never be touched, such as a
user's keychain file. That decision belongs entirely to `internal/protection-rules`, which is
consulted separately by the deletion engine. Keeping the two separate means a bug in the
structural walker cannot silently disable policy, and a bug in policy cannot silently disable
the structural walker.

## Open questions for implementation

- Exact identity tuple to snapshot: device and inode are the minimum; whether to also include a
  modification time or a content hash for small files is a tradeoff between race-window
  precision and cost, to be settled once real benchmarks exist.
- Cross-volume behavior: `openat`-based resolution assumes descriptors and inodes are stable
  within a single volume. A path that crosses a volume boundary during symlink resolution needs
  explicit handling, not an assumption that the same guarantees hold.
- Performance of a per-component descriptor walk against a shell-based single-syscall check,
  to be measured once there is a working implementation and a realistic workload to test it
  against.

## How this could improve with time

Once implemented, this design should be tested adversarially before anything is built on top of
it: symlink swaps mid-walk under concurrent load, TOCTOU races deliberately induced in a test
harness, and the same traversal and control-character fuzz inputs a mature tool would be
expected to reject. The design is written to make those attacks structurally hard; the test
suite's job is to prove that claim rather than assume it.
