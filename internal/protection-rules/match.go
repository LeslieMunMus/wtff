package protectionrules

import (
	"path/filepath"
	"strings"
)

// Evaluate decides whether a resolved path is protected, and names the rule
// responsible either way.
//
// Precedence has two levels, in this order:
//
//  1. A critical protection wins outright. No carve out overrides it, however
//     specific the carve out is. This exists so that adding a narrow exception
//     somewhere in the rule files can never, by accident of specificity, expose
//     a credential store or a user's documents.
//
//  2. Otherwise the most specific matching rule wins, and a tie goes to
//     protection. Ties are not hypothetical: a protect and an allow rule
//     written over the same path is how a disagreement between two rule files
//     shows up, and resolving it toward keeping data is the recoverable
//     direction to be wrong in.
func (s *Set) Evaluate(path string) Decision {
	if path == "" {
		return Decision{}
	}
	normalized := strings.TrimSuffix(path, "/")
	if normalized == "" {
		normalized = "/"
	}

	var best *Rule
	var criticalProtection *Rule

	for i := range s.rules {
		rule := &s.rules[i]
		if !rule.matches(normalized) {
			continue
		}
		if rule.Effect == EffectProtect && rule.Severity == SeverityCritical {
			if criticalProtection == nil {
				criticalProtection = rule
			}
			continue
		}
		if best == nil {
			best = rule
			continue
		}
		if rule.specificity > best.specificity {
			best = rule
			continue
		}
		if rule.specificity == best.specificity &&
			best.Effect == EffectAllow && rule.Effect == EffectProtect {
			best = rule
		}
	}

	if criticalProtection != nil {
		decision := Decision{
			Protected: true,
			RuleID:    criticalProtection.ID,
			Reason:    criticalProtection.Reason,
		}
		// A carve out that would have won on specificity, but did not because
		// the protection is critical, is worth naming. Someone wrote that carve
		// out expecting it to apply.
		if best != nil && best.Effect == EffectAllow &&
			best.specificity > criticalProtection.specificity {
			decision.Overridden = best.ID
		}
		return decision
	}

	if best == nil {
		return Decision{}
	}
	if best.Effect == EffectAllow {
		return Decision{RuleID: best.ID, Reason: best.Reason}
	}
	return Decision{Protected: true, RuleID: best.ID, Reason: best.Reason}
}

// Protected satisfies the deletion engine's policy interface.
func (s *Set) Protected(path string) (string, string, bool) {
	decision := s.Evaluate(path)
	return decision.RuleID, decision.Reason, decision.Protected
}

// matches reports whether a rule applies to a normalized path.
//
// Comparison ignores case throughout, because Apple filesystems are normally
// case insensitive and case preserving. A rule written for one spelling that
// failed to match another spelling of the same directory would protect nothing
// while appearing to protect something.
func (r *Rule) matches(path string) bool {
	for _, pattern := range r.patterns {
		switch r.Match.Type {
		case MatchExact:
			if strings.EqualFold(path, pattern) {
				return true
			}
		case MatchPrefix:
			if strings.EqualFold(path, pattern) || hasPathPrefixFold(path, pattern) {
				return true
			}
		case MatchGlob:
			if globMatchFold(pattern, path) {
				return true
			}
		}
	}
	return false
}

// hasPathPrefixFold reports whether path sits beneath prefix, comparing on
// component boundaries so that a rule for "/a/Cache" does not match
// "/a/CacheOfSomethingElse".
func hasPathPrefixFold(path, prefix string) bool {
	withSeparator := prefix + "/"
	return len(path) > len(withSeparator) &&
		strings.EqualFold(path[:len(withSeparator)], withSeparator)
}

// globMatchFold applies a glob without case sensitivity.
//
// A glob matches the path itself and anything beneath it, so a pattern written
// for a container directory covers its contents without every rule needing a
// second prefix entry.
func globMatchFold(pattern, path string) bool {
	lowerPattern := strings.ToLower(pattern)
	lowerPath := strings.ToLower(path)

	if matched, err := filepath.Match(lowerPattern, lowerPath); err == nil && matched {
		return true
	}
	// Walk up the path, testing each ancestor, so that a glob naming a
	// directory also covers everything inside it.
	for cut := strings.LastIndex(lowerPath, "/"); cut > 0; cut = strings.LastIndex(lowerPath, "/") {
		lowerPath = lowerPath[:cut]
		if matched, err := filepath.Match(lowerPattern, lowerPath); err == nil && matched {
			return true
		}
	}
	return false
}
