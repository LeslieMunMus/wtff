package terminalshell

import (
	"fmt"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	deletionengine "github.com/lesliemusengi/wtff/internal/deletion-engine"
	uninstallcore "github.com/lesliemusengi/wtff/internal/uninstall-core"
)

// uninstallSearchScreen is the entry point for the uninstall flow: a text
// field for an application name or bundle identifier.
//
// Discovery and matching happen inline in this one screen, rather than in a
// separate transient loading screen the way clean's flow works, since app
// discovery on a typical machine is fast enough that a full screen
// transition would be more motion than the wait justifies. What it finds
// determines what happens next: no match keeps the person here with an
// explanation, more than one match pushes a screen listing them, and exactly
// one proceeds straight to the same protection check and leftover plan flow
// wtff uninstall uses non-interactively.
type uninstallSearchScreen struct {
	deps  *Deps
	input textinput.Model
	err   string
}

func newUninstallSearchScreen(deps *Deps) *uninstallSearchScreen {
	input := textinput.New()
	input.Placeholder = "application name or bundle identifier"
	input.Focus()
	return &uninstallSearchScreen{deps: deps, input: input}
}

func (s *uninstallSearchScreen) Title() string { return "Uninstall" }

func (s *uninstallSearchScreen) Init() tea.Cmd { return textinput.Blink }

func (s *uninstallSearchScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "esc":
			return s, resetToMenu()
		case "enter":
			query := s.input.Value()
			if query == "" {
				return s, nil
			}
			return s, resolveApp(s.deps, query)
		}
	}

	if result, ok := msg.(appResolvedMsg); ok {
		return s.handleResolved(result)
	}

	var cmd tea.Cmd
	s.input, cmd = s.input.Update(msg)
	return s, cmd
}

func (s *uninstallSearchScreen) handleResolved(result appResolvedMsg) (Screen, tea.Cmd) {
	if result.err != nil {
		s.err = result.err.Error()
		return s, nil
	}
	switch len(result.matches) {
	case 0:
		s.err = fmt.Sprintf("no installed application matches %q", s.input.Value())
		return s, nil
	case 1:
		app := result.matches[0]
		if reason, protected := uninstallcore.IsProtectedApp(app); protected {
			s.err = reason
			return s, nil
		}
		s.err = ""
		return s, pushScreen(newPlanDiscoveringScreen(s.deps, app.DisplayName, leftoverPlanFor(app)))
	default:
		s.err = ""
		return s, pushScreen(newAppMatchesScreen(s.deps, result.matches))
	}
}

func (s *uninstallSearchScreen) View(theme Theme, width, height int) string {
	label := lipgloss.NewStyle().Foreground(theme.Muted).Render("Find an application to uninstall")
	box := lipgloss.NewStyle().
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(theme.Border).
		Padding(0, 1).
		Width(min(width-6, 60)).
		Render(s.input.View())

	lines := []string{label, "", box}
	if s.err != "" {
		lines = append(lines, "", lipgloss.NewStyle().Foreground(theme.Danger).Render(s.err))
	}
	lines = append(lines, "", renderKeyHints(theme, [2]string{"enter", "search"}, [2]string{"esc", "back"}))

	return lipgloss.NewStyle().Padding(1, 2).Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

type appResolvedMsg struct {
	matches []uninstallcore.InstalledApp
	err     error
}

func resolveApp(deps *Deps, query string) tea.Cmd {
	return func() tea.Msg {
		apps, _, err := uninstallcore.DiscoverApps(appSearchRoots(deps.Home))
		if err != nil {
			return appResolvedMsg{err: err}
		}
		return appResolvedMsg{matches: uninstallcore.FindApp(apps, query)}
	}
}

// appSearchRoots mirrors internal/cli's own list, so the shell and the
// non-interactive uninstall command look in exactly the same places.
func appSearchRoots(home string) []string {
	return []string{"/Applications", home + "/Applications"}
}

// appMatchesScreen lists every application an ambiguous query matched, so a
// person can pick the right one deliberately rather than have wtff guess.
type appMatchesScreen struct {
	deps    *Deps
	matches []uninstallcore.InstalledApp
	cursor  int
}

func newAppMatchesScreen(deps *Deps, matches []uninstallcore.InstalledApp) *appMatchesScreen {
	return &appMatchesScreen{deps: deps, matches: matches}
}

func (s *appMatchesScreen) Title() string { return "Uninstall · choose one" }

func (s *appMatchesScreen) Init() tea.Cmd { return nil }

func (s *appMatchesScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return s, nil
	}
	switch keyMsg.String() {
	case "up", "k":
		if s.cursor > 0 {
			s.cursor--
		}
	case "down", "j":
		if s.cursor < len(s.matches)-1 {
			s.cursor++
		}
	case "esc":
		return s, popScreen()
	case "enter":
		app := s.matches[s.cursor]
		if reason, protected := uninstallcore.IsProtectedApp(app); protected {
			return s, showStatus(reason, true)
		}
		return s, pushScreen(newPlanDiscoveringScreen(s.deps, app.DisplayName, leftoverPlanFor(app)))
	}
	return s, nil
}

func (s *appMatchesScreen) View(theme Theme, width, height int) string {
	var rows []string
	for i, app := range s.matches {
		prefix := "  "
		style := lipgloss.NewStyle()
		if i == s.cursor {
			prefix = "> "
			style = style.Foreground(theme.Accent).Bold(true)
		}
		rows = append(rows, style.Render(fmt.Sprintf("%s%s  (%s)  %s", prefix, app.DisplayName, app.BundleID, app.Path)))
	}
	hints := renderKeyHints(theme, [2]string{"↑↓", "select"}, [2]string{"enter", "choose"}, [2]string{"esc", "back"})
	return lipgloss.NewStyle().Padding(1, 2).Render(
		lipgloss.JoinVertical(lipgloss.Left, lipgloss.JoinVertical(lipgloss.Left, rows...), "", hints),
	)
}

// leftoverPlanFor builds the planFunc for one specific application's
// uninstall: the app bundle itself, plus every leftover
// internal/uninstall-core can justify with exact evidence, planned through
// the same deletion engine call the non-interactive uninstall command uses.
func leftoverPlanFor(app uninstallcore.InstalledApp) planFunc {
	return func(deps *Deps) (*deletionengine.Manifest, int, error) {
		candidates := []deletionengine.Candidate{{
			Path:   app.Path,
			RuleID: "uninstall-application-bundle",
			Reason: fmt.Sprintf("the application bundle for %s, matched by explicit selection", app.DisplayName),
		}}
		candidates = append(candidates, uninstallcore.DiscoverLeftovers(app, deps.Home)...)

		var skipped int
		manifest, err := deletionengine.Plan(candidates, deletionengine.PlanOptions{
			Command:      "uninstall",
			Action:       deletionengine.ActionStage,
			Policy:       deps.Rules,
			Log:          deps.Log,
			MeasureSizes: true,
			SkipSink: func(string, string) {
				skipped++
			},
		})
		if err != nil {
			return nil, 0, err
		}
		return manifest, skipped, nil
	}
}
