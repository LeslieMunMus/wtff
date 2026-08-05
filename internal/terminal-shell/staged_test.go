package terminalshell

import (
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	deletionengine "github.com/lesliemusengi/wtff/internal/deletion-engine"
	operationlog "github.com/lesliemusengi/wtff/internal/operation-log"
)

// The full staged-list-to-restore flow, against a real batch created by
// actually staging a file first, the same way TestPlanFlowEndToEndStagesARealFile
// proves the removal side. Restoring something that was never really staged
// would not prove anything about the code that restores real batches.
func TestStagedFlowEndToEndRestoresARealBatch(t *testing.T) {
	home := t.TempDir()
	target := filepath.Join(home, "cache-entry")
	writeTestFile(t, target, "reclaimable content")
	deps := testDeps(t, home)

	manifest, err := deletionengine.Plan(
		[]deletionengine.Candidate{{Path: target, RuleID: "r", Reason: "x"}},
		deletionengine.PlanOptions{Command: "test", Policy: deps.Rules, Log: deps.Log, MeasureSizes: true},
	)
	if err != nil || len(manifest.Entries) != 1 {
		t.Fatalf("setup plan: manifest=%+v err=%v", manifest, err)
	}

	// Staged at the same default root stagedListScreen itself reads from,
	// not an arbitrary path of this test's own choosing. testDeps sets HOME
	// to this test's isolated directory, so DefaultStagingRoot resolves
	// inside it rather than the real machine's actual staging area; using a
	// different root here would silently test a screen against data it can
	// never actually find.
	stagingRoot, err := deletionengine.DefaultStagingRoot()
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	staging, err := deletionengine.NewStagingArea(stagingRoot)
	if err != nil {
		t.Fatalf("setup staging: %v", err)
	}
	applyResult, err := deletionengine.Apply(manifest, deletionengine.ApplyOptions{
		Staging: staging, Policy: deps.Rules, Log: deps.Log,
	})
	if err != nil || applyResult.AppliedCount != 1 {
		t.Fatalf("setup apply: result=%+v err=%v", applyResult, err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatal("setup: file should have been staged away")
	}

	list := newStagedListScreen(deps)
	loadMsg := runCmd(t, list.Init())
	loaded, ok := loadMsg.(batchesLoadedMsg)
	if !ok || loaded.err != nil {
		t.Fatalf("batchesLoadedMsg = %+v, ok=%v", loaded, ok)
	}
	next, _ := list.Update(loaded)
	populated := next.(*stagedListScreen)
	if len(populated.batches) != 1 {
		t.Fatalf("batches = %+v, want 1", populated.batches)
	}

	_, pushCmd := populated.Update(tea.KeyMsg{Type: tea.KeyEnter})
	pushed := runCmd(t, pushCmd).(pushScreenMsg)
	confirm, ok := pushed.screen.(*undoConfirmScreen)
	if !ok {
		t.Fatalf("pushed screen = %T, want *undoConfirmScreen", pushed.screen)
	}

	afterYes, _ := confirm.Update(keyMsg("y"))
	applying, ok := afterYes.(*undoApplyingScreen)
	if !ok {
		t.Fatalf("expected *undoApplyingScreen, got %T", afterYes)
	}

	undoMsg := runCmd(t, applying.Init())
	undoReady, ok := undoMsg.(undoReadyMsg)
	if !ok || undoReady.err != nil {
		t.Fatalf("undoReadyMsg = %+v, ok=%v", undoReady, ok)
	}
	if undoReady.result.RestoredCount != 1 {
		t.Fatalf("restored count = %d, want 1", undoReady.result.RestoredCount)
	}

	afterUndo, _ := applying.Update(undoReady)
	results, ok := afterUndo.(*undoResultsScreen)
	if !ok {
		t.Fatalf("expected *undoResultsScreen, got %T", afterUndo)
	}
	if results.result.RestoredCount != 1 {
		t.Fatalf("results restored count = %d, want 1", results.result.RestoredCount)
	}

	contents, err := os.ReadFile(target)
	if err != nil || string(contents) != "reclaimable content" {
		t.Fatalf("restored contents = %q, %v", contents, err)
	}
}

func TestStagedListShowsEmptyStateWithoutError(t *testing.T) {
	home := t.TempDir()
	deps := testDeps(t, home)
	deps.Log = operationlog.Discard()

	list := newStagedListScreen(deps)
	loaded := runCmd(t, list.Init()).(batchesLoadedMsg)
	if loaded.err != nil {
		t.Fatalf("unexpected error listing an empty staging area: %v", loaded.err)
	}
	next, _ := list.Update(loaded)
	populated := next.(*stagedListScreen)
	if len(populated.batches) != 0 {
		t.Fatalf("batches = %+v, want none", populated.batches)
	}

	// Enter with nothing to select must not attempt to push anything.
	_, cmd := populated.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("enter with an empty batch list should not produce a command")
	}
}

func TestUndoConfirmScreenDeclineReturnsToList(t *testing.T) {
	deps := testDeps(t, t.TempDir())
	batch := &deletionengine.Batch{BatchID: "test-batch", Command: "test"}
	confirm := newUndoConfirmScreen(deps, batch)

	_, cmd := confirm.Update(keyMsg("n"))
	if cmd == nil {
		t.Fatal("declining should produce a command")
	}
	if _, ok := runCmd(t, cmd).(popScreenMsg); !ok {
		t.Fatal("declining should pop back to the staged list")
	}
}
