package terminalshell

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type menuEntry struct {
	label       string
	description string
	activate    func(deps *Deps) Screen
}

type menuScreen struct {
	deps    *Deps
	entries []menuEntry
	cursor  int
}

func newMenuScreen(deps *Deps) *menuScreen {
	return &menuScreen{
		deps: deps,
		entries: []menuEntry{
			{
				label:       "Clean",
				description: "Find and remove reclaimable cache directories",
				activate:    func(d *Deps) Screen { return newCleanDiscoveringScreen(d) },
			},
			{
				label:       "Uninstall",
				description: "Remove an installed application and its data",
				activate:    func(d *Deps) Screen { return newUninstallSearchScreen(d) },
			},
			{
				label:       "Staged",
				description: "Review or restore items removed earlier",
				activate:    func(d *Deps) Screen { return newStagedListScreen(d) },
			},
		},
	}
}

func (m *menuScreen) Title() string { return "" }

func (m *menuScreen) Init() tea.Cmd { return nil }

func (m *menuScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}

	switch keyMsg.String() {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.entries)-1 {
			m.cursor++
		}
	case "enter":
		next := m.entries[m.cursor].activate(m.deps)
		return m, pushScreen(next)
	case "q", "esc":
		return m, popScreen()
	}
	return m, nil
}

func (m *menuScreen) View(theme Theme, width, height int) string {
	var rows []string
	for i, entry := range m.entries {
		prefix := "  "
		labelStyle := lipgloss.NewStyle()
		if i == m.cursor {
			prefix = "> "
			labelStyle = labelStyle.Foreground(theme.Accent).Bold(true)
		}
		label := labelStyle.Render(entry.label)
		desc := lipgloss.NewStyle().Foreground(theme.Muted).Render("  " + entry.description)
		rows = append(rows, prefix+label+desc)
	}

	hints := renderKeyHints(theme,
		[2]string{"↑↓", "select"}, [2]string{"enter", "open"}, [2]string{"q", "quit"})

	content := lipgloss.JoinVertical(lipgloss.Left, rows...)
	return lipgloss.NewStyle().Padding(1, 2).Render(
		lipgloss.JoinVertical(lipgloss.Left, content, "", hints),
	)
}
