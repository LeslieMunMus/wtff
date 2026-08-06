# 0027: correcting the visibility check, and my own verification

Status: done.

The Full Disk Access check shipped in decision 0026 was misleading, and the
process of correcting it produced a second, worse error worth recording.

## The original defect

The check probed Safari, Mail, and Cookies to infer whether Full Disk Access
had been granted, then reported that "some locations are invisible to wtff".

wtff does not target those paths. It is an allowlist, not a scanner: it looks
in the handful of places its catalog names and nowhere else. So the check
answered a question nobody needed answered, and phrased the answer as though
wtff were being blocked. It prompted a person toward granting a broad
permission on the strength of a claim that was not about wtff at all.

The question worth answering is not whether a permission is granted. It is
whether anything wtff actually wants is being withheld. The check now walks the
catalog's own entries and reports only those, and an unreadable target is a
warning rather than a note, because it means clean silently proposes less than
it should.

The volume trash entry is skipped. Its path names where mount points appear
rather than a directory anything is removed from, and counting it made an empty
home report a category present.

## The worse error, which was mine

Asked whether granting Full Disk Access was necessary, I checked the six target
paths from a shell and reported that all of them were readable, concluding the
grant would buy nothing.

The check was written as `ls -A "$p" | wc -l` followed by testing `$?`, which
reads the exit status of `wc` and not of `ls`. `wc` succeeds on empty input. So
a denied directory reported "READABLE, 0 entries", and I read zero entries as
an empty Trash rather than a refused one.

`~/.Trash` is protected by TCC on macOS and genuinely cannot be listed without
the grant. Confirmed afterwards two ways: `ls` returns "Operation not
permitted", and a Python `os.listdir` raises `PermissionError`. The advice was
wrong, and the corrected check is what caught it.

Two lessons, both already in this project's rules and both worth restating
because neither prevented this: a pipeline's exit status is the last command's,
and a verification written in haste is not verification. The corrected check
disagreed with my conclusion, and the right response to a tool contradicting
you is to test the tool's claim rather than assume the tool is wrong.

## What remains true about safety

Nothing in the original safety analysis changes. wtff targets six paths and
never walks the disk, so the grant widens what it can see rather than what it
looks for. Safari, Mail, and Cookies remain outside its catalog entirely. SIP
is separate from Full Disk Access and continues to protect system trees. wtff's
own structural floor denies them independently, ninety five of this machine's
hundred and seven caches are excluded as Apple's, and clean stages by default.

The honest cost of the grant is that it attaches to the terminal application
rather than to wtff, because a command line program inherits its parent's
permissions, so everything run from that terminal receives it too.

## Verification

Full suite green. The corrected check reports a warning naming `~/.Trash` on
the development machine, which is the true state, and the mutation removing the
permission test fails the matching case.
