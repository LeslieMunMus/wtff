# Terminal interface

Status: requirement locked, implementation not started.

## Requirement

wtff presents as a full-screen terminal application, in the style of Claude Code's own CLI,
not as a scrolling sequence of printed lines. This applies to the interactive parts of `clean`
and `uninstall` (selection screens, previews, confirmation) and later to the analyzer and
status dashboard when those phases begin.

## Why this is being written down now

This requirement was stated explicitly as something that matters, ahead of the deletion engine
work that will happen first. The terminal shell is not built until later in the plan, but the
architectural decision needs to be locked in now so that the deletion engine's output is
designed to be consumed by a full-screen renderer from the start, rather than retrofitted onto
one later. A command that only ever prints lines to standard output is harder to wrap in a
full-screen shell than one that was designed with a rendering boundary from the beginning.

## Intended approach

Bubble Tea, run in alternate-screen mode, is the working assumption for the rendering layer.
This is a standard, well-supported pattern for full-screen terminal applications in Go and is
the same general approach used by other full-screen terminal tools in this space. This is a
starting assumption, not a final decision, and should be revisited once `internal/cli` and
`internal/terminal-shell` are actually being built.

## Design input

`theme-colours/theme.png`, at the project root, is a reference screenshot of a theme
configuration panel: light and dark variants, accent color, background, foreground, a
translucent sidebar toggle, and a contrast slider. This is design input for how wtff's own
theme should be structured (light, dark, and system-following variants, with an accent color
and a contrast setting), not a visual style to copy wholesale. Terminal color palettes are
constrained differently than a GUI app's, since they have to remain legible across whatever
terminal emulator and color scheme the user already has configured, and that constraint will
shape the actual palette more than the reference image does.

## Open questions for the terminal-shell phase

- Whether theme selection is a runtime flag, a config file setting, or both.
- Whether wtff should detect and adapt to the terminal's existing light or dark background,
  the way well-behaved terminal applications do, rather than assuming one.
- How the full-screen shell degrades when run non-interactively (piped output, CI, scripting),
  since `mo status --json` style automation output is a real requirement for a tool like this
  and a full-screen renderer must not be the only way to get data out of it.

## How this could improve with time

Once a first full-screen prototype exists, get it in front of real terminal emulators
(Terminal.app, iTerm2, Ghostty, Alacritty, kitty) before locking the rendering approach further,
since terminal capability differences are a common source of a full-screen TUI looking correct
in development and broken for some users.
