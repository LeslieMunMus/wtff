# 0018: a grabbable scrollbar, and a real install

Status: done.

## The scrollbar

One column read as a hairline rather than a control, and a pointer target one
cell wide is easy to miss. The bar is now two columns of solid block, thumb in
the main color over a track in the highlight color, and it can be grabbed and
dragged.

Rendering and dragging both go through one `scrollbarThumb` function. They are
inverses of each other, and two separate implementations of the same arithmetic
drift, which a person feels immediately as a thumb that does not stay where
they left it.

Pressing the thumb grabs it at the point of contact rather than snapping its
top to the cursor, so a grab near the thumb's bottom does not jump the view on
the first movement. Pressing empty track jumps the thumb there and grabs it at
its middle, matching the system scrollbar this is modelled on. Dragging works
while a flow runs, the same as the wheel and the page keys.

The wheel arrives as a press event too, so what separates a grab from a scroll
is the button rather than the action alone.

## A rounding bug the tests found

The inverse mapping truncated. `scrollbarThumb` divides down, so each thumb row
stands for a band of offsets, and truncating the inverse landed on the largest
offset of the band below. The thumb rendered one row above where it was
dropped, and crept upward on every drag.

The fix is ceiling division: the smallest offset that renders at a given row.
This was found by a round trip property test, not by reading the code, and it
is pinned by one that drops the thumb on every row and checks where it
re-renders.

## Running wtff without the long command

The proposed approach was a shell script in `~/bin`. Three things made it the
wrong fit here, two of them specific to this machine.

**It dropped arguments.** The script ran a fixed command with no `"$@"`, so
`wtff clean --dry-run` would have silently ignored everything after the name.
That amputates the entire command line interface.

**It edited the wrong file.** The instructions used `~/.bash_profile` and
`~/.bashrc`. This machine's shell is zsh, as macOS has defaulted to since
Catalina, and an interactive zsh reads `~/.zshrc`. The `PATH` would appear to
work in the session where it was sourced and be gone from the next window.

**It rebuilt on every launch**, which turns any compile error into a program
that will not start.

Go already answers this. `go install` produces a real binary on the `PATH` that
takes arguments and does not rebuild. A `Makefile` now wraps it, along with the
project's own verification, so `make check` is one command instead of four.

A related trap, caught while wiring it: the version was a `const`, and the
linker's `-X` flag can only write to a variable. Against a constant it fails
silently, so every build would have reported the same version while appearing
to work. It is a `var` now.

The `PATH` line uses a literal `$HOME/go/bin` rather than `$(go env GOPATH)`,
because the latter spawns a process on every shell start.

## Verification

Full suite green, `make check` clean. Verified against the installed binary
through a pseudo terminal: the bar renders two columns wide with the thumb in
`10;10;174` and the track in `225;225;253`, and synthetic SGR mouse events that
press the thumb low and drag it to the top bring the welcome box back into
view, which is the drag working end to end.

The install was verified in an interactive zsh, since `zsh -l -c` is
non-interactive and never reads `~/.zshrc`; checking it the wrong way reports a
failure that a real terminal would not have.

Guards verified by mutation: truncating the inverse reintroduces the creep and
fails its test, and narrowing the bar to one column fails the thickness test.

## Known limitations

The thumb has square ends. Rounded caps would need half block glyphs and read
worse at small sizes. There is no fade or auto hide; the bar is always present
once content overflows.
