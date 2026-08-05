# 0004: path validation implementation

Status: done.

## Context

Decision 0003 specified the design for structural path validation. This entry covers building
it. This is the foundation every destructive operation in wtff sits on, so it was implemented
first, on the stronger model tier, before anything was layered on top of it.

## What was built

`internal/path-validation`, package `pathvalidation`, in four source files:

- `doc.go` states what the package is responsible for and, as importantly, what it is not.
- `errors.go` defines the failure set. Callers distinguish failures with `errors.Is` rather
  than by matching message text, since messages carry per-call path detail.
- `denylist.go` holds the structural floor and the identity comparison that backs it.
- `resolve.go` holds the walker and the `Resolved` handle type.

The public surface is `Resolve(target string) (*Resolved, error)`. The handle exposes the
parent directory descriptor, the leaf name, the requested and resolved paths, the captured
identity, type predicates, `Verify`, and `Close`.

## How it works

Resolution proceeds one component at a time from the volume root. Each component is inspected
with `fstatat` without following links, and intermediate directories are opened with `openat`
using `O_NOFOLLOW`. What comes back is not a path string but an open descriptor to the
directory containing the target, plus the leaf name. Callers act with descriptor relative
syscalls, so the directory that was validated is the directory the kernel operates on, because
it is held open, not because a later check hoped it still matched.

Links in intermediate components are followed rather than refused. Refusing them is not viable
on macOS, where `/var`, `/tmp`, and `/etc` are themselves links into `/private` and many
legitimate cleanup targets live beneath them. Each hop is counted against a bound of forty, and
the denylist is re-applied on arrival, so a link cannot smuggle a target into a denied tree.

A link in the final component is never followed. Removing a link and removing what it points at
are different operations, and the handle resolves to the link.

The structural floor is deliberately small and is not the protection rule set. Its job is to
catch targets whose presence indicates a defect or an attack rather than a policy call that
went the wrong way. It is checked by text and by device and inode identity, because Apple
filesystems are usually case insensitive, so `/SYSTEM` and `/System` are one directory that
compares unequal as text, and because a path can reach a denied directory through a link that
leaves no trace in the string.

## Verification

Twenty eight test cases, all passing, `go vet` clean, `gofmt` clean. The suite covers malformed
input, the structural floor, home directory roots, case aliasing, ancestor links redirecting
into denied trees at two different depths, link loops, final component link handling in both
directions, identity capture, substitution detection, and descriptor lifetime.

Two checks are worth calling out because they test the claim rather than the code. The ancestor
redirect case was verified independently at a shell prompt: the requested path is textually
innocent, a prefix comparison against denied roots accepts it, and it resolves to
`/System/Library`. A string based validator passes that path; this package denies it. The
descriptor leak case runs four hundred failing resolutions and asserts the process descriptor
count does not grow, since a leak on the error path is the kind of defect that only appears
under sustained real use.

## What was deliberately not done

- No deletion. This package resolves and validates; it does not remove anything. The deletion
  engine is the next step and is the package that will consume these handles.
- No protection rule evaluation. That is a separate package by design, so a defect in one
  cannot silently disable the other.
- No privileged ancestor immutability check yet. It is specified in the deletion engine design
  and belongs with the code that performs privileged operations.
- The module path `github.com/lesliemusengi/wtff` is an assumption about eventual hosting. It
  was necessary to compile. Changing it later means rewriting import lines across the tree, so
  it is worth confirming before the tree grows.

## Known limitations

The identity re-check in `Verify` narrows but does not eliminate the window between checking
and acting. The parent directory is pinned by an open descriptor and cannot be swapped, which
is the substantive guarantee. Within that pinned directory a leaf could in principle be
replaced between `Verify` and the operation that follows it. Closing that fully requires the
operation itself to be identity aware at the syscall level, which is a deletion engine concern
and is noted there rather than solved here.

Cross volume behavior is untested. Descriptor and inode guarantees hold within a volume; a walk
that crosses a volume boundary during link resolution needs explicit handling and a test, and
currently has neither.

## How this could improve with time

Add a fuzz target over the string checks, since traversal and control character handling is
exactly the kind of surface where a hand written case list eventually misses something. Add a
concurrent test that races link swaps against an in flight walk under load, to exercise the
pinning guarantee rather than reasoning about it. Settle and test the cross volume case before
any feature depends on it.
