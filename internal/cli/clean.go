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

func runClean(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("clean", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dryRun := fs.Bool("dry-run", false, "show what would happen without changing anything")
	purge := fs.Bool("purge", false, "remove permanently instead of staging for undo")
	yes := fs.Bool("yes", false, "proceed without an interactive confirmation")
	if err := fs.Parse(reorderFlagsFirst(args, commonBoolFlags)); err != nil {
		return 2
	}
	if len(fs.Args()) > 0 {
		fmt.Fprintln(stderr, "wtff clean: no positional arguments are accepted, see wtff help")
		return 2
	}

	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(stderr, "wtff clean: cannot determine home directory:", err)
		return 1
	}

	catalog, err := cleancatalog.LoadBuiltin()
	if err != nil {
		fmt.Fprintln(stderr, "wtff clean: cannot load the cleanup catalog:", err)
		return 1
	}
	candidates, discoverySkips := cleancatalog.Discover(
		cleancatalog.StageableEntries(catalog.Entries()), home)

	rules, err := protectionrules.LoadBuiltin()
	if err != nil {
		fmt.Fprintln(stderr, "wtff clean: cannot load protection rules:", err)
		return 1
	}

	logPath, err := operationlog.DefaultPath()
	if err != nil {
		fmt.Fprintln(stderr, "wtff clean: cannot determine log location:", err)
		return 1
	}
	log, err := operationlog.Open(logPath, "clean")
	if err != nil {
		fmt.Fprintln(stderr, "wtff clean: cannot open operation log:", err)
		return 1
	}
	defer log.Close()

	action := deletionengine.ActionStage
	if *purge {
		action = deletionengine.ActionPurge
	}

	var skips []skippedCandidate
	for _, s := range discoverySkips {
		// A category missing from this machine entirely is the ordinary case,
		// not something worth a line of output; most categories will not apply
		// to most machines. An earlier version of this filter checked whether
		// the path was empty, which it never was, so it filtered nothing and
		// every dry run listed every absent category as if it were news. Only
		// exclusions applied to something that actually exists are shown.
		if !s.CategoryAbsent {
			skips = append(skips, skippedCandidate{path: s.Path, reason: s.Reason})
		}
	}

	manifest, err := deletionengine.Plan(candidates, deletionengine.PlanOptions{
		Command:      "clean",
		Action:       action,
		Policy:       rules,
		Log:          log,
		MeasureSizes: true,
		SkipSink: func(path, reason string) {
			skips = append(skips, skippedCandidate{path: path, reason: reason})
		},
	})
	if err != nil {
		fmt.Fprintln(stderr, "wtff clean: cannot plan:", err)
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

	if !approve(stdin, stdout, stderr, action, *yes) {
		fmt.Fprintln(stdout, "\naborted: nothing was removed")
		return 1
	}

	var staging *deletionengine.StagingArea
	if action == deletionengine.ActionStage {
		root, rootErr := deletionengine.DefaultStagingRoot()
		if rootErr != nil {
			fmt.Fprintln(stderr, "wtff clean: cannot determine staging location:", rootErr)
			return 1
		}
		staging, err = deletionengine.NewStagingArea(root)
		if err != nil {
			fmt.Fprintln(stderr, "wtff clean: cannot prepare staging area:", err)
			return 1
		}
	}

	result, err := deletionengine.Apply(manifest, deletionengine.ApplyOptions{
		Staging: staging,
		Policy:  rules,
		Log:     log,
	})
	if err != nil {
		fmt.Fprintln(stderr, "wtff clean: apply failed:", err)
		return 1
	}

	printResult(stdout, action, result)

	if logErr := log.Err(); logErr != nil {
		fmt.Fprintln(stderr, "wtff clean: warning, the operation log had a write failure:", logErr)
	}
	if result.FailedCount > 0 {
		return 1
	}
	return 0
}
