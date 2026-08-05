package terminalshell

import (
	"github.com/charmbracelet/lipgloss"
)

// Theme holds every color the shell uses, resolved once at startup rather
// than recomputed per render.
//
// Lip Gloss downsamples a declared color to whatever the terminal actually
// supports, ANSI 16, ANSI 256, or true color, using the same terminal
// capability detection termenv performs elsewhere in the Bubble Tea
// ecosystem. Colors are declared here as if true color were always
// available; correctness on a more limited terminal is Lip Gloss's job, not
// this package's.
type Theme struct {
	Accent    lipgloss.Color
	Muted     lipgloss.Color
	Success   lipgloss.Color
	Warning   lipgloss.Color
	Danger    lipgloss.Color
	Border    lipgloss.Color
	Highlight lipgloss.Color
}

// darkTheme and lightTheme are the two fixed palettes. Only two are offered,
// not a configurable accent or contrast level: the terminal already carries
// the user's own foreground and background choice, and wtff's job is to
// pick colors that read clearly against either, not to reproduce a GUI
// app's theme settings panel inside a terminal that already has one.
var (
	darkTheme = Theme{
		Accent:    lipgloss.Color("#5EA1F2"),
		Muted:     lipgloss.Color("#8A8F98"),
		Success:   lipgloss.Color("#59D499"),
		Warning:   lipgloss.Color("#E8B339"),
		Danger:    lipgloss.Color("#F2555A"),
		Border:    lipgloss.Color("#3A3D42"),
		Highlight: lipgloss.Color("#2A2D33"),
	}

	lightTheme = Theme{
		Accent:    lipgloss.Color("#1F6FEB"),
		Muted:     lipgloss.Color("#6E7581"),
		Success:   lipgloss.Color("#1A7F4E"),
		Warning:   lipgloss.Color("#9A6700"),
		Danger:    lipgloss.Color("#CF222E"),
		Border:    lipgloss.Color("#D0D7DE"),
		Highlight: lipgloss.Color("#EEF2F6"),
	}
)

// detectTheme picks a palette based on the terminal's own reported
// background. hasDarkBackground is injected rather than called directly
// here so theme selection is testable without a real terminal.
func detectTheme(hasDarkBackground bool) Theme {
	if hasDarkBackground {
		return darkTheme
	}
	return lightTheme
}
