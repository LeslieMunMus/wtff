package terminalshell

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func sampleItems() []selectableItem {
	return []selectableItem{
		{label: "one", sizeBytes: 100, sizeKnown: true, selected: true},
		{label: "two", sizeBytes: 200, sizeKnown: true, selected: true},
		{label: "three", sizeBytes: 0, sizeKnown: false, selected: false},
	}
}

func keyMsg(key string) tea.KeyMsg {
	switch key {
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case " ":
		return tea.KeyMsg{Type: tea.KeySpace}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
	}
}

func TestSelectListCursorMovement(t *testing.T) {
	l := newSelectList(sampleItems())
	l, _ = l.update(keyMsg("down"), 10)
	if l.cursor != 1 {
		t.Fatalf("cursor = %d, want 1", l.cursor)
	}
	l, _ = l.update(keyMsg("down"), 10)
	l, _ = l.update(keyMsg("down"), 10)
	if l.cursor != 2 {
		t.Fatalf("cursor = %d, want 2 (should not move past the last item)", l.cursor)
	}
	l, _ = l.update(keyMsg("up"), 10)
	if l.cursor != 1 {
		t.Fatalf("cursor = %d, want 1", l.cursor)
	}
}

func TestSelectListToggleSelection(t *testing.T) {
	l := newSelectList(sampleItems())
	if !l.items[0].selected {
		t.Fatal("setup: item 0 should start selected")
	}
	l, _ = l.update(keyMsg(" "), 10)
	if l.items[0].selected {
		t.Fatal("space did not deselect the cursor item")
	}
	l, _ = l.update(keyMsg(" "), 10)
	if !l.items[0].selected {
		t.Fatal("space did not reselect the cursor item")
	}
}

func TestSelectListToggleAll(t *testing.T) {
	l := newSelectList(sampleItems())
	l, _ = l.update(keyMsg("a"), 10)
	for i, item := range l.items {
		if !item.selected {
			t.Fatalf("item %d not selected after select-all", i)
		}
	}
	l, _ = l.update(keyMsg("a"), 10)
	for i, item := range l.items {
		if item.selected {
			t.Fatalf("item %d still selected after deselect-all", i)
		}
	}
}

func TestSelectListSelectedTotalBytes(t *testing.T) {
	l := newSelectList(sampleItems())
	total, allKnown := l.selectedTotalBytes()
	if total != 300 || !allKnown {
		t.Fatalf("total=%d allKnown=%v, want 300 true", total, allKnown)
	}

	// Selecting the unknown-size item must mark the total as partial rather
	// than silently treating the missing size as zero.
	l, _ = l.update(keyMsg("down"), 10)
	l, _ = l.update(keyMsg("down"), 10)
	l, _ = l.update(keyMsg(" "), 10)
	total, allKnown = l.selectedTotalBytes()
	if total != 300 || allKnown {
		t.Fatalf("total=%d allKnown=%v, want 300 false (partial)", total, allKnown)
	}
}

func TestSelectListEnterEmitsSelectedIndices(t *testing.T) {
	l := newSelectList(sampleItems())
	_, cmd := l.update(keyMsg("enter"), 10)
	if cmd == nil {
		t.Fatal("enter did not produce a command")
	}
	msg := cmd()
	confirm, ok := msg.(selectListConfirmMsg)
	if !ok {
		t.Fatalf("message type = %T, want selectListConfirmMsg", msg)
	}
	if len(confirm.selected) != 2 || confirm.selected[0] != 0 || confirm.selected[1] != 1 {
		t.Fatalf("selected = %v, want [0 1]", confirm.selected)
	}
}

func TestSelectListEmptyDoesNotPanic(t *testing.T) {
	l := newSelectList(nil)
	l, cmd := l.update(keyMsg("down"), 10)
	if cmd != nil {
		t.Fatal("expected no command from an empty list")
	}
	_ = l.view(darkTheme, 80, 20)
}

// Scrolling must keep the cursor visible within a bounded viewport. This is
// the mechanism a long candidate list, such as the roughly ninety entries a
// real ~/Library/Caches scan produces, depends on to stay usable at any
// terminal height.
func TestSelectListScrollsToKeepCursorVisible(t *testing.T) {
	items := make([]selectableItem, 20)
	for i := range items {
		items[i] = selectableItem{label: "item", selected: true}
	}
	l := newSelectList(items)

	for i := 0; i < 10; i++ {
		l, _ = l.update(keyMsg("down"), 5)
	}
	if l.cursor != 10 {
		t.Fatalf("cursor = %d, want 10", l.cursor)
	}
	if l.offset > l.cursor || l.cursor >= l.offset+5 {
		t.Fatalf("cursor %d not within visible window [%d, %d)", l.cursor, l.offset, l.offset+5)
	}
}
