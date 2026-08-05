package terminalshell

import tea "github.com/charmbracelet/bubbletea"

// Screen is one page of the interactive shell.
//
// Update returns the screen that should be current afterward, which is
// normally the receiver itself but does not have to be: a screen that
// finishes its own step, such as a search box after the person presses
// Enter, returns a different Screen to hand control to the next step
// directly, without going through App's push mechanism. Pushing is for
// "go forward, and Esc should come back to me"; returning a different
// screen from Update is for "I am done, something else is now in charge."
type Screen interface {
	Init() tea.Cmd
	Update(msg tea.Msg) (Screen, tea.Cmd)
	View(theme Theme, width, height int) string

	// Title is shown in the shell's header while this screen is current.
	Title() string
}

// pushScreenMsg asks App to put a new screen on top of the stack, so that
// Esc returns to the screen that pushed it.
type pushScreenMsg struct {
	screen Screen
}

// popScreenMsg asks App to remove the current screen and resume the one
// beneath it. Popping the last screen on the stack quits the program.
type popScreenMsg struct{}

// resetToMenuMsg asks App to discard every screen except the root menu.
//
// A results screen is terminal: there is nothing beneath it on the stack
// that still makes sense to return to, since the list and confirm screens
// under it describe a plan that has already been acted on. Popping through
// them one at a time would show stale data; resetting straight to the menu
// does not.
type resetToMenuMsg struct{}

// statusMsg asks App to show a transient status line, for feedback that
// does not warrant a whole screen, such as "operation log could not be
// written."
type statusMsg struct {
	text    string
	isError bool
}

func pushScreen(s Screen) tea.Cmd {
	return func() tea.Msg { return pushScreenMsg{screen: s} }
}

func popScreen() tea.Cmd {
	return func() tea.Msg { return popScreenMsg{} }
}

func resetToMenu() tea.Cmd {
	return func() tea.Msg { return resetToMenuMsg{} }
}

func showStatus(text string, isError bool) tea.Cmd {
	return func() tea.Msg { return statusMsg{text: text, isError: isError} }
}
