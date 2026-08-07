# 0037: the duplicate detector

Status: detector done. The browsing, merge, and staging interface is not built
yet.

`internal/duplicate-scan` finds files that are byte for byte identical. It only
reads. What happens to a group is the caller's decision, and anything that
happens goes through the deletion engine with the same validation, protection
rules, and staging as every other command.

## Matching is conservative on purpose

Three passes, agreed with the project manager: group by exact size, then by a
hash of the first 64KB, then by a full SHA-256 of everything that survives.
Only the last one decides.

The cheap passes exist to avoid reading terabytes of media, not to reach a
verdict. Reporting two different files as identical would invite someone to
delete real data, and no amount of speed is worth that.

The case that separates a correct matcher from a fast one is two files sharing
a long prefix and differing at the end, which is what an edited video or a
re-exported document looks like. A prefix comparison calls those duplicates. A
test plants exactly that shape and requires them not to be grouped.

## Oldest first, because merge depends on it

Groups are ordered by modification time, oldest first. The oldest copy is
usually the one a person put somewhere deliberately; later copies tend to be
accidents of downloading or duplicating. The planned merge keeps the first
entry in place, so this ordering is load bearing rather than presentational,
and a test pins it.

Groups themselves are ordered by reclaimable space, largest first, since the
reason someone runs this is to find what is worth acting on.

## A floor on size

Files below 4KB are ignored by default, adjustable by the caller. Thousands of
identical small files are normal on any machine, would dominate the results,
and reclaim nothing worth reading through.

## Red team

Three guards were each broken and the matching test confirmed to fail: matching
on size alone, treating the prefix hash as proof, and ordering newest first.

**A fourth test passed for the wrong reason, and the record is corrected rather
than the test quietly strengthened.** Removing the explicit symlink skip left
`TestSymlinksAreNeverReportedAsDuplicates` passing. Removing the regular file
check as well still left it passing. The reason is that a symlink's own size is
the length of its target path, a few dozen bytes, so the minimum size floor
excludes it before either guard is consulted.

The skip is kept, because it states the intent and because three independent
things now have to fail before a link could be matched, but the test's comment
now says plainly that it does not prove what its name suggests. A separate test
was added for the hazard a symlink genuinely presents to a walk, a loop that
never ends, which is load bearing.

## Known limitations

Hard links are reported as duplicates of each other. They are byte identical by
definition, but removing one reclaims nothing, so the space figure overstates
for trees that use them.

Files are hashed serially. Parallel hashing would help on large media
collections and is deliberately left until the interface exists and the cost is
observable on a real run.

The full hash reads every byte of every candidate that survives the prefix
pass. On a directory of large near identical files this is genuinely slow, and
the five minute deadline exists to bound it.
