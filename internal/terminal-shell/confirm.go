package terminalshell

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// confirmationWord is what a person types to approve an irreversible removal.
//
// It matches the word the command line side asks for, deliberately: the two
// interfaces should not disagree about what approval looks like, and someone
// who has used one should not have to learn a second answer for the other.
//
// A keystroke is the right amount of friction for staging, which undo can
// reverse. It is the wrong amount here. Enter is the key a person has just
// been pressing to get through the preceding steps, and the whole risk of this
// screen is that momentum carries them one step past where they meant to stop.
// A word has to be chosen.
const confirmationWord = "permanently"

// confirmWordBlock gates an irreversible action behind typing a word.
type confirmWordBlock struct {
	theme   Theme
	title   string
	warning string
	input   textinput.Model
	note    string
	onYes   func() tea.Cmd
	onNo    func() tea.Cmd
}

func newConfirmWordBlock(theme Theme, title, warning string,
	onYes, onNo func() tea.Cmd) *confirmWordBlock {

	input := textinput.New()
	input.Placeholder = confirmationWord
	input.Prompt = "❯ "
	input.CharLimit = 32
	input.Focus()

	return &confirmWordBlock{
		theme: theme, title: title, warning: warning,
		input: input, onYes: onYes, onNo: onNo,
	}
}

func (c *confirmWordBlock) Init() tea.Cmd { return textinput.Blink }

func (c *confirmWordBlock) Update(msg tea.Msg) (liveBlock, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		var cmd tea.Cmd
		c.input, cmd = c.input.Update(msg)
		return c, cmd
	}

	switch keyMsg.Type {
	case tea.KeyEsc:
		return c, c.onNo()
	case tea.KeyEnter:
		// Compared after trimming and lowercasing, so a trailing space or a
		// capital letter is not treated as a refusal. Anything that is not the
		// word, including a bare "y", is.
		if strings.ToLower(strings.TrimSpace(c.input.Value())) == confirmationWord {
			return c, c.onYes()
		}
		c.note = "type " + confirmationWord + " exactly, or press esc to cancel"
		c.input.SetValue("")
		return c, nil
	}

	var cmd tea.Cmd
	c.input, cmd = c.input.Update(msg)
	return c, cmd
}

func (c *confirmWordBlock) View(theme Theme, width int) string {
	rows := []string{
		lipgloss.NewStyle().Foreground(theme.Danger).Bold(true).Render(c.warning),
		"",
		lipgloss.NewStyle().Foreground(theme.Body).
			Render("type " + confirmationWord + " to confirm:"),
		c.input.View(),
	}
	if c.note != "" {
		rows = append(rows, lipgloss.NewStyle().Foreground(theme.Danger).Render(c.note))
	}
	rows = append(rows, "",
		renderKeyHints(theme, [2]string{"enter", "confirm"}, [2]string{"esc", "cancel"}))

	return renderTitledBox(theme, c.title,
		lipgloss.JoinVertical(lipgloss.Left, rows...), width-2)
}
