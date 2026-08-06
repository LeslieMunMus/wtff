package terminalshell

import (
	"fmt"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	deletionengine "github.com/lesliemusengi/wtff/internal/deletion-engine"
	uninstallcore "github.com/lesliemusengi/wtff/internal/uninstall-core"
)

// startUninstallFlow begins the uninstall flow. With a query already typed,
// "uninstall firefox", resolution starts immediately; bare "uninstall"
// opens a small input block asking which application, still pinned above
// the prompt like every other live step, never a separate screen.
func startUninstallFlow(deps *Deps, theme Theme, query string) liveBlock {
	if query != "" {
		return newResolveBlock(deps, theme, query)
	}
	return newNameQueryBlock(deps, theme)
}

// appSearchRoots mirrors internal/cli's own list, so the shell and the
// non-interactive uninstall command look in exactly the same places.
func appSearchRoots(home string) []string {
	return []string{"/Applications", home + "/Applications"}
}

type appResolvedMsg struct {
	query   string
	matches []uninstallcore.InstalledApp

	// all is every discovered application, carried alongside the matches so
	// the leftover check can tell a component genuinely left behind from one
	// another installed application still needs.
	all []uninstallcore.InstalledApp

	err error
}

func resolveAppCmd(deps *Deps, query string) tea.Cmd {
	return func() tea.Msg {
		apps, _, err := uninstallcore.DiscoverApps(appSearchRoots(deps.Home))
		if err != nil {
			return appResolvedMsg{query: query, err: err}
		}
		return appResolvedMsg{query: query, all: apps,
			matches: uninstallcore.FindApp(apps, query)}
	}
}

// handleResolved is the shared branch point after discovery, used by both
// the direct-query path and the typed-name path: no match reports and
// finishes, a protected application refuses and finishes, one match
// proceeds to the leftover scan, several matches go to disambiguation.
func handleResolved(deps *Deps, theme Theme, msg appResolvedMsg) tea.Cmd {
	if msg.err != nil {
		return finish(errorEntry(theme, "cannot discover applications: "+msg.err.Error()))
	}
	switch len(msg.matches) {
	case 0:
		return finish(errorEntry(theme, fmt.Sprintf("no installed application matches %q", msg.query)))
	case 1:
		return proceedWithApp(deps, theme, msg.matches[0], msg.all)
	default:
		return transition(newAppPickBlock(deps, theme, msg.matches, msg.all))
	}
}

// proceedWithApp runs the protection check and, when allowed, hands off to
// the shared scan flow over the application's own leftover plan.
func proceedWithApp(deps *Deps, theme Theme, app uninstallcore.InstalledApp,
	allApps []uninstallcore.InstalledApp) tea.Cmd {

	if reason, protected := uninstallcore.IsProtectedApp(app); protected {
		return finish(errorEntry(theme, app.DisplayName+" cannot be uninstalled: "+reason))
	}

	entries := []transcriptEntry{
		infoEntry(theme, fmt.Sprintf("Uninstalling %s (%s)", app.DisplayName, app.BundleID)),
	}

	// Root level components are reported before the plan rather than after the
	// removal. wtff runs unprivileged by design and cannot remove these, so the
	// honest thing is to say what will survive while the choice is still open.
	if leftovers := uninstallcore.InspectSystemIntegration(app, allApps); leftovers.Orphans() {
		var details []string
		for _, path := range leftovers.PrivilegedHelpers {
			details = append(details, "helper  "+path)
		}
		for _, path := range leftovers.LaunchDaemons {
			details = append(details, "job     "+path)
		}
		details = append(details, "remove these with sudo, or use the vendor's uninstaller")
		entries = append(entries, warningEntry(theme,
			fmt.Sprintf("%d privileged component(s) will be left behind, which wtff "+
				"cannot remove without elevated privileges", len(details)-1), details...))
	}

	return transition(
		newScanBlock(deps, theme, "uninstall "+app.DisplayName, leftoverPlanFor(app)),
		entries...,
	)
}

func newAppPickBlock(deps *Deps, theme Theme, matches, all []uninstallcore.InstalledApp) *pickBlock {
	rows := make([]string, len(matches))
	for i, app := range matches {
		rows[i] = fmt.Sprintf("%s  (%s)  %s", app.DisplayName, app.BundleID, app.Path)
	}
	return &pickBlock{
		theme: theme,
		title: "uninstall · choose one",
		rows:  rows,
		choose: func(index int) tea.Cmd {
			return proceedWithApp(deps, theme, matches[index], all)
		},
		cancel: func() tea.Cmd {
			return finish(cancelEntry(theme, "uninstall"))
		},
	}
}

// resolveBlock is the brief activity state while installed applications are
// discovered for an already-known query.
type resolveBlock struct {
	deps     *Deps
	theme    Theme
	query    string
	activity activityIndicator
}

func newResolveBlock(deps *Deps, theme Theme, query string) *resolveBlock {
	return &resolveBlock{deps: deps, theme: theme, query: query,
		activity: newActivityIndicator("Searching")}
}

func (r *resolveBlock) Init() tea.Cmd {
	return tea.Batch(resolveAppCmd(r.deps, r.query), r.activity.init())
}

func (r *resolveBlock) Update(msg tea.Msg) (liveBlock, tea.Cmd) {
	if resolved, ok := msg.(appResolvedMsg); ok {
		return r, handleResolved(r.deps, r.theme, resolved)
	}
	updated, cmd := r.activity.update(msg)
	r.activity = updated
	return r, cmd
}

func (r *resolveBlock) View(theme Theme, width int) string {
	return "  " + r.activity.view(theme)
}

// nameQueryBlock asks which application to uninstall when the command was
// typed bare. Matching stays exact through uninstallcore.FindApp; this
// block only collects the query.
type nameQueryBlock struct {
	deps  *Deps
	theme Theme
	input textinput.Model
}

func newNameQueryBlock(deps *Deps, theme Theme) *nameQueryBlock {
	input := textinput.New()
	input.Placeholder = "application name or bundle identifier"
	input.Prompt = "❯ "
	input.Focus()
	input.CharLimit = 128
	return &nameQueryBlock{deps: deps, theme: theme, input: input}
}

func (n *nameQueryBlock) Init() tea.Cmd { return textinput.Blink }

func (n *nameQueryBlock) Update(msg tea.Msg) (liveBlock, tea.Cmd) {
	if resolved, ok := msg.(appResolvedMsg); ok {
		return n, handleResolved(n.deps, n.theme, resolved)
	}
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.Type {
		case tea.KeyEsc:
			return n, finish(cancelEntry(n.theme, "uninstall"))
		case tea.KeyEnter:
			query := n.input.Value()
			if query == "" {
				return n, nil
			}
			return n, resolveAppCmd(n.deps, query)
		}
	}
	var cmd tea.Cmd
	n.input, cmd = n.input.Update(msg)
	return n, cmd
}

func (n *nameQueryBlock) View(theme Theme, width int) string {
	inner := n.input.View() + "\n" +
		renderKeyHints(theme, [2]string{"enter", "search"}, [2]string{"esc", "cancel"})
	return renderTitledBox(theme, "uninstall which application?", inner, width-2)
}

// leftoverPlanFor builds the planFunc for one specific application's
// uninstall: the app bundle itself, plus every leftover
// internal/uninstall-core can justify with exact evidence, planned through
// the same deletion engine call the non-interactive uninstall command uses.
func leftoverPlanFor(app uninstallcore.InstalledApp) planFunc {
	return func(deps *Deps, progress func(done, total int)) (*deletionengine.Manifest, int, error) {
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
			Progress:     progress,
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
