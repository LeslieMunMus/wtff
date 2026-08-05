# 0006: deletion engine implementation and red team pass

Status: done.

## Context

Decision 0003 specified the deletion engine. This entry covers building it, together with the
review pass that followed, recorded here rather than separately because three of the findings
changed the implementation before it was ever committed.

## What was built

`internal/operation-log`, package `operationlog`: an append only stream of JSON objects, one per
line, so a truncated final line stays discardable without invalidating the rest. Logging never
blocks an operation, and a write failure is reported through the writer's error state rather
than returned mid deletion, because refusing to delete over a bookkeeping failure trades a real
problem for a lesser one.

`internal/deletion-engine`, package `deletionengine`:

- `manifest.go` holds the plan format, its canonical encoding, and its digest.
- `policy.go` holds the policy interface and wtff's protection of its own state.
- `plan.go` turns candidates into a sealed manifest.
- `staging.go` holds the staging area and its batch records.
- `apply.go` executes a manifest.
- `undo.go` returns staged items to where they came from.
- `remove.go` holds descriptor relative recursive removal.

## Decisions worth recording

**The digest is computed over a hand written canonical encoding, not over JSON.** A digest is
only as trustworthy as the determinism of its input, and JSON leaves field ordering on re
encode, numeric formatting, and zero value rendering unpinned. Every variable length field is
written with an explicit length prefix, because plain concatenation is ambiguous: the paths
`/ab` and `/c` produce the same bytes as `/a` and `/bc`, so an attacker able to influence two
adjacent fields could shift content between them without changing the digest.

**Reading a manifest verifies it.** Verification is not left to the caller, because a manifest
that has been decoded but not checked is exactly the object a caller might hand to apply by
mistake, and nothing in the type distinguishes the two.

**Planning without a policy checker is refused rather than defaulted.** The failure mode of a
default is that forgetting to pass protection rules silently produces a plan with no protection.
`AllowAll` exists for the cases that genuinely want no policy, and is named so that a call site
relying on it is visible in review.

**The zero value of the action field stages.** A caller who forgets to set it gets the
reversible behavior rather than the destructive one.

**Policy is consulted again at apply time.** A manifest can be applied long after it was
planned, and the rule set may have changed in between.

**A failing entry does not abort the run.** Cleanup involves many independent items, and
stopping at the first one that changed underneath us would leave the operation half done with
no clear report. Every entry is attempted and every outcome recorded.

**Undo never overwrites.** If something now occupies an item's original location, the item stays
in staging and is reported. Replacing what is there would let undo destroy data that was never
part of the original operation.

**Cross volume staging is refused rather than worked around.** Moving across volumes means copy
then delete, and a copy that silently dropped extended attributes, access control entries, or
hard link structure would make undo a lie: the caller would be told the change was reversible,
and what came back would not be what left. Refusing is honest until a preserving copy is written
and tested.

## Red team findings

**One: self protection compared resolved paths against unresolved roots.** wtff's own state
directories were recorded as declared, built by joining the home directory with a fixed suffix,
while the comparison ran against a fully resolved path. On a home directory containing no links
the two happen to agree, which is why it passed review. The moment any component of the home
directory is a link they stop agreeing, silently, and the guard that prevents wtff from staging
its own staging area stops working. That specific failure is unrecoverable by design: staging the
staging area destroys the record undo depends on, so the act of causing it removes the means of
reversing it. Fixed by holding each root in both its declared and its resolved form. Found by a
test, not by reading the code, and pinned by a regression case that reaches the home directory
through a link.

**Two: undo opened the restore destination by path.** Opening a directory by path follows a link
on the final component, verified directly rather than assumed. A parent directory replaced by a
link while an item sat in staging would have had undo write the user's restored data into
whatever that link named. Restoring is a write, so a misdirected one leaks rather than destroys,
which makes it quieter and no less serious. Fixed by adding `ResolveDirectory` to path
validation and routing undo through it.

`ResolveDirectory` applies the tree denials in full but deliberately not the exact path denials.
Naming a protected root as a deletion target is always wrong; acting inside one is not, and an
item that legitimately lived directly beneath a directory such as `/Library` has to be
restorable to it. Applying the deletion floor to a container would have made undo fail for every
such item, which is a regression the first version of this fix would have introduced.

**Three: recursive removal went through a path.** Replaced with a descriptor relative walk that
refuses to follow links at every level, so a removal cannot be redirected after its target was
verified.

The honest limit of this one is worth stating. The test written for it passes against the
implementation it replaced, because the standard library's recursive remove does not follow
links either, which was confirmed by running it rather than assumed. The real improvement is the
removal of a path re-resolution after verification, and that gap is a race with no deterministic
test. It is recorded as reasoned rather than demonstrated, and the test was renamed to describe
what it actually shows.

**Four: staged names were not length bounded.** A staged name is derived from the original base
name, whose length the caller does not control. A name the filesystem refuses would turn a
removal that already happened into one that could be neither recorded nor undone. The readable
part is now trimmed; the index prefix carries uniqueness, so trimming cannot cause a collision.

## Verification

Ninety four tests passing across the tree, `go vet` clean, `gofmt` clean.

The manifest suite mutates all eighteen fields that influence what gets deleted and requires
every one to be caught, and separately checks that two manifests differing only in where a field
boundary falls produce different digests.

The engine suite covers the plan to apply identity boundary, digest tampering, policy re-checks,
staging and restore round trips including nested trees, refusal to overwrite on restore, staged
name collisions between identically named items from different directories, empty batch cleanup,
and the case where the staging area sits inside the directory being staged.

## Known limitations

Cross volume staging is unsupported and reported as a skip.

The plan to apply boundary is protected by identity comparison rather than by held descriptors,
because a manifest is meant to be saved and reviewed and descriptors do not survive that. This
is a deliberate trade and the reason identity is stored in the manifest.

Size measurement walks by path and is advisory. A tree that changes during measurement yields a
number that was true when taken.

The batch record is written after the moves. An interruption in between leaves items in staging
with no record, which is recoverable by hand and visible on inspection. The reverse order would
produce a record naming items that were never moved, which reads as authoritative and is not.

## How this could improve with time

Write the preserving cross volume copy, with tests covering extended attributes and access
control entries, and remove the skip. Add a retention policy that purges staged batches past
their window, which is what actually reclaims the space. Add a concurrent stress test that races
swaps against in flight applies. The protection rules package is the next dependency: the engine
currently runs against `AllowAll` in tests and has no real rule set to consult.
