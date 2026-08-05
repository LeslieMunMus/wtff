# 0016: purge, and permanent deletion from staging

Status: done.

Built on Opus, agreed in advance, because this adds the first paths in wtff
that destroy data with no way back.

## The shape the project manager asked for

Clean searches deeply and stages, so the decision to keep or discard is made
after seeing what was found, not before. Staging then offers both directions:
restore, or delete permanently. Alongside that, a shallow `purge` for things
that were meant to be gone anyway.

That split is now the design. Clean infers what is disposable and stages,
because an inference can be wrong. Purge does not infer, so it has nothing to
stage.

## What qualifies for purge

One entry: the Trash. The bar is deliberately not "regenerable," which nearly
every catalog entry already is. It is "the person already decided to discard
this." Emptying the Trash completes a decision they made; deleting a cache
enacts one wtff made on their behalf, and that is what staging exists to make
reversible.

Caches were considered and refused. Permanently deleting them would duplicate
clean while removing the property that makes clean safe.

Adding to the purgeable set requires more than a boolean. A `purgeable` entry
must carry a `purge_reason` of its own, and the loader refuses one without it,
or a `purge_reason` on an entry not marked purgeable. A test pins the shipped
set at exactly `trash-contents`, so growing it is a deliberate edit to a test
rather than a line in a file nobody reviews.

## DNS flush: no, and not later without its own design

It was raised as a maybe. It does not belong here. It needs sudo, which would
introduce privilege escalation to a toolkit that currently has none. It frees
no disk space. And it has no path, so it cannot be planned, validated against
protection rules, logged, or undone; every safety property in this project is
expressed in terms of a path. It is a system state operation wearing the same
word. If it is ever wanted, it is a separate command with its own review.

## Purging a staged batch

`StagingArea.PurgeBatch` is the one operation that withdraws a promise wtff
already made, so it refuses anything it cannot first prove is its own scratch
space. It descends by descriptor from the staging root rather than by path, it
requires the batch to sit directly inside that root, it requires the record's
identifier to agree with the directory it was found in, and `FindBatch`
validates a typed identifier as a single path component before joining it to
anything.

Protection rules are not re-run against a staged batch, and that is deliberate
rather than an omission: every item passed the rule check at plan time, staging
is the only way into a batch, and the rules describe original locations these
items no longer occupy. Re-running them against a staged name would be checking
the wrong thing and reporting it as safety.

## Friction

Reversible actions keep a keystroke. Irreversible ones require typing
`permanently`, in both the command line and the shell, deliberately the same
word in both so nobody learns two answers. In the shell, Enter alone can never
approve a deletion, because Enter is the key that got the person to that screen
and momentum is the entire risk. Restore is the resting cursor position on the
staged batch menu; reaching deletion takes a deliberate move.

## Red team

**A test that passed for the wrong reason.** The containment test built a
forged batch whose directory name did not match its identifier. It passed with
the containment check deleted, because the identifier check was refusing it
instead, and would have reported a guard as working long after someone removed
it. Rebuilt so containment is the only guard that can refuse it.

**A traversal test that proved nothing.** `FindBatch` was checked against ids
like `..` and `../../etc`, all of which fail anyway on a missing record. Added
a real, loadable batch record planted one level above the staging area, which
succeeds without the name check and is refused with it.

**A wrong label in the destructive flow, found by looking rather than
testing.** The rendered purge selection said "select items to stage" and hinted
"enter stage" while Enter led to permanent deletion, and the justification under
each row was the clean reason, which states the removal routes through the
reversible staging area. The interface was promising reversibility at the one
moment there was none. Every test in this change passed while that was true.
Fixed so the verb follows the manifest's action, and so purge candidates carry
their purge justification; both pinned by regression tests.

**A test that failed on its own name.** A check that purge planned nothing
under `Caches` matched the temporary directory Go builds from the test function
name, which contained the word. Rewritten to compare against the real path.

Each guard was then verified by mutation rather than by a passing name:
containment, the items-directory `O_NOFOLLOW`, the identifier cross-check, the
`FindBatch` component check, and the corrected purge label were each removed in
turn, and the corresponding test was confirmed to fail and then pass again.

## Verification

Full suite green, vet and gofmt clean. Beyond that, the compiled binary was
driven against isolated home directories, never the real machine: `purge
--dry-run` listed only Trash and changed nothing; answering `y` to the gate
aborted while `permanently` proceeded; a cache in the same home survived both;
a staged batch was purged and `undo` then failed rather than appearing to
succeed; traversal identifiers exited non-zero and deleted nothing. The shell
was driven through a pseudo terminal for the purge selection, the confirmation
gate refusing a wrong word, and the staged restore-or-delete menu through to a
completed deletion.

## Known limitations

Purge only covers the user's own `~/.Trash`. Per-volume `.Trashes` on external
disks are not touched yet, so a person who trashed something from an external
drive will not see it here.

`PurgeAll` reports the first failure and continues, so a staging area holding
one unreadable batch still clears the rest, but the summary names one cause
rather than all of them.
