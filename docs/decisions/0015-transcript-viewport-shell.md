# 0015: transcript viewport replaces the screen stack

Status: done.

## Context

Running a command replaced the entire view: the welcome box, the prompt, and all
history vanished, and the person landed on a separate screen showing one result.
That was the screen-stack model built into the first shell, working as built and
wrong against what was asked for. The project manager approved a sketch of the
intended model before any code changed, per the rule from decision 0013.

## The model

There is no screen stack. `App` owns a transcript of entries rendered into a
scrollable viewport, plus at most one interactive `liveBlock` pinned above the
prompt while a flow runs. The prompt band is anchored at the bottom and never
moves. Typing a command echoes it into the transcript and attaches its output
beneath, so history accumulates the way a message log does.

A flow advances by returning `flowMsg`, which carries transcript entries and
optionally a replacement block in one atomic step; a nil block means the flow
finished. Ordering cannot race, and a test can drive a flow exactly as the
runtime does.

Interactive steps render as a titled box above the prompt while live, then
collapse into a single transcript line once resolved, with the full listing
behind a disclosure toggle on ctrl+o. That is the approved treatment for
clean's selection, uninstall's disambiguation, and staged's batch picker,
which now all share two generic blocks rather than owning separate screens.

## Decisions worth recording

**The welcome box is the first transcript entry, not fixed chrome.** It scrolls
away as history accumulates rather than permanently occupying the viewport.

**Staging is not double-confirmed.** The selection box stages on Enter. Staging
is reversible by design, so a separate confirmation screen was ceremony rather
than safety. Permanent removal remains CLI-only behind `--purge`.

**Scrollback works during a flow.** PgUp and PgDn reach the viewport even while
a block is live, so a person can read earlier output while something runs.
Everything else goes to the block, which owns the keyboard as the interactive
step.

**Only uninstall takes an argument.** Trailing text on any other command is
refused rather than silently ignored, keeping exact matching honest per token.

## Verification

Thirty eight tests in this package, full project suite green, vet and gofmt
clean. Beyond unit tests, the compiled binary was driven through a pseudo
terminal: `staged`, `help`, and a wrong-case `Clean` each echoed and answered
into the transcript with the prompt in place; a real `clean` against the
developer machine showed a live spinner, then eighty six candidates in a pinned
selection box with the prompt still anchored, then `esc` collapsing to
`✗ clean cancelled` in history; and an isolated-HOME run staged a file, showed
the collapsed `▸ details` line, and expanded it on ctrl+o.

## Known limitations

The transcript is unbounded; a very long session grows memory. The activity
line still shows elapsed time only, since a live progress figure needs the
deletion engine to report partial results, which remains the deferred core fix
along with the unbounded size measurement that can hang a scan.
