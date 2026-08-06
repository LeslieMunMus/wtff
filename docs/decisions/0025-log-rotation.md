# 0025: bounding the operation log

Status: done.

The operation log grew without limit. It is the same unbounded growth as the
transcript in decision 0022, with a sharper edge: this file is the only record
that survives a permanent deletion, so the fix could not be to discard it.

## Rotating rather than truncating

At four mebibytes the active log is renamed to `operations.log.1`, the
previously rotated files shift along, and the fifth is dropped. Twenty
mebibytes total, roughly a hundred runs per file and several hundred across
all five.

Renaming rather than truncating is what makes this safe while another run holds
the file open. Two wtff runs writing at once is expected and the log is opened
append only for exactly that reason. A rename leaves the other run's descriptor
pointing at the renamed file, so its records land in `operations.log.1` instead
of being lost. Truncating in place would discard whatever it had already
written, which is the failure mode that matters most for a file whose whole
purpose is to say what was destroyed.

## Rotating at open, not per record

The decision is made once when the log is opened. A single run writes a few
hundred lines and cannot outgrow the limit between one open and the next, so a
stat per record would buy nothing for its cost.

A failure to rotate does not stop the log opening, and does not stop the
operation. Refusing to delete because a file could not be renamed trades a real
problem for a bookkeeping one, which is the position the package already took
for write failures. The failure is remembered and surfaces through `Err`, which
the commands already check at the end of a run.

## The package had no tests at all

That was the larger finding. The audit trail for every destructive operation in
this project was completely untested, which had gone unnoticed because the
coverage report simply said "no test files" rather than a low percentage.

Fifteen tests now cover it: one JSON object per line, filled in and explicit
timestamps, sixteen goroutines writing concurrently without corrupting a line,
the discard writer, a nil writer, records after close, owner only permissions
on both the file and its directory, and the rotation behavior above.

## Red team

Two of the new tests failed on their first run, and both were the test's fault
rather than the code's.

**A setup that rotated too early.** The concurrency test grew the log before
the first writer opened it, so that first open did the rotating and the writer
was already on a fresh file when the second run arrived. It proved nothing
about records surviving a rotation. Corrected to grow the log after the first
writer is holding it.

**A failure that was not a failure.** The rotation failure test put a directory
where the rotated file needed to go, expecting the rename to be refused. It is
not: the shifting loop renames that directory along to the next number like
anything else, and rotation succeeds. Corrected to make the containing
directory unwritable, which fails the renames for real.

Four guards were each removed in turn and the matching tests confirmed to fail:
rotation itself, renaming rather than truncating, shifting the older
generations, and reporting a rotation failure through `Err`.

## Verification

Full suite green. Against the installed binary: a log grown to 4,196,568 bytes
was rotated on the next run, leaving a 617 byte active log holding that run's
records as valid JSON, with all 4,196,568 bytes preserved in
`operations.log.1`.

## Known limitations

Rotation is by size only. A machine that runs wtff rarely keeps its history
indefinitely, which is the intended trade, but there is no way to ask for the
last ninety days specifically.

Two runs starting at the same moment can both decide to rotate. The rename is
atomic and the loser rotates an almost empty file, so the cost is one extra
generation consumed rather than any loss, but it is a real if unlikely waste.
