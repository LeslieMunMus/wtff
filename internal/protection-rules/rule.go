// Package protectionrules decides which structurally valid paths must not be
// removed, from declarative data rather than from code.
//
// The distinction from path validation is deliberate and load bearing. Path
// validation asks whether a path is structurally safe to touch. This package
// asks a different question: whether this particular thing, wherever it sits,
// is something a person would be upset to lose. A cache directory and a
// keychain are equally valid as paths, and only one of them is disposable.
//
// # Why every rule must state a source
//
// A protection list is the part of a cleanup tool that nobody reviews until it
// is wrong. Entries accumulate, their reasons live in the head of whoever added
// them, and years later no one can say whether a given line is load bearing or
// cargo. That is a maintenance problem and a safety problem at once: an entry
// nobody understands cannot be safely narrowed when it turns out to be too
// broad, and cannot be confidently trusted when it looks too narrow.
//
// So a rule that cannot say why it exists and where that justification came
// from does not load. Not as a style preference: the loader refuses it.
package protectionrules

import (
	"strings"
	"time"
)

// Effect is what a rule asserts about the paths it matches.
type Effect string

const (
	// EffectProtect marks matching paths as things wtff must not remove.
	EffectProtect Effect = "protect"

	// EffectAllow carves an exception out of a broader protection, for the
	// common shape where a directory holds durable data alongside a genuinely
	// rebuildable subdirectory. Without carve outs, protecting the parent means
	// the disposable part can never be reclaimed, and the pressure to fix that
	// tends to produce a narrower parent rule that protects too little.
	EffectAllow Effect = "allow"
)

// Severity separates rules that may be overridden from rules that may not.
type Severity string

const (
	// SeverityCritical marks protections that no carve out may override,
	// regardless of how specific the carve out is. Credential stores and
	// irreplaceable user data belong here: the cost of wrongly keeping them is
	// some disk space, and the cost of wrongly removing them is unbounded.
	SeverityCritical Severity = "critical"

	// SeverityStandard marks protections that a more specific carve out may
	// override.
	SeverityStandard Severity = "standard"
)

// MatchType is how a rule's pattern is compared against a path.
type MatchType string

const (
	// MatchExact matches one path and nothing beneath it.
	MatchExact MatchType = "exact"

	// MatchPrefix matches a path and everything beneath it.
	MatchPrefix MatchType = "prefix"

	// MatchGlob matches using shell style wildcards within a single component.
	MatchGlob MatchType = "glob"
)

// Provenance records where a rule's justification came from, so that a later
// reader can check it rather than trust it.
type Provenance struct {
	// Source names the document, vendor page, or observation the rule rests on.
	Source string `yaml:"source"`

	// Method distinguishes a rule derived from published documentation from one
	// derived by looking at a running system, because the two age differently:
	// documentation is stable and may be out of date, observation is current
	// and may not generalize beyond the machine it came from.
	Method string `yaml:"method"`

	// Verified is when the justification was last checked.
	Verified string `yaml:"verified"`
}

// Match is a rule's pattern and how to compare it.
type Match struct {
	Type MatchType `yaml:"type"`
	Path string    `yaml:"path"`
}

// Rule is one entry in the protection set.
type Rule struct {
	ID         string     `yaml:"id"`
	Match      Match      `yaml:"match"`
	Effect     Effect     `yaml:"effect"`
	Severity   Severity   `yaml:"severity"`
	Category   string     `yaml:"category"`
	Reason     string     `yaml:"reason"`
	Provenance Provenance `yaml:"provenance"`

	// userSupplied marks a rule that came from the user's own configuration
	// rather than from the set compiled into the binary.
	//
	// It exists so an override can be reported rather than merely allowed. A
	// person editing their own rules is entitled to carve an exception out of
	// a standard protection, and is not entitled to have wtff stay quiet about
	// it: the whole value of a protection list is that someone can trust it
	// without reading it, and an unannounced local override spends that trust.
	userSupplied bool

	// patterns holds every expanded, absolute form of Match.Path.
	//
	// There is more than one because a home directory reached through a link
	// produces a different string than the same directory reached directly, and
	// this package is asked about fully resolved paths. A rule stored only in
	// its declared form stops matching the moment the home directory contains a
	// link, silently and without any error to notice.
	patterns []string

	// specificity orders overlapping rules. Larger is more specific.
	specificity int

	// origin names the file a rule came from, for diagnostics.
	origin string
}

// Origin reports which rule file defined this rule.
func (r Rule) Origin() string { return r.origin }

// Patterns reports the expanded absolute patterns this rule matches on.
func (r Rule) Patterns() []string { return append([]string(nil), r.patterns...) }

// Decision is the outcome of evaluating a path against the rule set.
type Decision struct {
	// Protected is the answer callers act on.
	Protected bool

	// RuleID and Reason identify the rule responsible, whichever way the
	// decision went. A carve out that permitted removal is as worth naming as a
	// protection that prevented it, because both are surprising when wrong.
	RuleID string
	Reason string

	// Overridden names a protection that a carve out took precedence over, when
	// that happened. This is the shape of mistake most worth surfacing: a rule
	// that looks like it protects something, silently outranked.
	Overridden string
}

// ruleDocument is the on disk shape of a rule file.
type ruleDocument struct {
	Version int    `yaml:"version"`
	Rules   []Rule `yaml:"rules"`
}

// computeSpecificity ranks a pattern by how much of it is literal.
//
// Ordering overlapping rules by literal length is what makes precedence
// predictable to a person reading the files: the rule that says more about a
// path wins over the rule that says less. The match type breaks ties, since an
// exact rule describes one path while a prefix rule of the same text describes
// a whole tree.
func computeSpecificity(matchType MatchType, pattern string) int {
	literal := pattern
	if cut := strings.IndexAny(pattern, "*?["); cut >= 0 {
		literal = pattern[:cut]
	}

	var typeWeight int
	switch matchType {
	case MatchExact:
		typeWeight = 2
	case MatchPrefix:
		typeWeight = 1
	default:
		typeWeight = 0
	}
	return len(literal)*4 + typeWeight
}

// parseVerifiedDate checks that a provenance date is a real date.
func parseVerifiedDate(value string) (time.Time, error) {
	return time.Parse("2006-01-02", value)
}
