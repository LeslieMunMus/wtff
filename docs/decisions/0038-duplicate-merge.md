# 0038: merging duplicates rather than choosing between them

Status: merge engine done. The browsing interface is next.

`internal/duplicate-merge` gathers identical copies into one directory and
keeps every one of them.

## Why merge exists at all

The obvious design for a duplicate finder is "pick a winner, delete the rest,"
which assumes one copy is disposable. The project manager rejected that
framing: a person may have two copies of something and want both, under names
that say which is which.

That is the more honest model. The difference between "I have this twice by
accident" and "I have two versions of this" is one only the owner can draw, and
merge takes no position on it. Every copy survives, nothing is removed, and the
report says where each one went.

Staging the extras for deletion remains available for the case where a copy
genuinely is disposable. Merge is the option that does not require deciding.

## The oldest copy stays put

Its directory is the destination. The oldest copy is usually the one someone
put somewhere deliberately; later copies tend to be accidents of downloading or
duplicating. The detector already orders groups oldest first for this reason,
and that ordering is load bearing here rather than presentational.

A copy already sitting in the destination is left alone rather than renamed for
consistency, because moving a file to its own directory under a new name
achieves nothing and loses the name its owner chose.

## Never overwriting is the rule everything defers to

A merge moves files into a directory that already holds files, so this is not
theoretical. Four separate things enforce it:

Names are checked against the destination at planning time, with `Lstat` rather
than `Stat`, because a broken symlink occupies a name just as firmly as a real
file and renaming onto it would destroy it.

Names are reserved across the whole plan as they are allocated, so two copies
merging in one operation cannot both be promised the same destination.

The destination is checked again immediately before each rename. `os.Rename`
overwrites without complaint, and the window between planning and applying is
real.

A move that is refused leaves its source exactly where it was, and the
remaining moves still run, because stopping halfway leaves someone with a merge
they must finish by hand without knowing how far it got.

## Not undoable through wtff undo, and why that is acceptable

Merging is a rename, not a removal, so the staging area is not involved and
`wtff undo` does not apply. Nothing is destroyed: every copy is still on disk,
in a directory the report names, under a name the report gives. The operation
log records both the source and the destination of every move, because knowing
something moved without knowing where is not a record anyone can act on.

This is a smaller promise than staging makes, and it is honest about being
smaller rather than borrowing the word "undo" for something that would not
restore anything.

## Red team

Four guards were each broken in turn and the matching tests confirmed to fail:
the planning time collision check, the re-check before rename, reserving names
across a plan, and continuing past a failure.

## Known limitations

Merging across volumes will fail, since `os.Rename` cannot cross a filesystem
boundary. The failure is reported per move and the source is left in place, but
the message is the raw system error rather than an explanation.

A merge cannot be reversed by a command. Doing so would mean recording moves in
a form `undo` understands, which is real work and worth doing only if merges
turn out to need reversing in practice.
