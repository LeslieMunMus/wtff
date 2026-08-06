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
	if strings.Contains(bar, "\x1b[") {
		t.Fatal("a transcript that fits should show no bar")
	}
	for i, line := range lines {
		if lipgloss.Width(line) != scrollbarWidth {
			t.Fatalf("blank line %d is %d wide, want %d",
				i, lipgloss.Width(line), scrollbarWidth)
		}
	}
}

func TestScrollbarShowsThumbAndTrackWhenContentOverflows(t *testing.T) {
	withColor(t)

	bar := renderScrollbar(brandTheme, 10, 100, 0)
	thumb := scrollbarSegment(brandTheme.Accent)
	track := scrollbarSegment(brandTheme.Highlight)

	if !strings.Contains(bar, thumb) {
		t.Fatal("an overflowing transcript needs a thumb")
	}
	if !strings.Contains(bar, track) {
		t.Fatal("an overflowing transcript needs a track")
	}
}

// The bar stays slim, but the region that grabs it does not, since a terminal
// cannot change the pointer shape to show where the control is.
func TestGrabRegionIsWiderThanTheDrawnBar(t *testing.T) {
	if scrollbarGrabSlack < 1 {
		t.Fatal("the grab region should be more forgiving than the drawn bar")
	}
	for _, line := range strings.Split(renderScrollbar(brandTheme, 8, 80, 0), "\n") {
		if lipgloss.Width(line) != scrollbarWidth {
			t.Fatalf("line is %d wide, want %d", lipgloss.Width(line), scrollbarWidth)
		}
	}
}

// The bar is painted as a background on blank cells, not a block glyph. A
// glyph relies on the font filling its cell to the full line height, and many
// monospace fonts leave a gap, which renders a continuous bar as a dashed one.
func TestScrollbarIsPaintedAsABackgroundNotAGlyph(t *testing.T) {
	withColor(t)

	bar := renderScrollbar(brandTheme, 8, 80, 0)
	for _, glyph := range []string{"\u2588", "\u2503", "\u2502", "\u258c", "\u2590"} {
		if strings.Contains(bar, glyph) {
			t.Fatalf("the bar draws glyph %q, which leaves gaps in many fonts", glyph)
		}
	}
	if !strings.Contains(bar, "48;2;") {
		t.Fatal("the bar should be painted with a background color")
	}
}

// The thumb has to travel, or it reports the same position at the top and the
// bottom and tells a person nothing.
func TestScrollbarThumbMovesWithTheOffset(t *testing.T) {
	const height, total = 10, 100

	topStart, topSize := scrollbarThumb(height, total, 0)
	if topStart != 0 || topSize < 1 {
		t.Fatalf("at the top the thumb should start at row 0, got %d size %d",
			topStart, topSize)
	}

	endStart, endSize := scrollbarThumb(height, total, total-height)
	if endStart+endSize != height {
		t.Fatalf("at the bottom the thumb should reach the last row, got %d+%d of %d",
			endStart, endSize, height)
	}
	if endStart == 0 {
		t.Fatal("at the bottom the thumb should have left the first row")
	}
}

// A very long transcript must not shrink the thumb out of existence.
func TestScrollbarThumbNeverVanishes(t *testing.T) {
	if _, size := scrollbarThumb(10, 100000, 500); size < 1 {
		t.Fatal("the thumb must remain visible on a very long transcript")
	}
}

func TestScrollbarUsesTheBrandColors(t *testing.T) {
	withColor(t)

	bar := renderScrollbar(brandTheme, 10, 100, 0)
	thumb := scrollbarSegment(brandTheme.Accent)
	track := scrollbarSegment(brandTheme.Highlight)

	if !strings.Contains(bar, thumb) {
		t.Fatal("the thumb should carry the main color")
	}
	if !strings.Contains(bar, track) {
		t.Fatal("the track should carry the highlight color")
	}
}

// Rendering and dragging are inverses. If they disagree, the thumb does not
// stay under the pointer, which is the whole feel of a draggable bar.
func TestThumbPositionRoundTripsThroughTheDragMath(t *testing.T) {
	const height, total = 12, 200

	for offset := 0; offset <= total-height; offset++ {
		start, size := scrollbarThumb(height, total, offset)
		if size == 0 {
			t.Fatal("expected a thumb")
		}
		if start < 0 || start+size > height {
			t.Fatalf("offset %d put the thumb at %d..%d, outside 0..%d",
				offset, start, start+size, height)
		}

		// The property that makes a drag feel right: dropping the thumb on a
		// row must render it on that same row. Neighbouring offsets can share
		// a row, so the offsets need not match, but the rendered row must.
		landed := offsetForThumbTop(height, total, start)
		if again, _ := scrollbarThumb(height, total, landed); again != start {
			t.Fatalf("thumb dropped on row %d re-renders on row %d "+
				"(offset %d mapped back to %d)", start, again, offset, landed)
		}
	}
}

// Dragging down a row then reading the thumb back must not creep. Truncating
// the inverse made every drag land one row above the drop point, so a slow
// drag lost ground continuously.
func TestDraggingDoesNotCreep(t *testing.T) {
	const height, total = 12, 200

	for row := 0; row <= height; row++ {
		offset := offsetForThumbTop(height, total, row)
		start, size := scrollbarThumb(height, total, offset)
		if size == 0 {
			t.Fatal("expected a thumb")
		}
		// Rows past the end of travel clamp, which is correct rather than creep.
		if start != row && start+size != height {
			t.Fatalf("dropping the thumb on row %d rendered it on row %d", row, start)
		}
	}
}

func TestDragMathClampsToTheEnds(t *testing.T) {
	const height, total = 10, 100

	if got := offsetForThumbTop(height, total, -50); got != 0 {
		t.Fatalf("dragging above the top gave offset %d, want 0", got)
	}
	if got := offsetForThumbTop(height, total, 999); got != total-height {
		t.Fatalf("dragging past the bottom gave offset %d, want %d", got, total-height)
	}
}

func TestDragMathIsSafeWhenNothingScrolls(t *testing.T) {
	if got := offsetForThumbTop(10, 5, 3); got != 0 {
		t.Fatalf("a transcript that fits should stay at offset 0, got %d", got)
	}
	if _, size := scrollbarThumb(10, 5, 0); size != 0 {
		t.Fatal("a transcript that fits should have no thumb")
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

// longApp returns a ready app whose transcript overflows, so there is
// something to scroll and a thumb to grab.
func longApp(t *testing.T) App {
	t.Helper()
	app := newTestApp(t, testDeps(t, t.TempDir()))
	for i := 0; i < 200; i++ {
		app.entries = append(app.entries, infoEntry(app.theme, "line of history"))
	}
	app.refresh(true)
	return app
}

func press(app App, x, y int) App {
	model, _ := app.Update(tea.MouseMsg{
		Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: x, Y: y})
	return model.(App)
}

func dragTo(app App, x, y int) App {
	model, _ := app.Update(tea.MouseMsg{
		Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft, X: x, Y: y})
	return model.(App)
}

func TestDraggingTheThumbScrollsTheTranscript(t *testing.T) {
	app := longApp(t)
	barX := app.width - 1

	start, _ := scrollbarThumb(app.view.Height, app.view.TotalLineCount(), app.view.YOffset)
	app = press(app, barX, start)
	if !app.dragging {
		t.Fatal("pressing the thumb should begin a drag")
	}

	before := app.view.YOffset
	app = dragTo(app, barX, 0)
	if app.view.YOffset >= before {
		t.Fatalf("dragging to the top should scroll up, offset went %d to %d",
			before, app.view.YOffset)
	}
	if app.view.YOffset != 0 {
		t.Fatalf("dragging to the very top should reach offset 0, got %d",
			app.view.YOffset)
	}
}

// The thumb must stay under the pointer rather than snapping its top to the
// cursor, or a grab near the thumb's bottom jumps the view on first movement.
func TestGrabbingTheThumbKeepsItUnderThePointer(t *testing.T) {
	app := longApp(t)
	barX := app.width - 1

	start, size := scrollbarThumb(app.view.Height, app.view.TotalLineCount(), app.view.YOffset)
	if size < 2 {
		t.Skip("thumb too short to distinguish a grab offset")
	}

	grabRow := start + size - 1
	app = press(app, barX, grabRow)
	if app.dragGrab != size-1 {
		t.Fatalf("grab offset = %d, want %d", app.dragGrab, size-1)
	}
	// Pressing without moving must not shift the view at all.
	if newStart, _ := scrollbarThumb(app.view.Height, app.view.TotalLineCount(),
		app.view.YOffset); newStart != start {
		t.Fatalf("a press alone moved the thumb from %d to %d", start, newStart)
	}
}

// Pressing empty track is a "scroll to here" gesture, the same as the system
// scrollbar this is modelled on.
func TestPressingTheTrackJumpsTheThumbThere(t *testing.T) {
	app := longApp(t)
	barX := app.width - 1

	before := app.view.YOffset
	app = press(app, barX, 0)
	if app.view.YOffset >= before {
		t.Fatalf("pressing the track near the top should scroll up, %d to %d",
			before, app.view.YOffset)
	}
}

func TestReleaseEndsTheDrag(t *testing.T) {
	app := longApp(t)
	barX := app.width - 1

	app = press(app, barX, 2)
	if !app.dragging {
		t.Fatal("setup: expected a drag")
	}

	model, _ := app.Update(tea.MouseMsg{Action: tea.MouseActionRelease, X: barX, Y: 2})
	app = model.(App)
	if app.dragging {
		t.Fatal("releasing should end the drag")
	}

	// Movement after release must not scroll.
	before := app.view.YOffset
	app = dragTo(app, barX, 0)
	if app.view.YOffset != before {
		t.Fatal("movement after release should not scroll")
	}
}

// A click in the transcript is not a click on the bar, or selecting text
// would fling the view around.
func TestClickingTheTranscriptDoesNotStartADrag(t *testing.T) {
	app := longApp(t)

	app = press(app, 5, 3)
	if app.dragging {
		t.Fatal("a press in the transcript must not grab the scrollbar")
	}
}

// The bar sits beside the transcript only. A press below it belongs to the
// prompt band, not the scrollbar.
func TestPressingBelowTheTranscriptDoesNotStartADrag(t *testing.T) {
	app := longApp(t)

	app = press(app, app.width-1, app.view.Height+2)
	if app.dragging {
		t.Fatal("a press below the transcript must not grab the scrollbar")
	}
}

// The wheel arrives as a press too, so it must not be mistaken for a grab.
func TestWheelOverTheScrollbarScrollsRatherThanGrabs(t *testing.T) {
	app := longApp(t)

	model, _ := app.Update(tea.MouseMsg{
		Action: tea.MouseActionPress, Button: tea.MouseButtonWheelUp,
		X: app.width - 1, Y: 3})
	app = model.(App)

	if app.dragging {
		t.Fatal("the wheel must not begin a drag")
	}
}

// Dragging works while a flow runs, the same as the wheel and the page keys.
func TestDraggingWorksWhileAFlowRuns(t *testing.T) {
	app := longApp(t)
	app = typeInto(app, "uninstall")
	app, _ = pressEnter(app)
	if app.block == nil {
		t.Fatal("setup: expected a running flow")
	}

	before := app.view.YOffset
	app = press(app, app.width-1, app.view.Height-1)
	app = dragTo(app, app.width-1, 0)
	if app.view.YOffset >= before {
		t.Fatal("the scrollbar should be draggable while a flow runs")
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
