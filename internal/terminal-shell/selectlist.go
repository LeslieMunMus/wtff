package terminalshell

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// selectableItem is one row in a selectList: something with a display label,
// a size for the running total, and a reason shown when it is the cursor
// row, so a person can see why an item is here without leaving the list.
type selectableItem struct {
	label     string
	detail    string
	sizeBytes int64
	sizeKnown bool
	selected  bool
}

// selectList is a scrolling, checkbox style list shared by every screen that
// asks a person to choose which of several items to act on: clean's
// candidates, uninstall's leftovers, and staged's batches.
//
// It owns cursor movement, selection toggling, scrolling, and the running
// total of selected sizes. It owns nothing about what happens when the
// person confirms; that is the embedding screen's job, kept separate so this
// widget has no dependency on the deletion engine at all.
type selectList struct {
	items  []selectableItem
	cursor int
	offset int
}

func newSelectList(items []selectableItem) selectList {
	return selectList{items: items}
}

// selectListConfirmMsg is emitted when the person presses Enter, carrying
// the indices of every currently selected item so the embedding screen does
// not need to re-derive selection state itself.
type selectListConfirmMsg struct {
	selected []int
}

func (l selectList) update(msg tea.Msg, visibleRows int) (selectList, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok || len(l.items) == 0 {
		return l, nil
	}

	switch keyMsg.String() {
	case "up", "k":
		if l.cursor > 0 {
			l.cursor--
		}
	case "down", "j":
		if l.cursor < len(l.items)-1 {
			l.cursor++
		}
	case " ", "x":
		l.items[l.cursor].selected = !l.items[l.cursor].selected
	case "a":
		allSelected := true
		for _, item := range l.items {
			if !item.selected {
				allSelected = false
				break
			}
		}
		for i := range l.items {
			l.items[i].selected = !allSelected
		}
	case "enter":
		return l, func() tea.Msg { return selectListConfirmMsg{selected: l.selectedIndices()} }
	}

	l.scrollToCursor(visibleRows)
	return l, nil
}

func (l selectList) selectedIndices() []int {
	var indices []int
	for i, item := range l.items {
		if item.selected {
			indices = append(indices, i)
		}
	}
	return indices
}

func (l selectList) selectedTotalBytes() (total int64, allKnown bool) {
	allKnown = true
	for _, item := range l.items {
		if !item.selected {
			continue
		}
		if item.sizeKnown {
			total += item.sizeBytes
		} else {
			allKnown = false
		}
	}
	return total, allKnown
}

func (l selectList) selectedCount() int {
	count := 0
	for _, item := range l.items {
		if item.selected {
			count++
		}
	}
	return count
}

func (l *selectList) scrollToCursor(visibleRows int) {
	if visibleRows <= 0 {
		return
	}
	if l.cursor < l.offset {
		l.offset = l.cursor
	}
	if l.cursor >= l.offset+visibleRows {
		l.offset = l.cursor - visibleRows + 1
	}
}

func (l selectList) view(theme Theme, width, height int) string {
	if len(l.items) == 0 {
		return lipgloss.NewStyle().Foreground(theme.Muted).Render("nothing found")
	}

	// The detail line for the cursor row and the summary line both need
	// their own space, so the scrolling list itself gets what remains.
	const reservedRows = 2
	visibleRows := height - reservedRows
	if visibleRows < 1 {
		visibleRows = 1
	}

	var rows []string
	end := l.offset + visibleRows
	if end > len(l.items) {
		end = len(l.items)
	}
	for i := l.offset; i < end; i++ {
		rows = append(rows, l.renderRow(theme, i, width))
	}

	detail := ""
	if l.cursor < len(l.items) && l.items[l.cursor].detail != "" {
		detail = lipgloss.NewStyle().
			Foreground(theme.Muted).
			Width(width - 2).
			Render(truncate(l.items[l.cursor].detail, width-2))
	}

	total, allKnown := l.selectedTotalBytes()
	summary := fmt.Sprintf("%d of %d selected, %s", l.selectedCount(), len(l.items), humanBytes(total))
	if !allKnown && l.selectedCount() > 0 {
		summary += " (partial, some sizes unknown)"
	}
	summaryLine := lipgloss.NewStyle().Foreground(theme.Accent).Render(summary)

	return lipgloss.JoinVertical(lipgloss.Left, strings.Join(rows, "\n"), detail, summaryLine)
}

func (l selectList) renderRow(theme Theme, index int, width int) string {
	item := l.items[index]

	checkbox := "[ ]"
	if item.selected {
		checkbox = "[x]"
	}

	size := "?"
	if item.sizeKnown {
		size = humanBytes(item.sizeBytes)
	}

	prefix := "  "
	rowStyle := lipgloss.NewStyle()
	if index == l.cursor {
		prefix = "> "
		rowStyle = rowStyle.Background(theme.Highlight)
	}
	if item.selected {
		rowStyle = rowStyle.Foreground(theme.Accent)
	}

	sizeCol := lipgloss.NewStyle().Width(10).Align(lipgloss.Right).Render(size)
	labelWidth := width - len(prefix) - len(checkbox) - 1 - 10 - 2
	if labelWidth < 1 {
		labelWidth = 1
	}
	label := truncate(item.label, labelWidth)

	line := prefix + checkbox + " " + sizeCol + "  " + label
	return rowStyle.Width(width).Render(line)
}

func truncate(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if len(s) <= width {
		return s
	}
	if width <= 1 {
		return s[:width]
	}
	return s[:width-1] + "…"
}
