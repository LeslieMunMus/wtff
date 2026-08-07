package cli

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	deletionengine "github.com/lesliemunmus/wtff/internal/deletion-engine"
	operationlog "github.com/lesliemunmus/wtff/internal/operation-log"
)

// candidateRuleID and candidateReason are recorded against every path the user
// names directly on the command line.
//
// The deletion engine refuses a candidate with no rule id or reason, on the
// principle that a plan no one can explain does not belong in front of a
// person being asked to approve it. A path typed at a prompt has an obvious
// explanation: the person typed it. Recording that explicitly, rather than
// leaving the field blank because it seems self evident, keeps every entry in
// the audit log consistent regardless of how it was proposed.
const (
	candidateRuleID = "cli-explicit-selection"
	candidateReason = "named directly on the command line by wtff remove"
)

func runRemove(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("remove", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dryRun := fs.Bool("dry-run", false, "show what would happen without changing anything")
	purge := fs.Bool("purge", false, "remove permanently instead of staging for undo")
	yes := fs.Bool("yes", false, "proceed without an interactive confirmation")
	jsonOut := fs.Bool("json", false, "emit one JSON document instead of human readable output")
	if err := fs.Parse(reorderFlagsFirst(args, commonBoolFlags)); err != nil {
		return 2
	}
	if refuseInteractiveJSON("remove", stderr, *jsonOut, *dryRun, *yes) {
		return 2
	}
	targets := fs.Args()
	if len(targets) == 0 {
		fmt.Fprintln(stderr, "wtff remove: at least one path is required")
		return 2
	}

	candidates, err := buildCandidates(targets)
	if err != nil {
		fmt.Fprintln(stderr, "wtff remove:", err)
		return 1
	}

	rules, ok := loadRules("remove", stdout, stderr)
	if !ok {
		return 1
	}
	if !*jsonOut {
		reportOverrides(rules, stdout)
	}

	logPath, err := operationlog.DefaultPath()
	if err != nil {
		fmt.Fprintln(stderr, "wtff remove: cannot determine log location:", err)
		return 1
	}
	log, err := operationlog.Open(logPath, "remove")
	if err != nil {
		fmt.Fprintln(stderr, "wtff remove: cannot open operation log:", err)
		return 1
	}
	defer log.Close()

	action := deletionengine.ActionStage
	if *purge {
		action = deletionengine.ActionPurge
	}

	var skips []skippedCandidate
	manifest, err := deletionengine.Plan(candidates, deletionengine.PlanOptions{
		Command:      "remove",
		Action:       action,
		Policy:       rules,
		Log:          log,
		MeasureSizes: true,
		SkipSink: func(path, reason string) {
			skips = append(skips, skippedCandidate{path: path, reason: reason})
		},
	})
	if err != nil {
		fmt.Fprintln(stderr, "wtff remove: cannot plan:", err)
		return 1
	}

	if *jsonOut && *dryRun {
		if err := emitJSON(stdout, jsonDocument{
			Command: "remove", Plan: planToJSON(manifest, skips, true),
		}); err != nil {
			fmt.Fprintln(stderr, "wtff remove: cannot write JSON:", err)
			return 1
		}
		return 0
	}
	if !*jsonOut {
		printPlan(stdout, manifest, skips)
	}

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
			fmt.Fprintln(stderr, "wtff remove: cannot determine staging location:", rootErr)
			return 1
		}
		staging, err = deletionengine.NewStagingArea(root)
		if err != nil {
			fmt.Fprintln(stderr, "wtff remove: cannot prepare staging area:", err)
			return 1
		}
	}

	result, err := deletionengine.Apply(manifest, deletionengine.ApplyOptions{
		Staging: staging,
		Policy:  rules,
		Log:     log,
	})
	if err != nil {
		fmt.Fprintln(stderr, "wtff remove: apply failed:", err)
		return 1
	}

	if *jsonOut {
		if err := emitJSON(stdout, jsonDocument{
			Command: "remove", Result: resultToJSON(action, result),
		}); err != nil {
			fmt.Fprintln(stderr, "wtff remove: cannot write JSON:", err)
			return 1
		}
	} else {
		printResult(stdout, action, result)
	}

	if logErr := log.Err(); logErr != nil {
		fmt.Fprintln(stderr, "wtff remove: warning, the operation log had a write failure:", logErr)
	}
	if result.FailedCount > 0 {
		return 1
	}
	return 0
}

// approve gets confirmation appropriate to the action's reversibility. A purge
// requires the full confirmation word even when --yes was not required to be
// asked for, because an irreversible action deserves more friction than a
// reversible one, not the same friction.
func approve(stdin io.Reader, stdout, stderr io.Writer, action deletionengine.Action, skipPrompt bool) bool {
	if skipPrompt {
		return true
	}
	if !isInteractive(stdin) {
		fmt.Fprintln(stderr, "refusing to proceed: not an interactive session, pass --yes to confirm")
		return false
	}
	if action == deletionengine.ActionPurge {
		fmt.Fprintln(stdout, "\nthis removes the items above permanently. it cannot be undone.")
		return confirmPurge(stdin, stdout, fmt.Sprintf("type %q to confirm: ", confirmationWord))
	}
	return confirmStage(stdin, stdout, "\nproceed? [y/N] ")
}

type skippedCandidate struct {
	path   string
	reason string
}

func buildCandidates(targets []string) ([]deletionengine.Candidate, error) {
	candidates := make([]deletionengine.Candidate, 0, len(targets))
	for _, target := range targets {
		// Checked against the raw argument, before anything touches it. Both
		// expandHome and filepath.Abs clean their result internally, which
		// collapses a ".." lexically. An earlier version of this check ran
		// after expandHome and missed exactly this: expandHome's own Join call
		// silently resolved "~/../../etc/passwd" to "/etc/passwd" before the
		// check ever saw a ".." to reject. Checking first, before either
		// function runs, is the only place in this sequence nothing has
		// cleaned the string yet.
		if hasTraversalComponent(target) {
			return nil, fmt.Errorf("%s contains a parent directory reference, which is not accepted here", target)
		}

		absolute, err := filepath.Abs(expandHome(target))
		if err != nil {
			return nil, fmt.Errorf("cannot resolve %s: %w", target, err)
		}
		candidates = append(candidates, deletionengine.Candidate{
			Path:   absolute,
			RuleID: candidateRuleID,
			Reason: candidateReason,
		})
	}
	return candidates, nil
}

// hasTraversalComponent reports whether path contains a ".." as a complete
// path component, matching path validation's own definition so that a path
// this function accepts is rejected consistently, or not at all, by the layer
// underneath.
func hasTraversalComponent(path string) bool {
	for _, component := range strings.Split(path, "/") {
		if component == ".." {
			return true
		}
	}
	return false
}

// expandHome expands a leading ~ the way a shell would.
//
// A shell normally does this before wtff ever sees the argument, but wtff can
// also be invoked from a script or another program that passes an unexpanded
// path through, and failing there with a confusing "not absolute" error is
// worse than the small amount of code this takes.
func expandHome(path string) string {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if path == "~" {
		return home
	}
	return filepath.Join(home, path[2:])
}

func printPlan(w io.Writer, manifest *deletionengine.Manifest, skips []skippedCandidate) {
	if len(manifest.Entries) == 0 && len(skips) == 0 {
		fmt.Fprintln(w, "nothing to do")
		return
	}

	if len(manifest.Entries) > 0 {
		verb := "stage"
		if manifest.Action == deletionengine.ActionPurge {
			verb = "permanently remove"
		}
		fmt.Fprintf(w, "will %s %d item(s):\n", verb, len(manifest.Entries))
		for _, entry := range manifest.Entries {
			size := "unknown size"
			if entry.SizeKnown {
				size = humanBytes(entry.SizeBytes)
			}
			fmt.Fprintf(w, "  %s  %s\n", size, entry.ResolvedPath)
		}
		total := humanBytes(manifest.TotalBytes)
		if manifest.PartialSizing {
			total += " (partial, some sizes could not be measured)"
		}
		fmt.Fprintf(w, "total: %s\n", total)
	}

	if len(skips) > 0 {
		fmt.Fprintf(w, "skipped %d item(s):\n", len(skips))
		for _, skip := range skips {
			fmt.Fprintf(w, "  %s: %s\n", skip.path, skip.reason)
		}
	}
}

func printResult(w io.Writer, action deletionengine.Action, result *deletionengine.Result) {
	fmt.Fprintln(w)
	verb := "staged"
	if action == deletionengine.ActionPurge {
		verb = "removed permanently"
	}
	fmt.Fprintf(w, "%s %d item(s), %s\n",
		verb, result.AppliedCount, humanBytes(result.BytesApplied))

	if result.SkippedCount > 0 {
		fmt.Fprintf(w, "skipped %d item(s) at apply time:\n", result.SkippedCount)
		for _, outcome := range result.Outcomes {
			if outcome.Skipped {
				fmt.Fprintf(w, "  %s: %s\n", outcome.Entry.ResolvedPath, outcome.Reason)
			}
		}
	}
	if result.FailedCount > 0 {
		fmt.Fprintf(w, "failed to remove %d item(s):\n", result.FailedCount)
		for _, outcome := range result.Outcomes {
			if outcome.Err != nil {
				fmt.Fprintf(w, "  %s: %v\n", outcome.Entry.ResolvedPath, outcome.Err)
			}
		}
	}
	if result.Batch != nil {
		fmt.Fprintf(w, "undo with: wtff undo %s\n", result.Batch.BatchID)
	}
}
