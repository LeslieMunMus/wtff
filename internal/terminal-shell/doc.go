// Package terminalshell is wtff's full-screen interactive interface, run in
// the terminal's alternate screen buffer rather than as a scroll of printed
// lines.
//
// This package holds no deletion logic of its own, the same discipline
// internal/cli follows. Every screen that proposes or removes something
// calls into internal/deletion-engine, internal/clean-catalog,
// internal/protection-rules, or internal/uninstall-core exactly as the
// non-interactive commands do; the plan a person reviews on screen here and
// the plan wtff clean --dry-run would print are produced by the same code.
//
// # Structure
//
// A root model, App, owns a stack of screens and dispatches key events and
// window resizes to whichever screen is on top. Pushing a screen is how a
// flow moves forward, for example from the main menu into the clean
// candidate list; popping is how Esc goes back. The stack, not a single
// mutable "current step" field, is what makes going back always land
// exactly where the person came from, including back through a multi-step
// flow like search, then matches, then leftovers.
//
// Screens that present a list of removable items share one selection widget
// rather than each reimplementing checkbox state, scrolling, and size
// totals independently. Clean, uninstall's leftover step, and staged all
// build on it.
package terminalshell
