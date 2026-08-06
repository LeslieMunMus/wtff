package cli

import (
	"flag"
	"fmt"
	"io"

	"github.com/lesliemusengi/wtff/internal/diagnostics"
)

// runDoctor inspects wtff's own state and the environment it runs in.
//
// It exits non-zero when something needs a decision, so it can be run from a
// script or a scheduled job that only wants to hear about problems. Everything
// checked is printed regardless, because a diagnostic that only speaks when
// unhappy leaves a person unsure whether it looked at all.
func runDoctor(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(stderr)
	quiet := fs.Bool("quiet", false, "print only what needs attention")
	if err := fs.Parse(reorderFlagsFirst(args, commonBoolFlags)); err != nil {
		return 2
	}
	if len(fs.Args()) > 0 {
		fmt.Fprintln(stderr, "wtff doctor: no positional arguments are accepted")
		return 2
	}

	opts, err := diagnostics.DefaultOptions()
	if err != nil {
		fmt.Fprintln(stderr, "wtff doctor:", err)
		return 1
	}

	report := diagnostics.Run(opts)
	for _, finding := range report.Findings {
		if *quiet && finding.Level != diagnostics.LevelWarn {
			continue
		}
		fmt.Fprintf(stdout, "%-5s %-18s %s\n",
			marker(finding.Level), finding.Area, finding.Summary)
		for _, line := range finding.Detail {
			fmt.Fprintf(stdout, "                         %s\n", line)
		}
	}

	if report.NeedsAttention() {
		fmt.Fprintln(stdout, "\nsomething above needs a decision")
		return 1
	}
	if !*quiet {
		fmt.Fprintln(stdout, "\nnothing needs attention")
	}
	return 0
}

func marker(level diagnostics.Level) string {
	switch level {
	case diagnostics.LevelWarn:
		return "warn"
	case diagnostics.LevelNote:
		return "note"
	default:
		return "ok"
	}
}
