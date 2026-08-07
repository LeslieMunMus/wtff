// Package diagnostics inspects wtff's own state and the environment it runs
// in, and reports what a person can act on.
//
// The checks here are chosen by one rule: each has to describe a condition
// that is either already costing something or will surprise someone later,
// and each has to say what to do about it. A diagnostic that reports a number
// nobody can act on is decoration.
package diagnostics

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	cleancatalog "github.com/lesliemunmus/wtff/internal/clean-catalog"
	deletionengine "github.com/lesliemunmus/wtff/internal/deletion-engine"
	operationlog "github.com/lesliemunmus/wtff/internal/operation-log"
	protectionrules "github.com/lesliemunmus/wtff/internal/protection-rules"
	userconfig "github.com/lesliemunmus/wtff/internal/user-config"
)

// Level is how much a finding wants from the reader.
type Level int

const (
	// LevelOK reports something checked and found healthy. Worth printing,
	// because a diagnostic that only speaks when unhappy leaves a person
	// unsure whether it looked at all.
	LevelOK Level = iota

	// LevelNote reports something true and worth knowing that is not a
	// problem, such as space held in staging on purpose.
	LevelNote

	// LevelWarn reports something that needs a decision.
	LevelWarn
)

func (l Level) String() string {
	switch l {
	case LevelWarn:
		return "warn"
	case LevelNote:
		return "note"
	default:
		return "ok"
	}
}

// Finding is one checked condition.
type Finding struct {
	Area    string
	Level   Level
	Summary string

	// Detail carries the specifics, and for a warning it says what to do.
	Detail []string
}

// Report is everything the run found.
type Report struct {
	Findings []Finding
}

// NeedsAttention reports whether anything warrants a decision, which is what
// a non-zero exit status is for.
func (r Report) NeedsAttention() bool {
	for _, finding := range r.Findings {
		if finding.Level == LevelWarn {
			return true
		}
	}
	return false
}

// staleAfter is when a rule's provenance stops being evidence and starts being
// an assumption. macOS moves paths between releases, so a justification
// verified against a system two releases ago deserves rechecking.
const staleAfter = 365 * 24 * time.Hour

// forgottenAfter is when staged items stop looking deliberate. Staging exists
// so a decision can be deferred, not avoided, and space held this long is
// usually held by accident.
const forgottenAfter = 30 * 24 * time.Hour

// Options configures a run, so tests can point it somewhere other than the
// invoking user's real home.
type Options struct {
	Home        string
	StagingRoot string
	LogPath     string

	// ConfigRoot is where the user's own rules and catalog entries live.
	ConfigRoot string

	// Now is injected so the age based findings are testable.
	Now time.Time
}

// Run performs every check and returns what it found.
func Run(opts Options) Report {
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}

	var report Report
	report.add(checkStaging(opts)...)
	report.add(checkLog(opts)...)
	report.add(checkRules(opts)...)
	report.add(checkCatalog(opts)...)
	report.add(checkVisibility(opts)...)
	report.add(checkUserConfig(opts)...)
	return report
}

func (r *Report) add(findings ...Finding) {
	r.Findings = append(r.Findings, findings...)
}

// checkStaging reports what is held, what it costs, and anything in the
// staging area that is not a batch wtff can restore.
func checkStaging(opts Options) []Finding {
	root := opts.StagingRoot
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return []Finding{{
			Area: "staging", Level: LevelOK,
			Summary: "nothing is staged, and the staging area has not been created yet",
		}}
	}
	if err != nil {
		return []Finding{{
			Area: "staging", Level: LevelWarn,
			Summary: "the staging area cannot be read",
			Detail:  []string{err.Error(), "check permissions on " + root},
		}}
	}

	var findings []Finding
	if perm := permissionsOf(root); perm != "" && perm != "700" {
		findings = append(findings, Finding{
			Area: "staging", Level: LevelWarn,
			Summary: "the staging area is readable by other users",
			Detail: []string{
				fmt.Sprintf("%s is mode %s, expected 700", root, perm),
				"it holds files removed from your home directory, so it should be yours alone",
				"fix with: chmod 700 " + shellQuote(root),
			},
		})
	}

	area, err := deletionengine.NewStagingArea(root)
	if err != nil {
		return append(findings, Finding{
			Area: "staging", Level: LevelWarn,
			Summary: "the staging area cannot be opened",
			Detail:  []string{err.Error()},
		})
	}
	batches, err := area.ListBatches()
	if err != nil {
		return append(findings, Finding{
			Area: "staging", Level: LevelWarn,
			Summary: "staged batches cannot be listed",
			Detail:  []string{err.Error()},
		})
	}

	// Anything in the staging root that ListBatches did not accept. It skips
	// these silently by design, because an unreadable record may be a run
	// interrupted before it finished writing one, and the items are still
	// there. Silently is right for listing and wrong for a diagnostic: this is
	// exactly the recoverable data nobody knows they have.
	known := make(map[string]bool, len(batches))
	for _, batch := range batches {
		known[filepath.Base(batch.Dir())] = true
	}
	var orphans []string
	for _, entry := range entries {
		if entry.IsDir() && !known[entry.Name()] {
			orphans = append(orphans, filepath.Join(root, entry.Name()))
		}
	}
	if len(orphans) > 0 {
		detail := append([]string{}, orphans...)
		detail = append(detail,
			"these hold files wtff moved but cannot restore, because their record is missing or unreadable",
			"the files themselves are under items/ in each, and can be moved back by hand")
		findings = append(findings, Finding{
			Area: "staging", Level: LevelWarn,
			Summary: fmt.Sprintf("%d staging director%s wtff cannot restore",
				len(orphans), plural(len(orphans), "y", "ies")),
			Detail: detail,
		})
	}

	if len(batches) == 0 {
		return append(findings, Finding{
			Area: "staging", Level: LevelOK, Summary: "nothing is staged",
		})
	}

	var total int64
	var partial bool
	oldest := opts.Now
	for _, batch := range batches {
		for _, item := range batch.Items {
			if item.SizeKnown {
				total += item.SizeBytes
			} else {
				partial = true
			}
		}
		if batch.CreatedAt.Before(oldest) {
			oldest = batch.CreatedAt
		}
	}

	held := humanBytes(total)
	if partial {
		held = "at least " + held
	}
	summary := fmt.Sprintf("%d batch%s holding %s, restorable with undo",
		len(batches), plural(len(batches), "", "es"), held)

	level := LevelNote
	detail := []string{
		"run wtff staged to list them",
		"restore with wtff undo <batch-id>, or reclaim the space with wtff staged --purge <batch-id>",
	}
	if age := opts.Now.Sub(oldest); age > forgottenAfter {
		level = LevelWarn
		summary = fmt.Sprintf("%d batch%s holding %s, oldest staged %d days ago",
			len(batches), plural(len(batches), "", "es"), held, int(age.Hours()/24))
		detail = append([]string{
			"staging defers a decision rather than avoiding one, and this space is still held",
		}, detail...)
	}

	return append(findings, Finding{
		Area: "staging", Level: level, Summary: summary, Detail: detail,
	})
}

// checkLog reports the audit trail's size and permissions.
func checkLog(opts Options) []Finding {
	path := opts.LogPath
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return []Finding{{
			Area: "log", Level: LevelOK,
			Summary: "no operation log yet, which is expected before the first run",
		}}
	}
	if err != nil {
		return []Finding{{
			Area: "log", Level: LevelWarn,
			Summary: "the operation log cannot be read",
			Detail:  []string{err.Error()},
		}}
	}

	var findings []Finding
	if perm := permissionsOf(path); perm != "" && perm != "600" {
		findings = append(findings, Finding{
			Area: "log", Level: LevelWarn,
			Summary: "the operation log is readable by other users",
			Detail: []string{
				fmt.Sprintf("%s is mode %s, expected 600", path, perm),
				"it records the paths of everything removed from your home directory",
				"fix with: chmod 600 " + shellQuote(path),
			},
		})
	}

	total := info.Size()
	rotated := 0
	for i := 1; ; i++ {
		rotatedInfo, err := os.Stat(fmt.Sprintf("%s.%d", path, i))
		if err != nil {
			break
		}
		total += rotatedInfo.Size()
		rotated++
	}

	summary := fmt.Sprintf("%s recorded", humanBytes(total))
	if rotated > 0 {
		summary = fmt.Sprintf("%s recorded across the current log and %d rotated file%s",
			humanBytes(total), rotated, plural(rotated, "", "s"))
	}
	return append(findings, Finding{
		Area: "log", Level: LevelOK, Summary: summary,
		Detail: []string{path},
	})
}

// checkUserConfig reports what the person's own configuration adds, and
// anything it overrides.
//
// Doctor reported only the built in counts until this existed, which meant the
// one command whose job is saying what state wtff is in was quietly ignoring
// the files most likely to have changed that state.
func checkUserConfig(opts Options) []Finding {
	layout := userconfig.LayoutAt(opts.ConfigRoot)
	if !layout.Exists() {
		return []Finding{{
			Area: "configuration", Level: LevelOK,
			Summary: "none, so the built in rules and catalog are in effect unchanged",
			Detail:  []string{"create " + layout.Root + " to add your own"},
		}}
	}

	rules, err := userconfig.LoadRules(opts.Home, layout)
	if err != nil {
		return []Finding{{
			Area: "configuration", Level: LevelWarn,
			Summary: "your configuration does not load, so every command refuses to run",
			Detail:  []string{err.Error()},
		}}
	}

	findings := []Finding{{
		Area: "configuration", Level: LevelOK,
		Summary: "loaded from " + layout.Root,
	}}

	if overrides := rules.Overrides(); len(overrides) > 0 {
		detail := make([]string, 0, len(overrides)+1)
		for _, override := range overrides {
			detail = append(detail, fmt.Sprintf("%s is allowed by %s (%s), overriding %s",
				override.Path, override.UserRuleID, override.UserRuleFile,
				override.BuiltinRuleID))
		}
		detail = append(detail,
			"this is allowed and deliberate, and worth seeing written down somewhere "+
				"other than the file that did it")
		findings = append(findings, Finding{
			Area: "configuration", Level: LevelNote,
			Summary: fmt.Sprintf("%d built in protection%s overridden by your rules",
				len(overrides), plural(len(overrides), "", "s")),
			Detail: detail,
		})
	}
	return findings
}

func checkRules(opts Options) []Finding {
	rules, err := protectionrules.LoadBuiltin()
	if err != nil {
		return []Finding{{
			Area: "protection rules", Level: LevelWarn,
			Summary: "the built in protection rules do not load, so nothing is protected",
			Detail:  []string{err.Error(), "this build is broken, do not use it to remove anything"},
		}}
	}
	return []Finding{{
		Area: "protection rules", Level: LevelOK,
		Summary: fmt.Sprintf("%d rule%s loaded", rules.Len(), plural(rules.Len(), "", "s")),
	}}
}

func checkCatalog(opts Options) []Finding {
	catalog, err := cleancatalog.LoadBuiltin()
	if err != nil {
		return []Finding{{
			Area: "clean catalog", Level: LevelWarn,
			Summary: "the built in cleanup catalog does not load",
			Detail:  []string{err.Error()},
		}}
	}

	entries := catalog.Entries()
	var stale []string
	for _, entry := range entries {
		verified, err := time.Parse("2006-01-02", entry.Provenance.Verified)
		if err != nil {
			continue
		}
		if opts.Now.Sub(verified) > staleAfter {
			stale = append(stale, fmt.Sprintf("%s, last verified %s",
				entry.ID, entry.Provenance.Verified))
		}
	}

	findings := []Finding{{
		Area: "clean catalog", Level: LevelOK,
		Summary: fmt.Sprintf("%d categor%s loaded, %d of them removable permanently",
			len(entries), plural(len(entries), "y", "ies"),
			len(cleancatalog.PurgeableEntries(entries))),
	}}
	if len(stale) > 0 {
		findings = append(findings, Finding{
			Area: "clean catalog", Level: LevelNote,
			Summary: fmt.Sprintf("%d categor%s not verified against a running system for over a year",
				len(stale), plural(len(stale), "y", "ies")),
			Detail: append(stale,
				"macOS moves paths between releases, so these justifications are now assumptions"),
		})
	}
	return findings
}

// checkVisibility reports whether wtff can read the places it actually looks.
//
// An earlier version of this check probed Safari, Mail, and Cookies to infer
// whether Full Disk Access had been granted, then reported that "some
// locations are invisible to wtff". That was misleading in the worst
// direction: it prompted a person toward granting a broad permission that
// would have bought them nothing, because wtff does not target those paths and
// never has.
//
// wtff is an allowlist, not a scanner. It looks in the handful of places its
// catalog names and nowhere else, so the question worth answering is not
// whether a permission is granted but whether anything wtff actually wants is
// being withheld. Those give opposite answers on a machine where the grant is
// absent and every target is readable anyway, which is the ordinary case.
func checkVisibility(opts Options) []Finding {
	catalog, err := cleancatalog.LoadBuiltin()
	if err != nil {
		// The catalog check already reports this properly.
		return nil
	}

	var denied, checked []string
	for _, entry := range catalog.Entries() {
		// The volume trash entry's path names where mount points appear, not
		// a directory wtff removes anything from. Reading /Volumes says
		// nothing about whether the trash on an attached drive is reachable,
		// and counting it made an empty home report a category present.
		if entry.Kind == cleancatalog.KindVolumeTrash {
			continue
		}
		path := expandHome(entry.Path, opts.Home)
		if _, err := os.Lstat(path); err != nil {
			// Absent is the ordinary case for most categories on most
			// machines and says nothing about permissions.
			continue
		}
		checked = append(checked, path)
		if _, err := os.ReadDir(path); err != nil && os.IsPermission(err) {
			denied = append(denied, path)
		}
	}

	switch {
	case len(checked) == 0:
		return []Finding{{
			Area: "visibility", Level: LevelNote,
			Summary: "none of the categories wtff cleans exist on this machine",
		}}
	case len(denied) == 0:
		return []Finding{{
			Area: "visibility", Level: LevelOK,
			Summary: fmt.Sprintf("all %d categor%s present here are readable",
				len(checked), plural(len(checked), "y", "ies")),
			Detail: []string{
				"wtff only looks in the places its catalog names, so nothing else being " +
					"unreadable does not affect it",
			},
		}}
	default:
		return []Finding{{
			Area: "visibility", Level: LevelWarn,
			Summary: fmt.Sprintf("%d categor%s wtff cleans cannot be read, so it will under report",
				len(denied), plural(len(denied), "y that", "ies that")),
			Detail: append(denied,
				"wtff will leave these alone rather than remove something it cannot see, "+
					"which is the safe direction to be wrong",
				"if these are locations macOS protects, granting Full Disk Access to your "+
					"terminal in System Settings, Privacy and Security would make them visible",
			),
		}}
	}
}

// expandHome resolves a leading tilde against the supplied home directory
// rather than the invoking user's, so a test can point the whole check
// somewhere else.
func expandHome(path, home string) string {
	if path == "~" {
		return home
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(home, path[2:])
	}
	return path
}

// DefaultOptions resolves the paths wtff actually uses.
func DefaultOptions() (Options, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Options{}, fmt.Errorf("cannot determine home directory: %w", err)
	}
	stagingRoot, err := deletionengine.DefaultStagingRoot()
	if err != nil {
		return Options{}, err
	}
	logPath, err := operationlog.DefaultPath()
	if err != nil {
		return Options{}, err
	}
	layout, err := userconfig.DefaultLayout()
	if err != nil {
		return Options{}, err
	}
	return Options{Home: home, StagingRoot: stagingRoot, LogPath: logPath,
		ConfigRoot: layout.Root}, nil
}

func permissionsOf(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%o", info.Mode().Perm())
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// shellQuote wraps a path for a command a person is meant to paste, since home
// directories and volume names routinely contain spaces.
func shellQuote(path string) string {
	return "'" + path + "'"
}

func humanBytes(n int64) string {
	const unit = 1000
	if n < unit {
		return fmt.Sprintf("%dB", n)
	}
	div, exp := int64(unit), 0
	for value := n / unit; value >= unit; value /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%cB", float64(n)/float64(div), "kMGTPE"[exp])
}
