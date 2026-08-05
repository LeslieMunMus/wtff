package terminalshell

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// renderKeyHints formats a row of key-to-action pairs, in the low-key style
// of a terminal application's control legend rather than a full sentence
// per key.
func renderKeyHints(theme Theme, pairs ...[2]string) string {
	muted := lipgloss.NewStyle().Foreground(theme.Muted)
	accent := lipgloss.NewStyle().Foreground(theme.Accent)
	var parts []string
	for _, pair := range pairs {
		parts = append(parts, accent.Render(pair[0])+muted.Render(" "+pair[1]))
	}
	return strings.Join(parts, muted.Render(" · "))
}

// renderTitledBox draws content in a rounded border with the title embedded
// in the top border line.
//
// The replacement top line is computed from the box's own rendered width
// using display-width measurement rather than byte length, so the corners
// meet at any title or content width. An earlier version of this used
// hardcoded widths and len(), which misaligned as soon as either changed.
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
