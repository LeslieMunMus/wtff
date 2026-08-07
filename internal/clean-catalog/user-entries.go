package cleancatalog

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// LoadWithUserEntries loads the built in catalog and merges the user's own.
//
// A missing directory is not an error. Most machines will never have one.
//
// User entries are additive only, and there is no mechanism to remove or
// narrow a built in category. That is not an oversight: a built in entry only
// proposes candidates, and every one of them still passes the protection rules
// and the structural floor before anything happens to it. Something a person
// wants left alone belongs in a protection rule, which is the layer that
// answers that question, rather than in a subtraction from a list of
// suggestions.
func LoadWithUserEntries(userCatalogDir string) (*Catalog, error) {
	builtin, err := LoadBuiltin()
	if err != nil {
		return nil, err
	}

	userEntries, err := loadUserEntries(userCatalogDir, builtin)
	if err != nil {
		return nil, err
	}
	if len(userEntries) == 0 {
		return builtin, nil
	}
	return &Catalog{entries: append(builtin.Entries(), userEntries...)}, nil
}

func loadUserEntries(dir string, builtin *Catalog) ([]Entry, error) {
	files, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("cannot read %s: %w", dir, err)
	}

	reserved := make(map[string]bool)
	for _, entry := range builtin.Entries() {
		reserved[entry.ID] = true
	}

	var all []Entry
	seen := make(map[string]string)

	for _, file := range files {
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".yaml") {
			continue
		}
		path := filepath.Join(dir, file.Name())
		contents, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil, fmt.Errorf("cannot read %s: %w", path, readErr)
		}

		var document entryDocument
		if err := yaml.Unmarshal(contents, &document); err != nil {
			return nil, fmt.Errorf("cannot parse %s: %w", path, err)
		}
		if document.Version != catalogSchemaVersion {
			return nil, fmt.Errorf("%w: %s declares version %d, this build understands %d",
				ErrUnsupportedVersion, path, document.Version, catalogSchemaVersion)
		}

		for _, entry := range document.Entries {
			entry.origin = file.Name()
			entry.userSupplied = true

			if err := validate(&entry); err != nil {
				return nil, fmt.Errorf("%s: %w", path, err)
			}
			if reserved[entry.ID] {
				return nil, fmt.Errorf("%w: %q is the identifier of a built in entry",
					ErrDuplicateEntry, entry.ID)
			}
			if previous, duplicate := seen[entry.ID]; duplicate {
				return nil, fmt.Errorf("%w: %q appears in both %s and %s",
					ErrDuplicateEntry, entry.ID, previous, file.Name())
			}
			seen[entry.ID] = file.Name()

			// Permanent removal is not delegated. A built in purgeable entry
			// carries a justification argued in the repository and reviewable
			// by anyone; a local file marking something purgeable would be an
			// irreversible deletion authorised by a line nobody else ever
			// reads. A user entry can propose a category, and clean will stage
			// it, which is reversible.
			if entry.Purgeable {
				return nil, fmt.Errorf("%w: %s cannot mark itself purgeable, because "+
					"a user entry may not authorise irreversible removal; clean will "+
					"stage it reversibly instead", ErrInvalidEntry, entry.ID)
			}
			all = append(all, entry)
		}
	}
	return all, nil
}
