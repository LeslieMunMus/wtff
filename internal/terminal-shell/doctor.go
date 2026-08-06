package terminalshell

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/lesliemusengi/wtff/internal/diagnostics"
)

// startDoctorFlow reports wtff's own state into the transcript.
//
// It runs on a spinner block rather than inline because the checks touch the
// filesystem, and one of them deliberately attempts reads macOS may take its
// time refusing.
func startDoctorFlow(deps *Deps, theme Theme) liveBlock {
	return &doctorBlock{deps: deps, theme: theme,
		activity: newActivityIndicator("Checking")}
}

type doctorBlock struct {
	deps     *Deps
	theme    Theme
	activity activityIndicator
}

type doctorDoneMsg struct {
	report diagnostics.Report
	err    error
}

func (d *doctorBlock) Init() tea.Cmd {
	return tea.Batch(
		func() tea.Msg {
			opts, err := diagnostics.DefaultOptions()
			if err != nil {
				return doctorDoneMsg{err: err}
			}
			return doctorDoneMsg{report: diagnostics.Run(opts)}
		},
		d.activity.init(),
	)
}

func (d *doctorBlock) Update(msg tea.Msg) (liveBlock, tea.Cmd) {
	done, ok := msg.(doctorDoneMsg)
	if !ok {
		updated, cmd := d.activity.update(msg)
		d.activity = updated
		return d, cmd
	}

	if done.err != nil {
		return d, finish(errorEntry(d.theme, "cannot check: "+done.err.Error()))
	}

	entries := make([]transcriptEntry, 0, len(done.report.Findings)+1)
	for _, finding := range done.report.Findings {
		text := fmt.Sprintf("%s: %s", finding.Area, finding.Summary)
		switch finding.Level {
		case diagnostics.LevelWarn:
			entries = append(entries, warningEntry(d.theme, text, finding.Detail...))
		case diagnostics.LevelNote:
			// Notes carry their detail behind the disclosure toggle too, so a
			// healthy machine's report stays a handful of lines rather than a
			// wall a person learns to skip.
			entries = append(entries, infoDetailEntry(d.theme, text, finding.Detail...))
		default:
			entries = append(entries, successEntry(d.theme, text, finding.Detail...))
		}
	}
	if !done.report.NeedsAttention() {
		entries = append(entries, mutedEntry(d.theme, "Nothing needs attention."))
	}
	return d, finish(entries...)
}

func (d *doctorBlock) View(theme Theme, width int) string {
	return "  " + d.activity.view(theme)
}
