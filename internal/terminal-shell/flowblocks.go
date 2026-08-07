package terminalshell

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	deletionengine "github.com/lesliemunmus/wtff/internal/deletion-engine"
)

// liveBlock is the one interactive component pinned above the prompt while
// a flow is in progress: an activity line, a selection box, a
// disambiguation list. When a step resolves, the block collapses into a
// transcript entry and either hands off to the next block or clears. The
// viewport and the prompt never move; only this region changes.
type liveBlock interface {
	Init() tea.Cmd
	Update(msg tea.Msg) (liveBlock, tea.Cmd)
	View(theme Theme, width int) string
}

// flowMsg is how a flow advances the shell: entries to append to the
// transcript, and optionally a replacement live block, nil meaning the flow
// is finished. One atomic message rather than a sequence of separate ones,
// so ordering can never race and a test can drive a flow exactly as the
// runtime does.
type flowMsg struct {
	entries  []transcriptEntry
	setBlock bool
	block    liveBlock
}

// transition appends entries and replaces the live block in one step.
func transition(block liveBlock, entries ...transcriptEntry) tea.Cmd {
	return func() tea.Msg {
		return flowMsg{entries: entries, setBlock: true, block: block}
	}
}

// finish appends entries and clears the live block: the flow is done.
func finish(entries ...transcriptEntry) tea.Cmd {
	return transition(nil, entries...)
}

// planFunc produces a manifest for a scan step. Clean and uninstall's
// leftover discovery each supply their own; the blocks below know nothing
// about either, only that a manifest comes out.
type planFunc func(deps *Deps, progress func(done, total int)) (*deletionengine.Manifest, int, error)

// scanBlock is the activity state while candidates are found and validated.
// The spinner ticks on its own command, never on the scan itself, so a
// blocked filesystem call cannot freeze the indicator, which is the exact
// failure a static string had.
type scanBlock struct {
	deps     *Deps
	theme    Theme
	command  string
	fn       planFunc
	progress *progressCounter
	activity activityIndicator
}

func newScanBlock(deps *Deps, theme Theme, command string, fn planFunc) *scanBlock {
	progress := &progressCounter{}
	return &scanBlock{deps: deps, theme: theme, command: command, fn: fn,
		progress: progress,
		activity: newActivityIndicator("Scanning").withProgress(progress)}
}

type scanDoneMsg struct {
	manifest *deletionengine.Manifest
	skipped  int
	err      error
}

func (s *scanBlock) Init() tea.Cmd {
	return tea.Batch(
		func() tea.Msg {
			manifest, skipped, err := s.fn(s.deps, s.progress.report)
			return scanDoneMsg{manifest: manifest, skipped: skipped, err: err}
		},
		s.activity.init(),
	)
}

func (s *scanBlock) Update(msg tea.Msg) (liveBlock, tea.Cmd) {
	done, ok := msg.(scanDoneMsg)
	if !ok {
		updated, cmd := s.activity.update(msg)
		s.activity = updated
		return s, cmd
	}

	if done.err != nil {
		return s, finish(errorEntry(s.theme, "could not scan: "+done.err.Error()))
	}
	if len(done.manifest.Entries) == 0 {
		entries := []transcriptEntry{infoEntry(s.theme, "Nothing to stage: no eligible items found.")}
		if done.skipped > 0 {
			entries = append(entries, mutedEntry(s.theme,
				fmt.Sprintf("%d item(s) were protected or excluded.", done.skipped)))
		}
		return s, finish(entries...)
	}

	total := humanBytes(done.manifest.TotalBytes)
	if done.manifest.PartialSizing {
		total += " (partial)"
	}
	summary := successEntry(s.theme, fmt.Sprintf("Scan complete · %d candidate(s) · %s",
		len(done.manifest.Entries), total))
	entries := []transcriptEntry{summary}
	if done.skipped > 0 {
		entries = append(entries, mutedEntry(s.theme,
			fmt.Sprintf("%d item(s) protected or excluded, not shown.", done.skipped)))
	}
	return s, transition(newSelectionBlock(s.deps, s.theme, s.command, done.manifest), entries...)
}

func (s *scanBlock) View(theme Theme, width int) string {
	return "  " + s.activity.view(theme)
}

// selectionBlock is the checklist pinned above the prompt: the person
// reviews, toggles, and stages. Enter stages directly, per the approved
// sketch; staging is reversible by design, which is what makes a separate
// confirmation step ceremony rather than safety here.
type selectionBlock struct {
	deps     *Deps
	theme    Theme
	command  string
	manifest *deletionengine.Manifest
	list     selectList
	note     string
}

// selectionVisibleRows bounds the checklist height so a large scan does not
// shove the prompt off screen.
const selectionVisibleRows = 12

func newSelectionBlock(deps *Deps, theme Theme, command string, manifest *deletionengine.Manifest) *selectionBlock {
	items := make([]selectableItem, len(manifest.Entries))
	for i, entry := range manifest.Entries {
		items[i] = selectableItem{
			label:     entry.ResolvedPath,
			detail:    entry.Reason,
			sizeBytes: entry.SizeBytes,
			sizeKnown: entry.SizeKnown,
			selected:  true,
		}
	}
	return &selectionBlock{deps: deps, theme: theme, command: command,
		manifest: manifest, list: newSelectList(items)}
}

func (s *selectionBlock) Init() tea.Cmd { return nil }

func (s *selectionBlock) Update(msg tea.Msg) (liveBlock, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok && keyMsg.Type == tea.KeyEsc {
		return s, finish(cancelEntry(s.theme, s.command))
	}

	if confirm, ok := msg.(selectListConfirmMsg); ok {
		if len(confirm.selected) == 0 {
			s.note = "select at least one item first"
			return s, nil
		}
		filtered := filterManifest(s.manifest, confirm.selected)
		// Staging goes straight through, because it is reversible and a
		// confirmation for something undoable is ceremony. A purge does not,
		// and gets the word gate instead.
		if filtered.Action == deletionengine.ActionPurge {
			return s, transition(confirmPurgeManifest(s.deps, s.theme, s.command, filtered))
		}
		return s, transition(newApplyBlock(s.deps, s.theme, s.command, filtered))
	}

	updated, cmd := s.list.update(msg, selectionVisibleRows)
	s.list = updated
	return s, cmd
}

func (s *selectionBlock) View(theme Theme, width int) string {
	// The verb has to match what Enter actually does. A purge showing "select
	// items to stage" would promise reversibility at the one moment there is
	// none, which is the most expensive place in this program to be wrong
	// about a word. Caught by looking at a real rendered purge, not by a test.
	title, enterHint := " · select items to stage", "stage"
	if s.manifest.Action == deletionengine.ActionPurge {
		title, enterHint = " · select items to delete permanently", "delete"
	}

	boxWidth := width - 2
	inner := s.list.view(theme, boxWidth-4, selectionVisibleRows+2)
	if s.note != "" {
		inner += "\n" + lipgloss.NewStyle().Foreground(theme.Danger).Render(s.note)
	}
	inner += "\n" + renderKeyHints(theme,
		[2]string{"space", "toggle"}, [2]string{"a", "all"},
		[2]string{"enter", enterHint}, [2]string{"esc", "cancel"})
	return renderTitledBox(theme, s.command+title, inner, boxWidth)
}

// applyBlock runs Apply and reports the outcome into the transcript, with
// the full path list behind the disclosure toggle.
type applyBlock struct {
	deps     *Deps
	theme    Theme
	command  string
	manifest *deletionengine.Manifest
	progress *progressCounter
	activity activityIndicator
}

func newApplyBlock(deps *Deps, theme Theme, command string, manifest *deletionengine.Manifest) *applyBlock {
	// The manifest's own action decides what this block does, rather than a
	// separate flag the caller could set inconsistently with the manifest it
	// passed. Apply reads the action from the manifest too, so there is one
	// answer to "is this reversible" and both of us read it from the same place.
	label := "Staging"
	if manifest.Action == deletionengine.ActionPurge {
		label = "Deleting"
	}
	progress := &progressCounter{}
	return &applyBlock{deps: deps, theme: theme, command: command, manifest: manifest,
		progress: progress,
		activity: newActivityIndicator(label).withProgress(progress)}
}

// purging reports whether this block removes permanently.
func (a *applyBlock) purging() bool {
	return a.manifest.Action == deletionengine.ActionPurge
}

type applyDoneMsg struct {
	result *deletionengine.Result
	err    error
}

func (a *applyBlock) Init() tea.Cmd {
	return tea.Batch(
		func() tea.Msg {
			// A purge has nowhere to move things to, so it opens no staging
			// area. Creating one anyway would leave an empty batch directory
			// behind suggesting a restore point that does not exist.
			var staging *deletionengine.StagingArea
			if !a.purging() {
				opened, err := a.deps.newStagingArea()
				if err != nil {
					return applyDoneMsg{err: err}
				}
				staging = opened
			}
			result, err := deletionengine.Apply(a.manifest, deletionengine.ApplyOptions{
				Staging: staging, Policy: a.deps.Rules, Log: a.deps.Log,
				Progress: a.progress.report,
			})
			return applyDoneMsg{result: result, err: err}
		},
		a.activity.init(),
	)
}

func (a *applyBlock) Update(msg tea.Msg) (liveBlock, tea.Cmd) {
	done, ok := msg.(applyDoneMsg)
	if !ok {
		updated, cmd := a.activity.update(msg)
		a.activity = updated
		return a, cmd
	}

	verb, pastTense := "staging", "staged "
	if a.purging() {
		verb, pastTense = "deleting", "deleted"
	}

	if done.err != nil {
		return a, finish(errorEntry(a.theme, verb+" failed: "+done.err.Error()))
	}

	var details []string
	for _, outcome := range done.result.Outcomes {
		switch {
		case outcome.Applied:
			details = append(details, pastTense+" "+outcome.Entry.ResolvedPath)
		case outcome.Skipped:
			details = append(details, "skipped "+outcome.Entry.ResolvedPath+" ("+outcome.Reason+")")
		case outcome.Err != nil:
			details = append(details, "failed  "+outcome.Entry.ResolvedPath+" ("+outcome.Err.Error()+")")
		}
	}

	summary := fmt.Sprintf("Staged %d item(s) · %s · restore anytime with staged",
		done.result.AppliedCount, humanBytes(done.result.BytesApplied))
	if a.purging() {
		summary = fmt.Sprintf("Deleted %d item(s) permanently · %s reclaimed",
			done.result.AppliedCount, humanBytes(done.result.BytesApplied))
	}
	entries := []transcriptEntry{successEntry(a.theme, summary, details...)}
	if done.result.SkippedCount > 0 {
		entries = append(entries, mutedEntry(a.theme,
			fmt.Sprintf("%d item(s) skipped at apply time.", done.result.SkippedCount)))
	}
	if done.result.FailedCount > 0 {
		entries = append(entries, errorEntry(a.theme,
			fmt.Sprintf("%d item(s) failed.", done.result.FailedCount)))
	}
	return a, finish(entries...)
}

func (a *applyBlock) View(theme Theme, width int) string {
	return "  " + a.activity.view(theme)
}

// pickBlock is a generic single-choice cursor list pinned above the prompt,
// used for uninstall's disambiguation and staged's batch selection.
type pickBlock struct {
	theme  Theme
	title  string
	rows   []string
	cursor int
	choose func(index int) tea.Cmd
	cancel func() tea.Cmd
}

func (p *pickBlock) Init() tea.Cmd { return nil }

func (p *pickBlock) Update(msg tea.Msg) (liveBlock, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return p, nil
	}
	switch keyMsg.String() {
	case "up", "k":
		if p.cursor > 0 {
			p.cursor--
		}
	case "down", "j":
		if p.cursor < len(p.rows)-1 {
			p.cursor++
		}
	case "esc":
		return p, p.cancel()
	case "enter":
		if p.cursor < len(p.rows) {
			return p, p.choose(p.cursor)
		}
	}
	return p, nil
}

func (p *pickBlock) View(theme Theme, width int) string {
	body := lipgloss.NewStyle().Foreground(theme.Body)
	var rows []string
	for i, row := range p.rows {
		prefix := "  "
		style := body
		if i == p.cursor {
			prefix = "> "
			style = lipgloss.NewStyle().Foreground(theme.Accent).Bold(true).
				Background(theme.Highlight)
		}
		rows = append(rows, prefix+style.Render(row))
	}
	inner := lipgloss.JoinVertical(lipgloss.Left, rows...) + "\n" +
		renderKeyHints(theme, [2]string{"↑↓", "select"},
			[2]string{"enter", "choose"}, [2]string{"esc", "cancel"})
	return renderTitledBox(theme, p.title, inner, width-2)
}

// filterManifest builds a new, independently sealed manifest containing only
// the entries at the given indices from an already-planned manifest.
//
// The entries are reused as-is, including their captured identity; they were
// already validated once by Plan, and Apply revalidates identity and policy
// again regardless, so narrowing the set here can only remove entries a
// person did not select, never add one that was not independently justified.
func filterManifest(original *deletionengine.Manifest, keepIndices []int) *deletionengine.Manifest {
	filtered := &deletionengine.Manifest{
		Version:   original.Version,
		CreatedAt: timeNow(),
		Command:   original.Command,
		Action:    original.Action,
	}
	for _, i := range keepIndices {
		entry := original.Entries[i]
		filtered.Entries = append(filtered.Entries, entry)
		if entry.SizeKnown {
			filtered.TotalBytes += entry.SizeBytes
		} else {
			filtered.PartialSizing = true
		}
	}
	filtered.Seal()
	return filtered
}

// timeNow exists so filterManifest's timestamp source can be swapped in a
// test without reaching into the deletion engine's manifest internals.
var timeNow = func() time.Time { return time.Now().UTC() }
