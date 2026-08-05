package terminalshell

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const brandName = "wtff"

func renderHeader(theme Theme, title string, width int) string {
	brand := lipgloss.NewStyle().
		Foreground(theme.Accent).
		Bold(true).
		Render(brandName)

	titleStyle := lipgloss.NewStyle().Foreground(theme.Muted)
	line := brand
	if title != "" {
		line = brand + titleStyle.Render("  ·  "+title)
	}

	bar := lipgloss.NewStyle().
		Width(width).
		BorderStyle(lipgloss.NormalBorder()).
		BorderBottom(true).
		BorderForeground(theme.Border).
		Padding(0, 1)

	return bar.Render(line)
}

func renderFooter(theme Theme, status string, isError bool, width int) string {
	style := lipgloss.NewStyle().
		Width(width).
		BorderStyle(lipgloss.NormalBorder()).
		BorderTop(true).
		BorderForeground(theme.Border).
		Padding(0, 1)

	if status == "" {
		return style.Render("")
	}

	color := theme.Success
	if isError {
		color = theme.Danger
	}
	return style.Render(lipgloss.NewStyle().Foreground(color).Render(status))
}

// renderKeyHints formats a row of key-to-action pairs, in the low-key style
// of a terminal application's control legend rather than a full sentence
// per key.
func renderKeyHints(theme Theme, pairs ...[2]string) string {
	style := lipgloss.NewStyle().Foreground(theme.Muted)
	var parts []string
	for _, pair := range pairs {
		parts = append(parts, pair[0]+" "+pair[1])
	}
	return style.Render(strings.Join(parts, "   "))
}
