package terminalshell

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	spacescan "github.com/lesliemunmus/wtff/internal/space-scan"
)

// spaceTree builds a home with a known shape and returns deps pointed at it.
func spaceTree(t *testing.T, layout map[string]string) (*Deps, string) {
	t.Helper()
	home := t.TempDir()
	for rel, contents := range layout {
		full := filepath.Join(home, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("setup: %v", err)
		}
		if err := os.WriteFile(full, []byte(contents), 0o600); err != nil {
			t.Fatalf("setup: %v", err)
		}
	}
	return testDeps(t, home), home
}

func scanHome(t *testing.T, home string) *spacescan.Node {
	t.Helper()
	result, err := spacescan.Scan(spacescan.Options{Root: home})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	return result.Root
}

func TestSpaceIsAvailableFromThePrompt(t *testing.T) {
	deps := testDeps(t, t.TempDir())
	app := newTestApp(t, deps)

	app = typeInto(app, "space")
	app, cmd := pressEnter(app)
	if cmd == nil || app.block == nil {
		t.Fatal("space should start a flow")
	}
	if _, ok := app.block.(*spaceScanBlock); !ok {
		t.Fatalf("expected a scan block, got %T", app.block)
	}
}

// Nothing starts selected. Clean proposes a set it can justify from a
// catalog; this only shows what is there, and pre-selecting a person's own
// files because they are large would be an opinion the tool has no basis for.
func TestNothingIsPreselected(t *testing.T) {
	deps, home := spaceTree(t, map[string]string{
		"big/file.bin":   strings.Repeat("x", 5000),
		"small/file.txt": strings.Repeat("x", 10),
	})

	block := newSpaceBrowseBlock(deps, brandTheme, scanHome(t, home))
	for _, item := range block.list.items {
		if item.selected {
			t.Fatalf("%q started selected", item.label)
		}
	}
}

func TestBrowsingDescendsAndReturns(t *testing.T) {
	deps, home := spaceTree(t, map[string]string{
		"outer/inner/deep.txt": strings.Repeat("x", 100),
	})

	root := scanHome(t, home)
	block := newSpaceBrowseBlock(deps, brandTheme, root)
	if block.current.Path() != root.Path() {
		t.Fatal("browsing should start at the scan root")
	}

	// Right descends into the directory under the cursor.
	next, _ := block.Update(tea.KeyMsg{Type: tea.KeyRight})
	descended, ok := next.(*spaceBrowseBlock)
	if !ok {
		t.Fatalf("expected to stay in the browser, got %T", next)
	}
	if filepath.Base(descended.current.Path()) != "outer" {
		t.Fatalf("descended into %q, want outer", descended.current.Path())
	}

	// Left returns to the parent.
	back, _ := descended.Update(tea.KeyMsg{Type: tea.KeyLeft})
	returned := back.(*spaceBrowseBlock)
	if returned.current.Path() != root.Path() {
		t.Fatalf("left returned to %q, want the root", returned.current.Path())
	}
}

// Escape walks up before it leaves, so descending several levels and reaching
// for the key that means "back" does not throw someone out of the whole flow.
func TestEscapeWalksUpBeforeLeaving(t *testing.T) {
	deps, home := spaceTree(t, map[string]string{
		"outer/inner/deep.txt": strings.Repeat("x", 100),
	})

	root := scanHome(t, home)
	inner := newSpaceBrowseBlock(deps, brandTheme,
		root.Children[0].Children[0])

	// From a nested directory, escape climbs rather than exits.
	next, cmd := inner.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd != nil {
		t.Fatal("escape from a nested directory should not end the flow")
	}
	if _, ok := next.(*spaceBrowseBlock); !ok {
		t.Fatalf("expected to stay in the browser, got %T", next)
	}

	// At the top, escape leaves.
	atRoot := newSpaceBrowseBlock(deps, brandTheme, root)
	_, exitCmd := atRoot.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if exitCmd == nil {
		t.Fatal("escape at the top should end the flow")
	}
	flow := runCmd(t, exitCmd).(flowMsg)
	if flow.block != nil {
		t.Fatal("leaving the browser should clear the live block")
	}
}

func TestOpeningANonDirectoryIsRefusedWithAReason(t *testing.T) {
	deps, home := spaceTree(t, map[string]string{"file.txt": strings.Repeat("x", 100)})

	block := newSpaceBrowseBlock(deps, brandTheme, scanHome(t, home))
	next, _ := block.Update(tea.KeyMsg{Type: tea.KeyRight})
	browser := next.(*spaceBrowseBlock)
	if browser.note == "" {
		t.Fatal("trying to open a file should explain why it did not open")
	}
	if browser.current.Name != block.current.Name {
		t.Fatal("a refused open should not move")
	}
}

func TestStagingWithNothingSelectedIsRefused(t *testing.T) {
	deps, home := spaceTree(t, map[string]string{"file.txt": strings.Repeat("x", 100)})

	block := newSpaceBrowseBlock(deps, brandTheme, scanHome(t, home))
	next, cmd := block.Update(selectListConfirmMsg{})
	if cmd != nil {
		t.Fatal("an empty selection must not advance the flow")
	}
	if next.(*spaceBrowseBlock).note == "" {
		t.Fatal("the block should say why it refused")
	}
}

// The property the whole feature rests on: a path chosen by hand in this
// browser goes through the same Plan call as clean's catalog candidates, so
// the structural floor and every protection rule apply identically. A person
// can select their own keychain here; the engine is what refuses it.
func TestManualSelectionStillPassesThroughProtectionRules(t *testing.T) {
	home := t.TempDir()
	keychain := filepath.Join(home, "Library", "Keychains")
	writeTestFile(t, filepath.Join(keychain, "login.keychain-db"), "credential material")
	deps := testDeps(t, home)

	plan := newSpacePlanBlock(deps, brandTheme, []string{keychain})
	done := resolveBatch(t, plan.Init(), func(m tea.Msg) bool {
		_, ok := m.(scanDoneMsg)
		return ok
	}).(scanDoneMsg)
	if done.err != nil {
		t.Fatalf("plan: %v", done.err)
	}

	if len(done.manifest.Entries) != 0 {
		t.Fatalf("a protected path selected by hand reached the manifest: %+v",
			done.manifest.Entries)
	}
	if done.skipped == 0 {
		t.Fatal("the refusal should have been counted as a skip")
	}
	if _, err := os.Stat(keychain); err != nil {
		t.Fatal("the protected directory must be untouched")
	}
}

// An ordinary selection reaches the same checklist clean uses, which is what
// keeps staging, undo, and the disclosure toggle identical across commands.
func TestOrdinarySelectionReachesTheSharedChecklist(t *testing.T) {
	deps, home := spaceTree(t, map[string]string{
		"junk/big.bin": strings.Repeat("x", 4096),
	})

	plan := newSpacePlanBlock(deps, brandTheme, []string{filepath.Join(home, "junk")})
	done := resolveBatch(t, plan.Init(), func(m tea.Msg) bool {
		_, ok := m.(scanDoneMsg)
		return ok
	}).(scanDoneMsg)
	if done.err != nil {
		t.Fatalf("plan: %v", done.err)
	}

	_, cmd := plan.Update(done)
	flow := runCmd(t, cmd).(flowMsg)
	selection, ok := flow.block.(*selectionBlock)
	if !ok {
		t.Fatalf("expected the shared selection block, got %T", flow.block)
	}
	// Staged, not purged: a manual selection is as reversible as everything
	// else, and must not quietly become permanent because it was chosen by
	// hand rather than proposed by the catalog.
	if selection.manifest.Action != "stage" {
		t.Fatalf("action = %q, a hand picked selection must still be staged",
			selection.manifest.Action)
	}
}

// A directory whose total is a floor says so on its row, since that number is
// what a person is about to make a deletion decision against.
func TestPartialTotalsAreLabelledInTheList(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("running as root, which bypasses the permission under test")
	}
	home := t.TempDir()
	locked := filepath.Join(home, "locked")
	writeTestFile(t, filepath.Join(locked, "hidden.txt"), strings.Repeat("x", 500))
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatalf("setup: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	deps := testDeps(t, home)
	block := newSpaceBrowseBlock(deps, brandTheme, scanHome(t, home))

	var found bool
	for _, item := range block.list.items {
		if item.label == "locked" {
			found = true
			if !strings.Contains(item.detail, "floor") {
				t.Fatalf("an unreadable directory should say its total is a floor, got %q",
					item.detail)
			}
		}
	}
	if !found {
		t.Fatal("the unreadable directory should still be listed")
	}
}

func TestDisplayPathShortensTheHomePrefix(t *testing.T) {
	if got := displayPath("/Users/someone/Downloads", "/Users/someone"); got != "~/Downloads" {
		t.Fatalf("displayPath = %q, want ~/Downloads", got)
	}
	if got := displayPath("/Volumes/Drive/x", "/Users/someone"); got != "/Volumes/Drive/x" {
		t.Fatalf("a path outside home should be left alone, got %q", got)
	}
}
