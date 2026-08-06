package terminalshell

import (
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// activityIndicator is the shared "something is happening" line: a spinner,
// a label, and a live elapsed timer. Every screen that waits on a
// filesystem operation shows one instead of a static string, which looked
// identical whether the operation was progressing or completely hung.
//
// The spinner is the charmbracelet/bubbles spinner component, by explicit
// direction, rather than the hand-rolled glyph cycle it replaces. It renders
// in the main brand color, and the whole indicator stays one line: the
// project manager rejected an earlier multi-line animated canvas here, and
// the supplied reference for this element is a single compact status line.
//
// Alongside the timer it shows a live "so far of total" figure once the work
// reports one. Elapsed time says the program is alive; the counter says the
// work is actually advancing, which is the difference between a slow scan and
// a stuck one.
type activityIndicator struct {
	spin      spinner.Model
	label     string
	startedAt time.Time

	// progress is read on every render tick, never stored to from here. It is
	// nil for work that cannot report a total.
	progress *progressCounter
}

func newActivityIndicator(label string) activityIndicator {
	s := spinner.New(
		spinner.WithSpinner(spinner.MiniDot),
		spinner.WithStyle(lipgloss.NewStyle().Foreground(brandTheme.Accent)),
	)
	return activityIndicator{spin: s, label: label, startedAt: time.Now()}
}

// withProgress attaches a counter the work will report into.
func (a activityIndicator) withProgress(counter *progressCounter) activityIndicator {
	a.progress = counter
	return a
}

func (a activityIndicator) init() tea.Cmd {
	return a.spin.Tick
}

// update advances the spinner on its own tick messages and is a no-op for
// anything else, so a caller can route every message through it
// unconditionally.
func (a activityIndicator) update(msg tea.Msg) (activityIndicator, tea.Cmd) {
	var cmd tea.Cmd
	a.spin, cmd = a.spin.Update(msg)
	return a, cmd
}

// view renders the single status line.
func (a activityIndicator) view(theme Theme) string {
	elapsed := time.Since(a.startedAt).Round(time.Second)
	text := fmt.Sprintf("%s · %s", a.label, elapsed)
	if a.progress != nil {
		if counted := a.progress.label(); counted != "" {
			text = fmt.Sprintf("%s · %s · %s", a.label, counted, elapsed)
		}
	}
	status := lipgloss.NewStyle().Foreground(theme.Muted).Render(text)
	return a.spin.View() + "  " + status
}
