package cli

import (
	"flag"
	"fmt"
	"io"

	deletionengine "github.com/lesliemusengi/wtff/internal/deletion-engine"
)

func runStaged(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("staged", flag.ContinueOnError)
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil {
		return 2
	}

	root, err := deletionengine.DefaultStagingRoot()
	if err != nil {
		fmt.Fprintln(stderr, "wtff staged: cannot determine staging location:", err)
		return 1
	}
	staging, err := deletionengine.NewStagingArea(root)
	if err != nil {
		fmt.Fprintln(stderr, "wtff staged: cannot open staging area:", err)
		return 1
	}

	batches, err := staging.ListBatches()
	if err != nil {
		fmt.Fprintln(stderr, "wtff staged: cannot list batches:", err)
		return 1
	}
	if len(batches) == 0 {
		fmt.Fprintln(stdout, "nothing is staged")
		return 0
	}

	for _, batch := range batches {
		var total int64
		var partial bool
		for _, item := range batch.Items {
			if item.SizeKnown {
				total += item.SizeBytes
			} else {
				partial = true
			}
		}
		size := humanBytes(total)
		if partial {
			size += " (partial)"
		}
		fmt.Fprintf(stdout, "%s  %-8s  %d item(s)  %s  from %s\n",
			batch.BatchID, batch.Command, len(batch.Items), size,
			batch.CreatedAt.Local().Format("2006-01-02 15:04"))
	}
	return 0
}
