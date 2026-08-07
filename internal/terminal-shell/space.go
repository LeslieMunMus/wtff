package terminalshell

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	deletionengine "github.com/lesliemunmus/wtff/internal/deletion-engine"
	spacescan "github.com/lesliemunmus/wtff/internal/space-scan"
)

// startSpaceFlow measures the home directory and opens a browser over the
// result.
//
// The scan and the browsing are separate blocks because they fail differently:
// a scan can be denied or run out of time and still be worth showing, while
// browsing cannot start at all without a tree. Keeping them apart means the
// partial result of the first is the input to the second rather than a special
// case inside one block.
func startSpaceFlow(deps *Deps, theme Theme) liveBlock {
	return newSpaceScanBlock(deps, theme)
}

type spaceScanBlock struct {
	deps     *Deps
	theme    Theme
	progress *progressCounter
	activity activityIndicator
}

func newSpaceScanBlock(deps *Deps, theme Theme) *spaceScanBlock {
	progress := &progressCounter{}
	return &spaceScanBlock{
		deps:     deps,
		theme:    theme,
		progress: progress,
		activity: newActivityIndicator("Measuring").withProgress(progress),
	}
}

type spaceScanDoneMsg struct {
	result *spacescan.Result
	err    error
}

func (s *spaceScanBlock) Init() tea.Cmd {
	return tea.Batch(
		func() tea.Msg {
			result, err := spacescan.Scan(spacescan.Options{
				Root: s.deps.Home,
				// The total is unknown until the walk finishes, so the counter
				// reports entries seen against itself. A figure that only goes
				// up still distinguishes a slow scan from a stuck one, which
				// is the question a person is actually asking.
				Progress: func(scanned int) { s.progress.report(scanned, scanned) },
			})
			return spaceScanDoneMsg{result: result, err: err}
		},
		s.activity.init(),
	)
}

func (s *spaceScanBlock) Update(msg tea.Msg) (liveBlock, tea.Cmd) {
	done, ok := msg.(spaceScanDoneMsg)
	if !ok {
		updated, cmd := s.activity.update(msg)
		s.activity = updated
		return s, cmd
	}

	if done.err != nil {
		return s, finish(errorEntry(s.theme, "could not measure: "+done.err.Error()))
	}
	if len(done.result.Root.Children) == 0 {
		return s, finish(infoEntry(s.theme, "Nothing found under "+s.deps.Home+"."))
	}

	entries := []transcriptEntry{successEntry(s.theme, fmt.Sprintf(
		"Measured %s across %d entries in %s",
		humanBytes(done.result.Root.Size), done.result.Scanned,
		done.result.Elapsed.Round(1e8)))}

	// Both of these change what the numbers mean, so they are stated rather
	// than left for someone to infer from a total that looks lower than they
	// expected.
	if len(done.result.Denied) > 0 {
		entries = append(entries, mutedEntry(s.theme, fmt.Sprintf(
			"%d director%s could not be read, so totals containing them are a floor.",
			len(done.result.Denied), plural(len(done.result.Denied), "y", "ies"))))
	}
	if done.result.Truncated {
		entries = append(entries, mutedEntry(s.theme,
			"The measurement "+done.result.Reason+", so it is incomplete."))
	}

	return s, transition(newSpaceBrowseBlock(s.deps, s.theme, done.result.Root), entries...)
}

func (s *spaceScanBlock) View(theme Theme, width int) string {
	return "  " + s.activity.view(theme)
}

// spaceBrowseBlock walks the measured tree.
//
// Selection is per directory rather than global. Carrying marks across a
// descent would let a person select a directory, walk into it, select a child,
// and stage both the parent and something inside it, which the deletion engine
// would then have to reconcile. Staging what is marked here, where it is
// marked, keeps that situation from arising.
type spaceBrowseBlock struct {
	deps    *Deps
	theme   Theme
	current *spacescan.Node
	list    selectList
	note    string
}

const spaceVisibleRows = 14

func newSpaceBrowseBlock(deps *Deps, theme Theme, node *spacescan.Node) *spaceBrowseBlock {
	return &spaceBrowseBlock{
		deps:    deps,
		theme:   theme,
		current: node,
		list:    newSelectList(spaceItems(node)),
	}
}

// spaceItems turns one directory's children into rows, unselected.
//
// Nothing starts selected here, unlike clean's checklist. Clean proposes a set
// it can justify from a catalog; this proposes nothing, it only shows what is
// there. Pre-selecting a person's own files because they happen to be large
// would be the tool forming an opinion it has no basis for.
func spaceItems(node *spacescan.Node) []selectableItem {
	items := make([]selectableItem, 0, len(node.Children))
	for _, child := range node.Children {
		detail := "file"
		if child.IsDir {
			detail = fmt.Sprintf("directory, %d item(s)", len(child.Children))
			if !child.Complete {
				detail += ", partly unreadable so this total is a floor"
			}
		}
		items = append(items, selectableItem{
			label:     child.Name,
			detail:    detail,
			sizeBytes: child.Size,
			sizeKnown: child.Complete,
		})
	}
	return items
}

func (s *spaceBrowseBlock) Init() tea.Cmd { return nil }

func (s *spaceBrowseBlock) Update(msg tea.Msg) (liveBlock, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "esc":
			// Escape walks back up before it leaves, so a person who descended
			// several levels is not thrown out of the whole flow by the key
			// they reached for to go back one.
			if parent := s.current.Parent(); parent != nil {
				return newSpaceBrowseBlock(s.deps, s.theme, parent), nil
			}
			return s, finish(cancelEntry(s.theme, "space"))

		case "right", "l":
			if child := s.childUnderCursor(); child != nil && child.IsDir {
				if len(child.Children) == 0 {
					s.note = "that directory is empty or could not be read"
					return s, nil
				}
				return newSpaceBrowseBlock(s.deps, s.theme, child), nil
			}
			s.note = "only a directory can be opened"
			return s, nil

		case "left", "h":
			if parent := s.current.Parent(); parent != nil {
				return newSpaceBrowseBlock(s.deps, s.theme, parent), nil
			}
			s.note = "already at the top"
			return s, nil
		}
	}

	if confirm, ok := msg.(selectListConfirmMsg); ok {
		if len(confirm.selected) == 0 {
			s.note = "select something first, or press esc to go back"
			return s, nil
		}
		return s, transition(newSpacePlanBlock(
			s.deps, s.theme, s.selectedPaths(confirm.selected)))
	}

	updated, cmd := s.list.update(msg, spaceVisibleRows)
	s.list = updated
	return s, cmd
}

func (s *spaceBrowseBlock) childUnderCursor() *spacescan.Node {
	if s.list.cursor < 0 || s.list.cursor >= len(s.current.Children) {
		return nil
	}
	return s.current.Children[s.list.cursor]
}

func (s *spaceBrowseBlock) selectedPaths(indices []int) []string {
	paths := make([]string, 0, len(indices))
	for _, index := range indices {
		if index >= 0 && index < len(s.current.Children) {
			paths = append(paths, s.current.Children[index].Path())
		}
	}
	return paths
}

func (s *spaceBrowseBlock) View(theme Theme, width int) string {
	boxWidth := width - 2
	inner := s.list.view(theme, boxWidth-4, spaceVisibleRows+2)
	if s.note != "" {
		inner += "\n" + lipgloss.NewStyle().Foreground(theme.Danger).Render(s.note)
	}
	inner += "\n" + renderKeyHints(theme,
		[2]string{"→", "open"}, [2]string{"←", "back"},
		[2]string{"space", "select"}, [2]string{"enter", "stage"},
		[2]string{"esc", "up"})

	title := "space · " + displayPath(s.current.Path(), s.deps.Home)
	return renderTitledBox(theme, title, inner, boxWidth)
}

// displayPath shortens a path under the home directory to a tilde form, since
// the full path repeats the same prefix on every row of every screen.
func displayPath(path, home string) string {
	if home != "" && strings.HasPrefix(path, home) {
		return "~" + strings.TrimPrefix(path, home)
	}
	return path
}

// spacePlanBlock plans the selection through the deletion engine.
//
// Nothing selected here bypasses anything. The paths go through Plan exactly
// as clean's catalog candidates do, so path validation, the structural floor,
// and every protection rule apply identically. A person can select their own
// keychain in this browser; the engine is what refuses it.
type spacePlanBlock struct {
	deps     *Deps
	theme    Theme
	paths    []string
	activity activityIndicator
}

func newSpacePlanBlock(deps *Deps, theme Theme, paths []string) *spacePlanBlock {
	return &spacePlanBlock{
		deps: deps, theme: theme, paths: paths,
		activity: newActivityIndicator("Checking"),
	}
}

func (s *spacePlanBlock) Init() tea.Cmd {
	return tea.Batch(
		func() tea.Msg {
			candidates := make([]deletionengine.Candidate, 0, len(s.paths))
			for _, path := range s.paths {
				candidates = append(candidates, deletionengine.Candidate{
					Path:   path,
					RuleID: "space-manual-selection",
					Reason: "selected by hand in the space browser",
				})
			}

			var skipped int
			manifest, err := deletionengine.Plan(candidates, deletionengine.PlanOptions{
				Command:      "space",
				Action:       deletionengine.ActionStage,
				Policy:       s.deps.Rules,
				Log:          s.deps.Log,
				MeasureSizes: true,
				SkipSink:     func(string, string) { skipped++ },
			})
			return scanDoneMsg{manifest: manifest, skipped: skipped, err: err}
		},
		s.activity.init(),
	)
}

func (s *spacePlanBlock) Update(msg tea.Msg) (liveBlock, tea.Cmd) {
	done, ok := msg.(scanDoneMsg)
	if !ok {
		updated, cmd := s.activity.update(msg)
		s.activity = updated
		return s, cmd
	}

	if done.err != nil {
		return s, finish(errorEntry(s.theme, "could not plan: "+done.err.Error()))
	}
	if len(done.manifest.Entries) == 0 {
		entries := []transcriptEntry{infoEntry(s.theme, "Nothing eligible in that selection.")}
		if done.skipped > 0 {
			entries = append(entries, mutedEntry(s.theme, fmt.Sprintf(
				"%d item(s) were protected or excluded.", done.skipped)))
		}
		return s, finish(entries...)
	}

	entries := []transcriptEntry{successEntry(s.theme, fmt.Sprintf(
		"%d item(s) selected · %s", len(done.manifest.Entries),
		humanBytes(done.manifest.TotalBytes)))}
	if done.skipped > 0 {
		entries = append(entries, mutedEntry(s.theme, fmt.Sprintf(
			"%d item(s) protected or excluded, not shown.", done.skipped)))
	}

	return s, transition(
		newSelectionBlock(s.deps, s.theme, "space", done.manifest), entries...)
}

func (s *spacePlanBlock) View(theme Theme, width int) string {
	return "  " + s.activity.view(theme)
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
