package diagnostics

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	deletionengine "github.com/lesliemusengi/wtff/internal/deletion-engine"
	operationlog "github.com/lesliemusengi/wtff/internal/operation-log"
)

// fixture builds an isolated home with the paths wtff uses, so no check here
// can read the developer's real staging area or log.
func fixture(t *testing.T) Options {
	t.Helper()
	home := t.TempDir()
	return Options{
		Home:        home,
		StagingRoot: filepath.Join(home, "Library", "Application Support", "wtff", "staging"),
		LogPath:     filepath.Join(home, "Library", "Logs", "wtff", "operations.log"),
		Now:         time.Now(),
	}
}

func find(t *testing.T, report Report, area string) Finding {
	t.Helper()
	for _, finding := range report.Findings {
		if finding.Area == area {
			return finding
		}
	}
	t.Fatalf("no finding for %q in %+v", area, report.Findings)
	return Finding{}
}

func findAll(report Report, area string) []Finding {
	var found []Finding
	for _, finding := range report.Findings {
		if finding.Area == area {
			found = append(found, finding)
		}
	}
	return found
}

// stage puts one real file into the fixture's staging area and returns the
// batch, so the age based checks have something genuine to read.
func stage(t *testing.T, opts Options) *deletionengine.Batch {
	t.Helper()
	target := filepath.Join(opts.Home, "cache-entry")
	if err := os.WriteFile(target, []byte(strings.Repeat("x", 4096)), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}
	manifest, err := deletionengine.Plan(
		[]deletionengine.Candidate{{Path: target, RuleID: "r", Reason: "x"}},
		deletionengine.PlanOptions{Command: "test", Policy: deletionengine.AllowAll{},
			Log: operationlog.Discard(), MeasureSizes: true})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	area, err := deletionengine.NewStagingArea(opts.StagingRoot)
	if err != nil {
		t.Fatalf("staging area: %v", err)
	}
	if _, err := deletionengine.Apply(manifest, deletionengine.ApplyOptions{
		Staging: area, Policy: deletionengine.AllowAll{}, Log: operationlog.Discard(),
	}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	batches, err := area.ListBatches()
	if err != nil || len(batches) != 1 {
		t.Fatalf("expected one batch, got %d, %v", len(batches), err)
	}
	return batches[0]
}

func TestCleanMachineNeedsNoAttention(t *testing.T) {
	opts := fixture(t)
	report := Run(opts)

	if report.NeedsAttention() {
		for _, finding := range report.Findings {
			if finding.Level == LevelWarn {
				t.Errorf("unexpected warning: %s: %s", finding.Area, finding.Summary)
			}
		}
	}
	if len(report.Findings) < 5 {
		t.Fatalf("expected a finding per area, got %d", len(report.Findings))
	}
}

// A diagnostic that only speaks when unhappy leaves a person unsure whether it
// looked at all, so healthy areas are reported too.
func TestHealthyAreasAreStillReported(t *testing.T) {
	report := Run(fixture(t))
	for _, area := range []string{"staging", "log", "protection rules", "clean catalog"} {
		if find(t, report, area).Summary == "" {
			t.Errorf("%s reported nothing", area)
		}
	}
}

func TestRecentlyStagedIsANoteNotAWarning(t *testing.T) {
	opts := fixture(t)
	stage(t, opts)

	finding := find(t, Run(opts), "staging")
	if finding.Level != LevelNote {
		t.Fatalf("a fresh batch should be a note, got %s: %s", finding.Level, finding.Summary)
	}
	if !strings.Contains(finding.Summary, "restorable with undo") {
		t.Fatalf("the summary should say it can be restored, got %q", finding.Summary)
	}
	if Run(opts).NeedsAttention() {
		t.Fatal("staging something deliberately is not a problem")
	}
}

// Staging defers a decision rather than avoiding one. Space held for a month
// is usually held by accident, and that is worth saying.
func TestLongForgottenStagingIsAWarning(t *testing.T) {
	opts := fixture(t)
	stage(t, opts)
	opts.Now = opts.Now.Add(forgottenAfter + 24*time.Hour)

	report := Run(opts)
	finding := find(t, report, "staging")
	if finding.Level != LevelWarn {
		t.Fatalf("a month old batch should warn, got %s: %s", finding.Level, finding.Summary)
	}
	if !strings.Contains(finding.Summary, "days ago") {
		t.Fatalf("the summary should say how old it is, got %q", finding.Summary)
	}
	if !report.NeedsAttention() {
		t.Fatal("a forgotten batch should make the report need attention")
	}
}

// The listing skips unreadable batch directories silently, which is right for
// listing and wrong for a diagnostic: this is recoverable data nobody knows
// they have.
func TestStagingDirectoryWithoutARecordIsSurfaced(t *testing.T) {
	opts := fixture(t)
	orphan := filepath.Join(opts.StagingRoot, "20200101-000000-abandoned", "items")
	if err := os.MkdirAll(orphan, 0o700); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(orphan, "0001-something"),
		[]byte("recoverable"), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	report := Run(opts)
	var surfaced bool
	for _, finding := range findAll(report, "staging") {
		if strings.Contains(finding.Summary, "cannot restore") {
			surfaced = true
			if finding.Level != LevelWarn {
				t.Error("an unrestorable staging directory should warn")
			}
			if !strings.Contains(strings.Join(finding.Detail, "\n"), "abandoned") {
				t.Errorf("the detail should name the directory, got %v", finding.Detail)
			}
		}
	}
	if !surfaced {
		t.Fatalf("the orphaned directory was not reported: %+v", report.Findings)
	}
	if !report.NeedsAttention() {
		t.Fatal("data wtff cannot restore should need attention")
	}
}

// The staging area holds files taken from a home directory, and the log
// records their paths. Neither should be readable by anyone else.
func TestLoosePermissionsAreReported(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("running as root, which makes permissions moot")
	}
	opts := fixture(t)
	stage(t, opts)

	writer, err := operationlog.Open(opts.LogPath, "test")
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	writer.Record(operationlog.Event{Kind: operationlog.KindPurged, Path: "/tmp/x"})
	writer.Close()

	if err := os.Chmod(opts.StagingRoot, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.Chmod(opts.LogPath, 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	report := Run(opts)
	var stagingFlagged, logFlagged bool
	for _, finding := range report.Findings {
		if strings.Contains(finding.Summary, "readable by other users") {
			if finding.Area == "staging" {
				stagingFlagged = true
			}
			if finding.Area == "log" {
				logFlagged = true
			}
			if !strings.Contains(strings.Join(finding.Detail, "\n"), "chmod") {
				t.Errorf("%s should say how to fix it, got %v", finding.Area, finding.Detail)
			}
		}
	}
	if !stagingFlagged {
		t.Error("a world readable staging area was not reported")
	}
	if !logFlagged {
		t.Error("a world readable log was not reported")
	}
	if !report.NeedsAttention() {
		t.Error("loose permissions should need attention")
	}
}

func TestLogSizeCountsRotatedFiles(t *testing.T) {
	opts := fixture(t)
	if err := os.MkdirAll(filepath.Dir(opts.LogPath), 0o700); err != nil {
		t.Fatalf("setup: %v", err)
	}
	for _, name := range []string{opts.LogPath, opts.LogPath + ".1", opts.LogPath + ".2"} {
		if err := os.WriteFile(name, []byte(strings.Repeat("x", 1000)), 0o600); err != nil {
			t.Fatalf("setup: %v", err)
		}
	}

	finding := find(t, Run(opts), "log")
	if !strings.Contains(finding.Summary, "2 rotated files") {
		t.Fatalf("the summary should count rotated files, got %q", finding.Summary)
	}
	if !strings.Contains(finding.Summary, "3.0kB") {
		t.Fatalf("the summary should total every file, got %q", finding.Summary)
	}
}

// The rules and catalog are compiled in, so a failure to load means the build
// itself is broken and nothing is protected.
func TestBuiltinRulesAndCatalogLoad(t *testing.T) {
	report := Run(fixture(t))

	rules := find(t, report, "protection rules")
	if rules.Level != LevelOK {
		t.Fatalf("built in rules should load: %s", rules.Summary)
	}
	if !strings.Contains(rules.Summary, "rules loaded") {
		t.Fatalf("expected a rule count, got %q", rules.Summary)
	}

	catalog := find(t, report, "clean catalog")
	if catalog.Level != LevelOK {
		t.Fatalf("built in catalog should load: %s", catalog.Summary)
	}
	if !strings.Contains(catalog.Summary, "removable permanently") {
		t.Fatalf("expected the purgeable count, got %q", catalog.Summary)
	}
}

// A justification verified against a system two releases ago is an assumption,
// not evidence.
func TestStaleProvenanceIsNoted(t *testing.T) {
	opts := fixture(t)
	opts.Now = time.Now().Add(3 * staleAfter)

	var noted bool
	for _, finding := range findAll(Run(opts), "clean catalog") {
		if strings.Contains(finding.Summary, "not verified") {
			noted = true
			if finding.Level != LevelNote {
				t.Error("stale provenance is worth knowing, not a problem to fix now")
			}
		}
	}
	if !noted {
		t.Fatal("stale provenance was not noted")
	}
}

// The check that replaced a misleading one. The old probe read Safari, Mail,
// and Cookies to infer whether Full Disk Access was granted, and reported
// locations invisible to wtff when wtff does not target those paths at all.
// It prompted a person toward a broad permission grant that would have
// changed nothing.
func TestVisibilityReportsOnlyWhatWtffActuallyTargets(t *testing.T) {
	opts := fixture(t)

	// A location macOS protects, which wtff has no interest in.
	unreadable := filepath.Join(opts.Home, "Library", "Safari")
	if err := os.MkdirAll(unreadable, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.Chmod(unreadable, 0o000); err != nil {
		t.Fatalf("setup: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(unreadable, 0o755) })

	// A category wtff does target, readable.
	if err := os.MkdirAll(filepath.Join(opts.Home, ".Trash"), 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}

	report := Run(opts)
	finding := find(t, report, "visibility")
	if finding.Level != LevelOK {
		t.Fatalf("an unreadable path wtff never targets must not be reported: %s: %s",
			finding.Level, finding.Summary)
	}
	if report.NeedsAttention() {
		t.Fatal("wtff being unable to read something it does not want is not a problem")
	}
	for _, line := range append(finding.Detail, finding.Summary) {
		if strings.Contains(line, "Safari") {
			t.Fatalf("the report mentions a path wtff does not target: %q", line)
		}
	}
}

// When something wtff genuinely wants is withheld, that is a real warning,
// because clean will silently propose less than it should.
func TestUnreadableTargetIsAWarning(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("running as root, which bypasses the permissions under test")
	}
	opts := fixture(t)

	trash := filepath.Join(opts.Home, ".Trash")
	if err := os.MkdirAll(trash, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.Chmod(trash, 0o000); err != nil {
		t.Fatalf("setup: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(trash, 0o755) })

	report := Run(opts)
	finding := find(t, report, "visibility")
	if finding.Level != LevelWarn {
		t.Fatalf("an unreadable target should warn, got %s: %s",
			finding.Level, finding.Summary)
	}
	if !strings.Contains(finding.Summary, "under report") {
		t.Fatalf("the summary should say the consequence, got %q", finding.Summary)
	}
	detail := strings.Join(finding.Detail, "\n")
	if !strings.Contains(detail, trash) {
		t.Fatalf("the detail should name what cannot be read, got %q", detail)
	}
	if !strings.Contains(detail, "Full Disk Access") {
		t.Fatalf("the detail should offer the likely remedy, got %q", detail)
	}
	if !report.NeedsAttention() {
		t.Fatal("wtff being blocked from what it targets should need attention")
	}
}

// A machine with none of the categories present is reported honestly rather
// than as either healthy or broken.
func TestVisibilityWithNoCategoriesPresent(t *testing.T) {
	finding := find(t, Run(fixture(t)), "visibility")
	if finding.Level != LevelNote || !strings.Contains(finding.Summary, "none of the categories") {
		t.Fatalf("expected an honest nothing-to-check, got %s: %s",
			finding.Level, finding.Summary)
	}
}

// NeedsAttention drives the exit status, so it must be true for warnings only.
func TestNeedsAttentionTracksWarningsOnly(t *testing.T) {
	if (Report{Findings: []Finding{{Level: LevelOK}, {Level: LevelNote}}}).NeedsAttention() {
		t.Error("notes and healthy findings must not demand attention")
	}
	if !(Report{Findings: []Finding{{Level: LevelOK}, {Level: LevelWarn}}}).NeedsAttention() {
		t.Error("a warning must demand attention")
	}
}

// Diagnostics must never touch what they inspect. This is the one command a
// person runs when they already suspect something is wrong.
func TestRunChangesNothing(t *testing.T) {
	opts := fixture(t)
	batch := stage(t, opts)

	before := snapshot(t, opts.Home)
	Run(opts)
	after := snapshot(t, opts.Home)

	if len(before) != len(after) {
		t.Fatalf("running diagnostics changed the tree: %d paths before, %d after",
			len(before), len(after))
	}
	for path := range before {
		if !after[path] {
			t.Errorf("diagnostics removed %s", path)
		}
	}
	if _, err := os.Stat(batch.Dir()); err != nil {
		t.Errorf("the staged batch was disturbed: %v", err)
	}
}

func snapshot(t *testing.T, root string) map[string]bool {
	t.Helper()
	seen := map[string]bool{}
	err := filepath.Walk(root, func(path string, _ os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		seen[path] = true
		return nil
	})
	if err != nil {
		t.Fatalf("walking: %v", err)
	}
	return seen
}
