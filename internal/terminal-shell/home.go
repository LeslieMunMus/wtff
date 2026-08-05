package terminalshell

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// command is one thing the home screen can run, identified by the exact
// lowercase word a person types.
//
// activate is nil for quit and help: quit is handled directly by home
// screen's Update rather than through the same activation path as the
// others, since it does not produce a Screen to move to, and help toggles
// this screen's own display state instead of navigating anywhere.
type command struct {
	name        string
	description string
	activate    func(deps *Deps) Screen
}

// homeCommands is the full, fixed set of words home screen recognizes.
// There is no partial or case-insensitive form of this list: what
// dispatches a command is exact, case-sensitive equality against one of
// these names, checked in matchCommand. The palette below exists to help a
// person find the right word, not to let anything less than the right word
// run something.
var homeCommands = []command{
	{name: "clean", description: "Find and remove reclaimable cache directories",
		activate: func(d *Deps) Screen { return newCleanDiscoveringScreen(d) }},
	{name: "uninstall", description: "Remove an installed application and its data",
		activate: func(d *Deps) Screen { return newUninstallSearchScreen(d) }},
	{name: "staged", description: "Review or restore items removed earlier",
		activate: func(d *Deps) Screen { return newStagedListScreen(d) }},
	{name: "help", description: "Show this list"},
	{name: "quit", description: "Exit wtff"},
}

// matchCommand looks up a command by exact, case-sensitive name. wtff's own
// standing rule is that a person typing something means exactly what they
// typed; silently accepting "Clean" for "clean" would be a small kindness
// that also means the tool sometimes runs on input the person did not
// actually write.
func matchCommand(input string) (command, bool) {
	trimmed := strings.TrimSpace(input)
	for _, c := range homeCommands {
		if c.name == trimmed {
			return c, true
		}
	}
	return command{}, false
}

// filterCommands returns every command whose name starts with prefix, for
// the palette's live filtering as a person types after "/". This is a
// browsing aid over a fixed, fully enumerated list, not a resolution
// mechanism: the person still presses Enter on one specific highlighted
// entry, so nothing here weakens the exact-match rule matchCommand enforces
// for direct input.
func filterCommands(prefix string) []command {
	var out []command
	for _, c := range homeCommands {
		if strings.HasPrefix(c.name, prefix) {
			out = append(out, c)
		}
	}
	return out
}

// homeScreen is wtff's landing screen: a typed command prompt, replacing an
// earlier arrow-key menu entirely. Every available command is listed as
// visible reference text below the prompt at all times; typing "/" moves
// into an interactive filtered selection over that same list, for someone
// who would rather narrow and pick than type the full word from memory.
type homeScreen struct {
	deps  *Deps
	input textinput.Model

	paletteActive bool
	paletteCursor int

	errorMsg string
}

func newHomeScreen(deps *Deps) *homeScreen {
	input := textinput.New()
	input.Placeholder = "type a command, or / to browse"
	input.Focus()
	input.CharLimit = 64
	return &homeScreen{deps: deps, input: input}
}

func (h *homeScreen) Title() string { return "" }

func (h *homeScreen) Init() tea.Cmd { return textinput.Blink }

func (h *homeScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		var cmd tea.Cmd
		h.input, cmd = h.input.Update(msg)
		return h, cmd
	}

	if h.paletteActive {
		return h.updatePalette(keyMsg)
	}
	return h.updatePrompt(keyMsg)
}

func (h *homeScreen) updatePrompt(keyMsg tea.KeyMsg) (Screen, tea.Cmd) {
	if keyMsg.Type == tea.KeyEnter {
		return h.dispatch(h.input.Value())
	}

	var cmd tea.Cmd
	h.input, cmd = h.input.Update(keyMsg)

	if h.input.Value() == "/" {
		h.paletteActive = true
		h.paletteCursor = 0
	}
	return h, cmd
}

func (h *homeScreen) updatePalette(keyMsg tea.KeyMsg) (Screen, tea.Cmd) {
	filtered := filterCommands(strings.TrimPrefix(h.input.Value(), "/"))

	switch keyMsg.Type {
	case tea.KeyUp:
		if h.paletteCursor > 0 {
			h.paletteCursor--
		}
		return h, nil
	case tea.KeyDown:
		if h.paletteCursor < len(filtered)-1 {
			h.paletteCursor++
		}
		return h, nil
	case tea.KeyEsc:
		h.paletteActive = false
		h.input.SetValue("")
		return h, nil
	case tea.KeyEnter:
		if h.paletteCursor >= len(filtered) {
			return h, nil
		}
		return h.dispatch(filtered[h.paletteCursor].name)
	}

	var cmd tea.Cmd
	h.input, cmd = h.input.Update(keyMsg)

	if !strings.HasPrefix(h.input.Value(), "/") {
		h.paletteActive = false
		return h, cmd
	}
	if h.paletteCursor >= len(filterCommands(strings.TrimPrefix(h.input.Value(), "/"))) {
		h.paletteCursor = 0
	}
	return h, cmd
}

func (h *homeScreen) dispatch(rawName string) (Screen, tea.Cmd) {
	name := strings.TrimPrefix(rawName, "/")
	found, ok := matchCommand(name)
	if !ok {
		h.errorMsg = "unknown command: " + rawName + " (type / to browse)"
		return h, nil
	}

	h.errorMsg = ""
	h.input.SetValue("")
	h.paletteActive = false

	switch found.name {
	case "quit":
		return h, tea.Quit
	case "help":
		h.paletteActive = true
		h.paletteCursor = 0
		return h, nil
	default:
		if found.activate == nil {
			// Reachable only if a future command entry is added to
			// homeCommands without an activate function; this already
			// happened once while writing this file and was caught by
			// review before it ever compiled, not by this check. Kept as a
			// second line of defense: refusing cleanly is a better failure
			// than a nil pointer panic taking the whole program down.
			h.errorMsg = found.name + " has no handler wired up"
			return h, nil
		}
		return h, pushScreen(found.activate(h.deps))
	}
}

func (h *homeScreen) View(theme Theme, width, height int) string {
	panelWidth := min(width-4, 58)
	welcome := h.renderWelcomePanel(theme, panelWidth)

	prompt := lipgloss.NewStyle().
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(theme.Border).
		Padding(0, 1).
		Width(min(width-6, 60)).
		Render(h.input.View())

	sections := []string{welcome, "", prompt}

	if h.errorMsg != "" {
		sections = append(sections, "", lipgloss.NewStyle().Foreground(theme.Danger).Render(h.errorMsg))
	}

	sections = append(sections, "", h.renderCommandList(theme))

	if h.paletteActive {
		sections = append(sections, "", renderKeyHints(theme,
			[2]string{"↑↓", "select"}, [2]string{"enter", "run"}, [2]string{"esc", "clear"}))
	} else {
		sections = append(sections, "", renderKeyHints(theme,
			[2]string{"enter", "run"}, [2]string{"/", "browse"}, [2]string{"ctrl+c", "quit"}))
	}

	return lipgloss.NewStyle().Padding(1, 2).Render(lipgloss.JoinVertical(lipgloss.Left, sections...))
}

// renderWelcomePanel is the boxed header shown once at the top of the home
// screen: brand, version, the resting frame of the mark, and a one line
// orientation for someone seeing wtff for the first time. The mark is drawn
// at rest here, phase zero, fully spread; it only animates on screens that
// are actually waiting on something, so a person learns to read motion in
// this program as meaning work is happening, not as constant background
// decoration.
func (h *homeScreen) renderWelcomePanel(theme Theme, width int) string {
	brand := lipgloss.NewStyle().Bold(true).Foreground(theme.Accent).Render(brandName)
	version := lipgloss.NewStyle().Foreground(theme.Muted).Render(h.deps.Version)

	mark := logoFrame(0)

	tagline := lipgloss.NewStyle().Foreground(theme.Muted).Render(
		"A terminal-first macOS maintenance toolkit.")

	content := lipgloss.JoinVertical(lipgloss.Left,
		brand+"  "+version,
		"",
		mark,
		"",
		tagline,
	)

	return lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(theme.Border).
		Padding(1, 2).
		Width(width).
		Render(content)
}

func (h *homeScreen) renderCommandList(theme Theme) string {
	list := homeCommands
	if h.paletteActive {
		list = filterCommands(strings.TrimPrefix(h.input.Value(), "/"))
	}

	var rows []string
	for i, c := range list {
		nameStyle := lipgloss.NewStyle().Foreground(theme.Accent)
		prefix := "  "
		if h.paletteActive && i == h.paletteCursor {
			prefix = "> "
			nameStyle = nameStyle.Bold(true)
		}
		desc := lipgloss.NewStyle().Foreground(theme.Muted).Render("  " + c.description)
		rows = append(rows, prefix+nameStyle.Render(c.name)+desc)
	}
	if len(rows) == 0 {
		return lipgloss.NewStyle().Foreground(theme.Muted).Render("no matching command")
	}
	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}
