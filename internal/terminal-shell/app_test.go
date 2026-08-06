package terminalshell

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	cleancatalog "github.com/lesliemusengi/wtff/internal/clean-catalog"
	operationlog "github.com/lesliemusengi/wtff/internal/operation-log"
	protectionrules "github.com/lesliemusengi/wtff/internal/protection-rules"
)

// testDeps builds a Deps rooted at an isolated home directory.
//
// Setting HOME is not optional: the deletion engine's default staging and
// log paths always resolve against the real process environment, by design.
// A test that set Deps.Home without also setting the environment variable
// would still touch the real machine's staging area, which happened once
// during development and left a real file staged on the developer's machine.
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
	return &Deps{Home: home, Rules: rules, Catalog: catalog,
		Log: operationlog.Discard(), Version: "test"}
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

func runCmd(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected a command, got nil")
	}
	return cmd()
}

// newTestApp returns a sized, ready App, since almost every behavior here
// depends on the viewport existing.
func newTestApp(t *testing.T, deps *Deps) App {
	t.Helper()
	app := NewApp(deps, true, nil)
	model, _ := app.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	return model.(App)
}

func typeInto(app App, text string) App {
	for _, r := range text {
		model, _ := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		app = model.(App)
	}
	return app
}

func pressEnter(app App) (App, tea.Cmd) {
	model, cmd := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	return model.(App), cmd
}

func transcriptText(app App) string {
	var b strings.Builder
	for _, entry := range app.entries {
		b.WriteString(entry.render(app.theme))
		b.WriteString("\n")
	}
	return b.String()
}

// The core of the approved design: running a command must not replace the
// view. The prompt stays, the transcript grows, and history is preserved.
func TestRunningACommandKeepsThePromptAndGrowsTheTranscript(t *testing.T) {
	deps := testDeps(t, t.TempDir())
	app := newTestApp(t, deps)

	before := len(app.entries)
	app = typeInto(app, "staged")
	app, cmd := pressEnter(app)

	if len(app.entries) <= before {
		t.Fatal("running a command should append to the transcript")
	}
	if !strings.Contains(transcriptText(app), "staged") {
		t.Fatal("the transcript should echo the typed command")
	}
	if !strings.Contains(app.View(), "❯") {
		t.Fatal("the prompt must remain visible while a command runs")
	}
	if cmd == nil {
		t.Fatal("a command should start a flow")
	}
}

// The welcome box is the first transcript entry, so it scrolls away with
// history instead of permanently occupying the viewport.
func TestWelcomeIsTheFirstTranscriptEntry(t *testing.T) {
	deps := testDeps(t, t.TempDir())
	app := newTestApp(t, deps)

	if len(app.entries) != 1 {
		t.Fatalf("expected exactly the welcome entry, got %d", len(app.entries))
	}
	if !strings.Contains(app.entries[0].render(app.theme), "Welcome to wtff") {
		t.Fatal("the first entry should be the welcome box")
	}
}

// Exact, case-sensitive matching: a person typing something means exactly
// what they typed.
func TestWrongCaseIsRejectedIntoTheTranscript(t *testing.T) {
	deps := testDeps(t, t.TempDir())
	app := newTestApp(t, deps)

	app = typeInto(app, "Clean")
	app, cmd := pressEnter(app)

	if cmd != nil {
		t.Fatal("a wrong-case command must not start a flow")
	}
	if app.block != nil {
		t.Fatal("a wrong-case command must not pin a live block")
	}
	text := transcriptText(app)
	if !strings.Contains(text, "unknown command") || !strings.Contains(text, "Clean") {
		t.Fatalf("the transcript should record the rejection and what was typed: %q", text)
	}
}

func TestUnknownCommandIsRejectedIntoTheTranscript(t *testing.T) {
	deps := testDeps(t, t.TempDir())
	app := newTestApp(t, deps)
	app = typeInto(app, "banana")
	app, cmd := pressEnter(app)

	if cmd != nil || app.block != nil {
		t.Fatal("an unknown command must not run anything")
	}
	if !strings.Contains(transcriptText(app), "unknown command") {
		t.Fatal("the transcript should record the rejection")
	}
}

// Only uninstall takes an argument; everything else with trailing text is
// refused rather than silently ignoring the extra words.
func TestArgumentsAreRefusedForCommandsThatTakeNone(t *testing.T) {
	deps := testDeps(t, t.TempDir())
	app := newTestApp(t, deps)
	app = typeInto(app, "clean now")
	app, cmd := pressEnter(app)

	if cmd != nil || app.block != nil {
		t.Fatal("a command with an unexpected argument must not run")
	}
	if !strings.Contains(transcriptText(app), "takes no arguments") {
		t.Fatal("the transcript should explain the refusal")
	}
}

func TestUninstallAcceptsItsArgument(t *testing.T) {
	deps := testDeps(t, t.TempDir())
	app := newTestApp(t, deps)
	app = typeInto(app, "uninstall SomeApp")
	app, cmd := pressEnter(app)

	if cmd == nil || app.block == nil {
		t.Fatal("uninstall with an argument should start a flow")
	}
	if _, ok := app.block.(*resolveBlock); !ok {
		t.Fatalf("expected a resolve block, got %T", app.block)
	}
}

func TestBareUninstallAsksForAName(t *testing.T) {
	deps := testDeps(t, t.TempDir())
	app := newTestApp(t, deps)
	app = typeInto(app, "uninstall")
	app, _ = pressEnter(app)

	if _, ok := app.block.(*nameQueryBlock); !ok {
		t.Fatalf("expected a name query block, got %T", app.block)
	}
}

func TestHelpWritesToTheTranscriptWithoutAFlow(t *testing.T) {
	deps := testDeps(t, t.TempDir())
	app := newTestApp(t, deps)
	app = typeInto(app, "help")
	app, cmd := pressEnter(app)

	if cmd != nil || app.block != nil {
		t.Fatal("help should not start a flow")
	}
	text := transcriptText(app)
	for _, c := range homeCommands {
		if !strings.Contains(text, c.name) {
			t.Fatalf("help output missing command %q", c.name)
		}
	}
}

func TestQuitExits(t *testing.T) {
	deps := testDeps(t, t.TempDir())
	app := newTestApp(t, deps)
	app = typeInto(app, "quit")
	app, cmd := pressEnter(app)

	if !app.quitting {
		t.Fatal("quit should set quitting")
	}
	if _, ok := runCmd(t, cmd).(tea.QuitMsg); !ok {
		t.Fatal("quit should return tea.Quit")
	}
}

func TestCtrlCQuitsEvenWithAFlowRunning(t *testing.T) {
	deps := testDeps(t, t.TempDir())
	app := newTestApp(t, deps)
	app = typeInto(app, "clean")
	app, _ = pressEnter(app)
	if app.block == nil {
		t.Fatal("setup: expected a running flow")
	}

	model, cmd := app.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if !model.(App).quitting || cmd == nil {
		t.Fatal("ctrl+c must quit even while a flow is running")
	}
}

// A running block owns the keyboard, so ordinary keys reach the interactive
// step rather than the prompt behind it.
func TestKeysReachTheLiveBlockWhileAFlowRuns(t *testing.T) {
	deps := testDeps(t, t.TempDir())
	app := newTestApp(t, deps)
	app = typeInto(app, "uninstall")
	app, _ = pressEnter(app)

	app = typeInto(app, "abc")
	block, ok := app.block.(*nameQueryBlock)
	if !ok {
		t.Fatalf("expected the name query block, got %T", app.block)
	}
	if block.input.Value() != "abc" {
		t.Fatalf("the live block should have received the keys, got %q", block.input.Value())
	}
	if app.input.Value() != "" {
		t.Fatal("the prompt must not receive keys while a block is live")
	}
}

// flowMsg is the single mechanism a flow uses to append history and swap
// the pinned block; a nil block means the flow is over.
func TestFlowMsgAppendsEntriesAndClearsTheBlock(t *testing.T) {
	deps := testDeps(t, t.TempDir())
	app := newTestApp(t, deps)
	app = typeInto(app, "clean")
	app, _ = pressEnter(app)
	if app.block == nil {
		t.Fatal("setup: expected a running flow")
	}

	model, _ := app.Update(flowMsg{
		entries:  []transcriptEntry{infoEntry(app.theme, "done here")},
		setBlock: true,
		block:    nil,
	})
	app = model.(App)

	if app.block != nil {
		t.Fatal("a nil block in flowMsg should end the flow")
	}
	if !strings.Contains(transcriptText(app), "done here") {
		t.Fatal("flowMsg entries should be appended to the transcript")
	}
}

// The disclosure toggle keeps verbose output one keypress away without
// cluttering the viewport by default.
func TestCtrlOTogglesTheNewestDisclosure(t *testing.T) {
	deps := testDeps(t, t.TempDir())
	app := newTestApp(t, deps)
	app.entries = append(app.entries,
		successEntry(app.theme, "staged 2 items", "staged  /a", "staged  /b"))

	if strings.Contains(transcriptText(app), "/a") {
		t.Fatal("details should be collapsed by default")
	}

	model, _ := app.Update(tea.KeyMsg{Type: tea.KeyCtrlO})
	app = model.(App)
	if !strings.Contains(transcriptText(app), "/a") {
		t.Fatal("ctrl+o should expand the newest disclosure")
	}

	model, _ = app.Update(tea.KeyMsg{Type: tea.KeyCtrlO})
	app = model.(App)
	if strings.Contains(transcriptText(app), "/a") {
		t.Fatal("ctrl+o should collapse it again")
	}
}

func TestSlashOpensThePaletteAndFilters(t *testing.T) {
	deps := testDeps(t, t.TempDir())
	app := newTestApp(t, deps)

	app = typeInto(app, "/")
	if !app.paletteActive {
		t.Fatal("typing / should open the palette")
	}
	app = typeInto(app, "un")
	filtered := filterCommands(strings.TrimPrefix(app.input.Value(), "/"))
	if len(filtered) != 1 || filtered[0].name != "uninstall" {
		t.Fatalf("palette filtering wrong: %+v", filtered)
	}
}

func TestPaletteEnterRunsTheHighlightedCommand(t *testing.T) {
	deps := testDeps(t, t.TempDir())
	app := newTestApp(t, deps)
	app = typeInto(app, "/staged")
	app, cmd := pressEnter(app)

	if cmd == nil || app.block == nil {
		t.Fatal("palette enter should start the highlighted command")
	}
	if app.paletteActive {
		t.Fatal("running from the palette should close it")
	}
}

func TestPaletteEscapeClears(t *testing.T) {
	deps := testDeps(t, t.TempDir())
	app := newTestApp(t, deps)
	app = typeInto(app, "/cl")

	model, _ := app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	app = model.(App)
	if app.paletteActive || app.input.Value() != "" {
		t.Fatal("escape should close the palette and clear the input")
	}
}

// Every command must be dispatchable: the missing-handler omission happened
// once already during development.
func TestEveryCommandHasAHandler(t *testing.T) {
	for _, c := range homeCommands {
		if c.name == "quit" || c.name == "help" {
			continue
		}
		if c.start == nil {
			t.Errorf("command %q has no start function", c.name)
		}
	}
}

func TestDetectThemeReturnsTheBrandPalette(t *testing.T) {
	for _, dark := range []bool{true, false} {
		got := detectTheme(dark)
		if got.Accent != lipgloss.Color("#0A0AAE") {
			t.Fatalf("accent = %v, want the brand main color", got.Accent)
		}
		if got.Border != got.Accent {
			t.Fatal("borders must use the main color, per the theme specification")
		}
		if got.Body != lipgloss.Color("#3D3D3D") {
			t.Fatalf("body = %v, want the brand body color", got.Body)
		}
		if got.Highlight != lipgloss.Color("#E1E1FD") {
			t.Fatalf("highlight = %v, want the brand highlight color", got.Highlight)
		}
		if got.Success != lipgloss.Color("#0AAE0A") {
			t.Fatalf("success = %v, want the brand success color", got.Success)
		}
	}
}

// A transcript that only ever grows is a memory leak with a slow fuse. The
// ceiling drops the oldest, since the bottom is where a person is looking.
func TestTranscriptIsBounded(t *testing.T) {
	deps := testDeps(t, t.TempDir())
	app := newTestApp(t, deps)

	for i := 0; i < maxTranscriptEntries*2; i++ {
		app.appendEntries(infoEntry(app.theme, "entry"))
	}
	if len(app.entries) > maxTranscriptEntries {
		t.Fatalf("transcript grew to %d entries, ceiling is %d",
			len(app.entries), maxTranscriptEntries)
	}
}

// The newest entries are what survive trimming, or a person loses the result
// they just asked for while old history is kept.
func TestTranscriptKeepsTheNewestEntries(t *testing.T) {
	deps := testDeps(t, t.TempDir())
	app := newTestApp(t, deps)

	for i := 0; i < maxTranscriptEntries+50; i++ {
		app.appendEntries(infoEntry(app.theme, "filler"))
	}
	app.appendEntries(successEntry(app.theme, "the newest result"))

	last := app.entries[len(app.entries)-1].render(app.theme)
	if !strings.Contains(last, "the newest result") {
		t.Fatal("the newest entry should survive trimming")
	}
	if len(app.entries) > maxTranscriptEntries {
		t.Fatalf("transcript is %d entries, ceiling is %d",
			len(app.entries), maxTranscriptEntries)
	}
}

// Ordinary sessions must be nowhere near the ceiling, or it is a scrollback
// budget rather than a safety net.
func TestAnOrdinarySessionIsNotTrimmed(t *testing.T) {
	deps := testDeps(t, t.TempDir())
	app := newTestApp(t, deps)

	for i := 0; i < 20; i++ {
		app = typeInto(app, "help")
		app, _ = pressEnter(app)
	}
	if len(app.entries) >= maxTranscriptEntries {
		t.Fatalf("twenty commands produced %d entries, too close to the %d ceiling",
			len(app.entries), maxTranscriptEntries)
	}
}
