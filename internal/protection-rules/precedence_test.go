package protectionrules

import (
	"strings"
	"testing"
	"testing/fstest"
)

// These cases come from a review pass over the matcher rather than from the
// original design. Precedence is the part of a rule system that is easy to get
// subtly wrong and hard to notice, because a wrong answer still looks like an
// answer.

// Evaluate takes the first critical protection it encounters, which is correct
// only because rules are sorted by specificity when they load. That coupling is
// invisible at the point it matters, so it is pinned here: if the sort is
// removed or changed, this fails rather than quietly reporting the wrong rule.
func TestMostSpecificCriticalRuleIsTheOneReported(t *testing.T) {
	contents := `version: 1
rules:
  - id: broad-critical
    match: {type: prefix, path: "~/Vault"}
    effect: protect
    severity: critical
    category: testing
    reason: "The broad protection covering the whole vault directory."
    provenance: {source: "test", method: documentation, verified: "2026-08-05"}
  - id: narrow-critical
    match: {type: prefix, path: "~/Vault/keys"}
    effect: protect
    severity: critical
    category: testing
    reason: "The narrow protection covering the key material specifically."
    provenance: {source: "test", method: documentation, verified: "2026-08-05"}
`
	set, err := loadFromFS(
		fstest.MapFS{"rules/case.yaml": &fstest.MapFile{Data: []byte(contents)}},
		"rules", testHome)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	decision := set.Evaluate(testHome + "/Vault/keys/signing.pem")
	if !decision.Protected {
		t.Fatal("expected protection")
	}
	if decision.RuleID != "narrow-critical" {
		t.Fatalf("reported %s, expected the more specific critical rule", decision.RuleID)
	}
}

// A carve out that loses only because the protection it overlaps is critical
// must be named. Someone wrote that carve out expecting it to apply, and a
// silent override is the hardest kind of rule mistake to track down.
func TestCarveOutOverriddenByCriticalRuleIsNamed(t *testing.T) {
	contents := `version: 1
rules:
  - id: critical-protection
    match: {type: prefix, path: "~/Vault"}
    effect: protect
    severity: critical
    category: testing
    reason: "Critical protection over the entire vault directory."
    provenance: {source: "test", method: documentation, verified: "2026-08-05"}
  - id: hopeful-carve-out
    match: {type: prefix, path: "~/Vault/scratch"}
    effect: allow
    severity: standard
    category: testing
    reason: "A more specific carve out that expects to permit removal here."
    provenance: {source: "test", method: documentation, verified: "2026-08-05"}
`
	set, err := loadFromFS(
		fstest.MapFS{"rules/case.yaml": &fstest.MapFile{Data: []byte(contents)}},
		"rules", testHome)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	decision := set.Evaluate(testHome + "/Vault/scratch/temp.bin")
	if !decision.Protected {
		t.Fatal("a carve out overrode a critical protection")
	}
	if decision.Overridden != "hopeful-carve-out" {
		t.Fatalf("overridden carve out was reported as %q, expected it to be named",
			decision.Overridden)
	}
}

// Specificity is computed from the declared pattern rather than the expanded
// one, so that rule precedence does not change with the length of a particular
// machine's home directory.
//
// The consequence, documented here rather than left to be discovered: an
// absolute pattern written out in full outranks an equivalent home relative one,
// because the home relative form counts the tilde as a single character. Rules
// in this project are written home relative for exactly this reason, and this
// case exists so the behavior is visible if that convention is ever broken.
func TestSpecificityUsesTheDeclaredPattern(t *testing.T) {
	contents := `version: 1
rules:
  - id: home-relative-allow
    match: {type: prefix, path: "~/Shared/data"}
    effect: allow
    severity: standard
    category: testing
    reason: "Home relative carve out over the shared data directory."
    provenance: {source: "test", method: documentation, verified: "2026-08-05"}
  - id: absolute-protect
    match: {type: prefix, path: "/Users/example/Shared"}
    effect: protect
    severity: standard
    category: testing
    reason: "Absolute protection written out in full for the same tree."
    provenance: {source: "test", method: documentation, verified: "2026-08-05"}
`
	set, err := loadFromFS(
		fstest.MapFS{"rules/case.yaml": &fstest.MapFile{Data: []byte(contents)}},
		"rules", testHome)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	// The absolute pattern is longer as written, so it wins, even though the
	// home relative rule names a deeper path once expanded.
	decision := set.Evaluate(testHome + "/Shared/data/file.bin")
	if !decision.Protected || decision.RuleID != "absolute-protect" {
		t.Fatalf("precedence resolved to %q protected=%v, expected the absolute rule to win",
			decision.RuleID, decision.Protected)
	}
}

// A glob covers the directory it names and its contents, but must not leak
// upward into the directory's parents.
func TestGlobDoesNotProtectAncestorsOfItsMatch(t *testing.T) {
	set := loadForTest(t)

	container := testHome + "/Library/Containers/com.example.editor"
	if decision := set.Evaluate(container); decision.Protected {
		t.Fatalf("the container itself was protected by %s, which the Documents glob should not reach",
			decision.RuleID)
	}
	data := testHome + "/Library/Containers/com.example.editor/Data"
	if decision := set.Evaluate(data); decision.Protected {
		t.Fatalf("the container Data directory was protected by %s", decision.RuleID)
	}
}

// A glob wildcard must not cross a path separator, or a pattern naming one
// level would silently cover an arbitrarily deep tree.
func TestGlobWildcardDoesNotSpanSeparators(t *testing.T) {
	contents := `version: 1
rules:
  - id: single-level-glob
    match: {type: glob, path: "~/Apps/*/config"}
    effect: protect
    severity: standard
    category: testing
    reason: "Protects a config directory one level inside each application."
    provenance: {source: "test", method: documentation, verified: "2026-08-05"}
`
	set, err := loadFromFS(
		fstest.MapFS{"rules/case.yaml": &fstest.MapFile{Data: []byte(contents)}},
		"rules", testHome)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if !set.Evaluate(testHome + "/Apps/one/config").Protected {
		t.Fatal("the intended single level match failed")
	}
	if set.Evaluate(testHome + "/Apps/one/two/config").Protected {
		t.Fatal("the wildcard spanned a separator and matched a deeper path")
	}
}

// The shipped set must not protect so much that ordinary work becomes
// impossible. This walks a realistic set of reclaimable targets and fails if the
// rules have crept broad enough to cover them.
func TestShippedRulesDoNotOverreach(t *testing.T) {
	set := loadForTest(t)

	reclaimable := []string{
		testHome + "/Library/Caches",
		testHome + "/Library/Caches/com.apple.Safari/WebKitCache",
		testHome + "/Library/Application Support/Code/Cache",
		testHome + "/Library/Developer/Xcode/DerivedData",
		testHome + "/.npm/_cacache",
		testHome + "/.cache/pip",
		testHome + "/Downloads/installer.dmg",
	}

	for _, path := range reclaimable {
		if decision := set.Evaluate(path); decision.Protected {
			t.Errorf("%s is protected by %s, which would make ordinary cleanup impossible",
				path, decision.RuleID)
		}
	}
}

// Every shipped rule must actually be reachable. A rule whose pattern can never
// match anything is dead weight that reads as protection.
func TestEveryShippedRuleMatchesSomething(t *testing.T) {
	set := loadForTest(t)

	for _, rule := range set.Rules() {
		t.Run(rule.ID, func(t *testing.T) {
			pattern := rule.Patterns()[0]
			var probe string
			switch rule.Match.Type {
			case MatchExact:
				probe = pattern
			case MatchPrefix:
				probe = pattern + "/probe-entry"
			case MatchGlob:
				probe = strings.ReplaceAll(pattern, "*", "probe-component")
			}
			if !rule.matches(probe) {
				t.Fatalf("rule does not match a probe built from its own pattern: %s", probe)
			}
		})
	}
}
