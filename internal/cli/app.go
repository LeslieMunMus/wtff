package cli

import (
	"fmt"
	"io"
	"os"
)

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
