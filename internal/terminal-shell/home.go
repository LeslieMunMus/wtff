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
// earlier arrow-key menu entirely. The layout follows the reference the
// project manager supplied in mood-board/claude-welcome-menu.png: a
// full-width header box with the program name embedded in its top border,
// a two-column interior, welcome and mark on the left, orientation and the
// command list on the right, and the prompt anchored at the bottom of the
// screen rather than floating in the middle of it.
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
	input.Prompt = "❯ "
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

// View lays the screen out in three bands: the welcome box at the top, a
// filler region, and the prompt block anchored at the bottom, matching the
// supplied reference. Horizontal padding is applied per band rather than
// wrapping the whole screen, because the filler between the bands is raw
// newlines and must not carry padding-induced width.
func (h *homeScreen) View(theme Theme, width, height int) string {
	innerWidth := width - 4
	if innerWidth < 40 {
		innerWidth = 40
	}
	pad := lipgloss.NewStyle().Padding(0, 2)

	top := pad.Render(lipgloss.JoinVertical(lipgloss.Left,
		h.renderWelcomeBox(theme, innerWidth),
		"",
		h.renderNoteLine(theme),
	))
	bottom := pad.Render(h.renderPromptBlock(theme, innerWidth))

	filler := height - lipgloss.Height(top) - lipgloss.Height(bottom)
	if filler < 1 {
		filler = 1
	}

	return top + strings.Repeat("\n", filler) + bottom
}

// renderWelcomeBox is the full-width header: program name embedded in the
// top border, welcome and the mark on the left, orientation and commands on
// the right.
func (h *homeScreen) renderWelcomeBox(theme Theme, width int) string {
	leftWidth := 30
	rightWidth := width - leftWidth - 6
	if rightWidth < 20 {
		rightWidth = 20
	}

	muted := lipgloss.NewStyle().Foreground(theme.Muted)
	accent := lipgloss.NewStyle().Foreground(theme.Accent).Bold(true)

	left := lipgloss.NewStyle().Width(leftWidth).Align(lipgloss.Center).Render(
		lipgloss.JoinVertical(lipgloss.Center,
			lipgloss.NewStyle().Bold(true).Render("Welcome to wtff"),
			"",
			logoFrame(0),
			"",
			muted.Render("A terminal-first macOS maintenance toolkit"),
			muted.Render(h.deps.Home),
		))

	var commandRows []string
	for _, c := range homeCommands {
		commandRows = append(commandRows,
			lipgloss.NewStyle().Foreground(theme.Accent).Render(c.name)+
				muted.Render("  "+c.description))
	}

	right := lipgloss.NewStyle().Width(rightWidth).PaddingLeft(2).Render(
		lipgloss.JoinVertical(lipgloss.Left,
			accent.Render("Getting started"),
			"Type a command and press Enter. Commands are exact, lowercase words.",
			"Type / to browse and filter this list instead.",
			muted.Render(strings.Repeat("─", rightWidth-2)),
			accent.Render("Commands"),
			lipgloss.JoinVertical(lipgloss.Left, commandRows...),
		))

	body := lipgloss.JoinHorizontal(lipgloss.Top, left, right)
	return renderTitledBox(theme, "wtff "+h.deps.Version, body, width)
}

// renderNoteLine sits under the box: the current error when there is one,
// otherwise a quiet standing reassurance about reversibility.
func (h *homeScreen) renderNoteLine(theme Theme) string {
	if h.errorMsg != "" {
		return lipgloss.NewStyle().Foreground(theme.Danger).Render("▌ " + h.errorMsg)
	}
	return lipgloss.NewStyle().Foreground(theme.Muted).
		Render("▌ Removals are staged and reversible. Nothing is deleted permanently without --purge.")
}

// renderPromptBlock is the bottom-anchored band: the palette when active,
// then the framed input line between two dividers, then the key hints.
func (h *homeScreen) renderPromptBlock(theme Theme, width int) string {
	divider := lipgloss.NewStyle().Foreground(theme.Border).
		Render(strings.Repeat("─", width))

	var sections []string
	if h.paletteActive {
		sections = append(sections, h.renderPalette(theme), "")
	}
	sections = append(sections,
		divider,
		h.input.View(),
		divider,
		renderKeyHints(theme,
			[2]string{"enter", "run"}, [2]string{"/", "browse"}, [2]string{"ctrl+c", "quit"}),
	)
	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

// renderPalette is the filtered, cursor-driven list shown above the prompt
// while palette mode is active.
func (h *homeScreen) renderPalette(theme Theme) string {
	list := filterCommands(strings.TrimPrefix(h.input.Value(), "/"))
	if len(list) == 0 {
		return lipgloss.NewStyle().Foreground(theme.Muted).Render("no matching command")
	}

	muted := lipgloss.NewStyle().Foreground(theme.Muted)
	var rows []string
	for i, c := range list {
		nameStyle := lipgloss.NewStyle().Foreground(theme.Accent)
		prefix := "  "
		if i == h.paletteCursor {
			prefix = "> "
			nameStyle = nameStyle.Bold(true)
		}
		rows = append(rows, prefix+nameStyle.Render(c.name)+muted.Render("  "+c.description))
	}
	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}

// renderTitledBox draws content in a rounded border with the title embedded
// in the top border line, the way the reference layout does.
//
// The supplied example code did this with hardcoded widths and byte-length
// math, which misaligns the corners whenever the title or width changes;
// here the replacement top line is computed from the box's actual rendered
// width using display-width measurement, so the corners always meet.
func renderTitledBox(theme Theme, title, content string, width int) string {
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme.Border).
		Padding(0, 1).
		Width(width - 2).
		Render(content)

	lines := strings.Split(box, "\n")
	if len(lines) == 0 {
		return box
	}

	boxWidth := lipgloss.Width(lines[0])
	borderStyle := lipgloss.NewStyle().Foreground(theme.Border)
	titleStyle := lipgloss.NewStyle().Foreground(theme.Accent).Bold(true)

	fill := boxWidth - lipgloss.Width(title) - 5
	if fill < 0 {
		fill = 0
	}
	lines[0] = borderStyle.Render("╭─ ") + titleStyle.Render(title) +
		borderStyle.Render(" "+strings.Repeat("─", fill)+"╮")

	return strings.Join(lines, "\n")
}
