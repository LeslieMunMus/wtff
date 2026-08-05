package cli

import "strings"

// reorderFlagsFirst moves recognized boolean flags to the front of an
// argument list, preserving the relative order within both the flags and the
// remaining arguments, so that flag.FlagSet's own parser, which stops
// looking for flags at the first non-flag token, sees every flag regardless
// of where the person actually typed it.
//
// The standard library's flag package does not interleave flags and
// positional arguments: once it hits something that is not a recognized
// flag, everything after that point is treated as positional, silently,
// with no error. wtff remove /tmp/cache --dry-run demonstrated the
// consequence directly: --dry-run became a second path argument named
// literally "--dry-run", which did not exist and was reported as a skip, the
// dry-run flag itself was never set, and the command fell through to a real
// confirmation prompt. A person who typed the flag in that order and trusted
// it, exactly the order many other command line tools accept without
// complaint, would have had no dry run at all.
//
// A bare "--" argument is honored as the point after which nothing is
// reordered or treated as a flag, matching flag.FlagSet's own convention for
// explicitly ending flag parsing, so a path that happens to start with a
// dash can still be named.
func reorderFlagsFirst(args []string, knownFlags map[string]bool) []string {
	var flags, rest []string

	for i, arg := range args {
		if arg == "--" {
			rest = append(rest, args[i:]...)
			break
		}
		name := strings.TrimLeft(arg, "-")
		if strings.HasPrefix(arg, "-") && knownFlags[name] {
			flags = append(flags, arg)
			continue
		}
		rest = append(rest, arg)
	}

	return append(flags, rest...)
}

// commonBoolFlags lists every boolean flag used across wtff's commands, plus
// the help aliases flag.FlagSet recognizes on its own. A command that only
// defines some of these is unaffected by the ones it does not register:
// flag.Parse only acts on flags it knows about, so listing the full set here
// costs nothing per command and avoids each call site maintaining its own
// partial list that could drift from what its FlagSet actually defines.
var commonBoolFlags = map[string]bool{
	"dry-run": true,
	"purge":   true,
	"yes":     true,
	"help":    true,
	"h":       true,
}
