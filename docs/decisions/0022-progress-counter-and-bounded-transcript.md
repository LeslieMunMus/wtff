# 0022: a live progress figure, and a ceiling on history

Status: done.

## The counter

The activity line showed a spinner and elapsed time. Elapsed time says the
program is alive; it does not say the work is advancing, and those are
different questions when a scan takes minutes.

`Plan` and `Apply` now take an optional `Progress` hook, called once per
candidate or entry with how many have been handled and how many there are.

The hook is called from whatever goroutine is doing the work, which is never
the one Bubble Tea renders on. Sending a message per candidate would flood the
event loop with updates nobody can read at that rate, and writing model state
from the worker would be a data race. So the shell's counter holds two atomics:
the worker stores, and the spinner's own tick, which already fires several
times a second, loads. Updates coalesce to the frame rate for free, and the
engine keeps no opinion about how the number reaches a screen.

Nothing is shown until a total is known. A bare "0/0" during the discovery
phase, before the engine has candidates to count, would read as a stall at
exactly the moment the operation is working hardest. The figure is also clamped
to its total, because the engine reports the index before handling each item
and the last report is one short.

## The ceiling on history

The transcript only ever grew: an entry per command plus one per result, each
carrying its full path listing behind the disclosure toggle. A long session
leaked slowly and steadily.

History is now capped at five hundred entries, oldest dropped, because the
value of history falls off with age and the bottom is where a person is
looking. The number is a ceiling that prevents unbounded growth, not a
scrollback budget anyone should notice; a test pins that twenty ordinary
commands stay nowhere near it.

## Red team

**A test that could not verify its own claim.** Trimming copies into a fresh
array rather than reslicing in place, so dropped entries are released
immediately instead of at the next reallocation. A test asserted this by
watching capacity, and it passed with the copy removed: reslicing shrinks the
reported capacity too, so the two are not distinguishable that way. The
difference is real but modest and bounded, and no honest test could observe it,
so the test was deleted and the reason recorded in the code instead. A test
that cannot fail for the right reason is worse than none, because it is
trusted.

**A verification that proved nothing, and was not mistaken for proof.** The
first pty run of the counter captured no frames at all. Rather than assume
timing, a direct test confirmed the hook is called and receives the right
total. The counter was then seen on screen once the scan was made large enough
to outlast a render tick.

Five guards were each removed in turn and the matching test confirmed to fail:
the transcript cap, silence before a total, clamping to the total, and the
counter's presence on the activity line.

## Verification

Full suite green under the race detector, which is the check that matters for
the counter: a dedicated test hammers it from one goroutine while reading from
another, and anything but atomics fails it.

Against the installed binary through a pseudo terminal, a scan over forty cache
directories rendered `Scanning · 20/40 · 0s`.

## Known limitations

The counter counts candidates, not bytes, so forty small directories and forty
large ones advance at the same rate. Byte level progress would need the size
walk itself to report, which it currently cannot without giving up the
goroutine abandonment that bounds it.

An ordinary scan finishes faster than a render tick, so the counter often never
appears. That is the intended behavior rather than a defect: it exists for the
scans slow enough to make a person wonder, and those are the ones it shows up
for.
