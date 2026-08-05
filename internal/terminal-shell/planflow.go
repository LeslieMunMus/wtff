package terminalshell

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	deletionengine "github.com/lesliemusengi/wtff/internal/deletion-engine"
)

// planFunc produces a manifest for a plan-and-apply flow. Clean and
// uninstall's leftover step each supply their own, built from
// internal/clean-catalog or internal/uninstall-core respectively; this file
// knows nothing about either, only that a manifest comes out.
type planFunc func(deps *Deps) (*deletionengine.Manifest, int, error)

// planDiscoveringScreen is the loading state while candidates are being
// found and validated. This can take real time: clean's container scans and
// the deletion engine's own size measurement both do filesystem work, so a
// visible loading state matters rather than a frozen screen.
//
// The activity indicator ticks independently of the scan itself, on its own
// command, scheduled alongside the scan rather than by it. That
// independence is the entire point: a scan with no bound on how long a
// single filesystem call can take must never be what the animation is
// waiting on, or the indicator would freeze exactly when it matters most,
// which is precisely the failure a static "Scanning…" string already had.
type planDiscoveringScreen struct {
	deps     *Deps
	title    string
	fn       planFunc
	activity activityIndicator
}

func newPlanDiscoveringScreen(deps *Deps, title string, fn planFunc) *planDiscoveringScreen {
	return &planDiscoveringScreen{deps: deps, title: title, fn: fn, activity: newActivityIndicator("Scanning")}
}

type planReadyMsg struct {
	manifest     *deletionengine.Manifest
	skippedCount int
	err          error
}

func (s *planDiscoveringScreen) Title() string { return s.title }

func (s *planDiscoveringScreen) Init() tea.Cmd {
	return tea.Batch(
		func() tea.Msg {
			manifest, skipped, err := s.fn(s.deps)
			return planReadyMsg{manifest: manifest, skippedCount: skipped, err: err}
		},
		s.activity.init(),
	)
}

func (s *planDiscoveringScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	if ready, ok := msg.(planReadyMsg); ok {
		if ready.err != nil {
			return s, tea.Batch(showStatus("could not scan: "+ready.err.Error(), true), resetToMenu())
		}
		return newCandidateListScreen(s.deps, s.title, ready.manifest, ready.skippedCount), nil
	}
	updated, cmd := s.activity.update(msg)
	s.activity = updated
	return s, cmd
}

func (s *planDiscoveringScreen) View(theme Theme, width, height int) string {
	return lipgloss.NewStyle().Padding(1, 2).Render(s.activity.view(theme))
}

// candidateListScreen presents a plan's entries for selection.
type candidateListScreen struct {
	deps         *Deps
	title        string
	manifest     *deletionengine.Manifest
	skippedCount int
	list         selectList
	lastHeight   int
}

func newCandidateListScreen(deps *Deps, title string, manifest *deletionengine.Manifest, skippedCount int) *candidateListScreen {
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
	return &candidateListScreen{deps: deps, title: title, manifest: manifest, skippedCount: skippedCount, list: newSelectList(items)}
}

func (s *candidateListScreen) Title() string { return s.title }

func (s *candidateListScreen) Init() tea.Cmd { return nil }

func (s *candidateListScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok && keyMsg.String() == "esc" {
		// Not resetToMenu: this screen replaced whatever pushed the
		// discovering screen it followed, which is the menu for the clean
		// flow but will be a search or matches screen for uninstall's
		// leftover step. popScreen returns to whichever that was, which is
		// the generically correct behavior; resetToMenu would skip past a
		// search screen straight to the menu, discarding it unnecessarily.
		return s, popScreen()
	}

	updated, cmd := s.list.update(msg, s.lastHeight)
	s.list = updated

	if confirm, ok := msg.(selectListConfirmMsg); ok {
		if len(confirm.selected) == 0 {
			return s, showStatus("select at least one item first", true)
		}
		filtered := filterManifest(s.manifest, confirm.selected)
		return s, tea.Sequence(cmd, pushScreen(newConfirmScreen(s.deps, s.title, filtered)))
	}
	return s, cmd
}

func (s *candidateListScreen) View(theme Theme, width, height int) string {
	s.lastHeight = height
	body := s.list.view(theme, width-4, height-2)
	hints := renderKeyHints(theme,
		[2]string{"↑↓", "move"}, [2]string{"space", "toggle"}, [2]string{"a", "all"},
		[2]string{"enter", "review"}, [2]string{"esc", "back"})
	skipNote := ""
	if s.skippedCount > 0 {
		skipNote = lipgloss.NewStyle().Foreground(theme.Muted).
			Render(fmt.Sprintf("%d item(s) not shown, protected or already handled", s.skippedCount))
	}
	return lipgloss.NewStyle().Padding(1, 2).Render(
		lipgloss.JoinVertical(lipgloss.Left, body, "", skipNote, hints),
	)
}

// confirmScreen shows the final, filtered manifest and asks for approval
// before anything is touched. Pushed on top of candidateListScreen, so Esc
// returns to the list with the selection exactly as it was left.
type confirmScreen struct {
	deps     *Deps
	title    string
	manifest *deletionengine.Manifest
}

func newConfirmScreen(deps *Deps, title string, manifest *deletionengine.Manifest) *confirmScreen {
	return &confirmScreen{deps: deps, title: title, manifest: manifest}
}

func (s *confirmScreen) Title() string { return s.title + " · confirm" }

func (s *confirmScreen) Init() tea.Cmd { return nil }

func (s *confirmScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return s, nil
	}
	switch keyMsg.String() {
	case "y", "enter":
		return newApplyingScreen(s.deps, s.title, s.manifest), nil
	case "esc", "n":
		return s, popScreen()
	}
	return s, nil
}

func (s *confirmScreen) View(theme Theme, width, height int) string {
	verb := "stage"
	note := "This can be undone from the Staged menu."
	if s.manifest.Action == deletionengine.ActionPurge {
		verb = "permanently remove"
		note = "This cannot be undone."
	}
	summary := fmt.Sprintf("%s %d item(s), %s total", verb, len(s.manifest.Entries), humanBytes(s.manifest.TotalBytes))
	prompt := "Proceed? [y/N]"

	content := lipgloss.JoinVertical(lipgloss.Left,
		lipgloss.NewStyle().Bold(true).Render(summary),
		"",
		lipgloss.NewStyle().Foreground(theme.Muted).Render(note),
		"",
		lipgloss.NewStyle().Foreground(theme.Accent).Render(prompt),
	)
	return lipgloss.NewStyle().Padding(1, 2).Render(content)
}

// applyingScreen runs Apply and waits for it to finish. See
// planDiscoveringScreen for why its activity indicator ticks on its own
// command rather than depending on Apply itself to drive it.
type applyingScreen struct {
	deps     *Deps
	title    string
	manifest *deletionengine.Manifest
	activity activityIndicator
}

func newApplyingScreen(deps *Deps, title string, manifest *deletionengine.Manifest) *applyingScreen {
	verb := "Staging"
	if manifest.Action == deletionengine.ActionPurge {
		verb = "Removing"
	}
	return &applyingScreen{deps: deps, title: title, manifest: manifest, activity: newActivityIndicator(verb)}
}

type applyReadyMsg struct {
	result *deletionengine.Result
	err    error
}

func (s *applyingScreen) Title() string { return s.title }

func (s *applyingScreen) Init() tea.Cmd {
	return tea.Batch(
		func() tea.Msg {
			var staging *deletionengine.StagingArea
			if s.manifest.Action == deletionengine.ActionStage {
				var err error
				staging, err = s.deps.newStagingArea()
				if err != nil {
					return applyReadyMsg{err: err}
				}
			}
			result, err := deletionengine.Apply(s.manifest, deletionengine.ApplyOptions{
				Staging: staging, Policy: s.deps.Rules, Log: s.deps.Log,
			})
			return applyReadyMsg{result: result, err: err}
		},
		s.activity.init(),
	)
}

func (s *applyingScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	if ready, ok := msg.(applyReadyMsg); ok {
		if ready.err != nil {
			return s, tea.Batch(showStatus("apply failed: "+ready.err.Error(), true), resetToMenu())
		}
		return newResultsScreen(s.title, s.manifest.Action, ready.result), nil
	}
	updated, cmd := s.activity.update(msg)
	s.activity = updated
	return s, cmd
}

func (s *applyingScreen) View(theme Theme, width, height int) string {
	return lipgloss.NewStyle().Padding(1, 2).Render(s.activity.view(theme))
}

// resultsScreen shows what happened and waits for any key before returning
// to the menu. Results are terminal: there is nothing to go "back" to that
// still makes sense, so any key here resets to the menu rather than popping
// through the now-stale list and confirm screens beneath it.
type resultsScreen struct {
	title  string
	action deletionengine.Action
	result *deletionengine.Result
}

func newResultsScreen(title string, action deletionengine.Action, result *deletionengine.Result) *resultsScreen {
	return &resultsScreen{title: title, action: action, result: result}
}

func (s *resultsScreen) Title() string { return s.title + " · done" }

func (s *resultsScreen) Init() tea.Cmd { return nil }

func (s *resultsScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	if _, ok := msg.(tea.KeyMsg); ok {
		return s, resetToMenu()
	}
	return s, nil
}

func (s *resultsScreen) View(theme Theme, width, height int) string {
	verb := "Staged"
	if s.action == deletionengine.ActionPurge {
		verb = "Removed"
	}
	lines := []string{
		lipgloss.NewStyle().Foreground(theme.Success).Bold(true).
			Render(fmt.Sprintf("%s %d item(s), %s", verb, s.result.AppliedCount, humanBytes(s.result.BytesApplied))),
	}
	if s.result.SkippedCount > 0 {
		lines = append(lines, lipgloss.NewStyle().Foreground(theme.Warning).
			Render(fmt.Sprintf("%d item(s) skipped at apply time", s.result.SkippedCount)))
	}
	if s.result.FailedCount > 0 {
		lines = append(lines, lipgloss.NewStyle().Foreground(theme.Danger).
			Render(fmt.Sprintf("%d item(s) failed", s.result.FailedCount)))
	}
	if s.result.Batch != nil {
		lines = append(lines, "", lipgloss.NewStyle().Foreground(theme.Muted).
			Render("Undo from the Staged menu: "+s.result.Batch.BatchID))
	}
	lines = append(lines, "", lipgloss.NewStyle().Foreground(theme.Muted).Render("Press any key to continue"))

	return lipgloss.NewStyle().Padding(1, 2).Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}

// filterManifest builds a new, independently sealed manifest containing only
// the entries at the given indices from an already-planned manifest.
//
// The entries themselves are reused as-is, including their captured
// identity; they were already validated once by Plan. Apply re-validates
// identity and policy again regardless, the same guarantee every other path
// through the deletion engine relies on, so narrowing the set here is safe:
// it can only ever remove entries a person did not select, never add one
// that was not already independently justified.
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
// test without reaching into the deletion engine's own manifest internals.
var timeNow = func() (t time.Time) { return time.Now().UTC() }
