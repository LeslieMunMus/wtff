package cli

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// confirmationWord is what a person must type to approve an irreversible
// purge. A single keystroke answer is appropriate for a staged removal, which
// undo can reverse; it is not appropriate for one that cannot be undone. The
// full word means an accidental extra keypress, or a finger already moving
// toward Enter from a previous prompt, cannot approve a purge by accident.
const confirmationWord = "permanently"

// confirmStage asks for a short yes or no answer, appropriate for a reversible
// action.
func confirmStage(stdin io.Reader, stdout io.Writer, prompt string) bool {
	fmt.Fprint(stdout, prompt)
	answer := readLine(stdin)
	return answer == "y" || answer == "yes"
}

// confirmPurge asks for the full confirmation word, appropriate for an
// irreversible action. Anything else, including a bare "y", is treated as no.
func confirmPurge(stdin io.Reader, stdout io.Writer, prompt string) bool {
	fmt.Fprint(stdout, prompt)
	return readLine(stdin) == confirmationWord
}

func readLine(r io.Reader) string {
	line, _ := bufio.NewReader(r).ReadString('\n')
	return strings.ToLower(strings.TrimSpace(line))
}
