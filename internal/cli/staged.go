package cli

import (
	"flag"
	"fmt"
	"io"

	deletionengine "github.com/lesliemunmus/wtff/internal/deletion-engine"
	operationlog "github.com/lesliemunmus/wtff/internal/operation-log"
)

func runStaged(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("staged", flag.ContinueOnError)
	fs.SetOutput(stderr)
	purge := fs.Bool("purge", false, "permanently delete staged batches instead of listing them")
	all := fs.Bool("all", false, "with --purge, delete every staged batch")
	yes := fs.Bool("yes", false, "proceed without an interactive confirmation")
	jsonOut := fs.Bool("json", false, "emit one JSON document instead of human readable output")
	if err := fs.Parse(reorderFlagsFirst(args, commonBoolFlags)); err != nil {
		return 2
	}

	positional := fs.Args()
	if !*purge {
		if len(positional) > 0 {
			fmt.Fprintln(stderr, "wtff staged: no positional arguments are accepted without --purge")
			return 2
		}
		if *all {
			fmt.Fprintln(stderr, "wtff staged: --all is only meaningful with --purge")
			return 2
		}
	}

	staging, code := openStagingArea("staged", stderr)
	if staging == nil {
		return code
	}

	if *purge {
		if refuseInteractiveJSON("staged", stderr, *jsonOut, false, *yes) {
			return 2
		}
		return purgeStaged(staging, positional, *all, *yes, stdin, stdout, stderr)
	}
	if *jsonOut {
		return listStagedJSON(staging, stdout, stderr)
	}
	return listStaged(staging, stdout, stderr)
}

func openStagingArea(command string, stderr io.Writer) (*deletionengine.StagingArea, int) {
	root, err := deletionengine.DefaultStagingRoot()
	if err != nil {
		fmt.Fprintf(stderr, "wtff %s: cannot determine staging location: %v\n", command, err)
		return nil, 1
	}
	staging, err := deletionengine.NewStagingArea(root)
	if err != nil {
		fmt.Fprintf(stderr, "wtff %s: cannot open staging area: %v\n", command, err)
		return nil, 1
	}
	return staging, 0
}

func listStaged(staging *deletionengine.StagingArea, stdout, stderr io.Writer) int {
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
		fmt.Fprintf(stdout, "%s  %-8s  %d item(s)  %s  from %s\n",
			batch.BatchID, batch.Command, len(batch.Items), batchSize(batch),
			batch.CreatedAt.Local().Format("2006-01-02 15:04"))
	}
	return 0
}

// listStagedJSON emits the staged batches as one document.
func listStagedJSON(staging *deletionengine.StagingArea, stdout, stderr io.Writer) int {
	batches, err := staging.ListBatches()
	if err != nil {
		fmt.Fprintln(stderr, "wtff staged: cannot list batches:", err)
		return 1
	}

	// An empty array rather than null, so a consumer can iterate without a nil
	// check and cannot mistake "none staged" for "field missing".
	out := make([]jsonBatch, 0, len(batches))
	for _, batch := range batches {
		var bytes int64
		complete := true
		for _, item := range batch.Items {
			if item.SizeKnown {
				bytes += item.SizeBytes
			} else {
				complete = false
			}
		}
		out = append(out, jsonBatch{
			BatchID:      batch.BatchID,
			Command:      batch.Command,
			CreatedAt:    batch.CreatedAt,
			ItemCount:    len(batch.Items),
			Bytes:        bytes,
			SizeComplete: complete,
		})
	}

	if err := emitJSON(stdout, jsonDocument{Command: "staged", Staged: out}); err != nil {
		fmt.Fprintln(stderr, "wtff staged: cannot write JSON:", err)
		return 1
	}
	return 0
}

func batchSize(batch *deletionengine.Batch) string {
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
	return size
}

// purgeStaged permanently deletes staged batches.
//
// This is the point where wtff withdraws a promise it made earlier: staging
// told the person the removal was reversible, and this makes it not. So it
// names exactly what is about to go, and asks for the full confirmation word
// rather than a keystroke, the same bar every other irreversible path uses.
func purgeStaged(staging *deletionengine.StagingArea, ids []string, all, yes bool,
	stdin io.Reader, stdout, stderr io.Writer) int {

	if all && len(ids) > 0 {
		fmt.Fprintln(stderr, "wtff staged: give either --all or batch ids, not both")
		return 2
	}
	if !all && len(ids) == 0 {
		fmt.Fprintln(stderr, "wtff staged: --purge needs a batch id, or --all")
		return 2
	}

	var targets []*deletionengine.Batch
	if all {
		listed, err := staging.ListBatches()
		if err != nil {
			fmt.Fprintln(stderr, "wtff staged: cannot list batches:", err)
			return 1
		}
		targets = listed
	} else {
		for _, id := range ids {
			// Resolved through the staging area rather than by joining the id to
			// a path here, so an id shaped like a traversal is refused before it
			// reaches the filesystem.
			batch, err := staging.FindBatch(id)
			if err != nil {
				fmt.Fprintln(stderr, "wtff staged:", err)
				return 1
			}
			targets = append(targets, batch)
		}
	}

	if len(targets) == 0 {
		fmt.Fprintln(stdout, "nothing is staged")
		return 0
	}

	var items int
	fmt.Fprintln(stdout, "the following staged batches would be deleted permanently:")
	for _, batch := range targets {
		items += len(batch.Items)
		fmt.Fprintf(stdout, "  %s  %-8s  %d item(s)  %s\n",
			batch.BatchID, batch.Command, len(batch.Items), batchSize(batch))
	}
	fmt.Fprintf(stdout, "\n%d item(s) across %d batch(es) can no longer be restored after this.\n",
		items, len(targets))

	if !approve(stdin, stdout, stderr, deletionengine.ActionPurge, yes) {
		fmt.Fprintln(stdout, "\naborted: nothing was deleted")
		return 1
	}

	logPath, err := operationlog.DefaultPath()
	if err != nil {
		fmt.Fprintln(stderr, "wtff staged: cannot determine log location:", err)
		return 1
	}
	log, err := operationlog.Open(logPath, "purge")
	if err != nil {
		fmt.Fprintln(stderr, "wtff staged: cannot open operation log:", err)
		return 1
	}
	defer log.Close()

	var purged, failed int
	var bytes int64
	var partial bool
	exit := 0
	for _, batch := range targets {
		result, purgeErr := staging.PurgeBatch(batch, log)
		if result != nil {
			purged += result.PurgedCount
			failed += result.FailedCount
			bytes += result.BytesReclaimed
			partial = partial || result.SizePartial
		}
		if purgeErr != nil {
			fmt.Fprintln(stderr, "wtff staged:", purgeErr)
			exit = 1
		}
	}

	size := humanBytes(bytes)
	if partial {
		size = "at least " + size
	}
	fmt.Fprintf(stdout, "\ndeleted %d item(s) permanently, %s reclaimed\n", purged, size)
	if failed > 0 {
		fmt.Fprintf(stdout, "%d item(s) could not be deleted and are still staged\n", failed)
		exit = 1
	}
	if logErr := log.Err(); logErr != nil {
		fmt.Fprintln(stderr, "wtff staged: warning, the operation log had a write failure:", logErr)
	}
	return exit
}
