package cleancatalog

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	deletionengine "github.com/lesliemunmus/wtff/internal/deletion-engine"
)

// Skip records an entry or a container child that discovery did not turn
// into a candidate, and why.
//
// Absence is not a failure. Most categories will not apply to most machines:
// a container that does not exist yet, or does not exist at all, is normal
// and is recorded here rather than reported as an error, so a caller can
// choose whether to show it.
type Skip struct {
	EntryID string
	Path    string
	Reason  string

	// CategoryAbsent is true when the skip means "this category's root does
	// not exist on this machine at all," as opposed to "an item inside an
	// existing category was excluded." A caller reporting results to a person
	// typically wants the second kind and not the first: most categories will
	// not apply to most machines, and that is the ordinary case, not
	// something to explain.
	CategoryAbsent bool
}

// Discover turns catalog entries into deletion engine candidates by looking
// at what actually exists on disk.
//
// This function only reads the filesystem; it never validates a path the way
// internal/path-validation does, and it never removes anything. Every
// candidate it returns still passes through the deletion engine's own
// validation and protection rule check before anything happens to it. That
// is deliberate: discovery decides what to suggest, the engine decides what
// is safe, and those stay two different pieces of code so a mistake in one
// cannot silently widen the other.
func Discover(entries []Entry, home string) ([]deletionengine.Candidate, []Skip) {
	var candidates []deletionengine.Candidate
	var skips []Skip

	for _, entry := range entries {
		path := expandHome(entry.Path, home)

		switch entry.Kind {
		case KindOpaque:
			c, skip := discoverOpaque(entry, path)
			if skip != nil {
				skips = append(skips, *skip)
				continue
			}
			candidates = append(candidates, c)

		case KindContainer:
			found, containerSkips := discoverContainer(entry, path)
			candidates = append(candidates, found...)
			skips = append(skips, containerSkips...)

		case KindVolumeTrash:
			// Ignores the expanded path: this kind's path names where mount
			// points appear, and what it covers depends on what is attached.
			found, volumeSkips := discoverVolumeTrash(entry)
			candidates = append(candidates, found...)
			skips = append(skips, volumeSkips...)
		}
	}

	return candidates, skips
}

func discoverOpaque(entry Entry, path string) (deletionengine.Candidate, *Skip) {
	if _, err := os.Lstat(path); err != nil {
		return deletionengine.Candidate{}, &Skip{
			EntryID: entry.ID, Path: path, Reason: "not present on this machine",
			CategoryAbsent: true,
		}
	}
	return deletionengine.Candidate{
		Path:   path,
		RuleID: entry.ID,
		Reason: entry.Reason,
	}, nil
}

func discoverContainer(entry Entry, path string) ([]deletionengine.Candidate, []Skip) {
	items, err := os.ReadDir(path)
	if err != nil {
		return nil, []Skip{{
			EntryID: entry.ID, Path: path, Reason: "not present or not readable on this machine",
			CategoryAbsent: true,
		}}
	}

	var candidates []deletionengine.Candidate
	var skips []Skip

	for _, item := range items {
		name := item.Name()
		if excluded, prefix := hasExcludedPrefix(name, entry.ExcludePrefixes); excluded {
			skips = append(skips, Skip{
				EntryID: entry.ID,
				Path:    filepath.Join(path, name),
				Reason:  fmt.Sprintf("name starts with excluded prefix %q", prefix),
			})
			continue
		}
		candidates = append(candidates, deletionengine.Candidate{
			Path:   filepath.Join(path, name),
			RuleID: entry.ID,
			Reason: entry.Reason,
		})
	}
	return candidates, skips
}

// hasExcludedPrefix reports whether name starts with one of the given
// prefixes, compared case-insensitively, since Apple filesystems are
// normally case insensitive and an exclusion meant to catch com.apple.* must
// not miss a differently cased entry.
func hasExcludedPrefix(name string, prefixes []string) (bool, string) {
	lowerName := strings.ToLower(name)
	for _, prefix := range prefixes {
		if strings.HasPrefix(lowerName, strings.ToLower(prefix)) {
			return true, prefix
		}
	}
	return false, ""
}

// expandHome expands a leading ~ against a supplied home directory rather
// than os.UserHomeDir, so discovery can be pointed at a directory other than
// the invoking user's own during a test, without an environment variable.
func expandHome(path, home string) string {
	if path == "~" {
		return home
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(home, path[2:])
	}
	return path
}
