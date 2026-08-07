package cleancatalog

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeUserCatalog(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	document := "version: 1\nentries:\n" + body
	if err := os.WriteFile(filepath.Join(dir, name), []byte(document), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
}

const userEntry = `
  - id: my-build-cache
    category: local
    kind: container
    path: "~/.myproject/build-cache"
    reason: "A build cache this project regenerates on the next compile."
    provenance:
      source: "The person who owns this machine said so."
      method: system-inspection
      verified: "2026-08-06"
`

func TestUserEntryJoinsTheCatalog(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "catalog")
	writeUserCatalog(t, dir, "mine.yaml", userEntry)

	catalog, err := LoadWithUserEntries(dir)
	if err != nil {
		t.Fatalf("loading: %v", err)
	}

	var found bool
	for _, entry := range catalog.Entries() {
		if entry.ID == "my-build-cache" {
			found = true
			if !entry.UserSupplied() {
				t.Error("a user entry should be marked as one")
			}
		}
	}
	if !found {
		t.Fatal("the user entry did not join the catalog")
	}
}

// A built in purgeable entry carries a justification argued in the repository
// and reviewable by anyone. A local file marking something purgeable would be
// an irreversible deletion authorised by a line nobody else ever reads.
func TestUserEntryCannotAuthoriseIrreversibleRemoval(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "catalog")
	writeUserCatalog(t, dir, "reckless.yaml", `
  - id: delete-it-forever
    category: local
    kind: container
    path: "~/.myproject/scratch"
    reason: "Scratch space this project rebuilds whenever it is missing."
    purgeable: true
    purge_reason: "I am certain about this and want it gone permanently."
    provenance:
      source: "The person who owns this machine said so."
      method: system-inspection
      verified: "2026-08-06"
`)

	_, err := LoadWithUserEntries(dir)
	if !errors.Is(err, ErrInvalidEntry) {
		t.Fatalf("a user entry marking itself purgeable should be refused, got %v", err)
	}
	if !strings.Contains(err.Error(), "stage it reversibly") {
		t.Errorf("the refusal should say what happens instead, got %v", err)
	}
}

func TestUserEntryCannotReuseABuiltinIdentifier(t *testing.T) {
	builtin, err := LoadBuiltin()
	if err != nil {
		t.Fatalf("loading builtin: %v", err)
	}
	existing := builtin.Entries()[0].ID

	dir := filepath.Join(t.TempDir(), "catalog")
	writeUserCatalog(t, dir, "clash.yaml", `
  - id: `+existing+`
    category: local
    kind: container
    path: "~/.myproject/cache"
    reason: "Reusing an identifier that already belongs to a built in entry."
    provenance:
      source: "The person who owns this machine said so."
      method: system-inspection
      verified: "2026-08-06"
`)

	if _, err := LoadWithUserEntries(dir); !errors.Is(err, ErrDuplicateEntry) {
		t.Fatalf("expected a duplicate identifier refusal, got %v", err)
	}
}

func TestUserEntriesMustSatisfyTheSameSchema(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "catalog")
	writeUserCatalog(t, dir, "sloppy.yaml", `
  - id: no-provenance
    category: local
    kind: container
    path: "~/.myproject/cache"
    reason: "This entry has no provenance and should not load."
`)

	if _, err := LoadWithUserEntries(dir); err == nil {
		t.Fatal("an entry without provenance should not load")
	}
}

func TestBuiltinCatalogSurvivesTheMerge(t *testing.T) {
	builtin, err := LoadBuiltin()
	if err != nil {
		t.Fatalf("loading builtin: %v", err)
	}

	dir := filepath.Join(t.TempDir(), "catalog")
	writeUserCatalog(t, dir, "mine.yaml", userEntry)

	merged, err := LoadWithUserEntries(dir)
	if err != nil {
		t.Fatalf("loading: %v", err)
	}
	if merged.Len() != builtin.Len()+1 {
		t.Fatalf("merged catalog has %d entries, want %d plus one",
			merged.Len(), builtin.Len())
	}
}

func TestMissingUserCatalogIsNotAnError(t *testing.T) {
	catalog, err := LoadWithUserEntries(filepath.Join(t.TempDir(), "nothing"))
	if err != nil {
		t.Fatalf("an absent catalog directory should be fine, got %v", err)
	}
	if catalog.Len() == 0 {
		t.Fatal("the built in catalog should still be loaded")
	}
}

// The purgeable set is the blast radius of permanent removal, and it must not
// grow because someone dropped a file in a directory.
func TestUserEntriesNeverEnterThePurgeableSet(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "catalog")
	writeUserCatalog(t, dir, "mine.yaml", userEntry)

	catalog, err := LoadWithUserEntries(dir)
	if err != nil {
		t.Fatalf("loading: %v", err)
	}
	for _, entry := range PurgeableEntries(catalog.Entries()) {
		if entry.UserSupplied() {
			t.Errorf("user entry %s reached the purgeable set", entry.ID)
		}
	}
}
