package terminalshell

import (
	"io"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// App is the root Bubble Tea model, and the whole shell.
//
// There is no screen stack. An earlier version navigated between full
// screens, so running a command replaced the entire view and the prompt
// vanished; the project manager rejected that outright. The model here is a
// transcript instead: commands and their results accumulate in a scrollable
// viewport, one interactive block at a time is pinned above the prompt while
// a flow runs, and the prompt itself never moves.
type App struct {
	deps  *Deps
	theme Theme

	entries []transcriptEntry
	view    viewport.Model
	input   textinput.Model

	// block is the pinned interactive component, nil when no flow is
	// running. Exactly one at a time by construction: a flow hands off from
	// one block to the next through flowMsg rather than nesting.
	block liveBlock

	paletteActive bool
	paletteCursor int

	width, height int
	ready         bool
	quitting      bool
}

// NewApp constructs the shell. hasDarkBackground is retained for the theme
// hook even though the brand palette is currently unconditional.
func NewApp(deps *Deps, hasDarkBackground bool, _ any) App {
	theme := detectTheme(hasDarkBackground)

	input := textinput.New()
	input.Placeholder = "type a command, or / to browse"
	input.Prompt = "❯ "
	input.CharLimit = 128
	// The suggestion is styled apart from typed text on purpose: the two
	// occupy the same cells, and without a weight difference an empty prompt
	// and a real command read identically.
	input.PlaceholderStyle = lipgloss.NewStyle().Foreground(theme.Placeholder)
	input.Cursor.Style = lipgloss.NewStyle().Foreground(theme.Accent)
	input.Focus()

	return App{
		deps:  deps,
		theme: theme,
		input: input,
	}
}

// Init starts the cursor blinking, which is the shell's only standing signal
// that the program is alive and waiting rather than busy or wedged.
func (a App) Init() tea.Cmd { return textinput.Blink }

func (a App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return a.resize(msg), nil

	case flowMsg:
		return a.applyFlow(msg)

	case tea.MouseMsg:
		// The wheel scrolls the transcript, mid-flow included. These messages
		// previously reached nothing at all: the type switch had no case for
		// them and the fallthrough only fed a running block, so the wheel was
		// silently inert and the transcript looked unscrollable.
		var cmd tea.Cmd
		a.view, cmd = a.view.Update(msg)
		return a, cmd

	case tea.KeyMsg:
		return a.handleKey(msg)
	}

	// Anything else: spinner ticks to a running block, and cursor blink to the
	// prompt. The blink messages are not key messages, so routing only to the
	// block meant the cursor never animated once a flow had run.
	var cmds []tea.Cmd
	if a.block != nil {
		updated, cmd := a.block.Update(msg)
		a.block = updated
		cmds = append(cmds, cmd)
	}
	var inputCmd tea.Cmd
	a.input, inputCmd = a.input.Update(msg)
	cmds = append(cmds, inputCmd)
	return a, tea.Batch(cmds...)
}

// resize recomputes the viewport, which owns its own height so the prompt
// band stays anchored at the bottom regardless of transcript length.
func (a App) resize(msg tea.WindowSizeMsg) App {
	a.width, a.height = msg.Width, msg.Height
	viewHeight := a.viewportHeight()

	// The scrollbar's column is taken out of the viewport's own width rather
	// than added beside it, so the two together occupy exactly the terminal
	// width and nothing wraps.
	contentWidth := a.width - scrollbarWidth
	if contentWidth < 1 {
		contentWidth = 1
	}

	if !a.ready {
		a.view = viewport.New(contentWidth, viewHeight)
		a.ready = true
		// The welcome box is the first transcript entry rather than a fixed
		// header: it scrolls away as history accumulates, the way the
		// reference application's own greeting does.
		a.entries = append(a.entries, welcomeEntry(a.deps, a.theme, a.width-4))
	} else {
		a.view.Width = contentWidth
		a.view.Height = viewHeight
	}
	a.refresh(true)
	return a
}

// viewportHeight is what remains after the pinned block and prompt band.
func (a App) viewportHeight() int {
	h := a.height - lipgloss.Height(a.promptBand()) - 1
	if a.block != nil {
		h -= lipgloss.Height(a.block.View(a.theme, a.width-2)) + 1
	}
	if h < 3 {
		h = 3
	}
	return h
}

// refresh re-renders the transcript into the viewport, optionally jumping
// to the newest entry, which is what a person expects after running a
// command.
func (a *App) refresh(toBottom bool) {
	var blocks []string
	for _, entry := range a.entries {
		blocks = append(blocks, entry.render(a.theme))
	}
	a.view.SetContent(lipgloss.NewStyle().Padding(0, 2).
		Render(strings.Join(blocks, "\n\n")))
	if toBottom {
		a.view.GotoBottom()
	}
}

// applyFlow appends a flow's transcript entries and swaps the pinned block,
// then re-lays out, since gaining or losing a block changes viewport height.
func (a App) applyFlow(msg flowMsg) (tea.Model, tea.Cmd) {
	a.entries = append(a.entries, msg.entries...)
	var cmd tea.Cmd
	if msg.setBlock {
		a.block = msg.block
		if a.block != nil {
			cmd = a.block.Init()
		} else {
			// The flow is over, so the prompt takes the cursor back. Focus
			// follows where typing actually goes, which is the only honest
			// place for a blinking cursor to be.
			a.input.Focus()
			cmd = textinput.Blink
		}
	}
	if a.ready {
		a.view.Height = a.viewportHeight()
		a.refresh(true)
	}
	return a, cmd
}

func (a App) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// ctrl+c always quits, whatever is running.
	if msg.Type == tea.KeyCtrlC {
		a.quitting = true
		return a, tea.Quit
	}

	// Scrollback is always available, even mid-flow, so a person can read
	// back over earlier output while something runs.
	switch msg.Type {
	case tea.KeyPgUp, tea.KeyPgDown, tea.KeyHome, tea.KeyEnd:
		var cmd tea.Cmd
		a.view, cmd = a.view.Update(msg)
		return a, cmd
	case tea.KeyUp, tea.KeyDown:
		// The arrows scroll the transcript only when nothing else wants them.
		// The palette uses them to move its cursor, and a live block owns the
		// keyboard outright, so both are checked first. Page keys are not
		// conditional this way because nothing else claims them.
		if !a.paletteActive && a.block == nil {
			var cmd tea.Cmd
			a.view, cmd = a.view.Update(msg)
			return a, cmd
		}
	}

	// ctrl+o toggles the newest disclosure, keeping verbose output one
	// keypress away without cluttering the viewport by default.
	if msg.Type == tea.KeyCtrlO {
		for i := len(a.entries) - 1; i >= 0; i-- {
			if len(a.entries[i].details) > 0 {
				a.entries[i].expanded = !a.entries[i].expanded
				a.refresh(true)
				break
			}
		}
		return a, nil
	}

	// A running block owns the keyboard: it is the interactive step.
	if a.block != nil {
		updated, cmd := a.block.Update(msg)
		a.block = updated
		return a, cmd
	}

	if a.paletteActive {
		return a.handlePaletteKey(msg)
	}
	return a.handlePromptKey(msg)
}

func (a App) handlePromptKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyEnter {
		return a.submit(a.input.Value())
	}
	var cmd tea.Cmd
	a.input, cmd = a.input.Update(msg)
	if a.input.Value() == "/" {
		a.paletteActive = true
		a.paletteCursor = 0
	}
	return a, cmd
}

func (a App) handlePaletteKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	filtered := filterCommands(strings.TrimPrefix(a.input.Value(), "/"))

	switch msg.Type {
	case tea.KeyUp:
		if a.paletteCursor > 0 {
			a.paletteCursor--
		}
		return a, nil
	case tea.KeyDown:
		if a.paletteCursor < len(filtered)-1 {
			a.paletteCursor++
		}
		return a, nil
	case tea.KeyEsc:
		a.paletteActive = false
		a.input.SetValue("")
		return a, nil
	case tea.KeyEnter:
		if a.paletteCursor >= len(filtered) {
			return a, nil
		}
		return a.submit(filtered[a.paletteCursor].name)
	}

	var cmd tea.Cmd
	a.input, cmd = a.input.Update(msg)
	if !strings.HasPrefix(a.input.Value(), "/") {
		a.paletteActive = false
		return a, cmd
	}
	if a.paletteCursor >= len(filterCommands(strings.TrimPrefix(a.input.Value(), "/"))) {
		a.paletteCursor = 0
	}
	return a, cmd
}

// submit echoes the typed line into the transcript and dispatches it. Every
// path through here appends the echo first, so history always shows what was
// asked for, including the requests that were rejected.
func (a App) submit(raw string) (tea.Model, tea.Cmd) {
	typed := strings.TrimSpace(strings.TrimPrefix(raw, "/"))
	a.input.SetValue("")
	a.paletteActive = false
	if typed == "" {
		return a, nil
	}

	a.entries = append(a.entries, echoEntry(a.theme, typed))

	name, arg := typed, ""
	if idx := strings.IndexByte(typed, ' '); idx >= 0 {
		name, arg = typed[:idx], strings.TrimSpace(typed[idx+1:])
	}

	found, ok := matchCommand(name)
	if !ok {
		a.entries = append(a.entries,
			errorEntry(a.theme, "unknown command: "+name+" · type / to browse"))
		a.refresh(true)
		return a, nil
	}
	if arg != "" && !found.takesArg {
		a.entries = append(a.entries,
			errorEntry(a.theme, found.name+" takes no arguments"))
		a.refresh(true)
		return a, nil
	}

	switch found.name {
	case "quit":
		a.quitting = true
		return a, tea.Quit
	case "help":
		a.entries = append(a.entries, helpEntry(a.theme))
		a.refresh(true)
		return a, nil
	}

	if found.start == nil {
		// Reachable only if a command is added without a start function.
		// Refusing cleanly beats a nil call taking the program down; this
		// exact omission already happened once during development.
		a.entries = append(a.entries,
			errorEntry(a.theme, found.name+" has no handler wired up"))
		a.refresh(true)
		return a, nil
	}

	a.block = found.start(a.deps, a.theme, arg)
	// The block owns the keyboard now, so the prompt gives up its cursor
	// rather than blinking at a place that would ignore what is typed there.
	a.input.Blur()
	a.view.Height = a.viewportHeight()
	a.refresh(true)
	return a, a.block.Init()
}

func (a App) View() string {
	if a.quitting || !a.ready {
		return ""
	}

	transcript := lipgloss.JoinHorizontal(lipgloss.Top,
		a.view.View(),
		renderScrollbar(a.theme, a.view.Height, a.view.TotalLineCount(), a.view.YOffset),
	)

	sections := []string{transcript}
	if a.block != nil {
		sections = append(sections,
			lipgloss.NewStyle().Padding(0, 2).Render(a.block.View(a.theme, a.width-2)))
	}
	sections = append(sections, a.promptBand())
	return strings.Join(sections, "\n")
}

// promptBand is the fixed bottom region: the palette when browsing, the
// framed input between dividers, and the key hints.
func (a App) promptBand() string {
	width := a.width - 4
	if width < 20 {
		width = 20
	}
	divider := lipgloss.NewStyle().Foreground(a.theme.Border).
		Render(strings.Repeat("─", width))

	var rows []string
	if a.paletteActive {
		rows = append(rows, a.renderPalette(), "")
	}
	rows = append(rows,
		divider,
		a.input.View(),
		divider,
		renderKeyHints(a.theme,
			[2]string{"enter", "run"}, [2]string{"/", "browse"},
			[2]string{"ctrl+o", "details"}, [2]string{"ctrl+c", "quit"}),
	)
	return lipgloss.NewStyle().Padding(0, 2).
		Render(lipgloss.JoinVertical(lipgloss.Left, rows...))
}

func (a App) renderPalette() string {
	list := filterCommands(strings.TrimPrefix(a.input.Value(), "/"))
	if len(list) == 0 {
		return lipgloss.NewStyle().Foreground(a.theme.Muted).Render("no matching command")
	}
	body := lipgloss.NewStyle().Foreground(a.theme.Body)
	var rows []string
	for i, c := range list {
		nameStyle := lipgloss.NewStyle().Foreground(a.theme.Accent)
		rowStyle := lipgloss.NewStyle()
		prefix := "  "
		if i == a.paletteCursor {
			prefix = "> "
			nameStyle = nameStyle.Bold(true)
			rowStyle = rowStyle.Background(a.theme.Highlight)
		}
		rows = append(rows, rowStyle.Render(prefix+nameStyle.Render(c.name)+
			body.Render("  "+c.description)))
	}
	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}

// Run starts the shell against the given input and output, in the alternate
// screen buffer, with mouse wheel scrolling enabled for the transcript.
func Run(deps *Deps, hasDarkBackground bool, input io.Reader, output io.Writer) error {
	program := tea.NewProgram(
		NewApp(deps, hasDarkBackground, nil),
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
		tea.WithInput(input),
		tea.WithOutput(output),
	)
	_, err := program.Run()
	return err
}
