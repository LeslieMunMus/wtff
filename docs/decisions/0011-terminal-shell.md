# 0011: full-screen terminal shell

Status: done.

## Context

Every command existed as a scriptable, non-interactive surface. This entry covers
`internal/terminal-shell`, the full-screen interactive interface locked in as a requirement back
in decision 0001, built now on top of a complete safety core and command set.

## What was built

A root model, `App`, holding a stack of screens and dispatching key events and resizes to
whichever is on top. Pushing a screen is how a flow moves forward; a screen returning a
different screen from its own `Update` is how a transient step, such as a loading screen,
replaces itself once it has something to show. `resetToMenuMsg` discards everything back to the
root menu, which a terminal state such as a results screen needs, since nothing beneath it on
the stack, a stale candidate list or a stale confirm screen, still describes anything real once
the operation it led to has finished.

One reusable selection widget, `selectList`, backs every screen that presents a checklist:
clean's candidates and uninstall's leftovers share it outright, and staged's batch list borrows
its rendering conventions. It owns cursor movement, toggling, scrolling, and the running
selected-size total; it has no dependency on the deletion engine at all, so a defect in
selection bookkeeping and a defect in what gets deleted are structurally different bugs in
different files.

Three flows: Clean goes straight from the menu into catalog discovery. Uninstall adds a search
step, a text field matched through `internal/uninstall-core`'s own exact-match `FindApp`, an
ambiguity screen when more than one application matches, and the same protection check the
non-interactive command uses before anything proceeds. Staged lists real batches and restores
one through `Undo`.

Every screen that proposes or removes something calls the same `internal/deletion-engine`,
`internal/clean-catalog`, `internal/protection-rules`, and `internal/uninstall-core` functions
the non-interactive commands call. A plan reviewed in the shell and a plan `wtff clean
--dry-run` would print come from the same code, not two independent descriptions of what clean
does.

The interactive shell stages only. `--purge` exists on the scriptable commands and not here;
building the same "type the word to confirm" friction the CLI's purge path already has, inside
a full-screen text flow, is real work with no clean shortcut, and reversible-by-default is
already the correct thing to ship first.

## Decisions worth recording

**Bare `wtff` launches the shell, only when stdin is a real terminal.** A non-interactive
session, piped input, a script, CI, gets usage text instead, the same answer an unrecognized
command gets. Attempting to run a full-screen program against something that cannot answer a
terminal query is not a graceful failure to rely on, which the rest of this entry demonstrates
directly.

**A filtered manifest is resealed independently, not sliced from the original.** Choosing a
subset of a plan's entries for confirmation cannot reuse the original manifest's digest, since
the digest covers exact contents. The subset is built fresh and reasoned about separately in
`docs/architecture/deletion-engine-design.md`'s terms: the entries themselves were already
validated once by `Plan`, and `Apply` revalidates identity and policy again regardless, so
narrowing the set here can only remove entries a person did not select, never add one that was
not already independently justified.

## Two defects found while testing this stage, neither found by writing tests first

**`candidateListScreen`'s Esc handling assumed one specific position on the stack.** The first
version reset straight to the menu on Esc, which is correct when the screen sits directly above
the menu, true for clean, but wrong once the same screen is reused for uninstall's leftover
step, where it sits above a search screen. Resetting there would discard the search screen
Esc should have returned to. Found while wiring the second flow that reuses the first flow's
screen, before it ever ran, by noticing the assumption rather than by a failing test. Fixed to
pop one level, which is correct regardless of what is beneath it.

**A test built to prove isolation staged a real file into this machine's real staging area.**
`internal/deletion-engine`'s own `DefaultStagingRoot` and `DefaultPath`, which every apply and
every log write in this project goes through, always resolve against the real process
environment by design, the same way every command in `internal/cli` already relies on. A test
helper set a `Deps.Home` field for planning and protection matching but never set the `HOME`
environment variable those two functions actually read, which every other test file in this
project already does correctly. Running the suite staged a real file, from a temporary directory
that no longer existed by the time anyone looked, into the real
`~/Library/Application Support/wtff/staging`, found by running `wtff staged` against the real
machine afterward rather than assuming a green test suite meant nothing had happened elsewhere.
Removed by hand, confirmed clean, and the test helper fixed to set both.

## A real, upstream, currently unfixable finding, and an incomplete first attempt at fixing it

Testing the compiled binary under a pseudo-terminal that never answered a terminal capability
query showed the shell producing no output for a full five seconds, which looks identical to a
hang. The first fix wrapped the theme detection call in a goroutine raced against a two hundred
millisecond timeout, on the theory that abandoning a slow query would let the program proceed.
Rerunning the same pseudo-terminal test afterward showed no improvement at all: still five
seconds, still nothing rendered.

The actual cause is in Bubble Tea's own source, not this project's: `bubbletea` v1.3.10 declares
a package `init` function that unconditionally calls the same background detection, against the
real `os.Stdin` and `os.Stdout`, before any of wtff's own code runs, with a comment from its own
maintainers explaining why and stating the workaround will be removed in a future major version.
Go runs every imported package's `init` before `main`, in dependency order, which means nothing
downstream of `bubbletea` can run before it, race it, or cancel it. The first fix raced a call
that, by the time it ran, was only ever going to read an already-cached result or join a wait
that had already started; bounding a cache read protects against nothing. That fix was removed
rather than left in place implying a protection it did not provide.

Confirmed both directions rather than reasoned about in the abstract. Under a pseudo-terminal
that never answers, the delay is real and matches termenv's own five second `OSCTimeout`. Under
one built to answer both queries correctly, the same binary renders its complete first frame in
under twenty five milliseconds. The five second case is real for an environment with a pty but
no terminal emulator behind it, such as some CI configurations; it is not a risk for the actual
interactive terminal session this program is built to run in, which is the only environment this
matters for in practice.

## Verification

The full project suite, now including twenty nine tests in `internal/terminal-shell` and one in
`internal/cli` for the detection finding, passes with `go vet` and `gofmt` clean.

Screen logic is tested by driving `Update` directly with synthetic messages, including the two
pass sequence Bubble Tea's own runtime actually uses: a key press returns a command, running that
command produces a message, and only a second `Update` call fed that message contains the logic
that acts on it. An early version of the plan-flow test called `Update` once and asserted on the
result of that single call, which does not match how the real program is driven; it was rewritten
after checking `Bubble Tea`'s own `compactCmds` behavior to confirm what a single non-nil command
in a `Sequence` actually returns, rather than assuming.

Beyond unit tests, the compiled binary was driven through a real pseudo-terminal repeatedly:
menu navigation with arrow keys and Enter, opening and backing out of the uninstall search
screen, and a complete run of Clean's real discovery flow against this actual machine, toggling
a selection and aborting with Esc, with the real staging area checked before and after and
confirmed unchanged. A separate run timed `wtff clean --dry-run` directly for a fair comparison
after the interactive scan appeared slow; both take several seconds against this machine's real
cache volume, which is realistic filesystem work, not a defect introduced by the shell.

## Known limitations

No purge path in the shell, by design, not yet built. No number-key or type-ahead shortcuts in
list navigation. Terminal resize mid-session updates rendering on the next frame; a screen's own
cached height, used for scroll math, can be one frame stale immediately after a resize, which is
self-correcting and has not been treated as worth chasing further. The five second worst case
tied to Bubble Tea's own startup query remains open on this project's side; there is nothing to
fix here until an upstream release changes the underlying behavior.

## How this could improve with time

Revisit the Bubble Tea startup query note when v2 ships, since its own maintainers state the
workaround is temporary. Add category or per-item filtering to the candidate list once a real
catalog grows large enough that reviewing everything at once stops being the common case, the
same open item decision 0009 already named for the non-interactive `clean` command. Consider
whether a bounded, explicit-confirmation purge belongs in the shell once the staging-only
version has seen real use.
