package terminalshell

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	deletionengine "github.com/lesliemunmus/wtff/internal/deletion-engine"
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
		title: "staged · choose a batch",
		rows:  rows,
		choose: func(index int) tea.Cmd {
			return transition(newBatchActionBlock(deps, theme, batches[index]))
		},
		cancel: func() tea.Cmd {
			return finish(cancelEntry(theme, "staged"))
		},
	}
}

// newBatchActionBlock asks what to do with a chosen batch.
//
// Staging deliberately does not decide this at removal time. A person cleaning
// up does not yet know whether they will want something back, and the value of
// staging is precisely that they do not have to know. This is where they say,
// once they have seen what is actually held and had time to notice anything
// missing.
func newBatchActionBlock(deps *Deps, theme Theme, batch *deletionengine.Batch) *pickBlock {
	total, known := batchTotalBytes(batch)
	size := humanBytes(total)
	if !known {
		size += " (partial)"
	}

	return &pickBlock{
		theme: theme,
		title: fmt.Sprintf("staged · %s · %d item(s) · %s",
			batch.BatchID, len(batch.Items), size),
		rows: []string{
			"Restore everything to where it came from",
			"Delete permanently and reclaim the space",
		},
		choose: func(index int) tea.Cmd {
			if index == 0 {
				return transition(newUndoBlock(deps, theme, batch))
			}
			warning := fmt.Sprintf(
				"%d item(s), %s, will be deleted permanently. This cannot be undone.",
				len(batch.Items), size)
			return transition(newConfirmWordBlock(theme,
				"staged · confirm permanent deletion", warning,
				func() tea.Cmd {
					return transition(newPurgeBatchBlock(deps, theme, batch))
				},
				func() tea.Cmd {
					return finish(cancelEntry(theme, "staged"))
				},
			))
		},
		cancel: func() tea.Cmd {
			return finish(cancelEntry(theme, "staged"))
		},
	}
}

// purgeBatchBlock permanently deletes one staged batch.
type purgeBatchBlock struct {
	deps     *Deps
	theme    Theme
	batch    *deletionengine.Batch
	activity activityIndicator
}

func newPurgeBatchBlock(deps *Deps, theme Theme, batch *deletionengine.Batch) *purgeBatchBlock {
	return &purgeBatchBlock{deps: deps, theme: theme, batch: batch,
		activity: newActivityIndicator("Deleting")}
}

type purgeBatchDoneMsg struct {
	result *deletionengine.PurgeResult
	err    error
}

func (p *purgeBatchBlock) Init() tea.Cmd {
	return tea.Batch(
		func() tea.Msg {
			area, err := p.deps.newStagingArea()
			if err != nil {
				return purgeBatchDoneMsg{err: err}
			}
			result, err := area.PurgeBatch(p.batch, p.deps.Log)
			return purgeBatchDoneMsg{result: result, err: err}
		},
		p.activity.init(),
	)
}

func (p *purgeBatchBlock) Update(msg tea.Msg) (liveBlock, tea.Cmd) {
	done, ok := msg.(purgeBatchDoneMsg)
	if !ok {
		updated, cmd := p.activity.update(msg)
		p.activity = updated
		return p, cmd
	}

	if done.err != nil {
		return p, finish(errorEntry(p.theme, "delete failed: "+done.err.Error()))
	}

	// Successes and failures are separated rather than listed together. Mixing
	// them meant the reason an item failed sat behind the disclosure toggle of
	// a line announcing success, which is the last place someone looks for it.
	var deleted, failed []string
	for _, outcome := range done.result.Outcomes {
		if outcome.Purged {
			deleted = append(deleted, outcome.Item.OriginalPath)
			continue
		}
		reason := "unknown reason"
		if outcome.Err != nil {
			reason = outcome.Err.Error()
		}
		failed = append(failed, outcome.Item.OriginalPath+": "+reason)
	}

	var entries []transcriptEntry

	// No success line when nothing was deleted. A green tick over "Deleted 0
	// item(s)" reads as an operation that worked, next to an error saying it
	// did not.
	if done.result.PurgedCount > 0 {
		size := humanBytes(done.result.BytesReclaimed)
		if done.result.SizePartial {
			size = "at least " + size
		}
		entries = append(entries, successEntry(p.theme,
			fmt.Sprintf("Deleted %d item(s) permanently · %s reclaimed",
				done.result.PurgedCount, size),
			deleted...))
	}

	if done.result.FailedCount > 0 {
		entries = append(entries, errorEntry(p.theme,
			fmt.Sprintf("%d item(s) could not be deleted and are still staged.",
				done.result.FailedCount),
			failed...))
	}
	return p, finish(entries...)
}

func (p *purgeBatchBlock) View(theme Theme, width int) string {
	return "  " + p.activity.view(theme)
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

	// Split the same way a purge is, so a reason never ends up behind the
	// disclosure toggle of a line reporting success.
	var restored, skipped, failed []string
	for _, outcome := range done.result.Outcomes {
		switch {
		case outcome.Restored:
			restored = append(restored, outcome.Item.OriginalPath)
		case outcome.Err != nil:
			failed = append(failed, outcome.Item.OriginalPath+": "+outcome.Err.Error())
		default:
			skipped = append(skipped, outcome.Item.OriginalPath+": "+outcome.Reason)
		}
	}

	var entries []transcriptEntry
	if done.result.RestoredCount > 0 {
		entries = append(entries, successEntry(u.theme,
			fmt.Sprintf("Restored %d item(s)", done.result.RestoredCount), restored...))
	}
	if done.result.SkippedCount > 0 {
		entries = append(entries, mutedEntry(u.theme,
			fmt.Sprintf("%d item(s) left in staging.", done.result.SkippedCount)))
		// Why something was left is as actionable as why something failed,
		// most often an occupied original location, so it is shown too.
		entries = append(entries, infoDetailEntry(u.theme,
			"reasons they were left", skipped...))
	}
	if done.result.FailedCount > 0 {
		entries = append(entries, errorEntry(u.theme,
			fmt.Sprintf("%d item(s) failed to restore.", done.result.FailedCount),
			failed...))
	}
	return u, finish(entries...)
}

func (u *undoBlock) View(theme Theme, width int) string {
	return "  " + u.activity.view(theme)
}
