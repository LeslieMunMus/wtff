# 0024: widening what gets tested

Status: done.

## Coverage first, guessing second

The gap was described as testing only one macOS version on one architecture.
Before writing any of that, coverage was measured, because a matrix that runs
the same weak tests on three machines is three times as much evidence for the
same thing.

Overall coverage was 75.7 percent, and the thin parts were exactly where they
matter: `removeTreeAt`, the recursive permanent removal primitive, at 56.7
percent, with `walkSize`, `restoreItem`, and `ListBatches` in the sixties.
Those are not missing features. They are error paths, and error paths in a
deletion tool are the interesting half.

## Adversarial filesystem cases

Seven tests now cover conditions a real machine produces and a temporary
directory does not, by itself:

Awkward filenames: spaces, single and double quotes, dollars, semicolons, glob
characters, accented characters, an emoji, a leading dash, trailing dots, and a
name a hundred and sixty characters long, all staged and then restored. Names
reach the kernel as bytes through `renameat` and never pass through a shell,
and this pins that nothing along the way starts interpreting them.

An unreadable subdirectory during removal, which must be reported rather than
passed over. Reporting success for a tree that was not actually emptied is how
a tool loses data it claims to have handled. The same case during measurement,
which must yield a floor rather than a total.

An already absent target, which is the desired state rather than a failure,
because cleanup races with the applications being cleaned up.

Eight concurrent staging runs against one staging area, which must produce
eight distinct batches. Batch identifiers carry a random suffix precisely
because a timestamp alone repeats inside a second, and nothing had tested that.

Junk in the staging area, which must be ignored rather than parsed.

A restore into a location something else has taken, which must leave the item
staged rather than overwrite. Undo destroying data it never removed is the
worst failure this package could have.

## What the awkward names found

One case failed: a filename containing tab characters was refused by path
validation. That is correct, and the test was wrong to expect otherwise.

A path with control characters printed to a terminal can emit escape
sequences. A filename can move the cursor, rewrite the line above it, or change
what a confirmation prompt appears to say, which matters most at the prompt
asking whether to delete permanently. Refusing costs the disk space of one
unusual file. Accepting costs the trustworthiness of every path wtff prints.

The behavior now has its own test asserting the refusal and its reason, rather
than being an unexplained absence from a list.

## The matrix, and what is honestly not covered

Runner labels were read from `actions/runner-images` rather than assumed.
`macos-latest` is macOS 26 on Apple silicon, `macos-15` is the previous
release, and `macos-14` is deprecated. The matrix runs both supported versions
with `fail-fast: false`, so one failing image does not hide the other, and each
job prints `sw_vers` and `uname -m` so the logs say what actually ran.

Intel images exist only as `-intel` and `-large` variants, which bill at a
higher rate. Real Intel runtime coverage is therefore a paid decision rather
than an oversight, and it is not enabled. Standing in for it is `make
cross-check`, which builds and vets the amd64 target from an Apple silicon
host, now part of `make check` so the break is caught locally rather than in
CI.

That substitution is honest about its limits: it catches build level breakage
from an architecture specific type or syscall signature, and it cannot catch a
runtime difference. Verified by planting an amd64 only compile error, which the
host build passed and `cross-check` failed on.

## Verification

Full suite green, race detector green, `cross-check` green. Coverage is 76.1
percent, with `removeTreeAt` at 66.7 and `walkSize` at 77.3.

## Known limitations

Still untested: a second user account, a case sensitive volume, a read only
volume, a network mount, and any macOS older than 15. The first two are
reachable with more fixture work; the rest need hardware or a paid runner.

Coverage percentage is a weak signal and is recorded here as a starting point
for finding gaps, not as a target to raise.
