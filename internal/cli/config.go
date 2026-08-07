package cli

import (
	"fmt"
	"io"

	cleancatalog "github.com/lesliemunmus/wtff/internal/clean-catalog"
	protectionrules "github.com/lesliemunmus/wtff/internal/protection-rules"
	userconfig "github.com/lesliemunmus/wtff/internal/user-config"
)

// loadRules loads the protection rules a command should use, merging the
// user's own configuration, and reports any built in protection those override.
//
// The report is printed before anything is planned. Allowing an override at
// all was conditional on it never being silent, and a notice that arrives once
// a path has already been removed is not a warning, it is a receipt.
func loadRules(command string, stdout, stderr io.Writer) (*protectionrules.Set, bool) {
	layout, err := userconfig.DefaultLayout()
	if err != nil {
		fmt.Fprintf(stderr, "wtff %s: %v\n", command, err)
		return nil, false
	}

	home, err := homeDirectory()
	if err != nil {
		fmt.Fprintf(stderr, "wtff %s: %v\n", command, err)
		return nil, false
	}

	rules, err := userconfig.LoadRules(home, layout)
	if err != nil {
		// A configuration that does not load stops the command outright. The
		// alternative is running with fewer protections than the person
		// believes they have, which is the one failure this project refuses to
		// let happen quietly.
		fmt.Fprintf(stderr, "wtff %s: cannot load protection rules: %v\n", command, err)
		return nil, false
	}
	return rules, true
}

// reportOverrides prints any built in protection the user's configuration
// currently wins against.
func reportOverrides(rules *protectionrules.Set, stdout io.Writer) {
	overrides := rules.Overrides()
	if len(overrides) == 0 {
		return
	}
	fmt.Fprintf(stdout, "note: your configuration overrides %d built in protection(s):\n",
		len(overrides))
	for _, override := range overrides {
		fmt.Fprintf(stdout, "  %s is allowed by %s (%s), which overrides %s\n",
			override.Path, override.UserRuleID, override.UserRuleFile, override.BuiltinRuleID)
	}
	fmt.Fprintln(stdout)
}

// loadCatalog loads the cleanup catalog merged with the user's own entries.
func loadCatalog(command string, stderr io.Writer) (*cleancatalog.Catalog, bool) {
	layout, err := userconfig.DefaultLayout()
	if err != nil {
		fmt.Fprintf(stderr, "wtff %s: %v\n", command, err)
		return nil, false
	}
	catalog, err := userconfig.LoadCatalog(layout)
	if err != nil {
		fmt.Fprintf(stderr, "wtff %s: cannot load the cleanup catalog: %v\n", command, err)
		return nil, false
	}
	return catalog, true
}
