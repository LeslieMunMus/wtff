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

// scrollbarWidth is how many columns the scrollbar draws in, kept slim to
// match the system bar it is modelled on. The column is reserved whether or
// not the bar is visible, so the transcript does not reflow the moment content
// grows past one screen.
const scrollbarWidth = 1

// scrollbarGrabSlack widens the grabbable region to the left of the bar
// without widening the bar itself, the way a system scrollbar has a hit area
// larger than its visible track. The columns it covers are the transcript's
// right padding, so nothing readable is stolen.
const scrollbarGrabSlack = 2

// scrollbarSegment renders one row of the bar.
//
// The color is a background on blank cells rather than a foreground on a block
// glyph. A block glyph depends on the font filling its cell to the full line
// height, and many monospace fonts do not, which leaves thin horizontal gaps
// between rows and turns a continuous bar into a dashed one. A background
// fills the whole cell in every font.
func scrollbarSegment(color lipgloss.Color) string {
	return lipgloss.NewStyle().Background(color).
		Render(strings.Repeat(" ", scrollbarWidth))
}

// scrollbarThumb returns the thumb's top row and length for a given scroll
// position, or a zero length when there is nothing to scroll.
//
// Rendering and dragging both go through here on purpose. They are inverses
// of each other, and two separate implementations of the same arithmetic
// drift, which shows up as a thumb that does not sit where the pointer left
// it.
func scrollbarThumb(height, totalLines, offset int) (start, size int) {
	if height <= 0 || totalLines <= height {
		return 0, 0
	}

	// The thumb's length is the visible fraction of the whole, floored at one
	// line so it never disappears on a very long transcript.
	size = height * height / totalLines
	if size < 1 {
		size = 1
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
	if maxOffset > 0 {
		start = offset * (height - size) / maxOffset
	}
	return start, size
}

// offsetForThumbTop is the inverse of scrollbarThumb: given where a person
// dragged the thumb's top, it reports the scroll offset that puts it there.
func offsetForThumbTop(height, totalLines, thumbTop int) int {
	_, size := scrollbarThumb(height, totalLines, 0)
	if size == 0 {
		return 0
	}
	travel := height - size
	if travel <= 0 {
		return 0
	}
	if thumbTop < 0 {
		thumbTop = 0
	}
	if thumbTop > travel {
		thumbTop = travel
	}

	// Rounded up, not truncated. scrollbarThumb divides down, so each thumb
	// row stands for a band of offsets, and truncating here lands on the
	// largest offset of the band below: the thumb would render one row above
	// where it was dropped, and creep upward on every drag. The smallest
	// offset that renders at this row is the ceiling of the exact quotient.
	maxOffset := totalLines - height
	return (thumbTop*maxOffset + travel - 1) / travel
}

// renderScrollbar draws the transcript's vertical scrollbar: a thumb in the
// main color over a track in the highlight color.
//
// The viewport component has no scrollbar of its own, so without this a person
// has no indication that anything exists above the fold, which is exactly how
// a transcript that scrolls perfectly well reads as one that is stuck.
func renderScrollbar(theme Theme, height, totalLines, offset int) string {
	if height <= 0 {
		return ""
	}

	start, size := scrollbarThumb(height, totalLines, offset)
	if size == 0 {
		// Nothing to scroll: the column stays, blank, holding its place.
		blank := strings.Repeat(" ", scrollbarWidth)
		lines := make([]string, height)
		for i := range lines {
			lines[i] = blank
		}
		return strings.Join(lines, "\n")
	}

	track := scrollbarSegment(theme.Highlight)
	thumb := scrollbarSegment(theme.Accent)

	lines := make([]string, height)
	for i := range lines {
		if i >= start && i < start+size {
			lines[i] = thumb
		} else {
			lines[i] = track
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
