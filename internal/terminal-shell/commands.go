package terminalshell

import "strings"

// command is one thing the prompt can run, identified by the exact
// lowercase word a person types.
//
// start is nil for quit and help, which the dispatcher handles itself: quit
// produces no flow, and help emits a transcript entry directly. takesArg
// marks the one command that accepts a trailing argument; every other
// command with trailing text is rejected, keeping the exact-match rule
// intact per token.
type command struct {
	name        string
	description string
	takesArg    bool
	start       func(deps *Deps, theme Theme, arg string) liveBlock
}

// homeCommands is the full, fixed set of words the prompt recognizes.
// There is no partial or case-insensitive form of this list: what
// dispatches a command is exact, case-sensitive equality against one of
// these names, checked in matchCommand. The palette exists to help a person
// find the right word, not to let anything less than the right word run
// something.
var homeCommands = []command{
	{name: "clean", description: "Find and remove reclaimable cache directories",
		start: func(d *Deps, t Theme, _ string) liveBlock { return startCleanFlow(d, t) }},
	{name: "uninstall", description: "Remove an installed application and its data", takesArg: true,
		start: func(d *Deps, t Theme, arg string) liveBlock { return startUninstallFlow(d, t, arg) }},
	{name: "purge", description: "Empty the Trash permanently, nothing is staged",
		start: func(d *Deps, t Theme, _ string) liveBlock { return startPurgeFlow(d, t) }},
	{name: "staged", description: "Restore or permanently delete items removed earlier",
		start: func(d *Deps, t Theme, _ string) liveBlock { return startStagedFlow(d, t) }},
	{name: "doctor", description: "Check wtff's own state and this machine's setup",
		start: func(d *Deps, t Theme, _ string) liveBlock { return startDoctorFlow(d, t) }},
	{name: "help", description: "Show this list"},
	{name: "quit", description: "Exit wtff"},
}

// matchCommand looks up a command by exact, case-sensitive name. wtff's own
// standing rule is that a person typing something means exactly what they
// typed; silently accepting "Clean" for "clean" would be a small kindness
// that also means the tool sometimes runs on input the person did not
// actually write.
func matchCommand(input string) (command, bool) {
	trimmed := strings.TrimSpace(input)
	for _, c := range homeCommands {
		if c.name == trimmed {
			return c, true
		}
	}
	return command{}, false
}

// filterCommands returns every command whose name starts with prefix, for
// the palette's live filtering as a person types after "/". This is a
// browsing aid over a fixed, fully enumerated list, not a resolution
// mechanism: the person still presses Enter on one specific highlighted
// entry, so nothing here weakens the exact-match rule matchCommand enforces
// for direct input.
func filterCommands(prefix string) []command {
	var out []command
	for _, c := range homeCommands {
		if strings.HasPrefix(c.name, prefix) {
			out = append(out, c)
		}
	}
	return out
}
