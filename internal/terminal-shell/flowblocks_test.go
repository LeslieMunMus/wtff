package terminalshell

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	deletionengine "github.com/lesliemunmus/wtff/internal/deletion-engine"
)

// resolveBatch runs a command and, when it produces a tea.BatchMsg, runs
// every sub-command and returns the first result matching want.
//
// Blocks batch their real work alongside a spinner tick in Init, so a single
// call does not yield the work's own message directly. Batched commands
// carry no ordering guarantee, hence matching by type rather than position.
func resolveBatch(t *testing.T, cmd tea.Cmd, want func(tea.Msg) bool) tea.Msg {
	t.Helper()
	msg := runCmd(t, cmd)
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		if !want(msg) {
			t.Fatalf("single command produced %T, not what the caller wanted", msg)
		}
		return msg
	}
	for _, sub := range batch {
		if sub == nil {
			continue
		}
		if result := sub(); want(result) {
			return result
		}
	}
	t.Fatal("no sub-command in the batch produced the expected message")
	return nil
}

func isFlowMsg(m tea.Msg) bool {
	_, ok := m.(flowMsg)
	return ok
}

// The full clean flow through the blocks, against a real file: scan,
// select, stage. This is the shell equivalent of the round-trip tests the
// CLI package already runs, proving the block wiring against real deletion
// engine behavior rather than mocks.
func TestCleanFlowStagesARealFile(t *testing.T) {
	home := t.TempDir()
	target := filepath.Join(home, "cache-entry")
	writeTestFile(t, target, "reclaimable content")
	deps := testDeps(t, home)

	plan := func(d *Deps, _ func(done, total int)) (*deletionengine.Manifest, int, error) {
		manifest, err := deletionengine.Plan(
			[]deletionengine.Candidate{{Path: target, RuleID: "test-rule", Reason: "test candidate"}},
			deletionengine.PlanOptions{Command: "test", Policy: d.Rules, Log: d.Log, MeasureSizes: true},
		)
		return manifest, 0, err
	}

	scan := newScanBlock(deps, brandTheme, "clean", plan)
	done := resolveBatch(t, scan.Init(), func(m tea.Msg) bool {
		_, ok := m.(scanDoneMsg)
		return ok
	}).(scanDoneMsg)
	if done.err != nil {
		t.Fatalf("scan failed: %v", done.err)
	}

	_, cmd := scan.Update(done)
	flow := runCmd(t, cmd).(flowMsg)
	selection, ok := flow.block.(*selectionBlock)
	if !ok {
		t.Fatalf("expected a selection block, got %T", flow.block)
	}
	if len(selection.manifest.Entries) != 1 {
		t.Fatalf("planned %d entries, want 1", len(selection.manifest.Entries))
	}

	// Items start selected. Enter is two passes in Bubble Tea: the key press
	// returns a command, running it yields selectListConfirmMsg, and only a
	// second Update acts on it.
	_, first := selection.Update(tea.KeyMsg{Type: tea.KeyEnter})
	confirm := runCmd(t, first)
	if _, ok := confirm.(selectListConfirmMsg); !ok {
		t.Fatalf("first pass produced %T, want selectListConfirmMsg", confirm)
	}

	_, second := selection.Update(confirm)
	flow = runCmd(t, second).(flowMsg)
	apply, ok := flow.block.(*applyBlock)
	if !ok {
		t.Fatalf("expected an apply block, got %T", flow.block)
	}

	applyDone := resolveBatch(t, apply.Init(), func(m tea.Msg) bool {
		_, ok := m.(applyDoneMsg)
		return ok
	}).(applyDoneMsg)
	if applyDone.err != nil {
		t.Fatalf("apply failed: %v", applyDone.err)
	}
	if applyDone.result.AppliedCount != 1 {
		t.Fatalf("applied %d, want 1", applyDone.result.AppliedCount)
	}

	_, finalCmd := apply.Update(applyDone)
	final := runCmd(t, finalCmd).(flowMsg)
	if final.block != nil {
		t.Fatal("the flow should be finished")
	}
	if len(final.entries) == 0 {
		t.Fatal("the flow should report its outcome into the transcript")
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatal("the file was not staged away")
	}
}

// A protected path must never reach the selection block, the same guarantee
// the CLI already proves: the engine's protection check runs regardless of
// which presentation layer drives it.
func TestCleanFlowRespectsProtectionRules(t *testing.T) {
	home := t.TempDir()
	keychain := filepath.Join(home, "Library", "Keychains")
	writeTestFile(t, filepath.Join(keychain, "login.keychain-db"), "credential material")
	deps := testDeps(t, home)

	plan := func(d *Deps, _ func(done, total int)) (*deletionengine.Manifest, int, error) {
		var skipped int
		manifest, err := deletionengine.Plan(
			[]deletionengine.Candidate{{Path: keychain, RuleID: "test-rule", Reason: "test candidate"}},
			deletionengine.PlanOptions{
				Command: "test", Policy: d.Rules, Log: d.Log, MeasureSizes: true,
				SkipSink: func(string, string) { skipped++ },
			},
		)
		return manifest, skipped, err
	}

	scan := newScanBlock(deps, brandTheme, "clean", plan)
	done := resolveBatch(t, scan.Init(), func(m tea.Msg) bool {
		_, ok := m.(scanDoneMsg)
		return ok
	}).(scanDoneMsg)

	_, cmd := scan.Update(done)
	flow := runCmd(t, cmd).(flowMsg)
	if flow.block != nil {
		t.Fatalf("a protected path reached a live block: %T", flow.block)
	}
	if len(flow.entries) == 0 {
		t.Fatal("the flow should report that nothing was eligible")
	}
	if _, err := os.Stat(keychain); err != nil {
		t.Fatal("the protected directory must be untouched")
	}
}

// Cancelling the selection ends the flow without staging anything.
func TestSelectionEscapeCancelsWithoutStaging(t *testing.T) {
	home := t.TempDir()
	target := filepath.Join(home, "cache-entry")
	writeTestFile(t, target, "payload")
	deps := testDeps(t, home)

	manifest, err := deletionengine.Plan(
		[]deletionengine.Candidate{{Path: target, RuleID: "r", Reason: "x"}},
		deletionengine.PlanOptions{Command: "test", Policy: deps.Rules, Log: deps.Log},
	)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}

	selection := newSelectionBlock(deps, brandTheme, "clean", manifest)
	_, cmd := selection.Update(tea.KeyMsg{Type: tea.KeyEsc})
	flow := runCmd(t, cmd).(flowMsg)

	if flow.block != nil {
		t.Fatal("escape should end the flow")
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatal("cancelling must not stage anything")
	}
}

// Enter with nothing selected refuses rather than staging an empty set.
func TestSelectionRefusesAnEmptySelection(t *testing.T) {
	deps := testDeps(t, t.TempDir())
	manifest := &deletionengine.Manifest{
		Version: 1, Command: "test", Action: deletionengine.ActionStage,
		Entries: []deletionengine.Entry{{ResolvedPath: "/a", RuleID: "r", Reason: "x"}},
	}
	manifest.Seal()

	selection := newSelectionBlock(deps, brandTheme, "clean", manifest)
	selection.list.items[0].selected = false

	_, first := selection.Update(tea.KeyMsg{Type: tea.KeyEnter})
	confirm := runCmd(t, first)
	updated, second := selection.Update(confirm)

	if second != nil {
		t.Fatal("an empty selection must not advance the flow")
	}
	if block, ok := updated.(*selectionBlock); !ok || block.note == "" {
		t.Fatal("the block should explain why it refused")
	}
}

// filterManifest must produce an independently valid manifest, since Apply
// verifies the digest before acting on it.
func TestFilterManifestReseals(t *testing.T) {
	original := &deletionengine.Manifest{
		Version: 1, Command: "test", Action: deletionengine.ActionStage,
		Entries: []deletionengine.Entry{
			{ResolvedPath: "/a", RuleID: "r", Reason: "x", SizeBytes: 10, SizeKnown: true},
			{ResolvedPath: "/b", RuleID: "r", Reason: "x", SizeBytes: 20, SizeKnown: true},
			{ResolvedPath: "/c", RuleID: "r", Reason: "x", SizeBytes: 30, SizeKnown: true},
		},
	}
	original.Seal()

	filtered := filterManifest(original, []int{0, 2})
	if len(filtered.Entries) != 2 || filtered.TotalBytes != 40 {
		t.Fatalf("filtered = %d entries, %d bytes", len(filtered.Entries), filtered.TotalBytes)
	}
	if err := filtered.VerifyDigest(); err != nil {
		t.Fatalf("filtered manifest does not verify: %v", err)
	}
	if filtered.Digest == original.Digest {
		t.Fatal("a narrowed manifest must not keep the original digest")
	}
}

// An empty staging area reports into the transcript and ends, rather than
// leaving an empty picker pinned above the prompt.
func TestStagedFlowReportsAnEmptyArea(t *testing.T) {
	deps := testDeps(t, t.TempDir())
	block := startStagedFlow(deps, brandTheme)

	loaded := resolveBatch(t, block.Init(), func(m tea.Msg) bool {
		_, ok := m.(batchesLoadedMsg)
		return ok
	}).(batchesLoadedMsg)
	if loaded.err != nil {
		t.Fatalf("listing an empty staging area: %v", loaded.err)
	}

	_, cmd := block.Update(loaded)
	flow := runCmd(t, cmd).(flowMsg)
	if flow.block != nil {
		t.Fatal("an empty staging area should end the flow")
	}
	if !strings.Contains(flow.entries[0].render(brandTheme), "Nothing is staged") {
		t.Fatal("the transcript should say nothing is staged")
	}
}

// The full staged round trip: stage a file, then restore it through the
// flow's own blocks.
func TestStagedFlowRestoresARealBatch(t *testing.T) {
	home := t.TempDir()
	target := filepath.Join(home, "cache-entry")
	writeTestFile(t, target, "reclaimable content")
	deps := testDeps(t, home)

	manifest, err := deletionengine.Plan(
		[]deletionengine.Candidate{{Path: target, RuleID: "r", Reason: "x"}},
		deletionengine.PlanOptions{Command: "test", Policy: deps.Rules, Log: deps.Log, MeasureSizes: true},
	)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	root, err := deletionengine.DefaultStagingRoot()
	if err != nil {
		t.Fatalf("staging root: %v", err)
	}
	area, err := deletionengine.NewStagingArea(root)
	if err != nil {
		t.Fatalf("staging area: %v", err)
	}
	if _, err := deletionengine.Apply(manifest, deletionengine.ApplyOptions{
		Staging: area, Policy: deps.Rules, Log: deps.Log,
	}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatal("setup: the file should have been staged away")
	}

	block := startStagedFlow(deps, brandTheme)
	loaded := resolveBatch(t, block.Init(), func(m tea.Msg) bool {
		_, ok := m.(batchesLoadedMsg)
		return ok
	}).(batchesLoadedMsg)

	_, cmd := block.Update(loaded)
	flow := runCmd(t, cmd).(flowMsg)
	picker, ok := flow.block.(*pickBlock)
	if !ok {
		t.Fatalf("expected a batch picker, got %T", flow.block)
	}

	_, chooseCmd := picker.Update(tea.KeyMsg{Type: tea.KeyEnter})
	flow = runCmd(t, chooseCmd).(flowMsg)
	actions, ok := flow.block.(*pickBlock)
	if !ok {
		t.Fatalf("expected the restore-or-delete choice, got %T", flow.block)
	}

	// Restore is the first choice, so the cursor's resting position is the
	// reversible one. Reaching permanent deletion takes a deliberate move.
	_, restoreCmd := actions.Update(tea.KeyMsg{Type: tea.KeyEnter})
	flow = runCmd(t, restoreCmd).(flowMsg)
	undo, ok := flow.block.(*undoBlock)
	if !ok {
		t.Fatalf("expected an undo block, got %T", flow.block)
	}

	undoDone := resolveBatch(t, undo.Init(), func(m tea.Msg) bool {
		_, ok := m.(undoDoneMsg)
		return ok
	}).(undoDoneMsg)
	if undoDone.err != nil {
		t.Fatalf("undo: %v", undoDone.err)
	}
	if undoDone.result.RestoredCount != 1 {
		t.Fatalf("restored %d, want 1", undoDone.result.RestoredCount)
	}

	contents, err := os.ReadFile(target)
	if err != nil || string(contents) != "reclaimable content" {
		t.Fatalf("restored contents = %q, %v", contents, err)
	}
}

// An unmatched uninstall query reports into the transcript and ends.
func TestUninstallReportsNoMatch(t *testing.T) {
	deps := testDeps(t, t.TempDir())
	cmd := handleResolved(deps, brandTheme, appResolvedMsg{query: "Nonexistent"})
	flow := runCmd(t, cmd).(flowMsg)

	if flow.block != nil {
		t.Fatal("an unmatched query should end the flow")
	}
	if !strings.Contains(flow.entries[0].render(brandTheme), "no installed application matches") {
		t.Fatal("the transcript should report the failed match")
	}
}
