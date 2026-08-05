package terminalshell

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// activityTickInterval paces the logo animation. Fast enough to read as
// continuous motion, slow enough not to compete for CPU with whatever
// filesystem work is actually running underneath it.
const activityTickInterval = 90 * time.Millisecond

// logoHalfCycleTicks is how many ticks one direction of the breathe, out
// then in, takes. At the interval above this is roughly a two second cycle
// end to end.
const logoHalfCycleTicks = 11

// activityIndicator is the shared "something is happening" widget: the
// animated brand mark, an elapsed timer, and a label. Every screen that
// waits on a filesystem operation, discovering, applying, undoing, uses one
// instead of a static "Working…" string, which was the exact complaint that
// prompted building this: a frozen line of text and a hang look identical,
// and there was no way to tell them apart from the screen alone.
type activityIndicator struct {
	label     string
	startedAt time.Time
	ticks     int
}

func newActivityIndicator(label string) activityIndicator {
	return activityIndicator{label: label, startedAt: time.Now()}
}

type activityTickMsg struct{}

func (a activityIndicator) init() tea.Cmd {
	return tea.Tick(activityTickInterval, func(time.Time) tea.Msg {
		return activityTickMsg{}
	})
}

// update advances the animation on its own tick and is a no-op for anything
// else, so a caller can route every message through it unconditionally
// without first checking the message type itself.
func (a activityIndicator) update(msg tea.Msg) (activityIndicator, tea.Cmd) {
	if _, ok := msg.(activityTickMsg); !ok {
		return a, nil
	}
	a.ticks++
	return a, a.init()
}

func (a activityIndicator) view(theme Theme) string {
	mark := logoFrame(logoPhase(a.ticks, logoHalfCycleTicks))
	elapsed := time.Since(a.startedAt).Round(time.Second)
	status := lipgloss.NewStyle().Foreground(theme.Muted).
		Render(fmt.Sprintf("%s  ·  %s", a.label, elapsed))
	return lipgloss.JoinVertical(lipgloss.Left, mark, "", status)
}
