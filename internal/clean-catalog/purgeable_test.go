package cleancatalog

import (
	"strings"
	"testing"
	"testing/fstest"
)

func loadOne(t *testing.T, contents string) (*Catalog, error) {
	t.Helper()
	fileSystem := fstest.MapFS{"catalog/sample.yaml": &fstest.MapFile{Data: []byte(contents)}}
	return loadFromFS(fileSystem, "catalog")
}

const purgeableTemplate = `version: 1
entries:
  - id: sample-entry
    category: sample
    kind: container
    path: "~/.Sample"
    reason: "A sufficiently long justification for removing this directory."
%s
    provenance:
      source: "Confirmed by inspection."
      method: system-inspection
      verified: "2026-08-05"
`

// Marking an entry purgeable authorizes irreversible removal, so the loader
// refuses one that does not say why. Without this the list could grow by
// someone flipping a boolean in a file nobody reviews closely.
func TestPurgeableEntryMustJustifyItself(t *testing.T) {
	if _, err := loadOne(t, strings.Replace(purgeableTemplate, "%s",
		"    purgeable: true", 1)); err == nil {
		t.Fatal("a purgeable entry with no purge_reason should be refused")
	}

	if _, err := loadOne(t, strings.Replace(purgeableTemplate, "%s",
		"    purgeable: true\n    purge_reason: \"short\"", 1)); err == nil {
		t.Fatal("a purgeable entry with a label instead of a reason should be refused")
	}

	if _, err := loadOne(t, strings.Replace(purgeableTemplate, "%s",
		"    purgeable: true\n    purge_reason: \"The user already discarded these items deliberately.\"",
		1)); err != nil {
		t.Fatalf("a justified purgeable entry should load: %v", err)
	}
}

// A purge_reason on an entry that is not purgeable means someone intended one
// thing and wrote another. Refusing beats loading it with the reason ignored.
func TestPurgeReasonWithoutPurgeableIsRefused(t *testing.T) {
	if _, err := loadOne(t, strings.Replace(purgeableTemplate, "%s",
		"    purge_reason: \"The user already discarded these items deliberately.\"",
		1)); err == nil {
		t.Fatal("a purge_reason without purgeable should be refused")
	}
}

func TestOrdinaryEntryLoadsWithoutPurgeFields(t *testing.T) {
	catalog, err := loadOne(t, strings.Replace(purgeableTemplate, "%s", "", 1))
	if err != nil {
		t.Fatalf("loading: %v", err)
	}
	if catalog.Entries()[0].Purgeable {
		t.Fatal("an entry should not be purgeable unless it says so")
	}
}

// The shipped catalog's purgeable set is the blast radius of "wtff purge", so
// it is pinned here rather than left to drift. Adding to it should require
// changing this test on purpose.
func TestOnlyTrashIsPurgeableInTheShippedCatalog(t *testing.T) {
	catalog, err := LoadBuiltin()
	if err != nil {
		t.Fatalf("loading builtin catalog: %v", err)
	}

	purgeable := PurgeableEntries(catalog.Entries())
	if len(purgeable) != 1 {
		var names []string
		for _, entry := range purgeable {
			names = append(names, entry.ID)
		}
		t.Fatalf("expected exactly one purgeable entry, got %d: %v", len(purgeable), names)
	}
	if purgeable[0].ID != "trash-contents" {
		t.Fatalf("purgeable entry is %q, want trash-contents", purgeable[0].ID)
	}
}

// Caches are regenerable, which is not the same as disposable without a way
// back. They belong to clean, which stages, and this pins that boundary.
func TestCachesAreNotPurgeable(t *testing.T) {
	catalog, err := LoadBuiltin()
	if err != nil {
		t.Fatalf("loading builtin catalog: %v", err)
	}
	for _, entry := range catalog.Entries() {
		if entry.Category == "trash" {
			continue
		}
		if entry.Purgeable {
			t.Errorf("%s is a cache category and must not be purgeable", entry.ID)
		}
	}
}

func TestPurgeableEntriesFiltersRatherThanReordering(t *testing.T) {
	entries := []Entry{
		{ID: "a"},
		{ID: "b", Purgeable: true},
		{ID: "c"},
		{ID: "d", Purgeable: true},
	}
	got := PurgeableEntries(entries)
	if len(got) != 2 || got[0].ID != "b" || got[1].ID != "d" {
		t.Fatalf("filter returned %+v", got)
	}
}
