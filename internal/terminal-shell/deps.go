package terminalshell

import (
	"fmt"
	"os"

	cleancatalog "github.com/lesliemusengi/wtff/internal/clean-catalog"
	deletionengine "github.com/lesliemusengi/wtff/internal/deletion-engine"
	operationlog "github.com/lesliemusengi/wtff/internal/operation-log"
	protectionrules "github.com/lesliemusengi/wtff/internal/protection-rules"
)

// Deps bundles what every screen needs to reach the rest of wtff.
//
// Built once, at startup, and passed down rather than having each screen
// load its own copy. Loading the protection rule set or opening the
// operation log twice would not be incorrect, only wasteful, but a single
// shared instance also means every screen in one run is judging paths
// against the exact same loaded rule set, which matters if a rule file were
// ever changed on disk mid session.
type Deps struct {
	Home    string
	Rules   *protectionrules.Set
	Catalog *cleancatalog.Catalog
	Log     *operationlog.Writer
}

// NewDeps loads every shared dependency, in the same way internal/cli's
// commands do, so a plan built inside the shell and a plan built by wtff
// clean --dry-run are produced against identical inputs.
func NewDeps() (*Deps, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("cannot determine home directory: %w", err)
	}

	rules, err := protectionrules.LoadBuiltin()
	if err != nil {
		return nil, fmt.Errorf("cannot load protection rules: %w", err)
	}

	catalog, err := cleancatalog.LoadBuiltin()
	if err != nil {
		return nil, fmt.Errorf("cannot load the cleanup catalog: %w", err)
	}

	logPath, err := operationlog.DefaultPath()
	if err != nil {
		return nil, fmt.Errorf("cannot determine log location: %w", err)
	}
	log, err := operationlog.Open(logPath, "shell")
	if err != nil {
		return nil, fmt.Errorf("cannot open operation log: %w", err)
	}

	return &Deps{Home: home, Rules: rules, Catalog: catalog, Log: log}, nil
}

// Close releases resources Deps opened.
func (d *Deps) Close() {
	if d == nil {
		return
	}
	d.Log.Close()
}

// newStagingArea opens the staging area, done lazily rather than in NewDeps
// since not every screen stages anything, and creating the directory is a
// side effect worth deferring until it is actually needed.
func (d *Deps) newStagingArea() (*deletionengine.StagingArea, error) {
	root, err := deletionengine.DefaultStagingRoot()
	if err != nil {
		return nil, fmt.Errorf("cannot determine staging location: %w", err)
	}
	return deletionengine.NewStagingArea(root)
}
