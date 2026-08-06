package terminalshell

import (
	cleancatalog "github.com/lesliemusengi/wtff/internal/clean-catalog"
	deletionengine "github.com/lesliemusengi/wtff/internal/deletion-engine"
)

// startCleanFlow returns the first live block of the clean flow: a scan,
// then selection, then staging, all through the shared blocks.
func startCleanFlow(deps *Deps, theme Theme) liveBlock {
	return newScanBlock(deps, theme, "clean", cleanPlan)
}

// cleanPlan discovers candidates from the catalog and plans their removal.
//
// This is deliberately the same two calls, cleancatalog.Discover followed by
// deletionengine.Plan, that internal/cli's clean command makes. The shell
// and the non-interactive command are two presentations of identical
// underlying behavior, not two independent implementations of "what does
// clean do."
func cleanPlan(deps *Deps) (*deletionengine.Manifest, int, error) {
	candidates, discoverySkips := cleancatalog.Discover(
		cleancatalog.StageableEntries(deps.Catalog.Entries()), deps.Home)

	skipped := 0
	for _, s := range discoverySkips {
		if !s.CategoryAbsent {
			skipped++
		}
	}

	var planSkipped int
	manifest, err := deletionengine.Plan(candidates, deletionengine.PlanOptions{
		Command:      "clean",
		Action:       deletionengine.ActionStage,
		Policy:       deps.Rules,
		Log:          deps.Log,
		MeasureSizes: true,
		SkipSink: func(string, string) {
			planSkipped++
		},
	})
	if err != nil {
		return nil, 0, err
	}
	return manifest, skipped + planSkipped, nil
}
