package terminalshell

import (
	"github.com/charmbracelet/lipgloss"
)

// Theme holds every color the shell uses, resolved once at startup rather
// than recomputed per render.
//
// The values are the supplied brand palette, applied as directed: the main
// color carries every border, divider, heading, command name, and the
// prompt; body text carries sentences and descriptions; the secondary color
// carries hints and de-emphasized detail; the highlight color is the
// background of a selected row.
type Theme struct {
	// Accent is the main brand color: all lines, borders, headings, command
	// names, the prompt, and the spinner.
	Accent lipgloss.Color

	// Body is the reading-text color for sentences and descriptions.
	Body lipgloss.Color

	// Muted is the secondary color, for hints, timers, and detail a person
	// scans past rather than reads.
	Muted lipgloss.Color

	// Success and Danger are outcome colors. Success comes from the supplied
	// palette. Danger was not specified; #AE0A0A follows the palette's own
	// pattern, the main color's digits rotated the same way the supplied
	// green is, and stands until the project manager picks otherwise.
	Success lipgloss.Color
	Danger  lipgloss.Color

	// Warning is used for partial outcomes, such as a restore that skipped
	// items. Not in the supplied palette; flagged for a decision, amber
	// meanwhile.
	Warning lipgloss.Color

	// Border duplicates Accent by instruction: all lines and borders use the
	// main color. Kept as its own field so call sites read as intent.
	Border lipgloss.Color

	// Highlight is the selected-row background.
	Highlight lipgloss.Color
}

// brandTheme is the single palette, per the supplied theme specification.
//
// The specification also names #F5F5F5 as the background. The shell
// deliberately does not paint the terminal background: filling every cell
// fights the user's own terminal profile, breaks on resize, and the
// specification's background is best honored by running in a terminal
// profile with that background, which the reporting terminal already has.
// Foregrounds, borders, and the row highlight are where the palette is
// enforced from inside the program.
var brandTheme = Theme{
	Accent:    lipgloss.Color("#0A0AAE"),
	Body:      lipgloss.Color("#3D3D3D"),
	Muted:     lipgloss.Color("#BCBCFB"),
	Success:   lipgloss.Color("#0AAE0A"),
	Danger:    lipgloss.Color("#AE0A0A"),
	Warning:   lipgloss.Color("#9A6700"),
	Border:    lipgloss.Color("#0A0AAE"),
	Highlight: lipgloss.Color("#E1E1FD"),
}

// detectTheme now returns the brand palette unconditionally. The earlier
// dark and light pair is gone: the supplied palette is explicit and single,
// and honoring it beats adapting it. The known cost, recorded rather than
// hidden: on a dark-background terminal, #3D3D3D body text will read
// poorly. The palette is a light-background design, and a dark variant is a
// decision for the palette's owner, not something to improvise here. The
// parameter is kept so call sites and the startup detection path stay
// unchanged for when that variant exists.
func detectTheme(_ bool) Theme {
	return brandTheme
}
