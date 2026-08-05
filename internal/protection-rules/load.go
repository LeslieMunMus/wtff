package protectionrules

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// ruleSchemaVersion is the rule file format this build understands.
const ruleSchemaVersion = 1

//go:embed rules/*.yaml
var builtinRules embed.FS

// identifierPattern enforces the project naming convention on rule identifiers.
// They appear in logs and in user facing skip messages, so a consistent shape
// is worth requiring rather than hoping for.
var identifierPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// Loading errors. A caller cannot meaningfully continue past any of them, since
// the result would be a rule set that silently protects less than it claims.
var (
	ErrInvalidRule        = errors.New("invalid protection rule")
	ErrDuplicateRule      = errors.New("duplicate protection rule identifier")
	ErrUnsupportedVersion = errors.New("unsupported rule file version")
)

// Set is a loaded, validated collection of rules.
type Set struct {
	rules []Rule
}

// Rules reports the loaded rules, most specific first.
func (s *Set) Rules() []Rule { return append([]Rule(nil), s.rules...) }

// Len reports how many rules are loaded.
func (s *Set) Len() int { return len(s.rules) }

// LoadBuiltin loads the rule set compiled into the binary.
//
// The rules ship inside the executable rather than alongside it so that a
// single downloaded binary is complete. A tool that reads its safety rules from
// a file next to itself fails in the least helpful way possible: it runs, finds
// nothing, and protects nothing.
func LoadBuiltin() (*Set, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("cannot determine home directory: %w", err)
	}
	return loadFromFS(builtinRules, "rules", home)
}

// LoadBuiltinForHome loads the built in rules against a specific home
// directory. It exists for tests, which must not depend on the machine running
// them.
func LoadBuiltinForHome(home string) (*Set, error) {
	return loadFromFS(builtinRules, "rules", home)
}

func loadFromFS(fileSystem fs.FS, dir, home string) (*Set, error) {
	entries, err := fs.ReadDir(fileSystem, dir)
	if err != nil {
		return nil, fmt.Errorf("cannot read rule directory: %w", err)
	}

	homeVariants := homeDirectoryVariants(home)

	var all []Rule
	seen := make(map[string]string)

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		contents, readErr := fs.ReadFile(fileSystem, filepath.Join(dir, entry.Name()))
		if readErr != nil {
			return nil, fmt.Errorf("cannot read %s: %w", entry.Name(), readErr)
		}

		var document ruleDocument
		if err := yaml.Unmarshal(contents, &document); err != nil {
			return nil, fmt.Errorf("cannot parse %s: %w", entry.Name(), err)
		}
		if document.Version != ruleSchemaVersion {
			return nil, fmt.Errorf("%w: %s declares version %d, this build understands %d",
				ErrUnsupportedVersion, entry.Name(), document.Version, ruleSchemaVersion)
		}

		for _, rule := range document.Rules {
			rule.origin = entry.Name()
			if err := validate(&rule); err != nil {
				return nil, fmt.Errorf("%s: %w", entry.Name(), err)
			}
			if previous, duplicate := seen[rule.ID]; duplicate {
				return nil, fmt.Errorf("%w: %q appears in both %s and %s",
					ErrDuplicateRule, rule.ID, previous, entry.Name())
			}
			seen[rule.ID] = entry.Name()

			if err := expand(&rule, homeVariants); err != nil {
				return nil, fmt.Errorf("%s: %w", entry.Name(), err)
			}
			all = append(all, rule)
		}
	}

	if len(all) == 0 {
		// An empty rule set means every path is unprotected. That is never a
		// correct state to start from, and it is exactly what a packaging
		// mistake that drops the rule files would produce.
		return nil, fmt.Errorf("%w: no rules were loaded", ErrInvalidRule)
	}

	// Sorting once at load keeps evaluation a single ordered pass and makes the
	// precedence a caller sees identical to the precedence a reader can work
	// out from the files.
	sort.SliceStable(all, func(i, j int) bool {
		return all[i].specificity > all[j].specificity
	})

	return &Set{rules: all}, nil
}

// validate rejects a rule that cannot be acted on or cannot be audited.
//
// Every check here refuses rather than repairs. A rule set is data that decides
// what survives, and quietly correcting a malformed entry means shipping a rule
// nobody wrote.
func validate(rule *Rule) error {
	if rule.ID == "" {
		return fmt.Errorf("%w: a rule has no identifier", ErrInvalidRule)
	}
	if !identifierPattern.MatchString(rule.ID) {
		return fmt.Errorf("%w: identifier %q must be lowercase words joined by single hyphens",
			ErrInvalidRule, rule.ID)
	}
	switch rule.Effect {
	case EffectProtect, EffectAllow:
	default:
		return fmt.Errorf("%w: %s declares unknown effect %q", ErrInvalidRule, rule.ID, rule.Effect)
	}
	switch rule.Severity {
	case SeverityCritical, SeverityStandard:
	default:
		return fmt.Errorf("%w: %s declares unknown severity %q", ErrInvalidRule, rule.ID, rule.Severity)
	}
	if rule.Effect == EffectAllow && rule.Severity == SeverityCritical {
		// Critical means "no carve out may override this". A critical carve out
		// is a contradiction, and accepting one would leave its meaning to
		// whatever the matcher happened to do.
		return fmt.Errorf("%w: %s is a carve out marked critical, which has no meaning",
			ErrInvalidRule, rule.ID)
	}
	switch rule.Match.Type {
	case MatchExact, MatchPrefix, MatchGlob:
	default:
		return fmt.Errorf("%w: %s declares unknown match type %q",
			ErrInvalidRule, rule.ID, rule.Match.Type)
	}
	if rule.Match.Path == "" {
		return fmt.Errorf("%w: %s has an empty pattern", ErrInvalidRule, rule.ID)
	}
	if !strings.HasPrefix(rule.Match.Path, "/") && !strings.HasPrefix(rule.Match.Path, "~/") {
		return fmt.Errorf("%w: %s pattern %q must be absolute or start at the home directory",
			ErrInvalidRule, rule.ID, rule.Match.Path)
	}
	if strings.Contains(rule.Match.Path, "/../") || strings.HasSuffix(rule.Match.Path, "/..") {
		return fmt.Errorf("%w: %s pattern contains a parent reference", ErrInvalidRule, rule.ID)
	}
	if rule.Category == "" {
		return fmt.Errorf("%w: %s has no category", ErrInvalidRule, rule.ID)
	}
	if len(strings.TrimSpace(rule.Reason)) < 20 {
		// A reason has to survive contact with a reader who was not there when
		// it was written. A few words cannot, and a length floor is a crude but
		// effective way to keep placeholder text out of the file.
		return fmt.Errorf("%w: %s needs a reason that explains the risk, not a label",
			ErrInvalidRule, rule.ID)
	}
	if strings.TrimSpace(rule.Provenance.Source) == "" {
		return fmt.Errorf("%w: %s has no provenance source", ErrInvalidRule, rule.ID)
	}
	switch rule.Provenance.Method {
	case "documentation", "vendor-documentation", "system-inspection":
	default:
		return fmt.Errorf("%w: %s declares unknown provenance method %q",
			ErrInvalidRule, rule.ID, rule.Provenance.Method)
	}
	if _, err := parseVerifiedDate(rule.Provenance.Verified); err != nil {
		return fmt.Errorf("%w: %s has an unreadable verification date %q",
			ErrInvalidRule, rule.ID, rule.Provenance.Verified)
	}
	if rule.Match.Type == MatchGlob {
		if _, err := filepath.Match(rule.Match.Path, "/probe"); err != nil {
			return fmt.Errorf("%w: %s has a malformed glob: %v", ErrInvalidRule, rule.ID, err)
		}
	}
	return nil
}

// homeDirectoryVariants returns every spelling of the home directory that a
// resolved path might present.
//
// This mirrors a defect already found once in this project, where a guard built
// its paths from the declared home directory and compared them against resolved
// ones. The two agree only while no component of the home directory is a link,
// and when they stop agreeing the guard fails silently.
func homeDirectoryVariants(home string) []string {
	home = strings.TrimSuffix(home, "/")
	if home == "" {
		return nil
	}
	variants := []string{home}
	if resolved, err := filepath.EvalSymlinks(home); err == nil && resolved != home {
		variants = append(variants, strings.TrimSuffix(resolved, "/"))
	}
	return variants
}

// expand turns a declared pattern into every absolute form it can match.
func expand(rule *Rule, homeVariants []string) error {
	if strings.HasPrefix(rule.Match.Path, "~/") {
		if len(homeVariants) == 0 {
			return fmt.Errorf("%w: %s needs a home directory but none is known",
				ErrInvalidRule, rule.ID)
		}
		suffix := strings.TrimPrefix(rule.Match.Path, "~")
		for _, home := range homeVariants {
			rule.patterns = append(rule.patterns, home+suffix)
		}
	} else {
		rule.patterns = []string{rule.Match.Path}
	}

	for i, pattern := range rule.patterns {
		rule.patterns[i] = strings.TrimSuffix(pattern, "/")
	}
	// Specificity is taken from the declared pattern so that it does not vary
	// with the length of a particular machine's home directory.
	rule.specificity = computeSpecificity(rule.Match.Type, rule.Match.Path)
	return nil
}
