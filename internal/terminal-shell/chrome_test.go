package terminalshell

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// withColor forces a color profile for one test, so assertions about palette
// bytes are not vacuously true under a profile that strips them. Restored
// afterwards, since other tests assert on plain substrings that styling would
// break apart.
func withColor(t *testing.T) {
	t.Helper()
	previous := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(previous) })
}

// A transcript that fits needs no bar, but still needs the column, or the
// content reflows the moment history grows past one screen.
func TestScrollbarIsBlankWhenEverythingFits(t *testing.T) {
	bar := renderScrollbar(brandTheme, 10, 5, 0)
	lines := strings.Split(bar, "\n")
	if len(lines) != 10 {
		t.Fatalf("scrollbar has %d lines, want 10", len(lines))
	}
	if strings.ContainsAny(bar, "┃│") {
		t.Fatal("a transcript that fits should show no bar")
	}
}

func TestScrollbarShowsThumbAndTrackWhenContentOverflows(t *testing.T) {
	bar := renderScrollbar(brandTheme, 10, 100, 0)
	if !strings.Contains(bar, "┃") {
		t.Fatal("an overflowing transcript needs a thumb")
	}
	if !strings.Contains(bar, "│") {
		t.Fatal("an overflowing transcript needs a track")
	}
}

// The thumb has to travel, or it reports the same position at the top and the
// bottom and tells a person nothing.
func TestScrollbarThumbMovesWithTheOffset(t *testing.T) {
	const height, total = 10, 100

	top := strings.Split(renderScrollbar(brandTheme, height, total, 0), "\n")
	bottom := strings.Split(renderScrollbar(brandTheme, height, total, total-height), "\n")

	if !strings.Contains(top[0], "┃") {
		t.Fatal("at the top the thumb should sit on the first line")
	}
	if !strings.Contains(bottom[height-1], "┃") {
		t.Fatal("at the bottom the thumb should reach the last line")
	}
	if strings.Contains(bottom[0], "┃") {
		t.Fatal("at the bottom the thumb should have left the first line")
	}
}

// A very long transcript must not shrink the thumb out of existence.
func TestScrollbarThumbNeverVanishes(t *testing.T) {
	bar := renderScrollbar(brandTheme, 10, 100000, 500)
	if !strings.Contains(bar, "┃") {
		t.Fatal("the thumb must remain visible on a very long transcript")
	}
}

func TestScrollbarUsesTheBrandColors(t *testing.T) {
	withColor(t)

	bar := renderScrollbar(brandTheme, 10, 100, 0)
	thumb := lipgloss.NewStyle().Foreground(brandTheme.Accent).Render("┃")
	track := lipgloss.NewStyle().Foreground(brandTheme.Highlight).Render("│")

	if !strings.Contains(bar, thumb) {
		t.Fatal("the thumb should carry the main color")
	}
	if !strings.Contains(bar, track) {
		t.Fatal("the track should carry the highlight color")
	}
}

// The wheel reached nothing before this: mouse messages had no case in the
// type switch, and the fallthrough only fed a running block. The transcript
// scrolled perfectly well and looked completely stuck.
func TestMouseWheelScrollsTheTranscript(t *testing.T) {
	deps := testDeps(t, t.TempDir())
	app := newTestApp(t, deps)

	for i := 0; i < 60; i++ {
		app.entries = append(app.entries, infoEntry(app.theme, "line of history"))
	}
	app.refresh(true)
	if app.view.YOffset == 0 {
		t.Fatal("setup: the transcript should be scrolled down")
	}

	before := app.view.YOffset
	model, _ := app.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonWheelUp})
	if model.(App).view.YOffset >= before {
		t.Fatal("the wheel should scroll the transcript up")
	}
}

func TestArrowKeysScrollWhenThePromptIsIdle(t *testing.T) {
	deps := testDeps(t, t.TempDir())
	app := newTestApp(t, deps)
	for i := 0; i < 60; i++ {
		app.entries = append(app.entries, infoEntry(app.theme, "line of history"))
	}
	app.refresh(true)

	before := app.view.YOffset
	model, _ := app.Update(tea.KeyMsg{Type: tea.KeyUp})
	if model.(App).view.YOffset >= before {
		t.Fatal("up should scroll the transcript when nothing else claims it")
	}
}

// The palette owns the arrows while it is open, or choosing a command would
// scroll the history instead of moving the selection.
func TestArrowKeysDoNotScrollWhileThePaletteIsOpen(t *testing.T) {
	deps := testDeps(t, t.TempDir())
	app := newTestApp(t, deps)
	for i := 0; i < 60; i++ {
		app.entries = append(app.entries, infoEntry(app.theme, "line of history"))
	}
	app.refresh(true)
	app = typeInto(app, "/")

	before := app.view.YOffset
	model, _ := app.Update(tea.KeyMsg{Type: tea.KeyDown})
	if model.(App).view.YOffset != before {
		t.Fatal("the palette should own the arrows while it is open")
	}
}

func TestArrowKeysDoNotScrollWhileABlockIsLive(t *testing.T) {
	deps := testDeps(t, t.TempDir())
	app := newTestApp(t, deps)
	for i := 0; i < 60; i++ {
		app.entries = append(app.entries, infoEntry(app.theme, "line of history"))
	}
	app.refresh(true)
	app = typeInto(app, "uninstall")
	app, _ = pressEnter(app)

	before := app.view.YOffset
	model, _ := app.Update(tea.KeyMsg{Type: tea.KeyUp})
	if model.(App).view.YOffset != before {
		t.Fatal("a live block owns the keyboard, arrows included")
	}
}

// Page keys stay unconditional, so a person can read back over history while
// something is still running.
func TestPageKeysScrollEvenMidFlow(t *testing.T) {
	deps := testDeps(t, t.TempDir())
	app := newTestApp(t, deps)
	for i := 0; i < 60; i++ {
		app.entries = append(app.entries, infoEntry(app.theme, "line of history"))
	}
	app.refresh(true)
	app = typeInto(app, "uninstall")
	app, _ = pressEnter(app)

	before := app.view.YOffset
	model, _ := app.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	if model.(App).view.YOffset >= before {
		t.Fatal("page up should scroll even while a flow runs")
	}
}

// The suggestion and what a person actually typed share the same cells, so
// they must not share a color.
func TestPlaceholderIsStyledApartFromTypedText(t *testing.T) {
	deps := testDeps(t, t.TempDir())
	app := newTestApp(t, deps)

	got := app.input.PlaceholderStyle.GetForeground()
	if got != lipgloss.Color("#D6D6D6") {
		t.Fatalf("placeholder color = %v, want #D6D6D6", got)
	}
	if got == brandTheme.Body {
		t.Fatal("the suggestion must not share the body color")
	}
}

// The echoed command is a heading for the output indented beneath it. In the
// body color it rendered as near black against its own results.
func TestEchoedCommandUsesTheMainColor(t *testing.T) {
	withColor(t)

	body := echoEntry(brandTheme, "purge").body
	wantAccent := lipgloss.NewStyle().Foreground(brandTheme.Accent).Bold(true).Render("purge")
	notBody := lipgloss.NewStyle().Foreground(brandTheme.Body).Bold(true).Render("purge")

	if !strings.Contains(body, wantAccent) {
		t.Fatal("the echoed command should carry the main color")
	}
	if strings.Contains(body, notBody) {
		t.Fatal("the echoed command still carries the body color")
	}
}

// The cursor belongs where typing goes. A block owns the keyboard while it
// runs, so a cursor blinking at the prompt would be pointing at the wrong
// place.
func TestPromptFocusFollowsTheLiveBlock(t *testing.T) {
	deps := testDeps(t, t.TempDir())
	app := newTestApp(t, deps)

	if !app.input.Focused() {
		t.Fatal("the prompt should hold the cursor when idle")
	}

	app = typeInto(app, "uninstall")
	app, _ = pressEnter(app)
	if app.input.Focused() {
		t.Fatal("the prompt should release the cursor while a block runs")
	}

	model, _ := app.Update(flowMsg{setBlock: true, block: nil})
	if !model.(App).input.Focused() {
		t.Fatal("the prompt should take the cursor back when the flow ends")
	}
}

// Cursor blink messages are not key messages. Routing them only to a running
// block meant the cursor stopped animating for good once a flow had run, and
// the shell lost its only standing sign of life.
func TestBlinkMessagesReachThePrompt(t *testing.T) {
	deps := testDeps(t, t.TempDir())
	app := newTestApp(t, deps)

	// A message the app has no case for must still be offered to the prompt.
	_, cmd := app.Update(app.input.Cursor.BlinkCmd()())
	if cmd == nil {
		t.Fatal("an unhandled message should still reach the prompt's cursor")
	}
}

// The scrollbar's column comes out of the viewport's width, so the two
// together are exactly the terminal width and nothing wraps.
func TestViewportLeavesRoomForTheScrollbar(t *testing.T) {
	deps := testDeps(t, t.TempDir())
	app := newTestApp(t, deps)

	if app.view.Width+scrollbarWidth != app.width {
		t.Fatalf("viewport %d plus scrollbar %d does not fill width %d",
			app.view.Width, scrollbarWidth, app.width)
	}

	model, _ := app.Update(tea.WindowSizeMsg{Width: 80, Height: 30})
	resized := model.(App)
	if resized.view.Width+scrollbarWidth != 80 {
		t.Fatalf("after resize, viewport %d plus scrollbar does not fill 80",
			resized.view.Width)
	}
}

// No rendered line may exceed the terminal width, or the prompt is pushed off
// screen by wrapping.
func TestRenderedViewNeverExceedsTheTerminalWidth(t *testing.T) {
	deps := testDeps(t, t.TempDir())
	app := newTestApp(t, deps)
	for i := 0; i < 40; i++ {
		app.entries = append(app.entries, infoEntry(app.theme, strings.Repeat("x", 60)))
	}
	app.refresh(true)

	for i, line := range strings.Split(app.View(), "\n") {
		if width := lipgloss.Width(line); width > app.width {
			t.Fatalf("line %d is %d wide, terminal is %d", i, width, app.width)
		}
	}
}
