package cli

import (
	"flag"
	"fmt"
	"io"
	"os"

	cleancatalog "github.com/lesliemusengi/wtff/internal/clean-catalog"
	deletionengine "github.com/lesliemusengi/wtff/internal/deletion-engine"
	operationlog "github.com/lesliemusengi/wtff/internal/operation-log"
	protectionrules "github.com/lesliemusengi/wtff/internal/protection-rules"
)

// runPurge removes the catalog entries marked purgeable, permanently.
//
// This is the shallow counterpart to clean. Clean searches deeply, infers what
// is disposable, and stages what it finds so a wrong inference costs nothing.
// Purge does not infer: it acts only on entries whose contents the person has
// already discarded, which today is the Trash and nothing else. Because there
// is no inference to walk back, there is nothing to stage.
func runPurge(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("purge", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dryRun := fs.Bool("dry-run", false, "show what would happen without changing anything")
	yes := fs.Bool("yes", false, "proceed without an interactive confirmation")
	if err := fs.Parse(reorderFlagsFirst(args, commonBoolFlags)); err != nil {
		return 2
	}
	if len(fs.Args()) > 0 {
		fmt.Fprintln(stderr, "wtff purge: no positional arguments are accepted, see wtff help")
		return 2
	}

	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(stderr, "wtff purge: cannot determine home directory:", err)
		return 1
	}

	catalog, err := cleancatalog.LoadBuiltin()
	if err != nil {
		fmt.Fprintln(stderr, "wtff purge: cannot load the cleanup catalog:", err)
		return 1
	}
	entries := cleancatalog.PurgeableEntries(catalog.Entries())
	if len(entries) == 0 {
		fmt.Fprintln(stdout, "no catalog entries are marked purgeable")
		return 0
	}
	// One entry at a time, so each candidate carries that entry's purge
	// justification rather than its clean one. The clean reason explains why
	// staging is safe, which is not what is about to happen.
	var candidates []deletionengine.Candidate
	var discoverySkips []cleancatalog.Skip
	for _, entry := range entries {
		found, entrySkips := cleancatalog.Discover([]cleancatalog.Entry{entry}, home)
		for i := range found {
			found[i].Reason = entry.PurgeReason
		}
		candidates = append(candidates, found...)
		discoverySkips = append(discoverySkips, entrySkips...)
	}

	rules, err := protectionrules.LoadBuiltin()
	if err != nil {
		fmt.Fprintln(stderr, "wtff purge: cannot load protection rules:", err)
		return 1
	}

	logPath, err := operationlog.DefaultPath()
	if err != nil {
		fmt.Fprintln(stderr, "wtff purge: cannot determine log location:", err)
		return 1
	}
	log, err := operationlog.Open(logPath, "purge")
	if err != nil {
		fmt.Fprintln(stderr, "wtff purge: cannot open operation log:", err)
		return 1
	}
	defer log.Close()

	var skips []skippedCandidate
	for _, s := range discoverySkips {
		if !s.CategoryAbsent {
			skips = append(skips, skippedCandidate{path: s.Path, reason: s.Reason})
		}
	}

	// Protection rules still apply. Something protected that was dragged to the
	// Trash by hand is exactly the case where a person is about to lose
	// something they meant to keep, and the rules refusing is the whole point of
	// having them.
	manifest, err := deletionengine.Plan(candidates, deletionengine.PlanOptions{
		Command:      "purge",
		Action:       deletionengine.ActionPurge,
		Policy:       rules,
		Log:          log,
		MeasureSizes: true,
		SkipSink: func(path, reason string) {
			skips = append(skips, skippedCandidate{path: path, reason: reason})
		},
	})
	if err != nil {
		fmt.Fprintln(stderr, "wtff purge: cannot plan:", err)
		return 1
	}

	printPlan(stdout, manifest, skips)

	if len(manifest.Entries) == 0 {
		return 0
	}
	if *dryRun {
		fmt.Fprintln(stdout, "\ndry run: nothing was changed")
		return 0
	}

	if !approve(stdin, stdout, stderr, deletionengine.ActionPurge, *yes) {
		fmt.Fprintln(stdout, "\naborted: nothing was removed")
		return 1
	}

	result, err := deletionengine.Apply(manifest, deletionengine.ApplyOptions{
		Policy: rules,
		Log:    log,
	})
	if err != nil {
		fmt.Fprintln(stderr, "wtff purge: apply failed:", err)
		return 1
	}

	printResult(stdout, deletionengine.ActionPurge, result)

	if logErr := log.Err(); logErr != nil {
		fmt.Fprintln(stderr, "wtff purge: warning, the operation log had a write failure:", logErr)
	}
	if result.FailedCount > 0 {
		return 1
	}
	return 0
}
