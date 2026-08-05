package terminalshell

import (
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	uninstallcore "github.com/lesliemusengi/wtff/internal/uninstall-core"
)

func writeTestApp(t *testing.T, appsRoot, name, bundleID string) string {
	t.Helper()
	appPath := filepath.Join(appsRoot, name+".app")
	plist := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
<key>CFBundleIdentifier</key>
<string>` + bundleID + `</string>
<key>CFBundleDisplayName</key>
<string>` + name + `</string>
</dict>
</plist>
`
	writeTestFile(t, filepath.Join(appPath, "Contents", "Info.plist"), plist)
	return appPath
}

func typeInto(s *uninstallSearchScreen, text string) {
	for _, r := range text {
		next, _ := s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		s = next.(*uninstallSearchScreen)
	}
}

// A single unambiguous match proceeds straight to the leftover plan flow,
// the same two-pass Bubble Tea sequence proven in the plan flow tests:
// Update returns a command, running it produces a message, and a second
// Update call with that message is what actually advances the screen.
func TestUninstallSearchSingleMatchProceedsToLeftoverFlow(t *testing.T) {
	home := t.TempDir()
	writeTestApp(t, filepath.Join(home, "Applications"), "SampleApp", "com.example.sampleapp")
	deps := testDeps(t, home)

	screen := newUninstallSearchScreen(deps)
	typeInto(screen, "SampleApp")

	_, cmd := screen.Update(tea.KeyMsg{Type: tea.KeyEnter})
	resolved := runCmd(t, cmd).(appResolvedMsg)
	if len(resolved.matches) != 1 {
		t.Fatalf("matches = %+v, want exactly 1", resolved.matches)
	}

	next, pushCmd := screen.Update(resolved)
	if next != screen {
		t.Fatalf("expected the search screen itself to remain current, got %T", next)
	}
	if pushCmd == nil {
		t.Fatal("expected a push command for the single match")
	}
	pushed, ok := runCmd(t, pushCmd).(pushScreenMsg)
	if !ok {
		t.Fatalf("expected pushScreenMsg, got %T", pushed)
	}
	if _, ok := pushed.screen.(*planDiscoveringScreen); !ok {
		t.Fatalf("pushed screen type = %T, want *planDiscoveringScreen", pushed.screen)
	}
}

func TestUninstallSearchNoMatchShowsErrorAndStays(t *testing.T) {
	home := t.TempDir()
	deps := testDeps(t, home)

	screen := newUninstallSearchScreen(deps)
	typeInto(screen, "Nothing Installed")
	_, cmd := screen.Update(tea.KeyMsg{Type: tea.KeyEnter})
	resolved := runCmd(t, cmd).(appResolvedMsg)

	next, pushCmd := screen.Update(resolved)
	if pushCmd != nil {
		t.Fatal("a failed search should not push anything")
	}
	same, ok := next.(*uninstallSearchScreen)
	if !ok || same.err == "" {
		t.Fatalf("expected the search screen to remain with an error set, got %+v", same)
	}
}

// Multiple matches push a disambiguation screen rather than guessing, the
// same discipline internal/uninstall-core's own FindApp already enforces;
// this proves the shell surfaces that discipline rather than working around
// it.
func TestUninstallSearchMultipleMatchesPushesDisambiguation(t *testing.T) {
	home := t.TempDir()
	appsRoot := filepath.Join(home, "Applications")
	writeTestApp(t, appsRoot, "Notes", "com.first.notes")
	second := filepath.Join(appsRoot, "AnotherNotesApp.app")
	writeTestFile(t, filepath.Join(second, "Contents", "Info.plist"), `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
<key>CFBundleIdentifier</key>
<string>Notes</string>
<key>CFBundleDisplayName</key>
<string>Another Notes App</string>
</dict>
</plist>
`)
	deps := testDeps(t, home)

	screen := newUninstallSearchScreen(deps)
	typeInto(screen, "Notes")
	_, cmd := screen.Update(tea.KeyMsg{Type: tea.KeyEnter})
	resolved := runCmd(t, cmd).(appResolvedMsg)
	if len(resolved.matches) != 2 {
		t.Fatalf("matches = %+v, want 2", resolved.matches)
	}

	_, pushCmd := screen.Update(resolved)
	pushed := runCmd(t, pushCmd).(pushScreenMsg)
	matchesScreen, ok := pushed.screen.(*appMatchesScreen)
	if !ok {
		t.Fatalf("pushed screen type = %T, want *appMatchesScreen", pushed.screen)
	}
	if len(matchesScreen.matches) != 2 {
		t.Fatalf("disambiguation screen has %d matches, want 2", len(matchesScreen.matches))
	}
}

// An Apple-identified application must be refused here too, not only by the
// non-interactive uninstall command. This is the same protection proven in
// internal/uninstall-core's own tests, checked again at the point the shell
// actually uses it, since a refusal that is correct in the package that
// defines it is not automatically correct everywhere it is wired in.
func TestUninstallSearchRefusesAppleApplication(t *testing.T) {
	home := t.TempDir()
	writeTestApp(t, filepath.Join(home, "Applications"), "FakeSafari", "com.apple.FakeSafari")
	deps := testDeps(t, home)

	screen := newUninstallSearchScreen(deps)
	typeInto(screen, "FakeSafari")
	_, cmd := screen.Update(tea.KeyMsg{Type: tea.KeyEnter})
	resolved := runCmd(t, cmd).(appResolvedMsg)

	next, pushCmd := screen.Update(resolved)
	if pushCmd != nil {
		t.Fatal("an Apple application must not proceed to the leftover flow")
	}
	same := next.(*uninstallSearchScreen)
	if same.err == "" {
		t.Fatal("expected a protection explanation to be shown")
	}
}

// The full leftover plan built for one resolved application, run through
// the deletion engine for real, staging a real file this time via the
// uninstall flow's own leftoverPlanFor rather than a hand-built candidate
// list, so the wiring between uninstallcore and the shell is what gets
// tested, not just the deletion engine underneath it.
func TestLeftoverPlanForStagesRealLeftovers(t *testing.T) {
	home := t.TempDir()
	appPath := writeTestApp(t, filepath.Join(home, "Applications"), "SampleApp", "com.example.sampleapp")
	supportDir := filepath.Join(home, "Library", "Application Support", "com.example.sampleapp")
	writeTestFile(t, filepath.Join(supportDir, "config.json"), "app data")
	deps := testDeps(t, home)

	apps, _, err := uninstallcore.DiscoverApps([]string{filepath.Join(home, "Applications")})
	if err != nil || len(apps) != 1 {
		t.Fatalf("setup discovery: apps=%+v err=%v", apps, err)
	}

	fn := leftoverPlanFor(apps[0])
	manifest, _, err := fn(deps)
	if err != nil {
		t.Fatalf("leftoverPlanFor: %v", err)
	}

	// Compared against the resolved form of each expected path, not the
	// literal one t.TempDir() returned: macOS's temporary directory sits
	// beneath /var, itself a symlink to /private/var, so the deletion
	// engine's own resolved paths differ textually from what was written
	// even though they name the same objects. The same mismatch already
	// caught a test bug once earlier in this project; comparing unresolved
	// here would have reintroduced it rather than avoided it.
	resolvedAppPath, err := filepath.EvalSymlinks(appPath)
	if err != nil {
		t.Fatalf("resolving expected app path: %v", err)
	}
	resolvedSupportDir, err := filepath.EvalSymlinks(supportDir)
	if err != nil {
		t.Fatalf("resolving expected support dir: %v", err)
	}

	found := map[string]bool{}
	for _, entry := range manifest.Entries {
		found[entry.ResolvedPath] = true
	}
	if !found[resolvedAppPath] {
		t.Error("plan did not include the application bundle itself")
	}
	if !found[resolvedSupportDir] {
		t.Error("plan did not include the Application Support leftover")
	}
}

func TestAppMatchesScreenEscPops(t *testing.T) {
	deps := testDeps(t, t.TempDir())
	screen := newAppMatchesScreen(deps, []uninstallcore.InstalledApp{
		{Path: "/a", BundleID: "a", DisplayName: "A"},
	})
	_, cmd := screen.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("esc produced no command")
	}
	if _, ok := runCmd(t, cmd).(popScreenMsg); !ok {
		t.Fatal("esc should pop back to the search screen")
	}
}

func TestAppSearchRootsIncludesUserApplications(t *testing.T) {
	roots := appSearchRoots("/Users/example")
	want := map[string]bool{"/Applications": true, "/Users/example/Applications": true}
	if len(roots) != 2 {
		t.Fatalf("roots = %v", roots)
	}
	for _, r := range roots {
		if !want[r] {
			t.Fatalf("unexpected root %q", r)
		}
	}
}
