package terminalshell

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	deletionengine "github.com/lesliemusengi/wtff/internal/deletion-engine"
)

// startStagedFlow begins the staged flow: list what is held, pick a batch,
// restore it, all through pinned live blocks and transcript entries.
func startStagedFlow(deps *Deps, theme Theme) liveBlock {
	return &stagedLoadBlock{deps: deps, theme: theme,
		activity: newActivityIndicator("Loading")}
}

type stagedLoadBlock struct {
	deps     *Deps
	theme    Theme
	activity activityIndicator
}

type batchesLoadedMsg struct {
	batches []*deletionengine.Batch
	err     error
}

func (s *stagedLoadBlock) Init() tea.Cmd {
	return tea.Batch(
		func() tea.Msg {
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
		},
		s.activity.init(),
	)
}

func (s *stagedLoadBlock) Update(msg tea.Msg) (liveBlock, tea.Cmd) {
	loaded, ok := msg.(batchesLoadedMsg)
	if !ok {
		updated, cmd := s.activity.update(msg)
		s.activity = updated
		return s, cmd
	}

	if loaded.err != nil {
		return s, finish(errorEntry(s.theme, "cannot read the staging area: "+loaded.err.Error()))
	}
	if len(loaded.batches) == 0 {
		return s, finish(infoEntry(s.theme, "Nothing is staged."))
	}
	return s, transition(newBatchPickBlock(s.deps, s.theme, loaded.batches))
}

func (s *stagedLoadBlock) View(theme Theme, width int) string {
	return "  " + s.activity.view(theme)
}

func newBatchPickBlock(deps *Deps, theme Theme, batches []*deletionengine.Batch) *pickBlock {
	rows := make([]string, len(batches))
	for i, batch := range batches {
		total, known := batchTotalBytes(batch)
		size := humanBytes(total)
		if !known {
			size += " (partial)"
		}
		rows[i] = fmt.Sprintf("%s  %-10s  %d item(s)  %s  %s",
			batch.BatchID, batch.Command, len(batch.Items), size,
			batch.CreatedAt.Local().Format("2006-01-02 15:04"))
	}
	return &pickBlock{
		theme: theme,
		title: "staged · choose a batch to restore",
		rows:  rows,
		choose: func(index int) tea.Cmd {
			return transition(newUndoBlock(deps, theme, batches[index]))
		},
		cancel: func() tea.Cmd {
			return finish(cancelEntry(theme, "staged"))
		},
	}
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

// undoBlock restores one batch and reports the outcome into the transcript,
// restored paths behind the disclosure toggle.
type undoBlock struct {
	deps     *Deps
	theme    Theme
	batch    *deletionengine.Batch
	activity activityIndicator
}

func newUndoBlock(deps *Deps, theme Theme, batch *deletionengine.Batch) *undoBlock {
	return &undoBlock{deps: deps, theme: theme, batch: batch,
		activity: newActivityIndicator("Restoring")}
}

type undoDoneMsg struct {
	result *deletionengine.RestoreResult
	err    error
}

func (u *undoBlock) Init() tea.Cmd {
	return tea.Batch(
		func() tea.Msg {
			result, err := deletionengine.Undo(u.batch, u.deps.Log)
			return undoDoneMsg{result: result, err: err}
		},
		u.activity.init(),
	)
}

func (u *undoBlock) Update(msg tea.Msg) (liveBlock, tea.Cmd) {
	done, ok := msg.(undoDoneMsg)
	if !ok {
		updated, cmd := u.activity.update(msg)
		u.activity = updated
		return u, cmd
	}

	if done.err != nil {
		return u, finish(errorEntry(u.theme, "restore failed: "+done.err.Error()))
	}

	var details []string
	for _, outcome := range done.result.Outcomes {
		switch {
		case outcome.Restored:
			details = append(details, "restored "+outcome.Item.OriginalPath)
		case outcome.Err != nil:
			details = append(details, "failed   "+outcome.Item.OriginalPath+" ("+outcome.Err.Error()+")")
		default:
			details = append(details, "left     "+outcome.Item.OriginalPath+" ("+outcome.Reason+")")
		}
	}

	entries := []transcriptEntry{successEntry(u.theme,
		fmt.Sprintf("Restored %d item(s)", done.result.RestoredCount), details...)}
	if done.result.SkippedCount > 0 {
		entries = append(entries, mutedEntry(u.theme,
			fmt.Sprintf("%d item(s) left in staging.", done.result.SkippedCount)))
	}
	if done.result.FailedCount > 0 {
		entries = append(entries, errorEntry(u.theme,
			fmt.Sprintf("%d item(s) failed to restore.", done.result.FailedCount)))
	}
	return u, finish(entries...)
}

func (u *undoBlock) View(theme Theme, width int) string {
	return "  " + u.activity.view(theme)
}
