package protectionrules

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

const testHome = "/Users/example"

func loadForTest(t *testing.T) *Set {
	t.Helper()
	set, err := LoadBuiltinForHome(testHome)
	if err != nil {
		t.Fatalf("LoadBuiltinForHome: %v", err)
	}
	return set
}

// The rule files ship inside the binary. A packaging change that drops them
// would otherwise produce a tool that runs and protects nothing.
func TestBuiltinRulesLoad(t *testing.T) {
	set := loadForTest(t)
	if set.Len() < 20 {
		t.Fatalf("loaded %d rules, expected the full built in set", set.Len())
	}
}

// Every shipped rule must satisfy the same requirements the loader enforces on
// any rule. This is the check that keeps the provenance promise real rather
// than aspirational.
func TestEveryShippedRuleIsAuditable(t *testing.T) {
	for _, rule := range loadForTest(t).Rules() {
		t.Run(rule.ID, func(t *testing.T) {
			if len(strings.TrimSpace(rule.Reason)) < 20 {
				t.Error("reason does not explain the risk")
			}
			if strings.TrimSpace(rule.Provenance.Source) == "" {
				t.Error("no provenance source")
			}
			if _, err := parseVerifiedDate(rule.Provenance.Verified); err != nil {
				t.Errorf("unreadable verification date: %v", err)
			}
			if rule.Category == "" {
				t.Error("no category")
			}
			if len(rule.Patterns()) == 0 {
				t.Error("no expanded patterns")
			}
			for _, pattern := range rule.Patterns() {
				if !strings.HasPrefix(pattern, "/") {
					t.Errorf("pattern %q is not absolute after expansion", pattern)
				}
			}
		})
	}
}

func TestProtectsCredentialAndPersonalPaths(t *testing.T) {
	set := loadForTest(t)

	protected := []string{
		testHome + "/Library/Keychains",
		testHome + "/Library/Keychains/login.keychain-db",
		"/Library/Keychains/System.keychain",
		testHome + "/.ssh/id_ed25519",
		testHome + "/.gnupg/private-keys-v1.d",
		testHome + "/.aws/credentials",
		testHome + "/Documents/tax-return.pdf",
		testHome + "/Desktop/notes.txt",
		testHome + "/Pictures/Photos Library.photoslibrary/database",
		testHome + "/Library/Mail/V10/inbox.mbox",
		testHome + "/Library/Messages/chat.db",
		testHome + "/Library/Mobile Documents/com~apple~CloudDocs/report.pages",
		testHome + "/Library/CloudStorage/Dropbox/shared/deck.key",
		testHome + "/Library/Safari/Bookmarks.plist",
		testHome + "/Library/Application Support/com.apple.TCC/TCC.db",
	}

	for _, path := range protected {
		t.Run(path, func(t *testing.T) {
			decision := set.Evaluate(path)
			if !decision.Protected {
				t.Fatalf("%s was not protected", path)
			}
			if decision.RuleID == "" || decision.Reason == "" {
				t.Fatal("a protection must name the rule and the reason responsible")
			}
		})
	}
}

// Protection that covers everything is not protection, it is refusal to work.
// These paths are the ordinary reclaimable targets the tool exists to handle.
func TestOrdinaryCachesAreNotProtected(t *testing.T) {
	set := loadForTest(t)

	unprotected := []string{
		testHome + "/Library/Caches/com.example.app",
		testHome + "/Library/Caches/com.example.app/data.bin",
		testHome + "/Library/Logs/SomeApplication/session.log",
		testHome + "/Library/Application Support/SomeApp/Cache",
		testHome + "/Projects/website/node_modules",
		"/private/tmp/build-artifacts",
	}

	for _, path := range unprotected {
		t.Run(path, func(t *testing.T) {
			if decision := set.Evaluate(path); decision.Protected {
				t.Fatalf("%s was protected by %s, which is too broad", path, decision.RuleID)
			}
		})
	}
}

// Apple filesystems are normally case insensitive, so a rule that only matched
// one spelling would protect nothing while appearing to protect something.
func TestMatchingIgnoresCase(t *testing.T) {
	set := loadForTest(t)
	if !set.Evaluate(testHome + "/library/keychains/login.keychain-db").Protected {
		t.Fatal("a differently cased path reached the same directory and was not protected")
	}
	if !set.Evaluate(testHome + "/DOCUMENTS/thesis.pdf").Protected {
		t.Fatal("a differently cased documents path was not protected")
	}
}

// A prefix rule must stop at a component boundary, or a rule written for one
// directory silently covers every sibling whose name starts the same way.
func TestPrefixRulesRespectComponentBoundaries(t *testing.T) {
	set := loadForTest(t)
	if decision := set.Evaluate(testHome + "/DocumentsArchive/old.zip"); decision.Protected {
		t.Fatalf("a sibling directory was caught by %s", decision.RuleID)
	}
	if decision := set.Evaluate(testHome + "/.sshfs-mounts/config"); decision.Protected {
		t.Fatalf("a sibling of the ssh directory was caught by %s", decision.RuleID)
	}
}

// The carve out pattern this schema exists for: a cache inside an otherwise
// protected configuration directory can be reclaimed.
func TestCarveOutPermitsRebuildableSubdirectory(t *testing.T) {
	set := loadForTest(t)

	if !set.Evaluate(testHome + "/.docker/contexts").Protected {
		t.Fatal("the docker configuration directory should be protected")
	}
	if decision := set.Evaluate(testHome + "/.docker/cache/blobs"); decision.Protected {
		t.Fatalf("the carved out cache was still protected by %s", decision.RuleID)
	}
}

// A carve out must never override a critical protection, however specific it
// is. This is what stops a narrow exception from exposing a credential by
// accident of precedence.
func TestCarveOutCannotOverrideCriticalProtection(t *testing.T) {
	set := loadForTest(t)
	decision := set.Evaluate(testHome + "/.docker/config.json")
	if !decision.Protected {
		t.Fatal("a critical credential file was exposed by a surrounding carve out")
	}
	if decision.RuleID != "docker-registry-credentials" {
		t.Fatalf("protection came from %s, expected the critical credential rule", decision.RuleID)
	}
}

// A glob covers the directory it names and everything beneath it, so a rule for
// a sandbox Documents directory does not need a second entry for its contents.
func TestGlobRulesCoverTheirContents(t *testing.T) {
	set := loadForTest(t)

	protected := testHome + "/Library/Containers/com.example.editor/Data/Documents/draft.txt"
	if !set.Evaluate(protected).Protected {
		t.Fatal("a file inside a sandboxed Documents directory was not protected")
	}
	cache := testHome + "/Library/Containers/com.example.editor/Data/Library/Caches/thumbs"
	if decision := set.Evaluate(cache); decision.Protected {
		t.Fatalf("a sandbox cache was protected by %s, which is too broad", decision.RuleID)
	}
}

// The home directory may be reached through a link, in which case the resolved
// path this package is asked about differs from the declared one. A rule stored
// only in its declared form stops matching, silently. This defect has already
// occurred once in this project, in the deletion engine's self protection.
func TestRulesMatchAHomeDirectoryReachedThroughALink(t *testing.T) {
	base := t.TempDir()
	actual := filepath.Join(base, "actual-home")
	if err := os.MkdirAll(actual, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	linked := filepath.Join(base, "linked-home")
	if err := os.Symlink(actual, linked); err != nil {
		t.Fatalf("setup: %v", err)
	}

	set, err := LoadBuiltinForHome(linked)
	if err != nil {
		t.Fatalf("LoadBuiltinForHome: %v", err)
	}

	// Resolve the link the same way the deletion engine would before asking.
	resolvedHome, err := filepath.EvalSymlinks(linked)
	if err != nil {
		t.Fatalf("resolving: %v", err)
	}
	if !set.Evaluate(resolvedHome + "/Documents/private.txt").Protected {
		t.Fatal("a resolved path under a linked home directory was not protected")
	}
	if !set.Evaluate(linked + "/Documents/private.txt").Protected {
		t.Fatal("the declared form stopped matching once the resolved form was added")
	}
}

func TestSatisfiesThePolicyInterface(t *testing.T) {
	set := loadForTest(t)
	ruleID, reason, protected := set.Protected(testHome + "/Library/Keychains/login.keychain-db")
	if !protected || ruleID == "" || reason == "" {
		t.Fatalf("Protected returned %q, %q, %v", ruleID, reason, protected)
	}
}

// Loader validation. Each case is a way a rule file can be wrong that would
// otherwise produce a rule set protecting less than it appears to.
func TestLoaderRefusesUnusableRuleFiles(t *testing.T) {
	valid := `version: 1
rules:
  - id: sample-rule
    match: {type: prefix, path: "~/Sample"}
    effect: protect
    severity: standard
    category: testing
    reason: "A sufficiently descriptive reason for the sample rule."
    provenance: {source: "test", method: documentation, verified: "2026-08-05"}
`

	cases := map[string]string{
		"wrong version":      strings.Replace(valid, "version: 1", "version: 99", 1),
		"missing identifier": strings.Replace(valid, "id: sample-rule", `id: ""`, 1),
		"bad identifier":     strings.Replace(valid, "id: sample-rule", "id: Sample_Rule", 1),
		"unknown effect":     strings.Replace(valid, "effect: protect", "effect: destroy", 1),
		"unknown severity":   strings.Replace(valid, "severity: standard", "severity: urgent", 1),
		"unknown match type": strings.Replace(valid, "type: prefix", "type: regex", 1),
		"relative pattern":   strings.Replace(valid, `path: "~/Sample"`, `path: "Sample"`, 1),
		"parent reference":   strings.Replace(valid, `path: "~/Sample"`, `path: "~/Sample/../other"`, 1),
		"placeholder reason": strings.Replace(valid, `reason: "A sufficiently descriptive reason for the sample rule."`, `reason: "todo"`, 1),
		"missing provenance": strings.Replace(valid, `source: "test"`, `source: ""`, 1),
		"unknown method":     strings.Replace(valid, "method: documentation", "method: guessed", 1),
		"bad date":           strings.Replace(valid, `verified: "2026-08-05"`, `verified: "soon"`, 1),
		"critical carve out": strings.Replace(strings.Replace(valid,
			"effect: protect", "effect: allow", 1),
			"severity: standard", "severity: critical", 1),
	}

	for name, contents := range cases {
		t.Run(name, func(t *testing.T) {
			fileSystem := fstest.MapFS{
				"rules/sample.yaml": &fstest.MapFile{Data: []byte(contents)},
			}
			if _, err := loadFromFS(fileSystem, "rules", testHome); err == nil {
				t.Fatalf("loader accepted a rule file with: %s", name)
			}
		})
	}

	// The valid form must load, or the cases above prove nothing.
	fileSystem := fstest.MapFS{"rules/sample.yaml": &fstest.MapFile{Data: []byte(valid)}}
	if _, err := loadFromFS(fileSystem, "rules", testHome); err != nil {
		t.Fatalf("the control case failed to load: %v", err)
	}
}

func TestLoaderRefusesDuplicateIdentifiers(t *testing.T) {
	rule := `version: 1
rules:
  - id: shared-identifier
    match: {type: prefix, path: "~/One"}
    effect: protect
    severity: standard
    category: testing
    reason: "A sufficiently descriptive reason for this rule."
    provenance: {source: "test", method: documentation, verified: "2026-08-05"}
`
	fileSystem := fstest.MapFS{
		"rules/first.yaml":  &fstest.MapFile{Data: []byte(rule)},
		"rules/second.yaml": &fstest.MapFile{Data: []byte(rule)},
	}
	_, err := loadFromFS(fileSystem, "rules", testHome)
	if !errors.Is(err, ErrDuplicateRule) {
		t.Fatalf("loading duplicate identifiers = %v, want ErrDuplicateRule", err)
	}
}

// An empty rule set means nothing is protected, which is never a correct state
// to begin from and is exactly what dropping the rule files would produce.
func TestLoaderRefusesAnEmptyRuleSet(t *testing.T) {
	fileSystem := fstest.MapFS{
		"rules/empty.yaml": &fstest.MapFile{Data: []byte("version: 1\nrules: []\n")},
	}
	if _, err := loadFromFS(fileSystem, "rules", testHome); !errors.Is(err, ErrInvalidRule) {
		t.Fatalf("loading an empty rule set = %v, want a refusal", err)
	}
}

// Precedence must be decidable by a person reading the files, so the more
// specific rule wins and a tie keeps the data.
func TestMoreSpecificRuleWinsAndTiesKeepData(t *testing.T) {
	contents := `version: 1
rules:
  - id: broad-protection
    match: {type: prefix, path: "~/Workspace"}
    effect: protect
    severity: standard
    category: testing
    reason: "Protects the whole workspace directory by default."
    provenance: {source: "test", method: documentation, verified: "2026-08-05"}
  - id: narrow-carve-out
    match: {type: prefix, path: "~/Workspace/build-output"}
    effect: allow
    severity: standard
    category: testing
    reason: "Build output inside the workspace is rebuildable and reclaimable."
    provenance: {source: "test", method: documentation, verified: "2026-08-05"}
  - id: tied-protection
    match: {type: prefix, path: "~/Workspace/build-output"}
    effect: protect
    severity: standard
    category: testing
    reason: "A competing rule at identical specificity, to test tie handling."
    provenance: {source: "test", method: documentation, verified: "2026-08-05"}
`
	fileSystem := fstest.MapFS{"rules/precedence.yaml": &fstest.MapFile{Data: []byte(contents)}}
	set, err := loadFromFS(fileSystem, "rules", testHome)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if !set.Evaluate(testHome + "/Workspace/source.go").Protected {
		t.Fatal("the broad protection did not apply")
	}
	decision := set.Evaluate(testHome + "/Workspace/build-output/binary")
	if !decision.Protected {
		t.Fatal("a tie between protect and allow resolved toward removing data")
	}
	if decision.RuleID != "tied-protection" {
		t.Fatalf("tie resolved to %s, expected the protecting rule", decision.RuleID)
	}
}

// When a critical protection outranks a carve out that would otherwise have
// won, the carve out is named. Someone wrote it expecting it to apply, and a
// silent override is the hardest kind of rule mistake to find.
func TestOverriddenCarveOutIsReported(t *testing.T) {
	set := loadForTest(t)
	decision := set.Evaluate(testHome + "/.docker/config.json")
	if !decision.Protected {
		t.Fatal("expected protection")
	}
	// The docker cache carve out does not cover config.json, so nothing should
	// be reported as overridden here. This asserts the field is not populated
	// spuriously, which would train a reader to ignore it.
	if decision.Overridden != "" {
		t.Fatalf("reported %s as overridden when it does not match this path", decision.Overridden)
	}
}
