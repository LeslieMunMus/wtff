package terminalshell

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	deletionengine "github.com/lesliemusengi/wtff/internal/deletion-engine"
)

// stagedListScreen lists batches held in the staging area.
//
// This is a single selection list, not a checkbox list like
// candidateListScreen: a batch is restored as the one unit it was staged as,
// and there is no case in this version of the shell for restoring several
// unrelated batches in one action. Listing is done through Init's command
// rather than at construction time, even though reading a handful of batch
// records is normally fast, because "normally" is doing the work a
// synchronous call in the render loop should not be trusted to promise.
type stagedListScreen struct {
	deps    *Deps
	batches []*deletionengine.Batch
	loaded  bool
	loadErr string
	cursor  int
}

func newStagedListScreen(deps *Deps) *stagedListScreen {
	return &stagedListScreen{deps: deps}
}

func (s *stagedListScreen) Title() string { return "Staged" }

type batchesLoadedMsg struct {
	batches []*deletionengine.Batch
	err     error
}

func (s *stagedListScreen) Init() tea.Cmd {
	return func() tea.Msg {
		root, err := deletionengine.DefaultStagingRoot()
		if err != nil {
			return batchesLoadedMsg{err: err}
		}
		area, err := deletionengine.NewStagingArea(root)
		if err != nil {
			return batchesLoadedMsg{err: err}
		}
		batches, err := area.ListBatches()
		return batchesLoadedMsg{batches: batches, err: err}
	}
}

func (s *stagedListScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	if loaded, ok := msg.(batchesLoadedMsg); ok {
		s.loaded = true
		if loaded.err != nil {
			s.loadErr = loaded.err.Error()
			return s, nil
		}
		s.batches = loaded.batches
		return s, nil
	}

	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok || !s.loaded {
		return s, nil
	}
	switch keyMsg.String() {
	case "up", "k":
		if s.cursor > 0 {
			s.cursor--
		}
	case "down", "j":
		if s.cursor < len(s.batches)-1 {
			s.cursor++
		}
	case "esc", "q":
		return s, resetToMenu()
	case "enter":
		if len(s.batches) == 0 {
			return s, nil
		}
		return s, pushScreen(newUndoConfirmScreen(s.deps, s.batches[s.cursor]))
	}
	return s, nil
}

func (s *stagedListScreen) View(theme Theme, width, height int) string {
	if !s.loaded {
		return lipgloss.NewStyle().Padding(1, 2).
			Render(lipgloss.NewStyle().Foreground(theme.Muted).Render("Loading…"))
	}
	if s.loadErr != "" {
		return lipgloss.NewStyle().Padding(1, 2).
			Render(lipgloss.NewStyle().Foreground(theme.Danger).Render(s.loadErr))
	}
	if len(s.batches) == 0 {
		return lipgloss.NewStyle().Padding(1, 2).
			Render(lipgloss.NewStyle().Foreground(theme.Muted).Render("Nothing is staged"))
	}

	var rows []string
	for i, batch := range s.batches {
		prefix := "  "
		style := lipgloss.NewStyle()
		if i == s.cursor {
			prefix = "> "
			style = style.Foreground(theme.Accent).Bold(true)
		}
		total, known := batchTotalBytes(batch)
		size := humanBytes(total)
		if !known {
			size += " (partial)"
		}
		rows = append(rows, style.Render(fmt.Sprintf("%s%s   %-10s   %d item(s)   %s   %s",
			prefix, batch.BatchID, batch.Command, len(batch.Items), size,
			batch.CreatedAt.Local().Format("2006-01-02 15:04"))))
	}

	hints := renderKeyHints(theme, [2]string{"↑↓", "select"}, [2]string{"enter", "restore"}, [2]string{"esc", "back"})
	return lipgloss.NewStyle().Padding(1, 2).Render(
		lipgloss.JoinVertical(lipgloss.Left, lipgloss.JoinVertical(lipgloss.Left, rows...), "", hints),
	)
}

func batchTotalBytes(batch *deletionengine.Batch) (total int64, allKnown bool) {
	allKnown = true
	for _, item := range batch.Items {
		if item.SizeKnown {
			total += item.SizeBytes
		} else {
			allKnown = false
		}
	}
	return total, allKnown
}

// undoConfirmScreen confirms restoring one batch before acting.
type undoConfirmScreen struct {
	deps  *Deps
	batch *deletionengine.Batch
}

func newUndoConfirmScreen(deps *Deps, batch *deletionengine.Batch) *undoConfirmScreen {
	return &undoConfirmScreen{deps: deps, batch: batch}
}

func (s *undoConfirmScreen) Title() string { return "Staged · confirm" }

func (s *undoConfirmScreen) Init() tea.Cmd { return nil }

func (s *undoConfirmScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return s, nil
	}
	switch keyMsg.String() {
	case "y", "enter":
		return newUndoApplyingScreen(s.deps, s.batch), nil
	case "esc", "n":
		return s, popScreen()
	}
	return s, nil
}

func (s *undoConfirmScreen) View(theme Theme, width, height int) string {
	total, known := batchTotalBytes(s.batch)
	size := humanBytes(total)
	if !known {
		size += " (partial)"
	}
	summary := fmt.Sprintf("Restore %d item(s), %s, from %s", len(s.batch.Items), size, s.batch.BatchID)
	content := lipgloss.JoinVertical(lipgloss.Left,
		lipgloss.NewStyle().Bold(true).Render(summary),
		"",
		lipgloss.NewStyle().Foreground(theme.Accent).Render("Proceed? [y/N]"),
	)
	return lipgloss.NewStyle().Padding(1, 2).Render(content)
}

// undoApplyingScreen runs the restore and waits for it to finish.
type undoApplyingScreen struct {
	deps  *Deps
	batch *deletionengine.Batch
}

func newUndoApplyingScreen(deps *Deps, batch *deletionengine.Batch) *undoApplyingScreen {
	return &undoApplyingScreen{deps: deps, batch: batch}
}

type undoReadyMsg struct {
	result *deletionengine.RestoreResult
	err    error
}

func (s *undoApplyingScreen) Title() string { return "Staged" }

func (s *undoApplyingScreen) Init() tea.Cmd {
	return func() tea.Msg {
		result, err := deletionengine.Undo(s.batch, s.deps.Log)
		return undoReadyMsg{result: result, err: err}
	}
}

func (s *undoApplyingScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	ready, ok := msg.(undoReadyMsg)
	if !ok {
		return s, nil
	}
	if ready.err != nil {
		return s, tea.Batch(showStatus("restore failed: "+ready.err.Error(), true), resetToMenu())
	}
	return newUndoResultsScreen(ready.result), nil
}

func (s *undoApplyingScreen) View(theme Theme, width, height int) string {
	text := lipgloss.NewStyle().Foreground(theme.Muted).Render("Restoring…")
	return lipgloss.NewStyle().Padding(1, 2).Render(text)
}

type undoResultsScreen struct {
	result *deletionengine.RestoreResult
}

func newUndoResultsScreen(result *deletionengine.RestoreResult) *undoResultsScreen {
	return &undoResultsScreen{result: result}
}

func (s *undoResultsScreen) Title() string { return "Staged · done" }

func (s *undoResultsScreen) Init() tea.Cmd { return nil }

func (s *undoResultsScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	if _, ok := msg.(tea.KeyMsg); ok {
		return s, resetToMenu()
	}
	return s, nil
}

func (s *undoResultsScreen) View(theme Theme, width, height int) string {
	lines := []string{
		lipgloss.NewStyle().Foreground(theme.Success).Bold(true).
			Render(fmt.Sprintf("Restored %d item(s)", s.result.RestoredCount)),
	}
	if s.result.SkippedCount > 0 {
		lines = append(lines, lipgloss.NewStyle().Foreground(theme.Warning).
			Render(fmt.Sprintf("%d item(s) left in staging", s.result.SkippedCount)))
	}
	if s.result.FailedCount > 0 {
		lines = append(lines, lipgloss.NewStyle().Foreground(theme.Danger).
			Render(fmt.Sprintf("%d item(s) failed", s.result.FailedCount)))
	}
	lines = append(lines, "", lipgloss.NewStyle().Foreground(theme.Muted).Render("Press any key to continue"))
	return lipgloss.NewStyle().Padding(1, 2).Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}
