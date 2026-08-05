package terminalshell

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// stubScreen is a minimal Screen for exercising App's stack behavior in
// isolation from any real flow's logic.
type stubScreen struct {
	title      string
	updateFunc func(msg tea.Msg) (Screen, tea.Cmd)
}

func (s *stubScreen) Title() string { return s.title }
func (s *stubScreen) Init() tea.Cmd { return nil }
func (s *stubScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	if s.updateFunc != nil {
		return s.updateFunc(msg)
	}
	return s, nil
}
func (s *stubScreen) View(theme Theme, width, height int) string { return s.title }

func runCmd(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected a command, got nil")
	}
	return cmd()
}

func TestAppPushAddsToStackAndEscPops(t *testing.T) {
	root := &stubScreen{title: "root"}
	app := NewApp(nil, true, root)

	pushed := &stubScreen{title: "pushed"}
	model, _ := app.Update(pushScreenMsg{screen: pushed})
	app = model.(App)

	if len(app.stack) != 2 || app.current().Title() != "pushed" {
		t.Fatalf("stack after push = %+v", app.stack)
	}

	model, _ = app.Update(popScreenMsg{})
	app = model.(App)
	if len(app.stack) != 1 || app.current().Title() != "root" {
		t.Fatalf("stack after pop = %+v", app.stack)
	}
}

// Popping the last screen on the stack quits rather than leaving the
// program with no current screen, which View already guards against, but
// the popScreenMsg handler is the actual place that decision belongs.
func TestAppPoppingLastScreenQuits(t *testing.T) {
	app := NewApp(nil, true, &stubScreen{title: "root"})
	model, cmd := app.Update(popScreenMsg{})
	app = model.(App)
	if !app.quitting {
		t.Fatal("popping the last screen should set quitting")
	}
	if cmd == nil {
		t.Fatal("popping the last screen should return tea.Quit")
	}
}

func TestAppCtrlCQuitsRegardlessOfStackDepth(t *testing.T) {
	app := NewApp(nil, true, &stubScreen{title: "root"})
	model, _ := app.Update(pushScreenMsg{screen: &stubScreen{title: "deep"}})
	app = model.(App)
	model, _ = app.Update(pushScreenMsg{screen: &stubScreen{title: "deeper"}})
	app = model.(App)

	model, cmd := app.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	app = model.(App)
	if !app.quitting || cmd == nil {
		t.Fatal("ctrl+c should quit regardless of stack depth")
	}
}

// This is the behavior resultsScreen relies on: discard everything except
// the root screen, not just one level.
func TestAppResetToMenuTruncatesToRoot(t *testing.T) {
	app := NewApp(nil, true, &stubScreen{title: "root"})
	for _, title := range []string{"a", "b", "c"} {
		model, _ := app.Update(pushScreenMsg{screen: &stubScreen{title: title}})
		app = model.(App)
	}
	if len(app.stack) != 4 {
		t.Fatalf("setup: stack depth = %d, want 4", len(app.stack))
	}

	model, _ := app.Update(resetToMenuMsg{})
	app = model.(App)
	if len(app.stack) != 1 || app.current().Title() != "root" {
		t.Fatalf("stack after reset = %+v", app.stack)
	}
}

// A key event must reach the screen that is actually on top of the stack,
// not the root, and the screen's own returned replacement must become the
// new top of stack.
func TestAppDispatchesToTopOfStackAndAppliesReplacement(t *testing.T) {
	replaced := &stubScreen{title: "replaced"}
	top := &stubScreen{title: "top", updateFunc: func(tea.Msg) (Screen, tea.Cmd) {
		return replaced, nil
	}}
	root := &stubScreen{title: "root", updateFunc: func(tea.Msg) (Screen, tea.Cmd) {
		t.Fatal("root should not receive updates while a screen is stacked above it")
		return nil, nil
	}}

	app := NewApp(nil, true, root)
	model, _ := app.Update(pushScreenMsg{screen: top})
	app = model.(App)

	model, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	app = model.(App)
	if app.current().Title() != "replaced" {
		t.Fatalf("current screen = %q, want replaced", app.current().Title())
	}
	if len(app.stack) != 2 {
		t.Fatalf("replacement should not change stack depth, got %d", len(app.stack))
	}
}

func TestDetectThemeSelectsCorrectPalette(t *testing.T) {
	if got := detectTheme(true); got.Accent != darkTheme.Accent {
		t.Fatal("dark background did not select the dark theme")
	}
	if got := detectTheme(false); got.Accent != lightTheme.Accent {
		t.Fatal("light background did not select the light theme")
	}
}
