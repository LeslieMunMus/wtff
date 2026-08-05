package cleancatalog

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const catalogSchemaVersion = 1

//go:embed catalog/*.yaml
var builtinCatalog embed.FS

var identifierPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

var (
	ErrInvalidEntry       = errors.New("invalid catalog entry")
	ErrDuplicateEntry     = errors.New("duplicate catalog entry identifier")
	ErrUnsupportedVersion = errors.New("unsupported catalog file version")
)

// Catalog is a loaded, validated set of entries.
type Catalog struct {
	entries []Entry
}

// Entries reports the loaded entries.
func (c *Catalog) Entries() []Entry { return append([]Entry(nil), c.entries...) }

// Len reports how many entries are loaded.
func (c *Catalog) Len() int { return len(c.entries) }

// LoadBuiltin loads the catalog compiled into the binary.
//
// Like the protection rules, this ships inside the executable so a single
// downloaded binary is complete, rather than depending on a file that has to
// be installed alongside it and can go missing.
func LoadBuiltin() (*Catalog, error) {
	return loadFromFS(builtinCatalog, "catalog")
}

func loadFromFS(fileSystem fs.FS, dir string) (*Catalog, error) {
	files, err := fs.ReadDir(fileSystem, dir)
	if err != nil {
		return nil, fmt.Errorf("cannot read catalog directory: %w", err)
	}

	var all []Entry
	seen := make(map[string]string)

	for _, file := range files {
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".yaml") {
			continue
		}
		contents, readErr := fs.ReadFile(fileSystem, filepath.Join(dir, file.Name()))
		if readErr != nil {
			return nil, fmt.Errorf("cannot read %s: %w", file.Name(), readErr)
		}

		var document entryDocument
		if err := yaml.Unmarshal(contents, &document); err != nil {
			return nil, fmt.Errorf("cannot parse %s: %w", file.Name(), err)
		}
		if document.Version != catalogSchemaVersion {
			return nil, fmt.Errorf("%w: %s declares version %d, this build understands %d",
				ErrUnsupportedVersion, file.Name(), document.Version, catalogSchemaVersion)
		}

		for _, entry := range document.Entries {
			entry.origin = file.Name()
			if err := validate(&entry); err != nil {
				return nil, fmt.Errorf("%s: %w", file.Name(), err)
			}
			if previous, duplicate := seen[entry.ID]; duplicate {
				return nil, fmt.Errorf("%w: %q appears in both %s and %s",
					ErrDuplicateEntry, entry.ID, previous, file.Name())
			}
			seen[entry.ID] = file.Name()
			all = append(all, entry)
		}
	}

	if len(all) == 0 {
		return nil, fmt.Errorf("%w: no catalog entries were loaded", ErrInvalidEntry)
	}
	return &Catalog{entries: all}, nil
}

func validate(entry *Entry) error {
	if entry.ID == "" {
		return fmt.Errorf("%w: an entry has no identifier", ErrInvalidEntry)
	}
	if !identifierPattern.MatchString(entry.ID) {
		return fmt.Errorf("%w: identifier %q must be lowercase words joined by single hyphens",
			ErrInvalidEntry, entry.ID)
	}
	switch entry.Kind {
	case KindContainer, KindOpaque:
	default:
		return fmt.Errorf("%w: %s declares unknown kind %q", ErrInvalidEntry, entry.ID, entry.Kind)
	}
	if entry.Path == "" {
		return fmt.Errorf("%w: %s has no path", ErrInvalidEntry, entry.ID)
	}
	if !strings.HasPrefix(entry.Path, "/") && !strings.HasPrefix(entry.Path, "~/") {
		return fmt.Errorf("%w: %s path %q must be absolute or start at the home directory",
			ErrInvalidEntry, entry.ID, entry.Path)
	}
	if strings.Contains(entry.Path, "/../") || strings.HasSuffix(entry.Path, "/..") {
		return fmt.Errorf("%w: %s path contains a parent reference", ErrInvalidEntry, entry.ID)
	}
	if entry.Category == "" {
		return fmt.Errorf("%w: %s has no category", ErrInvalidEntry, entry.ID)
	}
	if len(strings.TrimSpace(entry.Reason)) < 20 {
		return fmt.Errorf("%w: %s needs a reason that explains the justification, not a label",
			ErrInvalidEntry, entry.ID)
	}
	if strings.TrimSpace(entry.Provenance.Source) == "" {
		return fmt.Errorf("%w: %s has no provenance source", ErrInvalidEntry, entry.ID)
	}
	switch entry.Provenance.Method {
	case "documentation", "vendor-documentation", "system-inspection":
	default:
		return fmt.Errorf("%w: %s declares unknown provenance method %q",
			ErrInvalidEntry, entry.ID, entry.Provenance.Method)
	}
	if _, err := time.Parse("2006-01-02", entry.Provenance.Verified); err != nil {
		return fmt.Errorf("%w: %s has an unreadable verification date %q",
			ErrInvalidEntry, entry.ID, entry.Provenance.Verified)
	}
	return nil
}
