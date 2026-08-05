package terminalshell

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func typeKeys(h *homeScreen, text string) {
	for _, r := range text {
		h.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
}

// The exact match requirement is the whole point of this interface: a
// person typing something is understood to mean exactly what they typed.
// Case must matter and partial words must not resolve.
func TestMatchCommandIsExactAndCaseSensitive(t *testing.T) {
	if _, ok := matchCommand("clean"); !ok {
		t.Fatal("exact lowercase match should succeed")
	}
	cases := []string{"Clean", "CLEAN", "clea", "cleaning", " clean ", "clean "}
	for _, input := range cases {
		if input == " clean " || input == "clean " {
			// Surrounding whitespace is trimmed, not a case or spelling
			// mismatch, so these are expected to match.
			if _, ok := matchCommand(input); !ok {
				t.Errorf("matchCommand(%q) should match after trimming whitespace", input)
			}
			continue
		}
		if _, ok := matchCommand(input); ok {
			t.Errorf("matchCommand(%q) matched, want no match", input)
		}
	}
}

func TestFilterCommandsPrefixMatch(t *testing.T) {
	results := filterCommands("un")
	if len(results) != 1 || results[0].name != "uninstall" {
		t.Fatalf("filterCommands(\"un\") = %+v, want only uninstall", results)
	}
	if len(filterCommands("")) != len(homeCommands) {
		t.Fatalf("filterCommands(\"\") should return every command")
	}
	if len(filterCommands("zzz")) != 0 {
		t.Fatal("filterCommands with no matches should return none")
	}
}

// Every command entry must have a working handler, or quit and help, which
// are handled specially in dispatch rather than through activate. This is
// the exact class of mistake caught once already while building this file:
// an entry added without activate wired up.
func TestEveryCommandIsDispatchable(t *testing.T) {
	for _, c := range homeCommands {
		if c.name == "quit" || c.name == "help" {
			continue
		}
		if c.activate == nil {
			t.Errorf("command %q has no activate function", c.name)
		}
	}
}

func TestHomeScreenDispatchesExactCommand(t *testing.T) {
	deps := testDeps(t, t.TempDir())
	h := newHomeScreen(deps)
	typeKeys(h, "clean")

	next, cmd := h.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if next != h {
		t.Fatalf("expected the home screen itself to remain current, got %T", next)
	}
	if cmd == nil {
		t.Fatal("expected a push command")
	}
	pushed, ok := runCmd(t, cmd).(pushScreenMsg)
	if !ok {
		t.Fatalf("expected pushScreenMsg, got %T", pushed)
	}
	if _, ok := pushed.screen.(*planDiscoveringScreen); !ok {
		t.Fatalf("pushed screen type = %T, want *planDiscoveringScreen", pushed.screen)
	}
}

func TestHomeScreenRejectsWrongCaseWithoutRunningAnything(t *testing.T) {
	deps := testDeps(t, t.TempDir())
	h := newHomeScreen(deps)
	typeKeys(h, "Clean")

	next, cmd := h.Update(tea.KeyMsg{Type: tea.KeyEnter})
	same := next.(*homeScreen)
	if cmd != nil {
		t.Fatal("a wrong-case command should not push anything")
	}
	if same.errorMsg == "" {
		t.Fatal("expected an error message explaining the rejection")
	}
	if !strings.Contains(same.errorMsg, "Clean") {
		t.Fatalf("error message %q does not name what was actually typed", same.errorMsg)
	}
}

func TestHomeScreenUnknownCommandShowsError(t *testing.T) {
	deps := testDeps(t, t.TempDir())
	h := newHomeScreen(deps)
	typeKeys(h, "banana")
	next, cmd := h.Update(tea.KeyMsg{Type: tea.KeyEnter})
	same := next.(*homeScreen)
	if cmd != nil || same.errorMsg == "" {
		t.Fatalf("expected a rejection with no command, got cmd=%v errorMsg=%q", cmd, same.errorMsg)
	}
}

func TestHomeScreenQuitReturnsTeaQuit(t *testing.T) {
	deps := testDeps(t, t.TempDir())
	h := newHomeScreen(deps)
	typeKeys(h, "quit")
	_, cmd := h.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("quit should produce a command")
	}
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Fatalf("quit produced %T, want tea.QuitMsg", msg)
	}
}

// Typing "/" must switch into palette mode automatically, not require a
// separate action, since that is the discovery mechanism a person reaches
// for without already knowing an exact command name.
func TestSlashActivatesPalette(t *testing.T) {
	deps := testDeps(t, t.TempDir())
	h := newHomeScreen(deps)
	typeKeys(h, "/")
	if !h.paletteActive {
		t.Fatal("typing / should activate the palette")
	}
}

func TestPaletteFiltersAsTyped(t *testing.T) {
	deps := testDeps(t, t.TempDir())
	h := newHomeScreen(deps)
	typeKeys(h, "/cl")
	if !h.paletteActive {
		t.Fatal("palette should still be active")
	}
	filtered := filterCommands(strings.TrimPrefix(h.input.Value(), "/"))
	if len(filtered) != 1 || filtered[0].name != "clean" {
		t.Fatalf("filtered = %+v, want only clean", filtered)
	}
}

// Enter within the palette runs the highlighted entry, an explicit
// selection from a fully enumerated list, not a fuzzy resolution of
// ambiguous partial text.
func TestPaletteEnterRunsHighlightedEntry(t *testing.T) {
	deps := testDeps(t, t.TempDir())
	h := newHomeScreen(deps)
	typeKeys(h, "/un")

	_, cmd := h.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected a push command")
	}
	pushed := runCmd(t, cmd).(pushScreenMsg)
	if _, ok := pushed.screen.(*uninstallSearchScreen); !ok {
		t.Fatalf("pushed screen type = %T, want *uninstallSearchScreen", pushed.screen)
	}
}

func TestPaletteEscClearsAndExits(t *testing.T) {
	deps := testDeps(t, t.TempDir())
	h := newHomeScreen(deps)
	typeKeys(h, "/cl")

	next, _ := h.Update(tea.KeyMsg{Type: tea.KeyEsc})
	same := next.(*homeScreen)
	if same.paletteActive {
		t.Fatal("esc should exit the palette")
	}
	if same.input.Value() != "" {
		t.Fatalf("esc should clear the input, still has %q", same.input.Value())
	}
}

// Backspacing out of the leading "/" must return to plain prompt mode
// rather than leaving the palette active with nothing to filter on.
func TestBackspacingOutOfSlashExitsPalette(t *testing.T) {
	deps := testDeps(t, t.TempDir())
	h := newHomeScreen(deps)
	typeKeys(h, "/")
	if !h.paletteActive {
		t.Fatal("setup: palette should be active")
	}
	h.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	if h.paletteActive {
		t.Fatal("removing the leading slash should exit the palette")
	}
}

func TestPaletteCursorStaysWithinFilteredBounds(t *testing.T) {
	deps := testDeps(t, t.TempDir())
	h := newHomeScreen(deps)
	typeKeys(h, "/")
	// Move down more times than there are entries; the cursor must clamp
	// rather than run past the end of the list.
	for i := 0; i < len(homeCommands)+3; i++ {
		h.Update(tea.KeyMsg{Type: tea.KeyDown})
	}
	if h.paletteCursor != len(homeCommands)-1 {
		t.Fatalf("paletteCursor = %d, want %d (clamped to the last entry)",
			h.paletteCursor, len(homeCommands)-1)
	}
}

func TestHelpCommandOpensPalette(t *testing.T) {
	deps := testDeps(t, t.TempDir())
	h := newHomeScreen(deps)
	typeKeys(h, "help")
	next, cmd := h.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("help should not push a screen")
	}
	same := next.(*homeScreen)
	if !same.paletteActive {
		t.Fatal("help should open the palette")
	}
}

// The defensive guard added after the missing-activate mistake: a command
// with no handler must show an error, not panic.
func TestDispatchWithMissingActivateDoesNotPanic(t *testing.T) {
	deps := testDeps(t, t.TempDir())
	h := newHomeScreen(deps)

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("dispatch panicked: %v", r)
		}
	}()

	original := homeCommands
	homeCommands = append([]command(nil), homeCommands...)
	homeCommands = append(homeCommands, command{name: "broken", description: "test"})
	defer func() { homeCommands = original }()

	typeKeys(h, "broken")
	_, cmd := h.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("a command with no handler should not push anything")
	}
	if h.errorMsg == "" {
		t.Fatal("expected an error explaining the missing handler")
	}
}
