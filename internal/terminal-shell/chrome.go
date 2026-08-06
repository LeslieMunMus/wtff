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

// scrollbarWidth is the single column the scrollbar occupies. It is reserved
// whether or not the bar is currently visible, so the transcript does not
// reflow the moment content grows past one screen.
const scrollbarWidth = 1

// renderScrollbar draws the transcript's vertical scrollbar as one column of
// height lines: a thumb in the main color over a track in the highlight color.
//
// The viewport component has no scrollbar of its own, so without this a person
// has no indication that anything exists above the fold, which is exactly how
// a transcript that scrolls perfectly well reads as one that is stuck.
func renderScrollbar(theme Theme, height, totalLines, offset int) string {
	track := lipgloss.NewStyle().Foreground(theme.Highlight)
	thumbStyle := lipgloss.NewStyle().Foreground(theme.Accent)

	if height <= 0 {
		return ""
	}
	// Nothing to scroll: the column stays, blank, holding its place.
	if totalLines <= height {
		return strings.TrimSuffix(strings.Repeat(" \n", height), "\n")
	}

	// The thumb's length is the visible fraction of the whole, floored at one
	// line so it never disappears on a very long transcript.
	thumb := height * height / totalLines
	if thumb < 1 {
		thumb = 1
	}

	maxOffset := totalLines - height
	if offset > maxOffset {
		offset = maxOffset
	}
	if offset < 0 {
		offset = 0
	}

	// Positioned from the scroll offset rather than a percentage, so the
	// bottom of the travel lands exactly at the bottom of the track.
	start := 0
	if maxOffset > 0 {
		start = offset * (height - thumb) / maxOffset
	}

	lines := make([]string, height)
	for i := range lines {
		if i >= start && i < start+thumb {
			lines[i] = thumbStyle.Render("┃")
		} else {
			lines[i] = track.Render("│")
		}
	}
	return strings.Join(lines, "\n")
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
