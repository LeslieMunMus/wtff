# 0014: brand palette applied, terminal logo removed

Status: done.

## Context

Second review round on the shell's appearance. Directives: remove the logo, since two terminal
renditions of it failed; apply the supplied brand palette, which the shell had not been using;
keep the input bottom-anchored with the space above it understood as a viewport that will hold
prompt and output history; use the bubbles spinner component for the activity glyph.

## What changed

The single brand palette replaces the dark and light pair: main color `#0A0AAE` on every
border, divider, heading, command name, and the prompt; body text `#3D3D3D`; secondary
`#BCBCFB` for hints; highlight `#E1E1FD` as the selected-row background; success `#0AAE0A`.
Danger was not specified; `#AE0A0A` follows the palette's own digit pattern and stands until
the palette's owner picks otherwise, as does amber for partial-outcome warnings. The specified
background `#F5F5F5` is honored by not painting one: filling every cell fights the terminal's
own profile, and the reporting terminal already runs that background. Recorded caveat: this is
a light-background palette, and body text will read poorly on a dark terminal until a dark
variant is specified by the palette's owner rather than improvised.

The logo is gone from the shell entirely. The welcome box's left column is text only. The
format answer, recorded: the source PNG was never the limitation, terminal cell resolution is;
what unblocks a faithful mark is either a small monochrome pixel grid of roughly thirty by ten
designed for half-block rendering, or approval to downsample the existing PNG into half-blocks
with a snapshot shown first.

The activity line's glyph is now the charmbracelet bubbles spinner component, MiniDot, in the
main color, replacing the hand-rolled frame cycle. Still one line: spinner, label, elapsed
timer.

## Verified, including one wrong verification

Suite green, vet and gofmt clean, one snapshot at a realistic size confirmed layout and
viewport spacing. A raw-byte check for the palette initially reported body text unstyled; the
truth was the check, not the theme: the color library rounds `#3D3D3D` to `60;60;60` in its
float conversion, one bit off and visually identical, and the check searched for the literal
`61;61;61`. The styled sequences are present around body text and were inspected directly.

## Deferred, awaiting go-ahead

The conversational viewport, where commands and their outputs accumulate as a scrollable
transcript above the prompt instead of the current screen-stack navigation, plus the
disclosure widget for expandable verbose activity logs, is a real interaction-model refactor.
Per the snapshot-first rule from 0013, it gets a rendered sketch for approval before it is
built. The telemetry counter beside the timer still needs deletion engine progress reporting,
the standing Opus-tier item.
