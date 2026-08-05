// Package cli implements wtff's non-interactive command surface: the commands
// that run to completion and print a result, as opposed to the full-screen
// interactive shell in internal/terminal-shell.
//
// This package owns no deletion logic. Every command here is a thin translation
// from command line arguments into a call against internal/deletion-engine,
// internal/protection-rules, and internal/operation-log. That is deliberate:
// the single funnel the deletion engine promises only holds if every entry
// point, including this one, actually goes through it rather than reimplementing
// a shortcut.
//
// Every command function takes its input and output as explicit parameters
// (an io.Reader for input, io.Writer for output and for errors) rather than
// reading os.Stdin and writing os.Stdout directly. That is what makes the
// commands testable without a real terminal, and it is also what will let the
// full-screen shell drive these same commands as library calls later, rather
// than shelling out to itself.
package cli
