package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	deletionengine "github.com/lesliemusengi/wtff/internal/deletion-engine"
	uninstallcore "github.com/lesliemusengi/wtff/internal/uninstall-core"
)

// runCompletion prints a completion script for the named shell.
//
// The script is emitted rather than installed. Writing into a shell's
// completion directory means guessing which of several locations is on the
// user's fpath, and doing it for them means a tool that removes files also
// quietly adds one somewhere they did not ask for.
func runCompletion(args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "wtff completion: exactly one shell is required, zsh or bash")
		return 2
	}

	switch args[0] {
	case "zsh":
		fmt.Fprint(stdout, zshCompletion())
	case "bash":
		fmt.Fprint(stdout, bashCompletion())
	default:
		fmt.Fprintf(stderr, "wtff completion: unsupported shell %q, expected zsh or bash\n", args[0])
		return 2
	}
	return 0
}

// completableCommands is the list both scripts are generated from, so a new
// command cannot be added to the dispatcher and forgotten here.
//
// Deliberately built from a single source rather than written twice. The usage
// text and this list drifting apart would be invisible until someone pressed
// tab and got a stale answer.
var completableCommands = []struct {
	name        string
	description string
	flags       []string
}{
	{"clean", "Find and remove reclaimable cache directories",
		[]string{"--dry-run", "--purge", "--yes", "--json"}},
	{"uninstall", "Remove an installed application and its data",
		[]string{"--dry-run", "--purge", "--yes", "--json"}},
	{"remove", "Remove one or more specific paths",
		[]string{"--dry-run", "--purge", "--yes", "--json"}},
	{"purge", "Permanently empty the Trash, nothing is staged",
		[]string{"--dry-run", "--yes", "--json"}},
	{"staged", "List, restore, or permanently delete staged batches",
		[]string{"--purge", "--all", "--yes", "--json"}},
	{"undo", "Restore a staged batch to where it came from",
		nil},
	{"doctor", "Check wtff's own state and this machine's setup",
		[]string{"--quiet", "--json"}},
	{"completion", "Print a completion script for zsh or bash", nil},
	{"version", "Print the version", nil},
	{"help", "Show usage", nil},
}

// runCompletionValues answers the dynamic half of completion: the values that
// depend on what is on this machine right now.
//
// These are hidden commands rather than documented ones. They exist for a
// shell to call, their output shape is whatever the scripts need, and
// promising anything about them to a person would make that shape a contract.
// A failure prints nothing and exits zero, because a completion that reports
// an error into the middle of a prompt is worse than one that offers no
// suggestions.
func runCompletionValues(kind string, stdout io.Writer) int {
	switch kind {
	case "__complete-batches":
		root, err := deletionengine.DefaultStagingRoot()
		if err != nil {
			return 0
		}
		area, err := deletionengine.NewStagingArea(root)
		if err != nil {
			return 0
		}
		batches, err := area.ListBatches()
		if err != nil {
			return 0
		}
		for _, batch := range batches {
			fmt.Fprintln(stdout, batch.BatchID)
		}

	case "__complete-apps":
		home, err := os.UserHomeDir()
		if err != nil {
			return 0
		}
		apps, _, err := uninstallcore.DiscoverApps(appSearchRoots(home))
		if err != nil {
			return 0
		}
		for _, app := range apps {
			// One name per line, so a shell can read them without deciding
			// where a name ends. Application names routinely contain spaces,
			// and splitting on whitespace is how "Google Chrome" becomes two
			// useless suggestions.
			fmt.Fprintln(stdout, app.DisplayName)
		}
	}
	return 0
}

func zshCompletion() string {
	var commands, cases strings.Builder
	for _, command := range completableCommands {
		fmt.Fprintf(&commands, "    '%s:%s'\n", command.name, zshQuote(command.description))
	}
	for _, command := range completableCommands {
		if len(command.flags) == 0 {
			continue
		}
		fmt.Fprintf(&cases, "        %s)\n          _values 'flag' %s\n          ;;\n",
			command.name, strings.Join(quoteAll(command.flags), " "))
	}

	return `#compdef wtff
# zsh completion for wtff.
#
# Install by putting this file somewhere on your fpath as _wtff, for example:
#   wtff completion zsh > /usr/local/share/zsh/site-functions/_wtff
# then start a new shell.

_wtff() {
  local -a commands
  commands=(
` + commands.String() + `  )

  _arguments -C \
    '1: :->command' \
    '*:: :->argument'

  case $state in
    command)
      _describe -t commands 'wtff command' commands
      ;;
    argument)
      case $words[1] in
        undo)
          # Batch identifiers are long and random by design, so completing
          # them is the difference between typing one and copying one.
          local -a batches
          batches=(${(f)"$(wtff __complete-batches 2>/dev/null)"})
          _describe -t batches 'staged batch' batches
          ;;
        uninstall)
          local -a apps
          apps=(${(f)"$(wtff __complete-apps 2>/dev/null)"})
          _describe -t apps 'installed application' apps
          ;;
        staged)
          local -a batches
          batches=(${(f)"$(wtff __complete-batches 2>/dev/null)"})
          _alternative \
            'flags:flag:(--purge --all --yes --json)' \
            "batches:staged batch:((${batches[*]}))"
          ;;
        completion)
          _values 'shell' zsh bash
          ;;
        remove)
          _files
          ;;
` + cases.String() + `      esac
      ;;
  esac
}

_wtff "$@"
`
}

func bashCompletion() string {
	var names []string
	var cases strings.Builder
	for _, command := range completableCommands {
		names = append(names, command.name)
		if len(command.flags) == 0 {
			continue
		}
		fmt.Fprintf(&cases, "      %s)\n        flags=\"%s\"\n        ;;\n",
			command.name, strings.Join(command.flags, " "))
	}

	// Written for bash 3.2, which is what macOS ships. No associative arrays,
	// no mapfile, no readarray: all of those arrived in bash 4 and would make
	// this fail on the exact system it is written for.
	return `# bash completion for wtff.
#
# Install by sourcing this from your bash startup file, for example:
#   wtff completion bash > ~/.wtff-completion.bash
#   echo 'source ~/.wtff-completion.bash' >> ~/.bash_profile
#
# Written for bash 3.2, the version macOS ships.

_wtff() {
  local cur prev command flags
  cur="${COMP_WORDS[COMP_CWORD]}"
  command="${COMP_WORDS[1]}"

  if [ "$COMP_CWORD" -eq 1 ]; then
    COMPREPLY=( $(compgen -W "` + strings.Join(names, " ") + `" -- "$cur") )
    return
  fi

  case "$command" in
    undo)
      local IFS=$'\n'
      COMPREPLY=( $(compgen -W "$(wtff __complete-batches 2>/dev/null)" -- "$cur") )
      return
      ;;
    uninstall)
      local IFS=$'\n'
      COMPREPLY=( $(compgen -W "$(wtff __complete-apps 2>/dev/null)" -- "$cur") )
      return
      ;;
    completion)
      COMPREPLY=( $(compgen -W "zsh bash" -- "$cur") )
      return
      ;;
    remove)
      COMPREPLY=( $(compgen -f -- "$cur") )
      return
      ;;
  esac

  flags=""
  case "$command" in
` + cases.String() + `  esac
  COMPREPLY=( $(compgen -W "$flags" -- "$cur") )
}

complete -F _wtff wtff
`
}

// zshQuote escapes what would otherwise end a description or a quoted string.
func zshQuote(s string) string {
	s = strings.ReplaceAll(s, `'`, `'\''`)
	return strings.ReplaceAll(s, ":", `\:`)
}

func quoteAll(values []string) []string {
	quoted := make([]string, 0, len(values))
	for _, value := range values {
		quoted = append(quoted, "'"+value+"'")
	}
	return quoted
}
