# 0034: the disk usage scanner

Status: scanner done. The browsing interface and selection are not built yet.

`internal/space-scan` measures where disk space has gone and returns a tree
sorted largest first. It only reads. A selection made against this tree will be
handed to the deletion engine, which applies the same path validation,
protection rules, and staging as every other command, so a mistake in the walk
cannot widen what is removable.

## Bounds, and why they match the deletion engine's

A home directory is not a bounded structure. The walk carries a two million
entry cap, a depth cap of 128, and a ninety second deadline. These mirror the
deletion engine's own limits on purpose: someone who has learned wtff's
behaviour in one place should not have to learn a different set of rules here.

A bound being hit truncates the walk rather than failing it. Cleanup work is
full of directories a person cannot read, and refusing to report anything
because one subdirectory was denied would make the feature useless on exactly
the machines that need it most.

## Completeness is tracked, not assumed

Every node carries `Complete`. It is false when the walk was denied, bounded,
or cut short anywhere beneath it, and it propagates upward. A directory whose
total is a floor says so.

This matters more here than almost anywhere else in the project: a person
reading this tree is deciding what to delete, and a partial sum presented as
exact would invite them to act on a number that is wrong in the direction that
costs them something.

## Symlinks are counted, never followed

A link contributes its own small entry and is not descended into. Following
would let one loop become an unbounded walk, and would let a link pointing out
of the scan root silently widen what the scan covers. A test plants a link to a
directory holding 50,000 bytes outside the root and requires the total to stay
at the 10,000 bytes genuinely inside it.

## Measured against a real home directory

636,548 entries in 17.5 seconds, 496GB total, 143 directories denied. The tree
correctly reported itself incomplete, named `~/Library` as a floor rather than
a total, and listed the denied paths, the first of which was `~/.Trash`.

That figure is a useful check on the design: seventeen seconds is slow enough
that the interface needs the progress counter, and fast enough that a person
will wait for it.

## Red team

Four guards were each removed in turn and the matching test confirmed to fail:
symlink following, the deadline check, marking a denied directory as complete,
and the largest first ordering.

## Known limitations

Hard linked files are counted once per link, so a tree using them can total
more than the disk actually holds. Deduplicating by inode is possible and adds
per node state; deferred until something demonstrates it matters.

Sizes are apparent size, matching what the rest of wtff reports, rather than
allocated blocks. Sparse and compressed files will therefore differ from `du`.

A single `ReadDir` against a mount that has stopped answering is unbounded, the
same limitation the deletion engine documents. The deadline is checked between
entries, not inside a syscall. Interrupting the program is what covers that
case.
