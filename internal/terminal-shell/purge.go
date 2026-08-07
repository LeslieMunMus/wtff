package terminalshell

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	cleancatalog "github.com/lesliemunmus/wtff/internal/clean-catalog"
	deletionengine "github.com/lesliemunmus/wtff/internal/deletion-engine"
)

// startPurgeFlow begins the shallow purge: the catalog entries whose contents
// the person already discarded, removed permanently.
//
// It reuses the same scan and selection blocks clean uses, so the review step
// looks identical and a person reads one interface rather than two. What
// differs is the manifest's action and the confirmation gate before it runs,
// which is where the difference actually matters.
func startPurgeFlow(deps *Deps, theme Theme) liveBlock {
	return newScanBlock(deps, theme, "purge", purgePlan)
}

func purgePlan(deps *Deps, progress func(done, total int)) (*deletionengine.Manifest, int, error) {
	entries := cleancatalog.PurgeableEntries(deps.Catalog.Entries())
	if len(entries) == 0 {
		return nil, 0, fmt.Errorf("no catalog entries are marked purgeable")
	}

	// Discovered one entry at a time so each candidate can carry that entry's
	// purge justification rather than its clean one. The clean reason explains
	// why staging the item is safe, which is not true here and would be read
	// as a promise of reversibility at exactly the wrong moment.
	var candidates []deletionengine.Candidate
	skipped := 0
	for _, entry := range entries {
		found, entrySkips := cleancatalog.Discover([]cleancatalog.Entry{entry}, deps.Home)
		for i := range found {
			found[i].Reason = entry.PurgeReason
		}
		candidates = append(candidates, found...)
		for _, skip := range entrySkips {
			if !skip.CategoryAbsent {
				skipped++
			}
		}
	}

	// Protection rules still run. Something protected that was dragged to the
	// Trash by hand is exactly the case where a person is one keypress from
	// losing what they meant to keep, and the rules refusing is the reason
	// they exist.
	manifest, err := deletionengine.Plan(candidates, deletionengine.PlanOptions{
		Command:      "purge",
		Action:       deletionengine.ActionPurge,
		Policy:       deps.Rules,
		Log:          deps.Log,
		MeasureSizes: true,
		Progress:     progress,
		SkipSink:     func(string, string) { skipped++ },
	})
	return manifest, skipped, err
}

// confirmPurgeManifest is the gate between choosing what to delete and
// deleting it. Reached only from a selection the person already narrowed, and
// it names the cost in the same breath as asking.
func confirmPurgeManifest(deps *Deps, theme Theme, command string,
	manifest *deletionengine.Manifest) liveBlock {

	warning := fmt.Sprintf(
		"%d item(s), %s, will be deleted permanently. This cannot be undone.",
		len(manifest.Entries), humanBytes(manifest.TotalBytes))

	return newConfirmWordBlock(theme, command+" · confirm permanent deletion", warning,
		func() tea.Cmd {
			return transition(newApplyBlock(deps, theme, command, manifest))
		},
		func() tea.Cmd {
			return finish(cancelEntry(theme, command))
		},
	)
}
