package cli

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	deletionengine "github.com/lesliemusengi/wtff/internal/deletion-engine"
	operationlog "github.com/lesliemusengi/wtff/internal/operation-log"
	protectionrules "github.com/lesliemusengi/wtff/internal/protection-rules"
	uninstallcore "github.com/lesliemusengi/wtff/internal/uninstall-core"
)

// appSearchRoots returns the directories wtff looks for installed
// applications in.
func appSearchRoots(home string) []string {
	return []string{"/Applications", filepath.Join(home, "Applications")}
}

func runUninstall(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("uninstall", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dryRun := fs.Bool("dry-run", false, "show what would happen without changing anything")
	purge := fs.Bool("purge", false, "remove permanently instead of staging for undo")
	yes := fs.Bool("yes", false, "proceed without an interactive confirmation")
	jsonOut := fs.Bool("json", false, "emit one JSON document instead of human readable output")
	if err := fs.Parse(reorderFlagsFirst(args, commonBoolFlags)); err != nil {
		return 2
	}
	if refuseInteractiveJSON("uninstall", stderr, *jsonOut, *dryRun, *yes) {
		return 2
	}
	remaining := fs.Args()
	if len(remaining) != 1 {
		fmt.Fprintln(stderr, "wtff uninstall: exactly one application name or bundle identifier is required")
		return 2
	}
	query := remaining[0]

	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(stderr, "wtff uninstall: cannot determine home directory:", err)
		return 1
	}

	apps, _, err := uninstallcore.DiscoverApps(appSearchRoots(home))
	if err != nil {
		fmt.Fprintln(stderr, "wtff uninstall: cannot discover installed applications:", err)
		return 1
	}

	matches := uninstallcore.FindApp(apps, query)
	switch len(matches) {
	case 0:
		fmt.Fprintf(stderr, "wtff uninstall: no installed application matches %q\n", query)
		return 1
	case 1:
		// exactly one, proceed below
	default:
		fmt.Fprintf(stderr, "wtff uninstall: %q matches more than one installed application:\n", query)
		for _, app := range matches {
			fmt.Fprintf(stderr, "  %s  (%s)  %s\n", app.DisplayName, app.BundleID, app.Path)
		}
		fmt.Fprintln(stderr, "use the exact bundle identifier to pick one")
		return 2
	}
	app := matches[0]

	if reason, protected := uninstallcore.IsProtectedApp(app); protected {
		fmt.Fprintf(stderr, "wtff uninstall: %s cannot be uninstalled: %s\n", app.DisplayName, reason)
		return 1
	}

	// Root level components are reported before the plan rather than after the
	// removal. wtff runs unprivileged by design and cannot remove these, so the
	// honest thing is to say what will survive while the choice is still open.
	// Suppressed under --json, where it would be prose in the middle of a
	// document. The same information is not yet in the JSON surface, which is
	// recorded as a known gap rather than papered over by printing it anyway.
	if leftovers := uninstallcore.InspectSystemIntegration(app, apps); leftovers.Orphans() && !*jsonOut {
		fmt.Fprintf(stdout, "note: removing %s leaves privileged components behind, "+
			"which wtff cannot remove because it does not run with elevated privileges:\n",
			app.DisplayName)
		for _, path := range leftovers.PrivilegedHelpers {
			fmt.Fprintf(stdout, "  helper  %s\n", path)
		}
		for _, path := range leftovers.LaunchDaemons {
			fmt.Fprintf(stdout, "  job     %s\n", path)
		}
		fmt.Fprintln(stdout, "  remove these with sudo, or use the vendor's uninstaller.")
		fmt.Fprintln(stdout)
	}

	candidates := []deletionengine.Candidate{{
		Path:   app.Path,
		RuleID: "uninstall-application-bundle",
		Reason: fmt.Sprintf("the application bundle for %s, matched by explicit selection", app.DisplayName),
	}}
	candidates = append(candidates, uninstallcore.DiscoverLeftovers(app, home)...)

	rules, err := protectionrules.LoadBuiltin()
	if err != nil {
		fmt.Fprintln(stderr, "wtff uninstall: cannot load protection rules:", err)
		return 1
	}

	logPath, err := operationlog.DefaultPath()
	if err != nil {
		fmt.Fprintln(stderr, "wtff uninstall: cannot determine log location:", err)
		return 1
	}
	log, err := operationlog.Open(logPath, "uninstall")
	if err != nil {
		fmt.Fprintln(stderr, "wtff uninstall: cannot open operation log:", err)
		return 1
	}
	defer log.Close()

	action := deletionengine.ActionStage
	if *purge {
		action = deletionengine.ActionPurge
	}

	var skips []skippedCandidate
	manifest, err := deletionengine.Plan(candidates, deletionengine.PlanOptions{
		Command:      "uninstall",
		Action:       action,
		Policy:       rules,
		Log:          log,
		MeasureSizes: true,
		SkipSink: func(path, reason string) {
			skips = append(skips, skippedCandidate{path: path, reason: reason})
		},
	})
	if err != nil {
		fmt.Fprintln(stderr, "wtff uninstall: cannot plan:", err)
		return 1
	}

	if !*jsonOut {
		fmt.Fprintf(stdout, "uninstalling %s (%s)\n\n", app.DisplayName, app.BundleID)
	}
	if *jsonOut && *dryRun {
		if err := emitJSON(stdout, jsonDocument{
			Command: "uninstall", Plan: planToJSON(manifest, skips, true),
		}); err != nil {
			fmt.Fprintln(stderr, "wtff uninstall: cannot write JSON:", err)
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
			fmt.Fprintln(stderr, "wtff uninstall: cannot determine staging location:", rootErr)
			return 1
		}
		staging, err = deletionengine.NewStagingArea(root)
		if err != nil {
			fmt.Fprintln(stderr, "wtff uninstall: cannot prepare staging area:", err)
			return 1
		}
	}

	result, err := deletionengine.Apply(manifest, deletionengine.ApplyOptions{
		Staging: staging,
		Policy:  rules,
		Log:     log,
	})
	if err != nil {
		fmt.Fprintln(stderr, "wtff uninstall: apply failed:", err)
		return 1
	}

	if *jsonOut {
		if err := emitJSON(stdout, jsonDocument{
			Command: "uninstall", Result: resultToJSON(action, result),
		}); err != nil {
			fmt.Fprintln(stderr, "wtff uninstall: cannot write JSON:", err)
			return 1
		}
	} else {
		printResult(stdout, action, result)
	}

	if logErr := log.Err(); logErr != nil {
		fmt.Fprintln(stderr, "wtff uninstall: warning, the operation log had a write failure:", logErr)
	}
	if result.FailedCount > 0 {
		return 1
	}
	return 0
}
