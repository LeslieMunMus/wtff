package terminalshell

import (
	"io"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// statusDisplayDuration is how long a transient status line stays visible.
const statusDisplayDuration = 4 * time.Second

// App is the root Bubble Tea model. It owns the screen stack, the resolved
// theme, and the current terminal size, and is otherwise deliberately thin:
// every actual behavior lives in whichever Screen is on top.
type App struct {
	deps  *Deps
	theme Theme

	stack []Screen

	width, height int

	statusText    string
	statusIsError bool
	statusToken   int

	quitting bool
}

// clearStatusMsg removes the status line once its display duration elapses.
type clearStatusMsg struct {
	// token guards against an old status's timer clearing a newer status
	// that was shown after it but before the old timer fired. Carried on the
	// message rather than read from a package level counter, so that two
	// App instances, as tests construct independently, cannot cross-clear
	// each other's status text.
	token int
}

// NewApp constructs the root model with the given dependencies and initial
// screen. hasDarkBackground selects the theme; callers pass the terminal's
// own reported background rather than this package guessing at it, which is
// what keeps theme selection testable without a real terminal attached.
func NewApp(deps *Deps, hasDarkBackground bool, initial Screen) App {
	return App{
		deps:  deps,
		theme: detectTheme(hasDarkBackground),
		stack: []Screen{initial},
	}
}

func (a App) Init() tea.Cmd {
	if len(a.stack) == 0 {
		return nil
	}
	return a.current().Init()
}

func (a App) current() Screen {
	return a.stack[len(a.stack)-1]
}

func (a App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width, a.height = msg.Width, msg.Height

	case tea.KeyMsg:
		// ctrl+c always quits immediately, regardless of how deep the screen
		// stack is. Every other key goes to the current screen first; only a
		// screen that does not consume Esc itself falls through to the
		// stack's own pop behavior, which is why this check happens after
		// dispatch rather than before.
		if msg.Type == tea.KeyCtrlC {
			a.quitting = true
			return a, tea.Quit
		}

	case pushScreenMsg:
		a.stack = append(a.stack, msg.screen)
		return a, msg.screen.Init()

	case popScreenMsg:
		if len(a.stack) <= 1 {
			a.quitting = true
			return a, tea.Quit
		}
		a.stack = a.stack[:len(a.stack)-1]
		return a, nil

	case resetToMenuMsg:
		if len(a.stack) > 1 {
			a.stack = a.stack[:1]
		}
		return a, nil

	case statusMsg:
		a.statusToken++
		a.statusText = msg.text
		a.statusIsError = msg.isError
		token := a.statusToken
		return a, tea.Tick(statusDisplayDuration, func(time.Time) tea.Msg {
			return clearStatusMsg{token: token}
		})

	case clearStatusMsg:
		if msg.token == a.statusToken {
			a.statusText = ""
		}
		return a, nil
	}

	if len(a.stack) == 0 {
		return a, nil
	}
	updated, cmd := a.current().Update(msg)
	a.stack[len(a.stack)-1] = updated
	return a, cmd
}

func (a App) View() string {
	if a.quitting || len(a.stack) == 0 {
		return ""
	}
	if a.width == 0 || a.height == 0 {
		// The first frame can render before the initial WindowSizeMsg
		// arrives. Printing nothing for one frame is preferable to laying
		// out content against a guessed size.
		return ""
	}

	header := renderHeader(a.theme, a.current().Title(), a.width)
	footer := renderFooter(a.theme, a.statusText, a.statusIsError, a.width)

	bodyHeight := a.height - lipgloss.Height(header) - lipgloss.Height(footer)
	if bodyHeight < 0 {
		bodyHeight = 0
	}
	body := a.current().View(a.theme, a.width, bodyHeight)

	return lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
}

// Run starts the shell against the given input and output, in the
// alternate screen buffer.
//
// input and output are explicit parameters, matching the same discipline
// internal/cli uses for its commands, so a caller can redirect them in a
// test rather than the program always reaching for the process's real
// stdin and stdout.
func Run(deps *Deps, hasDarkBackground bool, input io.Reader, output io.Writer) error {
	program := tea.NewProgram(
		NewApp(deps, hasDarkBackground, newHomeScreen(deps)),
		tea.WithAltScreen(),
		tea.WithInput(input),
		tea.WithOutput(output),
	)
	_, err := program.Run()
	return err
}
