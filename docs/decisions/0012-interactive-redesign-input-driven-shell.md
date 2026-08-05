# 0012: input driven shell, welcome panel, animated activity indicator

Status: done for the UI-tier work. The deletion engine timeout and live progress reporting
this entry's own findings point at are deliberately deferred, by explicit agreement, to a
separate entry on the Opus tier.

## Context

Real use surfaced a stuck `clean` run: over two hours with no progress, no indication whether
anything was happening, and an apparent inability to quit. That incident prompted a broader
redesign: an input driven command interface replacing the arrow key menu, a welcome panel, an
animated activity indicator built from wtff's own mark, and an elapsed timer. The user chose to
do the UI tier of this work first and defer the deletion engine core fix, which the incident
also revealed the need for, to its own entry once the model switches to Opus.

## Diagnosing the incident before redesigning around it

Before writing any code, the actual stuck process was inspected rather than assumed. `ps`
showed seven seconds of CPU time against roughly two hours of wall clock, meaning it was
blocked, not working. `lsof` and the operation log showed the last recorded activity was a
`"planned"` entry during size measurement, under `~/Library/Caches`, with zero staged items
anywhere: the run never reached `Apply` at all. `internal/deletion-engine`'s size measurement
walks a candidate's full directory tree with no time bound, only an entry count cap, and one
slow or blocked filesystem call, a cloud sync placeholder, a stale network mount, is enough to
freeze the entire operation indefinitely with no recourse. This is the real defect the incident
exposed, and it is deletion engine work, not shell work, which is why it is deferred rather than
patched around here.

## What was built

`internal/terminal-shell/home.go` replaces the arrow key menu entirely with a typed command
prompt. `internal/terminal-shell/logo.go` renders wtff's own mark, approximated on a fixed
character grid, animating between a spread and a near center frame. `internal/terminal-shell/activity.go`
is the shared "something is happening" widget: the animated mark plus a live elapsed timer,
replacing the static `"Scanning…"` and `"Working…"` strings across every screen that waits on a
filesystem operation.

## Decisions worth recording

**Command matching is exact and case sensitive, deliberately, with no fuzzy or partial
resolution.** Typing `Clean` for `clean` is refused with a clear error, not silently corrected.
This was an explicit requirement, reasoned the same way this project already reasons about
uninstall's exact bundle identifier matching: a person who typed something is understood to mean
exactly what they typed, and a tool that quietly forgives a typo is a tool that sometimes acts
on input the person did not actually write.

**A `/` prefix opens a browsable palette over the exact same fixed command list, and prefix
filtering there is not an exception to the exact match rule.** The palette narrows a fully
enumerated, finite set of real commands as a person types; running one still requires an
explicit Enter on one specific highlighted entry, never a resolution of ambiguous partial text.
Direct typed input outside the palette still requires the full exact word.

**The activity indicator's animation ticks on its own independent command, never on the
operation it is displayed beside.** `planDiscoveringScreen` and `applyingScreen` batch two
commands in `Init`: the real filesystem work, and the indicator's own tick loop. If the
indicator depended on progress signals from the operation itself, it would freeze at exactly the
moment freezing matters most, which is the same failure the static string already had. This
also means the indicator cannot yet show real progress, only that time is passing; a genuine
progress figure needs the operation itself to report partial results, which is deletion engine
work and stays out of this entry.

**The mark is drawn in a fixed brand color, `#0A0AAE`, independent of the light or dark theme
used everywhere else.** A logo that shifted with the terminal's background would just be another
themed element, not a mark.

## Two mistakes caught while building this, both before they shipped

**A command entry with no handler.** `homeCommands` for `staged` was written without its
`activate` function, which `dispatch` would have called as a nil function pointer, panicking the
whole program the first time someone typed the word. Caught by review immediately after writing
it, before it was ever built. A second, permanent guard was added in `dispatch` regardless,
refusing cleanly with an error message rather than trusting review to catch every future
instance of the same mistake: cheap insurance is worth adding even after review already caught
the fault it seemed to have found through, since it is the fault, not this one instance of
review, that recurring bugs come from.

**A batching change broke the existing plan flow tests' assumption about `Init`'s return
value.** Adding the activity indicator's tick command alongside each screen's real work means
`Init` now returns `tea.Batch(realWork, tick)` rather than the real work's command directly.
Calling that returns `tea.BatchMsg`, a slice of the original commands, not the real work's own
message. The existing tests, which called `Init()` and asserted directly on the result, failed
immediately, correctly, the moment this was built, and were fixed with a helper that unwraps a
batch and returns whichever sub-command produces the message a test actually wants. This is the
same category of mistake decision 0011 records for `tea.Sequence`: assuming a composition
helper's return shape instead of checking it.

## A real Ctrl+C investigation that reversed itself, and the actual finding underneath it

Ctrl+C during an active scan appeared not to work in manual testing, matching the original
incident report exactly. That led deep into Bubble Tea's own shutdown source: its
`handleCommands` dispatcher explicitly does not wait on individual command goroutines during
shutdown, its own comment states a blocked command's goroutine is deliberately leaked rather
than waited on, and `QuitMsg` and `InterruptMsg` are both intercepted by the core event loop
before ever reaching this project's own `Update`, returning from the loop immediately in either
case. None of that explained an observed multi-second stall, so the actual running binary was
instrumented directly with timestamps rather than reasoning about the library further in the
abstract. The instrumentation showed Ctrl+C handled and `Run` returned cleanly in eleven
milliseconds, yet a naive `kill(pid, 0)` liveness check five seconds later still reported the
process alive.

The contradiction was in the test harness, not the program: the pseudo terminal test scripts
used throughout this project's manual verification never reaped the child process, so an
already exited process remained a zombie, and `kill(pid, 0)` reports a zombie as alive because
its process table entry has not yet been removed. Rerun with `waitpid`, which distinguishes a
truly exited process from an unreaped one, Ctrl+C during an active real scan against this
machine exits the process in under a second. Confirmed, not assumed: this reverses the finding
that opened this investigation. Ctrl+C works correctly today. The instrumentation and the
uncorrected test methodology are both recorded here because reaching a wrong conclusion by an
uninstrumented pty test is a mistake worth being able to recognize a second time faster than the
first, not because the conclusion itself needed shipping code to fix.

The original incident's report that Ctrl+C did not respond remains only partially explained by
this. It may have been this same zombie-detection artifact observed by a person rather than a
script, in which case the program may have already exited while appearing not to; it may have
been specific to conditions this project's pty based testing cannot reproduce. What is settled,
by direct instrumentation rather than inference, is that the shutdown path itself is not at
fault today.

## Verification

Fifty two tests in `internal/terminal-shell` alone, up from twenty nine before this entry, full
project suite green, `go vet` and `gofmt` clean.

The compiled binary was driven through a real pseudo terminal repeatedly, with each round
correcting a flaw found in the previous one rather than trusting the first result: a rendering
artifact in an early palette check turned out to be the crude ANSI stripping script used to
visualize output, not the program, confirmed by checking `filterCommands` directly instead;
timing measurements during a real scan against this actual machine were treated as suspicious
until cross checked against the operation log and a direct timing of the equivalent
non-interactive command, which is what caught that both take comparably long against this
machine's real cache volume and neither is a shell specific regression; and the Ctrl+C liveness
check itself was wrong until reaping was added, described above.

## Known limitations

The activity indicator shows elapsed time, not progress; a real "so far of so far discovered"
figure needs the deletion engine to report partial results as it works, not only a final one,
which is the deferred core fix. There is still no way to cancel a specific in-flight operation
and return to the shell; Ctrl+C now confirmed to exit the whole program promptly is not the same
thing as an Esc that gracefully abandons one scan and returns to the menu, and building that
meaningfully still depends on the same deletion engine cancellation support. The logo's terminal
rendering is a deliberate approximation, block glyphs standing in for the source mark's
continuous curves, not a reproduction of it.

## How this could improve with time

The deletion engine entry this one points at twice needs to add a bounded, cancellable walk to
size measurement and path resolution, and a progress channel or callback Apply and Plan can
report through; once that exists, the activity indicator built here can show real counts instead
of only elapsed time, and a graceful per-operation cancel becomes possible for the first time.
Extend the pty based manual verification harness this entry found real bugs in with a proper
reap-on-exit helper by default, so the zombie detection mistake found here cannot recur silently
in a future session's ad hoc test script.
