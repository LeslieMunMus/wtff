// Package userconfig resolves where a person's own wtff configuration lives
// and loads what it holds.
//
// Configuration can add protection rules and add cleanup categories. What it
// cannot do is reach past the floor: a critical protection stays critical, and
// a carve out aimed at one is refused at load rather than ignored at
// evaluation, so a person learns their rule does nothing instead of believing
// it works.
package userconfig

import (
	"fmt"
	"os"
	"path/filepath"

	cleancatalog "github.com/lesliemusengi/wtff/internal/clean-catalog"
	protectionrules "github.com/lesliemusengi/wtff/internal/protection-rules"
)

// Layout is where configuration is read from.
//
// The XDG location rather than Application Support, chosen deliberately: this
// is a command line tool, the files are meant to be edited by hand and kept in
// a dotfiles repository, and Application Support is neither convenient for the
// first nor conventional for the second. wtff's own state, which nobody edits,
// stays where macOS expects it.
type Layout struct {
	Root       string
	RulesDir   string
	CatalogDir string
}

// DefaultLayout resolves the conventional location, honouring XDG_CONFIG_HOME
// when it is set, since a person who set it meant it.
func DefaultLayout() (Layout, error) {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return Layout{}, fmt.Errorf("cannot determine home directory: %w", err)
		}
		base = filepath.Join(home, ".config")
	}
	return LayoutAt(filepath.Join(base, "wtff")), nil
}

// LayoutAt builds a layout rooted at a specific directory.
func LayoutAt(root string) Layout {
	return Layout{
		Root:       root,
		RulesDir:   filepath.Join(root, "rules"),
		CatalogDir: filepath.Join(root, "catalog"),
	}
}

// Exists reports whether anything is configured, so a caller can say "none"
// rather than "zero rules".
func (l Layout) Exists() bool {
	_, err := os.Stat(l.Root)
	return err == nil
}

// LoadRules returns the built in protection rules merged with the user's.
func LoadRules(home string, layout Layout) (*protectionrules.Set, error) {
	return protectionrules.LoadWithUserRules(home, layout.RulesDir)
}

// LoadCatalog returns the built in cleanup catalog merged with the user's.
func LoadCatalog(layout Layout) (*cleancatalog.Catalog, error) {
	return cleancatalog.LoadWithUserEntries(layout.CatalogDir)
}
