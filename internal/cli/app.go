package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/charmbracelet/lipgloss"

	terminalshell "github.com/lesliemusengi/wtff/internal/terminal-shell"
)

// detectDarkBackground asks the terminal whether it has a dark background.
//
// This is a direct call, deliberately not wrapped in a timeout. An earlier
// version raced lipgloss.HasDarkBackground against a short deadline,
// intending to bound the worst case; that protected nothing. Bubble Tea's
// own package init function, in bubbletea's own source, unconditionally
// calls this exact same detection against the real os.Stdin and os.Stdout
// before any code in this program runs at all, specifically so that Lip
// Gloss's result is cached before a Program acquires the terminal. By the
// time this function executes, the detection has already happened and its
// result is cached; racing it again here only re-reads that cache, and a
// timeout on a cache read protects against nothing real.
//
// The actual risk this looked like it was guarding against is real, just
// not fixable from here. Confirmed directly: running the compiled binary
// under a pseudo-terminal that never answers the query left the program
// producing no output for the full five seconds termenv allows before
// giving up, which happens during Go's init phase, before main runs, and
// cannot be intercepted, bounded, or canceled by anything downstream of
// Bubble Tea. Confirmed the other direction too: under a pseudo-terminal
// that answers correctly, the same binary renders its first full frame in
// under 25 milliseconds. The five second case is real for an environment
// with a pty but no terminal emulator behind it, such as some CI runners;
// it is not a risk for an actual interactive terminal session, which is
// what this program is built to run in. Bubble Tea's own comment on the
// workaround says it will be removed in v2; this note should be revisited
// then.
func detectDarkBackground() bool {
	return lipgloss.HasDarkBackground()
}

// Version is wtff's version string. There is no released version yet; this
// value exists so every build reports something rather than an empty string.
const Version = "0.1.0-dev"

// Run dispatches a command line invocation and returns the process exit code.
//
// Input and output are explicit parameters rather than the process globals so
// that every command can be exercised by a test without a real terminal, and
// so a future caller, such as the full-screen shell, can drive a command while
// capturing its output instead of letting it print directly.
func Run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		if isInteractive(stdin) {
			return runShell(stdin, stdout, stderr)
		}
		// A non-interactive session (piped input, a script, CI) has no way to
		// drive a full-screen program, and Bubble Tea's own attempt to enter
		// raw terminal mode against something that is not a terminal is not a
		// graceful failure to rely on. Usage text is the honest answer here,
		// the same one an unrecognized command gets.
		printUsage(stdout)
		return 2
	}

	switch args[0] {
	case "clean":
		return runClean(args[1:], stdin, stdout, stderr)
	case "remove":
		return runRemove(args[1:], stdin, stdout, stderr)
	case "uninstall":
		return runUninstall(args[1:], stdin, stdout, stderr)
	case "undo":
		return runUndo(args[1:], stdin, stdout, stderr)
	case "staged":
		return runStaged(args[1:], stdout, stderr)
	case "version", "--version", "-v":
		fmt.Fprintln(stdout, Version)
		return 0
	case "help", "--help", "-h":
		printUsage(stdout)
		return 0
	default:
		fmt.Fprintf(stderr, "wtff: unknown command %q\n\n", args[0])
		printUsage(stderr)
		return 2
	}
}

// runShell launches the full-screen interactive shell. It requires stdin to
// be a real terminal file, which the caller must already have checked, since
// there is no meaningful way to run a full-screen program against a pipe.
func runShell(stdin io.Reader, stdout, stderr io.Writer) int {
	deps, err := terminalshell.NewDeps()
	if err != nil {
		fmt.Fprintln(stderr, "wtff: cannot start:", err)
		return 1
	}
	defer deps.Close()

	if err := terminalshell.Run(deps, detectDarkBackground(), stdin, stdout); err != nil {
		fmt.Fprintln(stderr, "wtff:", err)
		return 1
	}
	return 0
}

func printUsage(w io.Writer) {
	fmt.Fprint(w, `wtff, a terminal-first macOS maintenance toolkit.

Usage:
  wtff clean [--dry-run] [--purge] [--yes]
      Find and remove common reclaimable cache directories: third party
      application caches, developer tool caches, and Trash contents. Staged
      by default. Skips anything covered by a protection rule, or that this
      machine simply does not have.

  wtff uninstall [--dry-run] [--purge] [--yes] <app name or bundle id>
      Remove an installed application and its leftover data, matched by an
      exact name or bundle identifier. Staged by default. Refuses Apple's
      own applications regardless of where they are installed.

  wtff remove [--dry-run] [--purge] [--yes] <path>...
      Remove one or more paths. Staged by default, which is reversible
      through "wtff undo"; pass --purge to remove permanently instead.

  wtff undo <batch-id>
      Restore every item from a staged batch to where it came from.
      Find a batch id with "wtff staged".

  wtff staged
      List batches currently held in the staging area.

  wtff version
      Print the version.

Every command is safe by default: nothing is removed without either an
interactive confirmation or --yes, and removal is reversible unless --purge
is given explicitly.
`)
}

// isInteractive reports whether r is a terminal, so a command can require
// explicit confirmation rather than prompting into a pipe that will never
// answer.
func isInteractive(r io.Reader) bool {
	file, ok := r.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
