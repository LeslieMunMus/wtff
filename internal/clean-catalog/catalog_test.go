package cleancatalog

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

func TestBuiltinCatalogLoads(t *testing.T) {
	catalog, err := LoadBuiltin()
	if err != nil {
		t.Fatalf("LoadBuiltin: %v", err)
	}
	if catalog.Len() < 5 {
		t.Fatalf("loaded %d entries, expected the full built in set", catalog.Len())
	}
}

func TestEveryShippedEntryIsAuditable(t *testing.T) {
	catalog, err := LoadBuiltin()
	if err != nil {
		t.Fatalf("LoadBuiltin: %v", err)
	}
	for _, entry := range catalog.Entries() {
		t.Run(entry.ID, func(t *testing.T) {
			if len(strings.TrimSpace(entry.Reason)) < 20 {
				t.Error("reason does not explain the justification")
			}
			if strings.TrimSpace(entry.Provenance.Source) == "" {
				t.Error("no provenance source")
			}
			if entry.Category == "" {
				t.Error("no category")
			}
		})
	}
}

func TestLoaderRefusesUnusableEntries(t *testing.T) {
	valid := `version: 1
entries:
  - id: sample-entry
    category: testing
    kind: container
    path: "~/Sample"
    reason: "A sufficiently descriptive reason for this catalog entry."
    provenance: {source: "test", method: documentation, verified: "2026-08-05"}
`
	cases := map[string]string{
		"wrong version":      strings.Replace(valid, "version: 1", "version: 99", 1),
		"missing identifier": strings.Replace(valid, "id: sample-entry", `id: ""`, 1),
		"bad identifier":     strings.Replace(valid, "id: sample-entry", "id: Sample_Entry", 1),
		"unknown kind":       strings.Replace(valid, "kind: container", "kind: recursive", 1),
		"relative path":      strings.Replace(valid, `path: "~/Sample"`, `path: "Sample"`, 1),
		"parent reference":   strings.Replace(valid, `path: "~/Sample"`, `path: "~/Sample/../x"`, 1),
		"placeholder reason": strings.Replace(valid, `reason: "A sufficiently descriptive reason for this catalog entry."`, `reason: "todo"`, 1),
		"missing provenance": strings.Replace(valid, `source: "test"`, `source: ""`, 1),
		"unknown method":     strings.Replace(valid, "method: documentation", "method: guessed", 1),
		"bad date":           strings.Replace(valid, `verified: "2026-08-05"`, `verified: "soon"`, 1),
	}

	for name, contents := range cases {
		t.Run(name, func(t *testing.T) {
			fileSystem := fstest.MapFS{"catalog/sample.yaml": &fstest.MapFile{Data: []byte(contents)}}
			if _, err := loadFromFS(fileSystem, "catalog"); err == nil {
				t.Fatalf("loader accepted a catalog entry with: %s", name)
			}
		})
	}

	fileSystem := fstest.MapFS{"catalog/sample.yaml": &fstest.MapFile{Data: []byte(valid)}}
	if _, err := loadFromFS(fileSystem, "catalog"); err != nil {
		t.Fatalf("the control case failed to load: %v", err)
	}
}

func TestLoaderRefusesDuplicateIdentifiers(t *testing.T) {
	entry := `version: 1
entries:
  - id: shared-identifier
    category: testing
    kind: container
    path: "~/One"
    reason: "A sufficiently descriptive reason for this entry."
    provenance: {source: "test", method: documentation, verified: "2026-08-05"}
`
	fileSystem := fstest.MapFS{
		"catalog/first.yaml":  &fstest.MapFile{Data: []byte(entry)},
		"catalog/second.yaml": &fstest.MapFile{Data: []byte(entry)},
	}
	_, err := loadFromFS(fileSystem, "catalog")
	if !errors.Is(err, ErrDuplicateEntry) {
		t.Fatalf("loading duplicate identifiers = %v, want ErrDuplicateEntry", err)
	}
}

func writeTree(t *testing.T, root string, paths ...string) {
	t.Helper()
	for _, p := range paths {
		full := filepath.Join(root, p)
		if strings.HasSuffix(p, "/") {
			if err := os.MkdirAll(full, 0o755); err != nil {
				t.Fatalf("setup: %v", err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("setup: %v", err)
		}
		if err := os.WriteFile(full, []byte("x"), 0o600); err != nil {
			t.Fatalf("setup: %v", err)
		}
	}
}

func TestContainerDiscoveryEnumeratesChildrenAndExcludesPrefixed(t *testing.T) {
	home := t.TempDir()
	writeTree(t, home,
		"Library/Caches/ThirdPartyApp/blob.bin",
		"Library/Caches/AnotherApp/blob.bin",
		"Library/Caches/com.apple.something/state.plist",
		"Library/Caches/COM.APPLE.DIFFERENTCASE/state.plist",
	)

	entries := []Entry{{
		ID: "test-caches", Kind: KindContainer, Path: "~/Library/Caches",
		ExcludePrefixes: []string{"com.apple."},
		Reason:          "test",
	}}

	candidates, skips := Discover(entries, home)
	if len(candidates) != 2 {
		var got []string
		for _, c := range candidates {
			got = append(got, c.Path)
		}
		t.Fatalf("got %d candidates, want 2 (third party only): %v", len(candidates), got)
	}
	for _, c := range candidates {
		if strings.Contains(strings.ToLower(c.Path), "com.apple") {
			t.Fatalf("an Apple namespaced path was proposed as a candidate: %s", c.Path)
		}
	}

	excludedCount := 0
	for _, s := range skips {
		if strings.Contains(s.Reason, "excluded prefix") {
			excludedCount++
		}
	}
	if excludedCount != 2 {
		t.Fatalf("expected 2 exclusion skips (both casings caught case-insensitively), got %d", excludedCount)
	}
}

// Every candidate must carry a rule id and reason, since the deletion engine
// refuses an unexplained candidate. This is the contract point between
// discovery and the engine, and it is worth asserting directly rather than
// trusting that every code path sets both fields.
func TestDiscoveredCandidatesCarryRuleIDAndReason(t *testing.T) {
	home := t.TempDir()
	writeTree(t, home, "Library/Caches/SomeApp/blob.bin")

	entries := []Entry{{
		ID: "test-caches", Kind: KindContainer, Path: "~/Library/Caches",
		Reason: "a real reason for this test entry",
	}}
	candidates, _ := Discover(entries, home)
	if len(candidates) != 1 {
		t.Fatalf("got %d candidates, want 1", len(candidates))
	}
	if candidates[0].RuleID != "test-caches" || candidates[0].Reason == "" {
		t.Fatalf("candidate missing rule id or reason: %+v", candidates[0])
	}
}

func TestOpaqueDiscoveryProposesTheDirectoryAsOneUnit(t *testing.T) {
	home := t.TempDir()
	writeTree(t, home, ".npm/_cacache/content-v2/entry-one", ".npm/_cacache/index-v5/entry-two")

	entries := []Entry{{
		ID: "test-opaque", Kind: KindOpaque, Path: "~/.npm/_cacache", Reason: "test",
	}}
	candidates, skips := Discover(entries, home)
	if len(skips) != 0 {
		t.Fatalf("unexpected skips: %+v", skips)
	}
	if len(candidates) != 1 {
		t.Fatalf("got %d candidates, want exactly 1 (the whole directory as one unit)", len(candidates))
	}
	if !strings.HasSuffix(candidates[0].Path, "_cacache") {
		t.Fatalf("candidate path = %s, want the container itself", candidates[0].Path)
	}
}

// A category that does not exist on this machine is not an error. Most
// categories will not apply to most machines, and reporting that as a failure
// would make discovery unusable on any machine missing even one tool.
func TestMissingCategoryIsSkippedNotFailed(t *testing.T) {
	home := t.TempDir()
	entries := []Entry{
		{ID: "absent-container", Kind: KindContainer, Path: "~/DoesNotExist", Reason: "test"},
		{ID: "absent-opaque", Kind: KindOpaque, Path: "~/AlsoAbsent", Reason: "test"},
	}
	candidates, skips := Discover(entries, home)
	if len(candidates) != 0 {
		t.Fatalf("got %d candidates for nonexistent categories, want 0", len(candidates))
	}
	if len(skips) != 2 {
		t.Fatalf("got %d skips, want 2", len(skips))
	}
}

// CategoryAbsent is the field a caller uses to tell "this category does not
// exist on this machine" apart from "an item inside an existing category was
// excluded." A caller in internal/cli filtered on the wrong signal at first,
// checking whether the path was empty, which it never is, so the filter did
// nothing. This pins the field itself so that mistake cannot recur unnoticed.
func TestCategoryAbsentDistinguishesMissingFromExcluded(t *testing.T) {
	home := t.TempDir()
	writeTree(t, home, "Library/Caches/com.apple.something/state.plist")

	entries := []Entry{
		{ID: "present-container", Kind: KindContainer, Path: "~/Library/Caches",
			ExcludePrefixes: []string{"com.apple."}, Reason: "test"},
		{ID: "absent-container", Kind: KindContainer, Path: "~/DoesNotExist", Reason: "test"},
		{ID: "absent-opaque", Kind: KindOpaque, Path: "~/AlsoAbsent", Reason: "test"},
	}
	_, skips := Discover(entries, home)

	var absentCount, excludedCount int
	for _, s := range skips {
		if s.CategoryAbsent {
			absentCount++
			if s.Path == "" {
				t.Error("an absent-category skip should still carry the path that was checked")
			}
		} else {
			excludedCount++
		}
	}
	if absentCount != 2 {
		t.Fatalf("got %d absent-category skips, want 2", absentCount)
	}
	if excludedCount != 1 {
		t.Fatalf("got %d excluded-item skips, want 1", excludedCount)
	}
}

func TestExpandHomeHandlesTildeForms(t *testing.T) {
	if got := expandHome("~/Library/Caches", "/Users/example"); got != "/Users/example/Library/Caches" {
		t.Fatalf("expandHome = %q", got)
	}
	if got := expandHome("~", "/Users/example"); got != "/Users/example" {
		t.Fatalf("expandHome(~) = %q", got)
	}
	if got := expandHome("/already/absolute", "/Users/example"); got != "/already/absolute" {
		t.Fatalf("expandHome should not alter an absolute path, got %q", got)
	}
}
