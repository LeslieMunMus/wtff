package protectionrules

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeUserRules(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	document := "version: 1\nrules:\n" + body
	if err := os.WriteFile(filepath.Join(dir, name), []byte(document), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
}

// A user rule that adds protection is the ordinary case and must simply work.
//
// The path deliberately sits outside everything the built in set already
// covers. An earlier version used ~/Documents/thesis and failed, because
// ~/Documents is already protected by a built in rule: the path was protected,
// just not by the rule the test named. Asserting on the responsible rule only
// means anything where no other rule reaches.
func TestUserRuleCanAddProtection(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "config", "rules")
	writeUserRules(t, dir, "mine.yaml", `
  - id: my-thesis
    match:
      type: prefix
      path: "~/workshop-notes"
    effect: protect
    severity: standard
    category: personal
    reason: "Years of work that no cleanup tool has any business touching."
    provenance:
      source: "The person who owns this machine said so."
      method: system-inspection
      verified: "2026-08-06"
`)

	set, err := LoadWithUserRules(home, dir)
	if err != nil {
		t.Fatalf("loading: %v", err)
	}

	decision := set.Evaluate(filepath.Join(home, "workshop-notes", "chapter-1"))
	if !decision.Protected {
		t.Fatal("a user protection rule did not protect anything")
	}
	if decision.RuleID != "my-thesis" {
		t.Fatalf("protected by %q, want my-thesis", decision.RuleID)
	}
}

// A carve out against an ordinary built in protection is allowed, which is the
// decision this whole feature turns on: the machine's owner is entitled to
// override a judgement call wtff made on their behalf.
func TestUserRuleCanOverrideAStandardProtection(t *testing.T) {
	home := t.TempDir()
	builtin, err := LoadBuiltinForHome(home)
	if err != nil {
		t.Fatalf("loading builtin: %v", err)
	}

	var target *Rule
	for _, rule := range builtin.Rules() {
		if rule.Effect == EffectProtect && rule.Severity == SeverityStandard {
			copied := rule
			target = &copied
			break
		}
	}
	if target == nil {
		t.Skip("no standard protection in the built in set to override")
	}

	// A path the built in rule protects, made more specific so the carve out
	// wins on the existing precedence rather than on anything new.
	protected := target.Patterns()[0]
	carved := filepath.Join(strings.TrimSuffix(protected, "/"), "one-subdirectory")

	dir := filepath.Join(home, "config", "rules")
	writeUserRules(t, dir, "mine.yaml", `
  - id: my-carve-out
    match:
      type: prefix
      path: "`+carved+`"
    effect: allow
    severity: standard
    category: local
    reason: "I know what this is and I want it cleaned on this machine."
    provenance:
      source: "The person who owns this machine said so."
      method: system-inspection
      verified: "2026-08-06"
`)

	set, err := LoadWithUserRules(home, dir)
	if err != nil {
		t.Fatalf("loading: %v", err)
	}
	if set.Evaluate(carved).Protected {
		t.Fatalf("a more specific user carve out did not override %s", target.ID)
	}
}

// The floor. Credentials and keychains are marked critical precisely so that
// no local configuration, however emphatic, can reach them.
func TestUserRuleCannotOverrideACriticalProtection(t *testing.T) {
	home := t.TempDir()
	builtin, err := LoadBuiltinForHome(home)
	if err != nil {
		t.Fatalf("loading builtin: %v", err)
	}

	var critical *Rule
	for _, rule := range builtin.Rules() {
		if rule.Effect == EffectProtect && rule.Severity == SeverityCritical {
			copied := rule
			critical = &copied
			break
		}
	}
	if critical == nil {
		t.Fatal("the built in set has no critical protection, which would be the real problem")
	}

	target := critical.Patterns()[0]
	dir := filepath.Join(home, "config", "rules")
	writeUserRules(t, dir, "reckless.yaml", `
  - id: let-me-through
    match:
      type: prefix
      path: "`+filepath.Join(strings.TrimSuffix(target, "/"), "inside")+`"
    effect: allow
    severity: standard
    category: local
    reason: "Trying to carve an exception out of something critical."
    provenance:
      source: "A person who has not read the documentation."
      method: system-inspection
      verified: "2026-08-06"
`)

	_, err = LoadWithUserRules(home, dir)
	if !errors.Is(err, ErrOverridesCritical) {
		t.Fatalf("a carve out against a critical protection should be refused, got %v", err)
	}
	// Refused at load rather than ignored at evaluation, so the person learns
	// their rule does nothing instead of believing it works.
	if !strings.Contains(err.Error(), critical.ID) {
		t.Errorf("the refusal should name the rule it collides with, got %v", err)
	}
}

// Reusing a built in identifier would make two different rules answer to the
// same name in the log.
func TestUserRuleCannotReuseABuiltinIdentifier(t *testing.T) {
	home := t.TempDir()
	builtin, err := LoadBuiltinForHome(home)
	if err != nil {
		t.Fatalf("loading builtin: %v", err)
	}
	existing := builtin.Rules()[0].ID

	dir := filepath.Join(home, "config", "rules")
	writeUserRules(t, dir, "clash.yaml", `
  - id: `+existing+`
    match:
      type: prefix
      path: "~/somewhere"
    effect: protect
    severity: standard
    category: local
    reason: "Reusing an identifier that already belongs to a built in rule."
    provenance:
      source: "The person who owns this machine said so."
      method: system-inspection
      verified: "2026-08-06"
`)

	if _, err := LoadWithUserRules(home, dir); !errors.Is(err, ErrDuplicateRule) {
		t.Fatalf("expected a duplicate identifier refusal, got %v", err)
	}
}

// User rules are held to the same schema as built in ones, provenance
// included. A local rule nobody can account for later is the same maintenance
// problem the requirement exists to prevent.
func TestUserRulesMustSatisfyTheSameSchema(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "config", "rules")
	writeUserRules(t, dir, "sloppy.yaml", `
  - id: no-provenance
    match:
      type: prefix
      path: "~/somewhere"
    effect: protect
    severity: standard
    category: local
    reason: "This rule has no provenance and should not load."
`)

	if _, err := LoadWithUserRules(home, dir); err == nil {
		t.Fatal("a rule without provenance should not load")
	}
}

// Most machines will never have a configuration directory, and requiring one
// would make the ordinary case the exceptional one.
func TestMissingConfigurationIsNotAnError(t *testing.T) {
	home := t.TempDir()

	set, err := LoadWithUserRules(home, filepath.Join(home, "nothing", "here"))
	if err != nil {
		t.Fatalf("an absent configuration directory should be fine, got %v", err)
	}
	if set.Len() == 0 {
		t.Fatal("the built in rules should still be loaded")
	}
}

// Overrides have to be reportable before anything is planned. Allowing them at
// all was conditional on their never being silent.
func TestOverridesAreReportable(t *testing.T) {
	home := t.TempDir()
	builtin, err := LoadBuiltinForHome(home)
	if err != nil {
		t.Fatalf("loading builtin: %v", err)
	}

	var target *Rule
	for _, rule := range builtin.Rules() {
		if rule.Effect == EffectProtect && rule.Severity == SeverityStandard {
			copied := rule
			target = &copied
			break
		}
	}
	if target == nil {
		t.Skip("no standard protection to override")
	}
	carved := filepath.Join(strings.TrimSuffix(target.Patterns()[0], "/"), "one-subdirectory")

	dir := filepath.Join(home, "config", "rules")
	writeUserRules(t, dir, "mine.yaml", `
  - id: my-carve-out
    match:
      type: prefix
      path: "`+carved+`"
    effect: allow
    severity: standard
    category: local
    reason: "I know what this is and I want it cleaned on this machine."
    provenance:
      source: "The person who owns this machine said so."
      method: system-inspection
      verified: "2026-08-06"
`)

	set, err := LoadWithUserRules(home, dir)
	if err != nil {
		t.Fatalf("loading: %v", err)
	}

	overrides := set.Overrides()
	if len(overrides) != 1 {
		t.Fatalf("expected one reported override, got %+v", overrides)
	}
	if overrides[0].UserRuleID != "my-carve-out" {
		t.Errorf("override names %q as the user rule", overrides[0].UserRuleID)
	}
	if overrides[0].BuiltinRuleID != target.ID {
		t.Errorf("override names %q as the built in rule, want %s",
			overrides[0].BuiltinRuleID, target.ID)
	}
	if overrides[0].UserRuleFile != "mine.yaml" {
		t.Errorf("override should name the file it came from, got %q", overrides[0].UserRuleFile)
	}
}

// A configuration with no carve outs has nothing to report, so an ordinary run
// stays quiet.
func TestNoOverridesWhenNothingIsCarvedOut(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "config", "rules")
	writeUserRules(t, dir, "mine.yaml", `
  - id: my-thesis
    match:
      type: prefix
      path: "~/workshop-notes"
    effect: protect
    severity: standard
    category: personal
    reason: "Years of work that no cleanup tool has any business touching."
    provenance:
      source: "The person who owns this machine said so."
      method: system-inspection
      verified: "2026-08-06"
`)

	set, err := LoadWithUserRules(home, dir)
	if err != nil {
		t.Fatalf("loading: %v", err)
	}
	if overrides := set.Overrides(); len(overrides) != 0 {
		t.Fatalf("an additive configuration overrides nothing, got %+v", overrides)
	}
}

// The built in rules must survive the merge. A configuration file that
// silently replaced them would be the worst possible outcome of this feature.
func TestBuiltinRulesSurviveTheMerge(t *testing.T) {
	home := t.TempDir()
	builtin, err := LoadBuiltinForHome(home)
	if err != nil {
		t.Fatalf("loading builtin: %v", err)
	}

	dir := filepath.Join(home, "config", "rules")
	writeUserRules(t, dir, "mine.yaml", `
  - id: my-thesis
    match:
      type: prefix
      path: "~/workshop-notes"
    effect: protect
    severity: standard
    category: personal
    reason: "Years of work that no cleanup tool has any business touching."
    provenance:
      source: "The person who owns this machine said so."
      method: system-inspection
      verified: "2026-08-06"
`)

	merged, err := LoadWithUserRules(home, dir)
	if err != nil {
		t.Fatalf("loading: %v", err)
	}
	if merged.Len() != builtin.Len()+1 {
		t.Fatalf("merged set has %d rules, want %d built in plus one",
			merged.Len(), builtin.Len())
	}

	present := make(map[string]bool)
	for _, rule := range merged.Rules() {
		present[rule.ID] = true
	}
	for _, rule := range builtin.Rules() {
		if !present[rule.ID] {
			t.Errorf("built in rule %s was lost in the merge", rule.ID)
		}
	}
}
