# 0005: path validation red team pass

Status: done.

## Context

A standing rule was added that every stage gets an adversarial review before it is called done,
on the reasoning that a defect is cheapest to fix at the stage that introduced it. This entry
records the first such pass, applied to the path validation work from decision 0004, which had
already been committed and reported as complete.

The pass found two defects in the implementation and two in its test suite. Recording the ones
that were wrong matters as much as the ones that were right, since the reason they were missed
is more reusable than the fixes.

## Finding one: a parent reference inside a link target was handled by an assumption

`splitComponents` carried a comment stating that a parent reference in a link target would be
handled by the walk failing to find the component. That was asserted, not verified, and it is
false. A parent reference is an ordinary directory entry, not a link, so inspecting it without
following links succeeds and opening it without following links succeeds. The walk climbed.

Containment was not breached, because the denylist is compared by device and inode identity and
still refused denied trees on arrival. What broke was reporting. The logical path string kept
accumulating components while the descriptor chain moved the other way, producing a reported
path like `/a/b/../../System` that never collapsed. A preview showing one location while the
operation lands on another is precisely the failure this package exists to prevent, so this was
treated as a defect of the same seriousness as a containment failure rather than a cosmetic one.

Fixed by handling a parent reference as an explicit case in the walk, ascending on the
descriptor and collapsing the logical string in the same step so the two cannot drift apart.
The parent directory is checked against the denied tree identities after the ascent.

Verified directly rather than by test name alone: resolving a target through an ascending link
now reports a fully collapsed path, and the requested and resolved paths are both retained and
visibly differ.

## Finding two: a window between inspecting a component and opening it

The walk inspected each intermediate component and then opened it as two separate operations.
Opening without following links rejects an entry swapped for a link in between, but not one
swapped for a different directory.

The practical exposure was low, since the denied trees are root owned and an unprivileged
attacker cannot move them, but the mitigation is one additional call. After opening, the
descriptor's own identity is compared against what was inspected, and a mismatch is refused.
From that point the walk holds the object it checked rather than a name that referred to it a
moment earlier.

This fix is reasoned rather than tested. Reliably losing a deliberately induced race in a unit
test is difficult, and a test that usually passes would be worse than none. Noted as a gap.

## Finding three: a test that passed for the wrong reason

The test asserting that an ascending link cannot reach a denied tree used ten parent references.
From a temporary directory that lands on an intermediate directory rather than the volume root,
so the walk refused because a component did not exist, not because a denied tree was refused.
The test passed and proved nothing.

Fixed by overshooting deliberately, since ascending past the volume root stays at the root and
counting exactly is not required. This is the second time in this project that a test has been
found to pass for a reason other than the one its name claims, which is why the standing rule
now requires confirming that a test fails when the protection is removed.

## Finding four: a test that normalized away the thing it was testing

A test intended to feed a parent reference into resolution built its input with `filepath.Join`,
which collapses parent references before returning. The value under test never contained one.
Rebuilt using string concatenation.

That failure also exposed dead code. The new parent reference branch included a guard for the
case where the reference is the final remaining component, and that case is unreachable: a link
is only followed as an intermediate component, so at least one component always follows a
spliced target, and caller supplied parent references are rejected before the walk begins. The
guard was kept, because it costs one comparison and protects against a future change returning a
handle whose leaf name is a parent reference, but it is now labelled as unreachable and
deliberately has no test. A test asserting unreachable behavior documents only that it is
unreachable, while implying coverage that does not exist.

## Verification

Forty nine tests passing, `go vet` clean, `gofmt` clean. Four new regression cases cover
ascending link path collapsing, ascent into a denied tree, ascent past the volume root, a link
whose whole target is a parent reference, and caller supplied parent references built so they
survive to the validator.

## How this could improve with time

The unopposed race in finding two deserves a stress test that runs swaps against in flight
walks under load and asserts that no walk ever completes holding an object it did not inspect,
accepting that such a test is probabilistic. The string level checks still deserve a fuzz
target. Both were already listed as gaps in decision 0004 and remain open.
