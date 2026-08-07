package terminalshell

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	deletionengine "github.com/lesliemunmus/wtff/internal/deletion-engine"
)

// homeWithTrash builds an isolated home holding one discarded file.
func homeWithTrash(t *testing.T) (string, string) {
	t.Helper()
	home := t.TempDir()
	trashed := filepath.Join(home, ".Trash", "discarded-file")
	writeTestFile(t, trashed, "already thrown away")
	return home, trashed
}

func TestPurgeIsAvailableFromThePrompt(t *testing.T) {
	deps := testDeps(t, t.TempDir())
	app := newTestApp(t, deps)

	app = typeInto(app, "purge")
	app, cmd := pressEnter(app)
	if cmd == nil || app.block == nil {
		t.Fatal("purge should start a flow")
	}
	if _, ok := app.block.(*scanBlock); !ok {
		t.Fatalf("expected a scan block, got %T", app.block)
	}
}

// The purge plan must carry the purge action, since that single field is what
// decides whether Apply stages or destroys.
func TestPurgePlanUsesThePurgeAction(t *testing.T) {
	home, _ := homeWithTrash(t)
	deps := testDeps(t, home)

	manifest, _, err := purgePlan(deps, nil)
	if err != nil {
		t.Fatalf("purge plan: %v", err)
	}
	if manifest.Action != deletionengine.ActionPurge {
		t.Fatalf("action = %q, want purge", manifest.Action)
	}
	if len(manifest.Entries) != 1 {
		t.Fatalf("planned %d entries, want the one discarded file", len(manifest.Entries))
	}
}

// Purge must not widen into the caches that clean handles. This is the same
// boundary the catalog test pins, checked here through the shell's own plan.
func TestPurgePlanLeavesCachesAlone(t *testing.T) {
	home, trashed := homeWithTrash(t)
	cacheDir := filepath.Join(home, "Library", "Caches", "com.example.app")
	writeTestFile(t, filepath.Join(cacheDir, "data"), "regenerable")
	deps := testDeps(t, home)

	manifest, _, err := purgePlan(deps, nil)
	if err != nil {
		t.Fatalf("purge plan: %v", err)
	}

	// Compared against the real cache directory rather than by searching the
	// path for "Caches". An earlier version of this test did the latter and
	// failed on its own name: Go builds temporary directories from the test
	// function's name, and this one contains the word.
	for _, entry := range manifest.Entries {
		if strings.HasPrefix(entry.ResolvedPath, cacheDir) {
			t.Fatalf("purge planned a cache path: %s", entry.ResolvedPath)
		}
	}
	// Matched on the suffix, because the engine reports the fully resolved
	// path and macOS resolves the temporary directory's /var through a link
	// into /private/var.
	if len(manifest.Entries) != 1 ||
		!strings.HasSuffix(manifest.Entries[0].ResolvedPath, filepath.Join(".Trash", "discarded-file")) {
		t.Fatalf("purge should have planned only %s, got %d entries: %+v",
			trashed, len(manifest.Entries), manifest.Entries)
	}
}

// A purge selection must reach the confirmation gate, never Apply directly.
// This is the difference between purge and clean in the one place it matters.
func TestPurgeSelectionRoutesThroughConfirmation(t *testing.T) {
	home, _ := homeWithTrash(t)
	deps := testDeps(t, home)

	manifest, _, err := purgePlan(deps, nil)
	if err != nil {
		t.Fatalf("purge plan: %v", err)
	}

	selection := newSelectionBlock(deps, brandTheme, "purge", manifest)
	_, first := selection.Update(tea.KeyMsg{Type: tea.KeyEnter})
	confirm := runCmd(t, first)
	_, second := selection.Update(confirm)
	flow := runCmd(t, second).(flowMsg)

	if _, ok := flow.block.(*confirmWordBlock); !ok {
		t.Fatalf("a purge selection must reach the confirmation gate, got %T", flow.block)
	}
}

// The staging path must not gain that gate, since a confirmation for something
// undoable is friction without a purpose.
func TestStagingSelectionSkipsConfirmation(t *testing.T) {
	home := t.TempDir()
	target := filepath.Join(home, "cache-entry")
	writeTestFile(t, target, "payload")
	deps := testDeps(t, home)

	manifest, err := deletionengine.Plan(
		[]deletionengine.Candidate{{Path: target, RuleID: "r", Reason: "x"}},
		deletionengine.PlanOptions{Command: "clean", Action: deletionengine.ActionStage,
			Policy: deps.Rules, Log: deps.Log},
	)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}

	selection := newSelectionBlock(deps, brandTheme, "clean", manifest)
	_, first := selection.Update(tea.KeyMsg{Type: tea.KeyEnter})
	confirm := runCmd(t, first)
	_, second := selection.Update(confirm)
	flow := runCmd(t, second).(flowMsg)

	if _, ok := flow.block.(*applyBlock); !ok {
		t.Fatalf("staging should go straight to apply, got %T", flow.block)
	}
}

// The selection box must not describe a purge as staging. This defect shipped
// through every passing test above and was only visible in a rendered purge:
// the box said "select items to stage" and hinted "enter stage" while Enter
// led to permanent deletion, and the reason text under each row said the
// removal would be reversible.
func TestPurgeSelectionNeverPromisesStaging(t *testing.T) {
	home, _ := homeWithTrash(t)
	deps := testDeps(t, home)

	manifest, _, err := purgePlan(deps, nil)
	if err != nil {
		t.Fatalf("purge plan: %v", err)
	}

	view := newSelectionBlock(deps, brandTheme, "purge", manifest).View(brandTheme, 100)
	for _, forbidden := range []string{"select items to stage", "enter stage"} {
		if strings.Contains(view, forbidden) {
			t.Errorf("a purge selection shows %q", forbidden)
		}
	}
	if !strings.Contains(view, "permanently") {
		t.Error("a purge selection should say the deletion is permanent")
	}

	// The per-row justification must be the purge reason, not the clean one,
	// which explains why staging is safe and is false here.
	for _, entry := range manifest.Entries {
		if strings.Contains(entry.Reason, "staging area") {
			t.Errorf("a purge candidate carries the staging justification: %q", entry.Reason)
		}
	}
}

// Staging must keep its own wording, so this does not become a purge that
// merely looks reversible or a clean that reads as destructive.
func TestStagingSelectionStillSaysStage(t *testing.T) {
	home := t.TempDir()
	target := filepath.Join(home, "cache-entry")
	writeTestFile(t, target, "payload")
	deps := testDeps(t, home)

	manifest, err := deletionengine.Plan(
		[]deletionengine.Candidate{{Path: target, RuleID: "r", Reason: "x"}},
		deletionengine.PlanOptions{Command: "clean", Action: deletionengine.ActionStage,
			Policy: deps.Rules, Log: deps.Log},
	)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}

	view := newSelectionBlock(deps, brandTheme, "clean", manifest).View(brandTheme, 100)
	if !strings.Contains(view, "stage") {
		t.Error("a staging selection should still say stage")
	}
	if strings.Contains(view, "permanently") {
		t.Error("a staging selection must not claim permanence")
	}
}

func TestConfirmWordBlockRequiresTheExactWord(t *testing.T) {
	var approved bool
	block := newConfirmWordBlock(brandTheme, "test", "this cannot be undone",
		func() tea.Cmd { approved = true; return nil },
		func() tea.Cmd { return nil },
	)

	// A bare yes is the answer a person gives on reflex. It must not work.
	for _, wrong := range []string{"y", "yes", "perm", "delete"} {
		block.input.SetValue(wrong)
		updated, _ := block.Update(tea.KeyMsg{Type: tea.KeyEnter})
		if approved {
			t.Fatalf("%q approved an irreversible action", wrong)
		}
		if updated.(*confirmWordBlock).note == "" {
			t.Fatalf("%q should have produced an explanation", wrong)
		}
	}

	block.input.SetValue(confirmationWord)
	block.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !approved {
		t.Fatal("the confirmation word should approve")
	}
}

// Enter on its own is the dangerous case: it is the key that got the person
// this far, and momentum is the whole risk this gate exists to interrupt.
func TestConfirmWordBlockIgnoresBareEnter(t *testing.T) {
	var approved bool
	block := newConfirmWordBlock(brandTheme, "test", "warning",
		func() tea.Cmd { approved = true; return nil },
		func() tea.Cmd { return nil },
	)

	for i := 0; i < 5; i++ {
		updated, _ := block.Update(tea.KeyMsg{Type: tea.KeyEnter})
		block = updated.(*confirmWordBlock)
	}
	if approved {
		t.Fatal("repeated Enter approved an irreversible action")
	}
}

func TestConfirmWordBlockEscapeCancels(t *testing.T) {
	var cancelled bool
	block := newConfirmWordBlock(brandTheme, "test", "warning",
		func() tea.Cmd { return nil },
		func() tea.Cmd { cancelled = true; return nil },
	)
	block.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if !cancelled {
		t.Fatal("escape should cancel")
	}
}

// Whitespace and case are typing noise, not a refusal. Rejecting them would
// train someone to retype until it works, which is the opposite of deliberate.
func TestConfirmWordBlockToleratesCaseAndSpacing(t *testing.T) {
	for _, accepted := range []string{"permanently", "PERMANENTLY", "  permanently  "} {
		var approved bool
		block := newConfirmWordBlock(brandTheme, "test", "warning",
			func() tea.Cmd { approved = true; return nil },
			func() tea.Cmd { return nil },
		)
		block.input.SetValue(accepted)
		block.Update(tea.KeyMsg{Type: tea.KeyEnter})
		if !approved {
			t.Fatalf("%q should have been accepted", accepted)
		}
	}
}

// The full shell path from a staged batch to permanent deletion, against real
// files, ending with the data actually gone.
func TestStagedBatchCanBeDeletedPermanentlyThroughTheShell(t *testing.T) {
	home := t.TempDir()
	target := filepath.Join(home, "cache-entry")
	writeTestFile(t, target, "reclaimable content")
	deps := testDeps(t, home)

	manifest, err := deletionengine.Plan(
		[]deletionengine.Candidate{{Path: target, RuleID: "r", Reason: "x"}},
		deletionengine.PlanOptions{Command: "clean", Policy: deps.Rules,
			Log: deps.Log, MeasureSizes: true},
	)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	area, err := deps.newStagingArea()
	if err != nil {
		t.Fatalf("staging area: %v", err)
	}
	if _, err := deletionengine.Apply(manifest, deletionengine.ApplyOptions{
		Staging: area, Policy: deps.Rules, Log: deps.Log,
	}); err != nil {
		t.Fatalf("apply: %v", err)
	}

	batches, err := area.ListBatches()
	if err != nil || len(batches) != 1 {
		t.Fatalf("expected one staged batch, got %d, %v", len(batches), err)
	}
	batchDir := batches[0].Dir()

	// Choose the batch, then the second action, which is permanent deletion.
	actions := newBatchActionBlock(deps, brandTheme, batches[0])
	_, downCmd := actions.Update(tea.KeyMsg{Type: tea.KeyDown})
	if downCmd != nil {
		t.Fatal("moving the cursor should not produce a command")
	}
	_, chooseCmd := actions.Update(tea.KeyMsg{Type: tea.KeyEnter})
	flow := runCmd(t, chooseCmd).(flowMsg)

	gate, ok := flow.block.(*confirmWordBlock)
	if !ok {
		t.Fatalf("permanent deletion must be gated, got %T", flow.block)
	}

	gate.input.SetValue(confirmationWord)
	_, approveCmd := gate.Update(tea.KeyMsg{Type: tea.KeyEnter})
	flow = runCmd(t, approveCmd).(flowMsg)

	purge, ok := flow.block.(*purgeBatchBlock)
	if !ok {
		t.Fatalf("expected a purge block, got %T", flow.block)
	}

	done := resolveBatch(t, purge.Init(), func(m tea.Msg) bool {
		_, ok := m.(purgeBatchDoneMsg)
		return ok
	}).(purgeBatchDoneMsg)
	if done.err != nil {
		t.Fatalf("purge: %v", done.err)
	}
	if done.result.PurgedCount != 1 {
		t.Fatalf("purged %d, want 1", done.result.PurgedCount)
	}

	if _, err := os.Stat(batchDir); !os.IsNotExist(err) {
		t.Fatal("the staged batch should be gone")
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatal("a purge must not restore the file")
	}

	_, finalCmd := purge.Update(done)
	final := runCmd(t, finalCmd).(flowMsg)
	if final.block != nil {
		t.Fatal("the flow should be finished")
	}
	if !strings.Contains(final.entries[0].render(brandTheme), "permanently") {
		t.Fatal("the transcript should say the deletion was permanent")
	}
}

// Restore stays the resting choice, so the cursor's default position is the
// reversible one and reaching deletion takes a deliberate move.
func TestBatchActionDefaultsToRestore(t *testing.T) {
	deps := testDeps(t, t.TempDir())
	batch := &deletionengine.Batch{BatchID: "test-batch",
		Items: []deletionengine.StagedItem{{OriginalPath: "/a", StagedName: "0000-a"}}}

	actions := newBatchActionBlock(deps, brandTheme, batch)
	if actions.cursor != 0 {
		t.Fatal("the cursor should rest on restore")
	}
	if !strings.Contains(actions.rows[0], "Restore") {
		t.Fatalf("the first choice should be restore, got %q", actions.rows[0])
	}
	if !strings.Contains(actions.rows[1], "permanently") {
		t.Fatalf("the second choice should name permanence, got %q", actions.rows[1])
	}
}

// Cancelling the gate must leave the batch intact and restorable.
func TestCancellingTheGateLeavesTheBatchStaged(t *testing.T) {
	home := t.TempDir()
	target := filepath.Join(home, "cache-entry")
	writeTestFile(t, target, "payload")
	deps := testDeps(t, home)

	manifest, err := deletionengine.Plan(
		[]deletionengine.Candidate{{Path: target, RuleID: "r", Reason: "x"}},
		deletionengine.PlanOptions{Command: "clean", Policy: deps.Rules, Log: deps.Log},
	)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	area, _ := deps.newStagingArea()
	if _, err := deletionengine.Apply(manifest, deletionengine.ApplyOptions{
		Staging: area, Policy: deps.Rules, Log: deps.Log,
	}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	batches, _ := area.ListBatches()

	actions := newBatchActionBlock(deps, brandTheme, batches[0])
	actions.Update(tea.KeyMsg{Type: tea.KeyDown})
	_, chooseCmd := actions.Update(tea.KeyMsg{Type: tea.KeyEnter})
	flow := runCmd(t, chooseCmd).(flowMsg)
	gate := flow.block.(*confirmWordBlock)

	_, cancelCmd := gate.Update(tea.KeyMsg{Type: tea.KeyEsc})
	flow = runCmd(t, cancelCmd).(flowMsg)
	if flow.block != nil {
		t.Fatal("cancelling should end the flow")
	}

	still, err := area.ListBatches()
	if err != nil || len(still) != 1 {
		t.Fatalf("the batch should still be staged, got %d, %v", len(still), err)
	}
}
