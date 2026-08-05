package terminalshell

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// transcriptEntry is one block of history in the viewport: a typed command,
// an output, a result. Entries accumulate downward and scroll up, the
// message-log model the project manager specified; nothing in the shell
// replaces the whole view anymore.
type transcriptEntry struct {
	// body is the pre-styled visible content.
	body string

	// details are collapsible lines behind the disclosure toggle, so the
	// viewport stays readable while the full listing remains one keypress
	// away.
	details []string

	expanded bool
}

// render returns the entry with its disclosure line when details exist.
func (e transcriptEntry) render(theme Theme) string {
	if len(e.details) == 0 {
		return e.body
	}
	if e.expanded {
		detail := lipgloss.NewStyle().Foreground(theme.Body).
			Render(strings.Join(e.details, "\n"))
		return e.body + "\n" + indentLines(detail, "      ")
	}
	toggle := lipgloss.NewStyle().Foreground(theme.Muted).
		Render(fmt.Sprintf("▸ details (%d lines) · ctrl+o", len(e.details)))
	return e.body + "\n    " + toggle
}

func indentLines(s, prefix string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = prefix + line
	}
	return strings.Join(lines, "\n")
}

// Entry constructors. All output lines are indented beneath the echoed
// command that produced them, matching the approved sketch.

// echoEntry records the command a person typed, prompt glyph included, so
// the transcript reads as the conversation it is.
func echoEntry(theme Theme, typed string) transcriptEntry {
	return transcriptEntry{body: lipgloss.NewStyle().Foreground(theme.Accent).Bold(true).Render("❯ ") +
		lipgloss.NewStyle().Foreground(theme.Body).Bold(true).Render(typed)}
}

func infoEntry(theme Theme, text string) transcriptEntry {
	return transcriptEntry{body: "  " + lipgloss.NewStyle().Foreground(theme.Body).Render(text)}
}

func mutedEntry(theme Theme, text string) transcriptEntry {
	return transcriptEntry{body: "  " + lipgloss.NewStyle().Foreground(theme.Muted).Render(text)}
}

func successEntry(theme Theme, text string, details ...string) transcriptEntry {
	return transcriptEntry{
		body: "  " + lipgloss.NewStyle().Foreground(theme.Success).Render("✓ ") +
			lipgloss.NewStyle().Foreground(theme.Body).Render(text),
		details: details,
	}
}

func errorEntry(theme Theme, text string) transcriptEntry {
	return transcriptEntry{body: "  " + lipgloss.NewStyle().Foreground(theme.Danger).Render("✗ "+text)}
}

func cancelEntry(theme Theme, what string) transcriptEntry {
	return mutedEntry(theme, "✗ "+what+" cancelled")
}

// helpEntry lists every command, transcript-native, so help output lands in
// history like any other result instead of switching anywhere.
func helpEntry(theme Theme) transcriptEntry {
	body := lipgloss.NewStyle().Foreground(theme.Body)
	accent := lipgloss.NewStyle().Foreground(theme.Accent)
	var rows []string
	for _, c := range homeCommands {
		rows = append(rows, "  "+accent.Render(c.name)+body.Render("  "+c.description))
	}
	return transcriptEntry{body: strings.Join(rows, "\n")}
}

// welcomeEntry is the greeting box, now simply the first entry in the
// transcript: it scrolls away as history accumulates, the way the reference
// application's own welcome box does, instead of staying pinned and eating
// viewport space forever.
func welcomeEntry(deps *Deps, theme Theme, width int) transcriptEntry {
	leftWidth := 30
	rightWidth := width - leftWidth - 8
	if rightWidth < 20 {
		rightWidth = 20
	}

	muted := lipgloss.NewStyle().Foreground(theme.Muted)
	body := lipgloss.NewStyle().Foreground(theme.Body)
	accent := lipgloss.NewStyle().Foreground(theme.Accent).Bold(true)

	left := lipgloss.NewStyle().Width(leftWidth).Align(lipgloss.Center).Render(
		lipgloss.JoinVertical(lipgloss.Center,
			accent.Render("Welcome to wtff"),
			"",
			body.Render("A terminal-first macOS"),
			body.Render("maintenance toolkit"),
			"",
			muted.Render(deps.Home),
		))

	var commandRows []string
	for _, c := range homeCommands {
		commandRows = append(commandRows,
			lipgloss.NewStyle().Foreground(theme.Accent).Render(c.name)+
				body.Render("  "+c.description))
	}

	right := lipgloss.NewStyle().Width(rightWidth).PaddingLeft(2).Render(
		lipgloss.JoinVertical(lipgloss.Left,
			accent.Render("Getting started"),
			body.Render("Type a command and press Enter. Commands are exact, lowercase words."),
			body.Render("Type / to browse and filter the list instead."),
			lipgloss.NewStyle().Foreground(theme.Border).Render(strings.Repeat("─", rightWidth-2)),
			accent.Render("Commands"),
			lipgloss.JoinVertical(lipgloss.Left, commandRows...),
		))

	columns := lipgloss.JoinHorizontal(lipgloss.Top, left, right)
	return transcriptEntry{body: renderTitledBox(theme, "wtff "+deps.Version, columns, width)}
}
