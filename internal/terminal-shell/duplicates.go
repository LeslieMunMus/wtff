package terminalshell

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	deletionengine "github.com/lesliemunmus/wtff/internal/deletion-engine"
	duplicatemerge "github.com/lesliemunmus/wtff/internal/duplicate-merge"
	duplicatescan "github.com/lesliemunmus/wtff/internal/duplicate-scan"
)

// startDuplicatesFlow searches the home directory for identical files.
func startDuplicatesFlow(deps *Deps, theme Theme) liveBlock {
	return newDuplicateScanBlock(deps, theme)
}

type duplicateScanBlock struct {
	deps     *Deps
	theme    Theme
	progress *progressCounter
	activity activityIndicator
}

func newDuplicateScanBlock(deps *Deps, theme Theme) *duplicateScanBlock {
	progress := &progressCounter{}
	return &duplicateScanBlock{
		deps: deps, theme: theme, progress: progress,
		activity: newActivityIndicator("Comparing").withProgress(progress),
	}
}

type duplicateScanDoneMsg struct {
	result *duplicatescan.Result
	err    error
}

func (d *duplicateScanBlock) Init() tea.Cmd {
	return tea.Batch(
		func() tea.Msg {
			result, err := duplicatescan.Find(duplicatescan.Options{
				Root:     d.deps.Home,
				Progress: func(scanned int) { d.progress.report(scanned, scanned) },
			})
			return duplicateScanDoneMsg{result: result, err: err}
		},
		d.activity.init(),
	)
}

func (d *duplicateScanBlock) Update(msg tea.Msg) (liveBlock, tea.Cmd) {
	done, ok := msg.(duplicateScanDoneMsg)
	if !ok {
		updated, cmd := d.activity.update(msg)
		d.activity = updated
		return d, cmd
	}

	if done.err != nil {
		return d, finish(errorEntry(d.theme, "could not compare: "+done.err.Error()))
	}
	if len(done.result.Groups) == 0 {
		return d, finish(infoEntry(d.theme, "No duplicate files found."))
	}

	entries := []transcriptEntry{successEntry(d.theme, fmt.Sprintf(
		"%d group(s) of identical files · %s reclaimable · compared %d file(s) in %s",
		len(done.result.Groups), humanBytes(done.result.Reclaimable()),
		done.result.Scanned, done.result.Elapsed.Round(time.Millisecond*100)))}

	if len(done.result.Denied) > 0 {
		entries = append(entries, mutedEntry(d.theme, fmt.Sprintf(
			"%d path(s) could not be read and were not compared.", len(done.result.Denied))))
	}
	if done.result.Truncated {
		entries = append(entries, mutedEntry(d.theme,
			"The comparison "+done.result.Reason+", so there may be more."))
	}

	return d, transition(
		newDuplicateGroupBlock(d.deps, d.theme, done.result.Groups), entries...)
}

func (d *duplicateScanBlock) View(theme Theme, width int) string {
	return "  " + d.activity.view(theme)
}

// duplicateGroupBlock picks which set of identical files to deal with.
type duplicateGroupBlock struct {
	deps   *Deps
	theme  Theme
	groups []duplicatescan.Group
	pick   *pickBlock
}

func newDuplicateGroupBlock(deps *Deps, theme Theme,
	groups []duplicatescan.Group) *duplicateGroupBlock {

	block := &duplicateGroupBlock{deps: deps, theme: theme, groups: groups}

	rows := make([]string, len(groups))
	for i, group := range groups {
		rows[i] = fmt.Sprintf("%-10s  %d copies  %s",
			humanBytes(group.Reclaimable()), len(group.Files),
			filepath.Base(group.Oldest().Path))
	}

	block.pick = &pickBlock{
		theme: theme,
		title: fmt.Sprintf("duplicates · %d group(s), largest saving first", len(groups)),
		rows:  rows,
		choose: func(index int) tea.Cmd {
			return transition(newDuplicateCopiesBlock(deps, theme, groups[index]))
		},
		cancel: func() tea.Cmd { return finish(cancelEntry(theme, "duplicates")) },
	}
	return block
}

func (d *duplicateGroupBlock) Init() tea.Cmd { return d.pick.Init() }

func (d *duplicateGroupBlock) Update(msg tea.Msg) (liveBlock, tea.Cmd) {
	updated, cmd := d.pick.Update(msg)
	if next, ok := updated.(*pickBlock); ok {
		d.pick = next
		return d, cmd
	}
	return updated, cmd
}

func (d *duplicateGroupBlock) View(theme Theme, width int) string {
	return d.pick.View(theme, width)
}

// duplicateCopiesBlock shows every copy in one group and offers the two
// things worth doing with them.
//
// Merge and stage are deliberately both present rather than one being the
// default. They answer different questions: merge is for copies a person wants
// to keep and gather, staging is for copies that are genuinely surplus. A tool
// that only offered the second would be assuming the answer.
type duplicateCopiesBlock struct {
	deps  *Deps
	theme Theme
	group duplicatescan.Group
	list  selectList
	note  string
}

const duplicateVisibleRows = 10

func newDuplicateCopiesBlock(deps *Deps, theme Theme,
	group duplicatescan.Group) *duplicateCopiesBlock {

	items := make([]selectableItem, 0, len(group.Files))
	for i, file := range group.Files {
		// The oldest is labelled rather than hidden. Someone deciding what to
		// remove needs to see which copy the merge would keep in place, and
		// which one they are about to select.
		role := "copy"
		if i == 0 {
			role = "oldest, kept by merge"
		}
		items = append(items, selectableItem{
			label:     displayPath(file.Path, deps.Home),
			detail:    fmt.Sprintf("%s · modified %s", role, file.ModTime.Format("2006-01-02 15:04")),
			sizeBytes: file.Size,
			sizeKnown: true,
		})
	}

	return &duplicateCopiesBlock{
		deps: deps, theme: theme, group: group,
		list: newSelectList(items),
	}
}

func (d *duplicateCopiesBlock) Init() tea.Cmd { return nil }

func (d *duplicateCopiesBlock) Update(msg tea.Msg) (liveBlock, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "esc":
			return d, finish(cancelEntry(d.theme, "duplicates"))

		case "o":
			// Opening the file is the only honest way to compare two images
			// or documents from a terminal. It reveals rather than changes,
			// so it needs no confirmation.
			return d, d.reveal()

		case "m":
			return d, transition(newDuplicateMergeBlock(d.deps, d.theme, d.group))
		}
	}

	if confirm, ok := msg.(selectListConfirmMsg); ok {
		if len(confirm.selected) == 0 {
			d.note = "select the copies to stage, or press m to merge them instead"
			return d, nil
		}
		if len(confirm.selected) == len(d.group.Files) {
			// Staging every copy would remove the file entirely, which is
			// almost certainly not what someone browsing duplicates meant.
			// Refusing beats a confirmation nobody reads.
			d.note = "that is every copy, leave at least one unselected"
			return d, nil
		}
		return d, transition(newDuplicateStageBlock(
			d.deps, d.theme, d.selectedPaths(confirm.selected)))
	}

	updated, cmd := d.list.update(msg, duplicateVisibleRows)
	d.list = updated
	return d, cmd
}

// reveal opens the copy under the cursor with whatever macOS uses for it.
func (d *duplicateCopiesBlock) reveal() tea.Cmd {
	if d.list.cursor < 0 || d.list.cursor >= len(d.group.Files) {
		return nil
	}
	path := d.group.Files[d.list.cursor].Path
	return func() tea.Msg {
		// Errors are deliberately dropped. Failing to open a preview is not
		// worth interrupting a person mid decision, and the transcript would
		// fill with noise from files macOS has no handler for.
		_ = exec.Command("/usr/bin/open", "-R", path).Run()
		return nil
	}
}

func (d *duplicateCopiesBlock) selectedPaths(indices []int) []string {
	paths := make([]string, 0, len(indices))
	for _, index := range indices {
		if index >= 0 && index < len(d.group.Files) {
			paths = append(paths, d.group.Files[index].Path)
		}
	}
	return paths
}

func (d *duplicateCopiesBlock) View(theme Theme, width int) string {
	boxWidth := width - 2
	inner := d.list.view(theme, boxWidth-4, duplicateVisibleRows+2)
	if d.note != "" {
		inner += "\n" + lipgloss.NewStyle().Foreground(theme.Danger).Render(d.note)
	}
	inner += "\n" + renderKeyHints(theme,
		[2]string{"space", "select"}, [2]string{"enter", "stage selected"},
		[2]string{"m", "merge all"}, [2]string{"o", "open"},
		[2]string{"esc", "cancel"})

	title := fmt.Sprintf("duplicates · %d identical copies · %s each",
		len(d.group.Files), humanBytes(d.group.Size))
	return renderTitledBox(theme, title, inner, boxWidth)
}

// duplicateMergeBlock gathers every copy beside the oldest one.
type duplicateMergeBlock struct {
	deps     *Deps
	theme    Theme
	group    duplicatescan.Group
	activity activityIndicator
}

func newDuplicateMergeBlock(deps *Deps, theme Theme,
	group duplicatescan.Group) *duplicateMergeBlock {
	return &duplicateMergeBlock{deps: deps, theme: theme, group: group,
		activity: newActivityIndicator("Merging")}
}

type duplicateMergeDoneMsg struct {
	plan   duplicatemerge.Plan
	result *duplicatemerge.Result
	err    error
}

func (d *duplicateMergeBlock) Init() tea.Cmd {
	return tea.Batch(
		func() tea.Msg {
			plan, err := duplicatemerge.PlanMerge(d.group)
			if err != nil {
				return duplicateMergeDoneMsg{err: err}
			}
			result, err := duplicatemerge.Apply(plan, d.deps.Log)
			return duplicateMergeDoneMsg{plan: plan, result: result, err: err}
		},
		d.activity.init(),
	)
}

func (d *duplicateMergeBlock) Update(msg tea.Msg) (liveBlock, tea.Cmd) {
	done, ok := msg.(duplicateMergeDoneMsg)
	if !ok {
		updated, cmd := d.activity.update(msg)
		d.activity = updated
		return d, cmd
	}

	if done.err != nil {
		return d, finish(errorEntry(d.theme, "merge failed: "+done.err.Error()))
	}

	var moved, failed []string
	for _, outcome := range done.result.Outcomes {
		if outcome.Moved {
			moved = append(moved, displayPath(outcome.Move.From, d.deps.Home)+
				" is now "+displayPath(outcome.Move.To, d.deps.Home))
			continue
		}
		reason := "unknown reason"
		if outcome.Err != nil {
			reason = outcome.Err.Error()
		}
		failed = append(failed, displayPath(outcome.Move.From, d.deps.Home)+": "+reason)
	}

	var entries []transcriptEntry
	if done.result.MovedCount == 0 && done.result.FailedCount == 0 {
		entries = append(entries, infoEntry(d.theme,
			"Every copy was already in "+displayPath(done.plan.Destination, d.deps.Home)+"."))
	}
	if done.result.MovedCount > 0 {
		// Where things went is the point of a merge, so it is not hidden
		// behind the disclosure toggle the way a deletion's path list is.
		entries = append(entries, infoExpandedEntry(d.theme, fmt.Sprintf(
			"Merged %d copy(s) into %s, nothing was deleted",
			done.result.MovedCount, displayPath(done.plan.Destination, d.deps.Home)),
			moved...))
	}
	if done.result.FailedCount > 0 {
		entries = append(entries, errorEntry(d.theme, fmt.Sprintf(
			"%d copy(s) could not be moved and are where they were.",
			done.result.FailedCount), failed...))
	}
	return d, finish(entries...)
}

func (d *duplicateMergeBlock) View(theme Theme, width int) string {
	return "  " + d.activity.view(theme)
}

// duplicateStageBlock plans selected copies for removal through the deletion
// engine, exactly as every other command does.
type duplicateStageBlock struct {
	deps     *Deps
	theme    Theme
	paths    []string
	activity activityIndicator
}

func newDuplicateStageBlock(deps *Deps, theme Theme, paths []string) *duplicateStageBlock {
	return &duplicateStageBlock{deps: deps, theme: theme, paths: paths,
		activity: newActivityIndicator("Checking")}
}

func (d *duplicateStageBlock) Init() tea.Cmd {
	return tea.Batch(
		func() tea.Msg {
			candidates := make([]deletionengine.Candidate, 0, len(d.paths))
			for _, path := range d.paths {
				candidates = append(candidates, deletionengine.Candidate{
					Path:   path,
					RuleID: "duplicate-manual-selection",
					Reason: "a surplus copy, selected by hand from a group of identical files",
				})
			}

			var skipped int
			manifest, err := deletionengine.Plan(candidates, deletionengine.PlanOptions{
				Command:      "duplicates",
				Action:       deletionengine.ActionStage,
				Policy:       d.deps.Rules,
				Log:          d.deps.Log,
				MeasureSizes: true,
				SkipSink:     func(string, string) { skipped++ },
			})
			return scanDoneMsg{manifest: manifest, skipped: skipped, err: err}
		},
		d.activity.init(),
	)
}

func (d *duplicateStageBlock) Update(msg tea.Msg) (liveBlock, tea.Cmd) {
	done, ok := msg.(scanDoneMsg)
	if !ok {
		updated, cmd := d.activity.update(msg)
		d.activity = updated
		return d, cmd
	}

	if done.err != nil {
		return d, finish(errorEntry(d.theme, "could not plan: "+done.err.Error()))
	}
	if len(done.manifest.Entries) == 0 {
		entries := []transcriptEntry{infoEntry(d.theme, "Nothing eligible in that selection.")}
		if done.skipped > 0 {
			entries = append(entries, mutedEntry(d.theme, fmt.Sprintf(
				"%d copy(s) were protected or excluded.", done.skipped)))
		}
		return d, finish(entries...)
	}

	return d, transition(
		newSelectionBlock(d.deps, d.theme, "duplicates", done.manifest),
		successEntry(d.theme, fmt.Sprintf("%d copy(s) selected · %s",
			len(done.manifest.Entries), humanBytes(done.manifest.TotalBytes))))
}

func (d *duplicateStageBlock) View(theme Theme, width int) string {
	return "  " + d.activity.view(theme)
}
