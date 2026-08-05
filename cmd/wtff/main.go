// Command wtff is the entrypoint binary. It parses no flags itself; that is
// internal/cli's job. This file only wires the process's real stdin, stdout,
// stderr, and exit code to internal/cli.Run.
package main

import (
	"os"

	"github.com/lesliemusengi/wtff/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
