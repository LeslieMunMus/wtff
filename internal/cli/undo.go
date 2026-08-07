package cli

import (
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"regexp"

	deletionengine "github.com/lesliemunmus/wtff/internal/deletion-engine"
	operationlog "github.com/lesliemunmus/wtff/internal/operation-log"
)

// batchIDPattern matches exactly what StagingArea generates: a UTC timestamp
// and a twelve character hex suffix, joined by a hyphen. Anything else is
// refused, which is what keeps a path separator or a parent reference from
// ever reaching a filesystem call built from this value.
var batchIDPattern = regexp.MustCompile(`^[0-9]{8}-[0-9]{6}-[0-9a-f]{12}$`)

func validateBatchID(id string) error {
	if !batchIDPattern.MatchString(id) {
		return fmt.Errorf("%q is not a valid batch id, run wtff staged to list real ones", id)
	}
	return nil
}

func runUndo(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("undo", flag.ContinueOnError)
	fs.SetOutput(stderr)
	if err := fs.Parse(reorderFlagsFirst(args, commonBoolFlags)); err != nil {
		return 2
	}
	remaining := fs.Args()
	if len(remaining) != 1 {
		fmt.Fprintln(stderr, "wtff undo: exactly one batch id is required, see wtff staged")
		return 2
	}
	batchID := remaining[0]

	// The id came from the command line, which makes it untrusted input, not
	// the trusted directory name it resembles. A value such as "../../etc"
	// would join outside the staging root entirely. Batch ids are generated
	// only as a timestamp and a hex suffix, so anything containing a path
	// separator or resolving to a different name than it started as is
	// rejected before it reaches a filesystem call.
	if err := validateBatchID(batchID); err != nil {
		fmt.Fprintln(stderr, "wtff undo:", err)
		return 2
	}

	root, err := deletionengine.DefaultStagingRoot()
	if err != nil {
		fmt.Fprintln(stderr, "wtff undo: cannot determine staging location:", err)
		return 1
	}

	batch, err := deletionengine.LoadBatch(filepath.Join(root, batchID))
	if err != nil {
		fmt.Fprintln(stderr, "wtff undo: cannot find that batch, run wtff staged to list what is available:", err)
		return 1
	}
	if len(batch.Items) == 0 {
		fmt.Fprintln(stdout, "that batch has nothing left to restore")
		return 0
	}

	logPath, err := operationlog.DefaultPath()
	if err != nil {
		fmt.Fprintln(stderr, "wtff undo: cannot determine log location:", err)
		return 1
	}
	log, err := operationlog.Open(logPath, "undo")
	if err != nil {
		fmt.Fprintln(stderr, "wtff undo: cannot open operation log:", err)
		return 1
	}
	defer log.Close()

	result, err := deletionengine.Undo(batch, log)
	if err != nil {
		fmt.Fprintln(stderr, "wtff undo: failed:", err)
		return 1
	}

	fmt.Fprintf(stdout, "restored %d item(s)\n", result.RestoredCount)
	if result.SkippedCount > 0 {
		fmt.Fprintf(stdout, "left %d item(s) in staging:\n", result.SkippedCount)
		for _, outcome := range result.Outcomes {
			if !outcome.Restored && outcome.Err == nil {
				fmt.Fprintf(stdout, "  %s: %s\n", outcome.Item.OriginalPath, outcome.Reason)
			}
		}
	}
	if result.FailedCount > 0 {
		fmt.Fprintf(stdout, "failed to restore %d item(s):\n", result.FailedCount)
		for _, outcome := range result.Outcomes {
			if outcome.Err != nil {
				fmt.Fprintf(stdout, "  %s: %v\n", outcome.Item.OriginalPath, outcome.Err)
			}
		}
		return 1
	}
	return 0
}
