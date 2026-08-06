# 0019: license, continuous integration, and a check that could not fail

Status: done.

## Scrollbar refinement

The first draggable bar was two columns of block glyph. On a real screen it
read as a dashed line rather than a rail, because a block glyph depends on the
font filling its cell to the full line height and many monospace fonts leave a
gap between rows.

It is now painted as a background color on blank cells, which fills the whole
cell in every font, and is back to a single column to match the system bar it
is modelled on. The grabbable region stays wider than the drawn bar, covering
the transcript's right padding, so a slim bar is still easy to catch.

The wheel moved three lines per event, the component's default. macOS sends a
stream of events for one trackpad gesture, so three lines each read as jumps.
It is one line per event now.

**A terminal program cannot change the pointer's shape.** That is the terminal
emulator's business, and no escape sequence hands it over, so the bar cannot
show a grab cursor on hover the way a window system control would. The
compensation is the wider hit region rather than a cue that cannot be drawn.

## License

MIT, in `LICENSE`, with the README's license section pointing at it and
restating the provenance position: architectural ideas and documented facts
about macOS are not copyrightable, specific expression including a curated
protection list as a compilation is, and every rule and catalog entry here
carries a provenance field naming its primary source so the claim can be
checked rather than trusted.

## Continuous integration

One workflow, `.github/workflows/check.yml`, running `make check`, `make race`,
and a build that runs `wtff version`.

It runs on macOS rather than the cheaper Linux runner. wtff uses descriptor
relative syscalls and resolves real system paths, so a Linux runner would
report a green build for behavior this project never executes. It also fetches
full history, because the Makefile stamps the version from `git describe` and a
shallow clone has nothing to describe.

## Red team: the check that could not fail

The `dash-scan` target added in the previous change was a silent no-op, and it
reported clean while examining nothing.

It called `grep -P`, a GNU extension that BSD grep does not have. On macOS grep
exited with an error, the shell's `!` negation turned that error into success,
and the target passed. The previous entry's claim that the dash scan was clean
was therefore unfounded, and would have been equally unfounded in CI.

This is the project's own recurring failure mode in a new place: a check whose
name asserts a property it does not verify. It is worse than no check, because
a missing check is visibly missing and this one was trusted.

The scan is now a Go test. Go is already a dependency and behaves the same on
every machine, and, most importantly, the scanner can itself be tested: one
case feeds it a known em dash and a known en dash and requires it to find them,
and ordinary hyphens and commas and requires it not to. A detector that matched
nothing would fail that test rather than silently bless the tree.

Verified in both directions: with a planted em dash in `docs/`, `make
dash-scan` fails; with the tree clean it passes. Its first real run also caught
the two literal dashes in its own constant declarations, which are now written
as code point escapes so the file does not trip the rule it enforces.

## Verification

`make check`, `make race`, and the build all pass locally, which is the exact
sequence CI runs. The scrollbar was confirmed against the installed binary
through a pseudo terminal: the thumb paints as background `48;2;10;10;174` and
the track as `48;2;225;225;253`, no block glyph remains anywhere in the output,
and a press two columns to the left of the drawn bar still drags it.

## Known limitations

CI does not yet test on Intel, on older macOS versions, or against a second
user account. The workflow has never run on GitHub, since the repository has no
remote, so its first push is its first real execution.
