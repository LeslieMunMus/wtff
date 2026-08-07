package terminalshell

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	duplicatescan "github.com/lesliemunmus/wtff/internal/duplicate-scan"
)

func dupGroup(t *testing.T, home string, paths ...string) duplicatescan.Group {
	t.Helper()
	group := duplicatescan.Group{Size: 8 * 1024, Digest: "test"}
	base := time.Now().Add(-time.Duration(len(paths)) * time.Hour)
	for i, rel := range paths {
		full := filepath.Join(home, rel)
		writeTestFile(t, full, strings.Repeat("x", 8*1024))
		group.Files = append(group.Files, duplicatescan.File{
			Path: full, Size: 8 * 1024, ModTime: base.Add(time.Duration(i) * time.Hour),
		})
	}
	return group
}

func TestDuplicatesIsAvailableFromThePrompt(t *testing.T) {
	deps := testDeps(t, t.TempDir())
	app := newTestApp(t, deps)

	app = typeInto(app, "duplicates")
	app, cmd := pressEnter(app)
	if cmd == nil || app.block == nil {
		t.Fatal("duplicates should start a flow")
	}
	if _, ok := app.block.(*duplicateScanBlock); !ok {
		t.Fatalf("expected a scan block, got %T", app.block)
	}
}

// The oldest is labelled rather than hidden, since someone deciding what to
// remove needs to see which copy a merge would keep in place.
func TestTheOldestCopyIsLabelled(t *testing.T) {
	home := t.TempDir()
	deps := testDeps(t, home)
	group := dupGroup(t, home, "Documents/report.pdf", "Downloads/report.pdf")

	block := newDuplicateCopiesBlock(deps, brandTheme, group)
	if !strings.Contains(block.list.items[0].detail, "oldest") {
		t.Fatalf("the first row should say it is the oldest, got %q",
			block.list.items[0].detail)
	}
	if strings.Contains(block.list.items[1].detail, "oldest") {
		t.Fatalf("only the first row is the oldest, got %q", block.list.items[1].detail)
	}
}

// Nothing is preselected, for the same reason as the space browser: the tool
// is showing what is there, not proposing an answer.
func TestNoCopyIsPreselected(t *testing.T) {
	home := t.TempDir()
	deps := testDeps(t, home)
	group := dupGroup(t, home, "a/f.bin", "b/f.bin")

	block := newDuplicateCopiesBlock(deps, brandTheme, group)
	for _, item := range block.list.items {
		if item.selected {
			t.Fatalf("%q started selected", item.label)
		}
	}
}

// Staging every copy removes the file entirely, which is almost certainly not
// what someone browsing duplicates meant. Refusing beats a confirmation
// nobody reads.
func TestStagingEveryCopyIsRefused(t *testing.T) {
	home := t.TempDir()
	deps := testDeps(t, home)
	group := dupGroup(t, home, "a/f.bin", "b/f.bin")

	block := newDuplicateCopiesBlock(deps, brandTheme, group)
	next, cmd := block.Update(selectListConfirmMsg{selected: []int{0, 1}})
	if cmd != nil {
		t.Fatal("selecting every copy must not advance the flow")
	}
	note := next.(*duplicateCopiesBlock).note
	if !strings.Contains(note, "every copy") {
		t.Fatalf("the refusal should explain why, got %q", note)
	}
}

func TestStagingSomeCopiesIsAllowed(t *testing.T) {
	home := t.TempDir()
	deps := testDeps(t, home)
	group := dupGroup(t, home, "a/f.bin", "b/f.bin")

	block := newDuplicateCopiesBlock(deps, brandTheme, group)
	_, cmd := block.Update(selectListConfirmMsg{selected: []int{1}})
	if cmd == nil {
		t.Fatal("staging a surplus copy should advance the flow")
	}
	flow := runCmd(t, cmd).(flowMsg)
	if _, ok := flow.block.(*duplicateStageBlock); !ok {
		t.Fatalf("expected a stage block, got %T", flow.block)
	}
}

// A duplicate selected by hand is not a bypass: it goes through the same Plan
// call, policy, and staging as everything else.
func TestStagedDuplicatesGoThroughTheProtectionRules(t *testing.T) {
	home := t.TempDir()
	keychain := filepath.Join(home, "Library", "Keychains")
	writeTestFile(t, filepath.Join(keychain, "login.keychain-db"), "credentials")
	deps := testDeps(t, home)

	block := newDuplicateStageBlock(deps, brandTheme, []string{keychain})
	done := resolveBatch(t, block.Init(), func(m tea.Msg) bool {
		_, ok := m.(scanDoneMsg)
		return ok
	}).(scanDoneMsg)
	if done.err != nil {
		t.Fatalf("plan: %v", done.err)
	}
	if len(done.manifest.Entries) != 0 {
		t.Fatalf("a protected path reached the manifest: %+v", done.manifest.Entries)
	}
	if _, err := os.Stat(keychain); err != nil {
		t.Fatal("the protected directory must be untouched")
	}
}

// Staging a duplicate is reversible like every other removal, and must not
// quietly become permanent because it was chosen from this screen.
func TestStagedDuplicatesAreStagedNotPurged(t *testing.T) {
	home := t.TempDir()
	deps := testDeps(t, home)
	target := filepath.Join(home, "b", "f.bin")
	writeTestFile(t, target, strings.Repeat("x", 4096))

	block := newDuplicateStageBlock(deps, brandTheme, []string{target})
	done := resolveBatch(t, block.Init(), func(m tea.Msg) bool {
		_, ok := m.(scanDoneMsg)
		return ok
	}).(scanDoneMsg)
	if done.err != nil {
		t.Fatalf("plan: %v", done.err)
	}
	if done.manifest.Action != "stage" {
		t.Fatalf("action = %q, a duplicate must still be staged", done.manifest.Action)
	}
}

// Merge keeps every copy. The report says where each went, and nothing is
// removed, which is the whole distinction from staging.
func TestMergeMovesEveryCopyAndDeletesNothing(t *testing.T) {
	home := t.TempDir()
	deps := testDeps(t, home)
	group := dupGroup(t, home, "Documents/report.pdf", "Downloads/report.pdf")

	block := newDuplicateMergeBlock(deps, brandTheme, group)
	done := resolveBatch(t, block.Init(), func(m tea.Msg) bool {
		_, ok := m.(duplicateMergeDoneMsg)
		return ok
	}).(duplicateMergeDoneMsg)
	if done.err != nil {
		t.Fatalf("merge: %v", done.err)
	}
	if done.result.MovedCount != 1 {
		t.Fatalf("moved %d, want 1", done.result.MovedCount)
	}

	// Both copies still exist, in the oldest copy's directory.
	documents := filepath.Join(home, "Documents")
	for _, name := range []string{"report.pdf", "report copy.pdf"} {
		if _, err := os.Stat(filepath.Join(documents, name)); err != nil {
			t.Errorf("%s missing after merge: %v", name, err)
		}
	}

	_, cmd := block.Update(done)
	flow := runCmd(t, cmd).(flowMsg)
	if flow.block != nil {
		t.Fatal("the merge should end the flow")
	}

	// Where things went is the point, so it must be reported, and the report
	// must say nothing was deleted.
	var rendered string
	for _, entry := range flow.entries {
		rendered += entry.render(brandTheme) + "\n"
	}
	if !strings.Contains(rendered, "nothing was deleted") {
		t.Fatalf("the merge report should say nothing was deleted:\n%s", rendered)
	}
	if !strings.Contains(rendered, "report copy.pdf") {
		t.Fatalf("the report should name where the copy went:\n%s", rendered)
	}
}
