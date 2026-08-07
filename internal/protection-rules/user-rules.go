package protectionrules

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// ErrOverridesCritical is returned when a user rule tries to carve an
// exception out of a protection that may not be carved.
//
// Refused at load rather than ignored at evaluation. A rule that quietly does
// nothing is worse than one that will not load: the person wrote it expecting
// an effect, and silence lets them believe they got one.
var ErrOverridesCritical = errors.New("user rule cannot override a critical protection")

// LoadWithUserRules loads the built in rules and merges the user's own.
//
// User rules join the same set and are evaluated by the same precedence: a
// more specific rule wins, and a critical protection wins over everything
// regardless of specificity. That last part is what keeps this safe to offer.
// Credentials, keychains, and irreplaceable personal data are marked critical
// precisely so that no local configuration, however emphatic, can reach them.
//
// A missing configuration directory is not an error. Most machines will never
// have one, and requiring it would make the ordinary case the exceptional one.
func LoadWithUserRules(home, userRulesDir string) (*Set, error) {
	builtin, err := LoadBuiltinForHome(home)
	if err != nil {
		return nil, err
	}

	userRules, err := loadUserRules(userRulesDir, home, builtin)
	if err != nil {
		return nil, err
	}
	if len(userRules) == 0 {
		return builtin, nil
	}

	merged := append(builtin.Rules(), userRules...)
	sort.SliceStable(merged, func(i, j int) bool {
		return merged[i].specificity > merged[j].specificity
	})
	return &Set{rules: merged}, nil
}

func loadUserRules(dir, home string, builtin *Set) ([]Rule, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("cannot read %s: %w", dir, err)
	}

	homeVariants := homeDirectoryVariants(home)

	// Built in identifiers are reserved. A user rule reusing one would make
	// two different rules answer to the same name in the log, and there is no
	// reading of that which helps anyone.
	reserved := make(map[string]bool)
	for _, rule := range builtin.Rules() {
		reserved[rule.ID] = true
	}

	var all []Rule
	seen := make(map[string]string)

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		contents, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil, fmt.Errorf("cannot read %s: %w", path, readErr)
		}

		var document ruleDocument
		if err := yaml.Unmarshal(contents, &document); err != nil {
			return nil, fmt.Errorf("cannot parse %s: %w", path, err)
		}
		if document.Version != ruleSchemaVersion {
			return nil, fmt.Errorf("%w: %s declares version %d, this build understands %d",
				ErrUnsupportedVersion, path, document.Version, ruleSchemaVersion)
		}

		for _, rule := range document.Rules {
			rule.origin = entry.Name()
			rule.userSupplied = true

			if err := validate(&rule); err != nil {
				return nil, fmt.Errorf("%s: %w", path, err)
			}
			if reserved[rule.ID] {
				return nil, fmt.Errorf("%w: %q is the identifier of a built in rule",
					ErrDuplicateRule, rule.ID)
			}
			if previous, duplicate := seen[rule.ID]; duplicate {
				return nil, fmt.Errorf("%w: %q appears in both %s and %s",
					ErrDuplicateRule, rule.ID, previous, entry.Name())
			}
			seen[rule.ID] = entry.Name()

			if err := expand(&rule, homeVariants); err != nil {
				return nil, fmt.Errorf("%s: %w", path, err)
			}
			if err := refuseCriticalOverride(rule, builtin); err != nil {
				return nil, fmt.Errorf("%s: %w", path, err)
			}
			all = append(all, rule)
		}
	}
	return all, nil
}

// refuseCriticalOverride rejects a carve out aimed at a critical protection.
//
// Evaluation would already ignore it, since a critical protection wins
// regardless of specificity. Refusing at load is the difference between a
// person learning their rule does nothing and a person believing it works.
func refuseCriticalOverride(rule Rule, builtin *Set) error {
	if rule.Effect != EffectAllow {
		return nil
	}

	// The path the carve out names is checked against the built in set as
	// though it were a real target, which is exactly what it will be.
	// Every expanded pattern is checked, not just the first. A rule naming a
	// home relative path expands to more than one absolute form, and a carve
	// out that reached a critical protection through only one of them would
	// pass a check that looked at the other.
	for _, pattern := range rule.patterns {
		decision := builtin.Evaluate(pattern)
		if !decision.Protected {
			continue
		}
		for _, existing := range builtin.Rules() {
			if existing.ID == decision.RuleID && existing.Severity == SeverityCritical {
				return fmt.Errorf("%w: %q allows %s, which %s protects as critical: %s",
					ErrOverridesCritical, rule.ID, rule.Match.Path, existing.ID, existing.Reason)
			}
		}
	}
	return nil
}

// Override describes a built in protection that a user rule takes precedence
// over, so a command can say so before it acts.
type Override struct {
	UserRuleID    string
	UserRuleFile  string
	BuiltinRuleID string
	Path          string
}

// Overrides reports every built in protection a user rule currently wins
// against.
//
// This is computed rather than recorded during evaluation because it has to be
// available before anything is planned. The point of allowing overrides at all
// was that they would never be silent, and a report that only appears once a
// path has already been skipped or removed arrives too late to be a warning.
func (s *Set) Overrides() []Override {
	var builtinOnly []Rule
	var userAllows []Rule
	for _, rule := range s.rules {
		switch {
		case rule.userSupplied && rule.Effect == EffectAllow:
			userAllows = append(userAllows, rule)
		case !rule.userSupplied:
			builtinOnly = append(builtinOnly, rule)
		}
	}
	if len(userAllows) == 0 {
		return nil
	}

	builtin := &Set{rules: builtinOnly}
	var overrides []Override
	for _, rule := range userAllows {
		for _, pattern := range rule.patterns {
			before := builtin.Evaluate(pattern)
			if !before.Protected {
				continue
			}
			if after := s.Evaluate(pattern); after.Protected {
				continue
			}
			overrides = append(overrides, Override{
				UserRuleID:    rule.ID,
				UserRuleFile:  rule.origin,
				BuiltinRuleID: before.RuleID,
				Path:          rule.Match.Path,
			})
			break
		}
	}
	return overrides
}
