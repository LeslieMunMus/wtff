package terminalshell

import (
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	cleancatalog "github.com/lesliemusengi/wtff/internal/clean-catalog"
	deletionengine "github.com/lesliemusengi/wtff/internal/deletion-engine"
	operationlog "github.com/lesliemusengi/wtff/internal/operation-log"
	protectionrules "github.com/lesliemusengi/wtff/internal/protection-rules"
)

// testDeps builds a Deps rooted at an isolated home directory.
//
// Setting HOME is not optional here, and forgetting it is not a harmless
// gap: DefaultStagingRoot and DefaultPath, used by every screen that stages
// or logs anything, always resolve against the real process environment,
// deliberately, the same way every command in internal/cli already works.
// deps.Home only ever affects candidate discovery and protection rule
// matching. A test that set deps.Home without also setting the environment
// variable would still touch the real machine's actual staging area for
// every apply, which is exactly what happened building this test: a
// synthetic file from a run of this suite was found staged for real in
// ~/Library/Application Support/wtff/staging afterward and had to be
// removed by hand. t.Setenv here is not a formality; it is the fix.
func testDeps(t *testing.T, home string) *Deps {
	t.Helper()
	t.Setenv("HOME", home)
	rules, err := protectionrules.LoadBuiltinForHome(home)
	if err != nil {
		t.Fatalf("loading rules: %v", err)
	}
	catalog, err := cleancatalog.LoadBuiltin()
	if err != nil {
		t.Fatalf("loading catalog: %v", err)
	}
	return &Deps{Home: home, Rules: rules, Catalog: catalog, Log: operationlog.Discard()}
}

func writeTestFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}
}

// The full plan-and-apply flow, driven the same way the real program drives
// it: Init produces a command, running that command produces a message,
// feeding that message into Update produces the next screen, and so on
// through to a real file being staged. This is the shell equivalent of the
// stage-and-undo round trip tests internal/cli already runs, proving the
// screen wiring against real deletion engine behavior rather than only
// against its own mocks.
func TestPlanFlowEndToEndStagesARealFile(t *testing.T) {
	home := t.TempDir()
	target := filepath.Join(home, "cache-entry")
	writeTestFile(t, target, "reclaimable content")
	deps := testDeps(t, home)

	fakePlan := func(d *Deps) (*deletionengine.Manifest, int, error) {
		manifest, err := deletionengine.Plan(
			[]deletionengine.Candidate{{Path: target, RuleID: "test-rule", Reason: "test candidate"}},
			deletionengine.PlanOptions{Command: "test", Policy: d.Rules, Log: d.Log, MeasureSizes: true},
		)
		return manifest, 0, err
	}

	discovering := newPlanDiscoveringScreen(deps, "Test", fakePlan)
	msg := runCmd(t, discovering.Init())
	ready, ok := msg.(planReadyMsg)
	if !ok || ready.err != nil {
		t.Fatalf("planReadyMsg = %+v, ok=%v", ready, ok)
	}

	next, _ := discovering.Update(ready)
	list, ok := next.(*candidateListScreen)
	if !ok {
		t.Fatalf("expected *candidateListScreen, got %T", next)
	}
	if len(list.manifest.Entries) != 1 {
		t.Fatalf("planned %d entries, want 1", len(list.manifest.Entries))
	}

	// Item starts selected by default. Bubble Tea's real event loop is two
	// passes here, not one: pressing enter returns a command from this
	// Update call, the runtime executes that command to get
	// selectListConfirmMsg, and only a second Update call, fed that message,
	// contains the logic that pushes the confirm screen. A test that called
	// Update once with the key press and expected the push immediately would
	// be testing a flow that does not match how the real program drives it,
	// which is exactly the kind of mismatch that has produced false passes
	// elsewhere in this project.
	_, firstCmd := list.Update(keyMsg("enter"))
	confirmListMsg := runCmd(t, firstCmd)
	if _, ok := confirmListMsg.(selectListConfirmMsg); !ok {
		t.Fatalf("first pass produced %T, want selectListConfirmMsg", confirmListMsg)
	}

	_, secondCmd := list.Update(confirmListMsg)
	pushMsg := runCmd(t, secondCmd)
	pushed, ok := pushMsg.(pushScreenMsg)
	if !ok {
		t.Fatalf("second pass produced %T, want pushScreenMsg", pushMsg)
	}
	confirm, ok := pushed.screen.(*confirmScreen)
	if !ok {
		t.Fatalf("pushed screen type = %T, want *confirmScreen", pushed.screen)
	}
	if len(confirm.manifest.Entries) != 1 {
		t.Fatalf("confirm manifest has %d entries, want 1", len(confirm.manifest.Entries))
	}

	afterYes, _ := confirm.Update(keyMsg("y"))
	applying, ok := afterYes.(*applyingScreen)
	if !ok {
		t.Fatalf("expected *applyingScreen after confirming, got %T", afterYes)
	}

	applyMsg := runCmd(t, applying.Init())
	applyReady, ok := applyMsg.(applyReadyMsg)
	if !ok || applyReady.err != nil {
		t.Fatalf("applyReadyMsg = %+v, ok=%v", applyReady, ok)
	}
	if applyReady.result.AppliedCount != 1 {
		t.Fatalf("applied %d, want 1", applyReady.result.AppliedCount)
	}

	afterApply, _ := applying.Update(applyReady)
	results, ok := afterApply.(*resultsScreen)
	if !ok {
		t.Fatalf("expected *resultsScreen, got %T", afterApply)
	}
	if results.result.AppliedCount != 1 {
		t.Fatalf("results applied count = %d, want 1", results.result.AppliedCount)
	}

	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatal("the file was not actually staged away")
	}
}

// A protected path must never even reach the selectable list, the same
// guarantee internal/cli's clean and remove commands already prove: the
// engine's own protection check runs regardless of which presentation layer
// is driving it.
func TestPlanFlowRespectsProtectionRules(t *testing.T) {
	home := t.TempDir()
	keychainDir := filepath.Join(home, "Library", "Keychains")
	writeTestFile(t, filepath.Join(keychainDir, "login.keychain-db"), "credential material")
	deps := testDeps(t, home)

	fakePlan := func(d *Deps) (*deletionengine.Manifest, int, error) {
		var skipped int
		manifest, err := deletionengine.Plan(
			[]deletionengine.Candidate{{Path: keychainDir, RuleID: "test-rule", Reason: "test candidate"}},
			deletionengine.PlanOptions{
				Command: "test", Policy: d.Rules, Log: d.Log, MeasureSizes: true,
				SkipSink: func(string, string) { skipped++ },
			},
		)
		return manifest, skipped, err
	}

	discovering := newPlanDiscoveringScreen(deps, "Test", fakePlan)
	ready := runCmd(t, discovering.Init()).(planReadyMsg)
	next, _ := discovering.Update(ready)
	list := next.(*candidateListScreen)

	if len(list.manifest.Entries) != 0 {
		t.Fatalf("a protected path reached the candidate list: %+v", list.manifest.Entries)
	}
	if list.skippedCount != 1 {
		t.Fatalf("skipped count = %d, want 1", list.skippedCount)
	}
}

// Pressing enter with nothing selected must not push a confirm screen; it
// should tell the person to select something instead. This needs a real
// item that exists but starts deselected: an empty list short circuits
// inside selectList.update before it ever reaches the enter case at all,
// which would make this test pass without ever touching the refusal logic
// it exists to check.
func TestCandidateListRefusesToConfirmWithNothingSelected(t *testing.T) {
	home := t.TempDir()
	deps := testDeps(t, home)

	manifest := &deletionengine.Manifest{
		Version: 1, Command: "test", Action: deletionengine.ActionStage,
		Entries: []deletionengine.Entry{{ResolvedPath: "/a", RuleID: "r", Reason: "x"}},
	}
	manifest.Seal()
	list := newCandidateListScreen(deps, "Test", manifest, 0)
	list.list.items[0].selected = false

	// First pass: enter still produces selectListConfirmMsg, since that part
	// of selectList's own logic does not know or care whether anything is
	// selected.
	_, firstCmd := list.Update(keyMsg("enter"))
	confirmListMsg := runCmd(t, firstCmd)
	if confirm, ok := confirmListMsg.(selectListConfirmMsg); !ok || len(confirm.selected) != 0 {
		t.Fatalf("first pass = %+v, want selectListConfirmMsg with nothing selected", confirmListMsg)
	}

	// Second pass is where candidateListScreen's own refusal lives.
	_, secondCmd := list.Update(confirmListMsg)
	if secondCmd == nil {
		t.Fatal("expected a status command explaining the refusal")
	}
	statusResult := secondCmd()
	status, ok := statusResult.(statusMsg)
	if !ok {
		t.Fatalf("second pass produced %T, want statusMsg", statusResult)
	}
	if !status.isError {
		t.Fatal("refusing to confirm with nothing selected should be reported as an error status")
	}
}

// candidateListScreen's Esc must pop, not reset to menu, so it works
// correctly whether it is sitting directly above the menu (clean) or above a
// search screen (uninstall's leftover step). This is the specific defect
// found and fixed while wiring the uninstall flow: reset-to-menu would have
// skipped past the search screen it should return to.
func TestCandidateListEscPopsRatherThanResettingToMenu(t *testing.T) {
	deps := testDeps(t, t.TempDir())
	manifest := &deletionengine.Manifest{Version: 1, Command: "test", Action: deletionengine.ActionStage}
	manifest.Seal()
	list := newCandidateListScreen(deps, "Test", manifest, 0)

	_, cmd := list.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("esc produced no command")
	}
	if _, ok := cmd().(popScreenMsg); !ok {
		t.Fatalf("esc produced %T, want popScreenMsg", cmd())
	}
}

func TestFilterManifestProducesAnIndependentlyValidManifest(t *testing.T) {
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
	if len(filtered.Entries) != 2 {
		t.Fatalf("filtered has %d entries, want 2", len(filtered.Entries))
	}
	if filtered.TotalBytes != 40 {
		t.Fatalf("filtered total = %d, want 40", filtered.TotalBytes)
	}
	if err := filtered.VerifyDigest(); err != nil {
		t.Fatalf("filtered manifest does not verify: %v", err)
	}
	if filtered.Digest == original.Digest {
		t.Fatal("filtered manifest has the same digest as the original despite fewer entries")
	}
}
