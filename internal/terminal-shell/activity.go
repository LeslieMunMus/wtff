package terminalshell

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// activityTickInterval paces the spinner. Fast enough to read as motion,
// slow enough not to compete for CPU with the filesystem work underneath.
const activityTickInterval = 150 * time.Millisecond

// spinnerFrames is the compact pulse glyph cycle, drawn in the brand color.
//
// The first version of the activity display rendered the full animated mark
// across a multi-line canvas, which on a real screen looked like a field of
// drifting dots and was rejected. The reference the project manager supplied
// is a single line: a small pulsing glyph, then the status text and timer.
// This matches that shape; the full mark now appears only on the welcome
// screen, static, so motion in this program always means work is happening.
var spinnerFrames = []string{"✻", "✼", "✽", "✼"}

// activityIndicator is the shared "something is happening" line: a pulsing
// glyph, a label, and a live elapsed timer. Every screen that waits on a
// filesystem operation shows one instead of a static string, which looked
// identical whether the operation was progressing or completely hung.
//
// It shows elapsed time, not progress. A real "cleaned so far of total"
// figure needs the deletion engine to report partial results while it
// works, which is the deferred core fix; when that lands, this line is
// where the figure goes.
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
// else, so a caller can route every message through it unconditionally.
func (a activityIndicator) update(msg tea.Msg) (activityIndicator, tea.Cmd) {
	if _, ok := msg.(activityTickMsg); !ok {
		return a, nil
	}
	a.ticks++
	return a, a.init()
}

// view renders the single status line.
func (a activityIndicator) view(theme Theme) string {
	glyph := lipgloss.NewStyle().Foreground(brandColor).Bold(true).
		Render(spinnerFrames[a.ticks%len(spinnerFrames)])
	elapsed := time.Since(a.startedAt).Round(time.Second)
	status := lipgloss.NewStyle().Foreground(theme.Muted).
		Render(fmt.Sprintf("%s · %s", a.label, elapsed))
	return glyph + "  " + status
}
