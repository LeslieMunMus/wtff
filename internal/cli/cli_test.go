package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// newFixtureHome points every command at an isolated home directory, so a test
// run never touches the real machine's staging area or operation log.
func newFixtureHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	return home
}

func writeFixtureFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}
}

func TestNoArgumentsPrintsUsageAndFails(t *testing.T) {
	var out bytes.Buffer
	code := Run(nil, strings.NewReader(""), &out, &out)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(out.String(), "Usage:") {
		t.Fatal("expected usage text")
	}
}

func TestUnknownCommandFails(t *testing.T) {
	var out, errOut bytes.Buffer
	code := Run([]string{"not-a-real-command"}, strings.NewReader(""), &out, &errOut)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(errOut.String(), "unknown command") {
		t.Fatalf("stderr = %q, want it to name the unknown command", errOut.String())
	}
}

func TestVersionPrints(t *testing.T) {
	var out bytes.Buffer
	if code := Run([]string{"version"}, strings.NewReader(""), &out, &out); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if strings.TrimSpace(out.String()) != Version {
		t.Fatalf("printed %q, want %q", out.String(), Version)
	}
}

func TestRemoveDryRunChangesNothing(t *testing.T) {
	home := newFixtureHome(t)
	target := filepath.Join(home, "scratch", "cache.bin")
	writeFixtureFile(t, target, "payload")

	var out, errOut bytes.Buffer
	code := Run([]string{"remove", "--dry-run", target}, strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "dry run") {
		t.Fatalf("output did not mention dry run: %s", out.String())
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("dry run removed the file: %v", err)
	}
}

// The full lifecycle through the public entrypoint: stage, list, undo. This is
// the same path a real terminal session takes, run against a real filesystem
// rather than only against the deletion engine's own package tests.
func TestRemoveStageListAndUndoRoundTrip(t *testing.T) {
	home := newFixtureHome(t)
	target := filepath.Join(home, "scratch", "cache.bin")
	writeFixtureFile(t, target, "reclaimable content")

	var removeOut, removeErr bytes.Buffer
	code := Run([]string{"remove", "--yes", target}, strings.NewReader(""), &removeOut, &removeErr)
	if code != 0 {
		t.Fatalf("remove exit code = %d, stderr = %s", code, removeErr.String())
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("file was not removed from its original location: %v", err)
	}

	batchID := extractBatchID(t, removeOut.String())

	var stagedOut bytes.Buffer
	if code := Run([]string{"staged"}, strings.NewReader(""), &stagedOut, &stagedOut); code != 0 {
		t.Fatalf("staged exit code = %d", code)
	}
	if !strings.Contains(stagedOut.String(), batchID) {
		t.Fatalf("staged output %q does not list the batch %q", stagedOut.String(), batchID)
	}

	var undoOut, undoErr bytes.Buffer
	code = Run([]string{"undo", batchID}, strings.NewReader(""), &undoOut, &undoErr)
	if code != 0 {
		t.Fatalf("undo exit code = %d, stderr = %s", code, undoErr.String())
	}
	contents, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("file was not restored: %v", err)
	}
	if string(contents) != "reclaimable content" {
		t.Fatalf("restored contents = %q, want the original", contents)
	}

	var afterOut bytes.Buffer
	if code := Run([]string{"staged"}, strings.NewReader(""), &afterOut, &afterOut); code != 0 {
		t.Fatalf("staged exit code = %d", code)
	}
	if !strings.Contains(afterOut.String(), "nothing is staged") {
		t.Fatalf("expected an empty staging area after a full restore, got %q", afterOut.String())
	}
}

var batchIDInOutput = regexp.MustCompile(`wtff undo ([0-9]{8}-[0-9]{6}-[0-9a-f]{12})`)

func extractBatchID(t *testing.T, output string) string {
	t.Helper()
	match := batchIDInOutput.FindStringSubmatch(output)
	if match == nil {
		t.Fatalf("could not find a batch id in output: %q", output)
	}
	return match[1]
}

// A non-interactive session (piped input, as in a script or CI) must never
// proceed without --yes. Prompting into a pipe would block forever waiting for
// an answer nothing will send.
func TestRemoveRefusesNonInteractiveWithoutYes(t *testing.T) {
	home := newFixtureHome(t)
	target := filepath.Join(home, "scratch", "cache.bin")
	writeFixtureFile(t, target, "payload")

	var out, errOut bytes.Buffer
	code := Run([]string{"remove", target}, strings.NewReader("y\n"), &out, &errOut)
	if code == 0 {
		t.Fatal("a non-interactive run without --yes should not succeed")
	}
	if !strings.Contains(errOut.String(), "not an interactive session") {
		t.Fatalf("stderr = %q, want an explanation of the refusal", errOut.String())
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("file should still be present: %v", err)
	}
}

func TestRemoveWithYesSkipsThePrompt(t *testing.T) {
	home := newFixtureHome(t)
	target := filepath.Join(home, "scratch", "cache.bin")
	writeFixtureFile(t, target, "payload")

	var out, errOut bytes.Buffer
	code := Run([]string{"remove", "--yes", target}, strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, errOut.String())
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatal("file should have been staged away")
	}
}

// Protection rules apply through the CLI exactly as they apply through the
// engine directly. This is the same guarantee the deletion engine's own
// integration test checks, exercised here through the actual command a person
// types.
func TestRemoveRespectsProtectionRules(t *testing.T) {
	home := newFixtureHome(t)
	keychainDir := filepath.Join(home, "Library", "Keychains")
	if err := os.MkdirAll(keychainDir, 0o700); err != nil {
		t.Fatalf("setup: %v", err)
	}
	keychain := filepath.Join(keychainDir, "login.keychain-db")
	writeFixtureFile(t, keychain, "credential material")

	var out, errOut bytes.Buffer
	code := Run([]string{"remove", "--yes", keychain}, strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, errOut.String())
	}
	if _, err := os.Stat(keychain); err != nil {
		t.Fatalf("a protected keychain was removed: %v", err)
	}
	if !strings.Contains(out.String(), "user-keychain-directory") {
		t.Fatalf("output did not explain the protection: %s", out.String())
	}
}

func TestRemoveRequiresAtLeastOnePath(t *testing.T) {
	newFixtureHome(t)
	var out, errOut bytes.Buffer
	code := Run([]string{"remove", "--yes"}, strings.NewReader(""), &out, &errOut)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
}

func TestUndoRejectsAPathTraversalAttempt(t *testing.T) {
	newFixtureHome(t)
	var out, errOut bytes.Buffer
	code := Run([]string{"undo", "../../../etc"}, strings.NewReader(""), &out, &errOut)
	if code != 2 {
		t.Fatalf("exit code = %d, want a rejection, stderr = %s", code, errOut.String())
	}
	if !strings.Contains(errOut.String(), "not a valid batch id") {
		t.Fatalf("stderr = %q, want a validation message", errOut.String())
	}
}

func TestUndoRejectsAnAbsolutePathAsABatchID(t *testing.T) {
	newFixtureHome(t)
	var out, errOut bytes.Buffer
	code := Run([]string{"undo", "/etc/passwd"}, strings.NewReader(""), &out, &errOut)
	if code != 2 {
		t.Fatalf("exit code = %d, want a rejection, stderr = %s", code, errOut.String())
	}
}

func TestUndoReportsAnUnknownBatchCleanly(t *testing.T) {
	newFixtureHome(t)
	var out, errOut bytes.Buffer
	// Well formed but does not exist.
	code := Run([]string{"undo", "20260101-000000-abcdefabcdef"}, strings.NewReader(""), &out, &errOut)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), "cannot find that batch") {
		t.Fatalf("stderr = %q, want a clear not-found message", errOut.String())
	}
}

func TestStagedReportsEmptyAreaClearly(t *testing.T) {
	newFixtureHome(t)
	var out bytes.Buffer
	if code := Run([]string{"staged"}, strings.NewReader(""), &out, &out); code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	if !strings.Contains(out.String(), "nothing is staged") {
		t.Fatalf("output = %q", out.String())
	}
}

func TestHumanBytesFormatting(t *testing.T) {
	cases := map[int64]string{
		0:             "0B",
		1:             "1B",
		999:           "999B",
		1000:          "1.0KB",
		1500:          "1.5KB",
		1_000_000:     "1.0MB",
		1_500_000:     "1.5MB",
		1_000_000_000: "1.0GB",
	}
	for input, want := range cases {
		if got := humanBytes(input); got != want {
			t.Errorf("humanBytes(%d) = %q, want %q", input, got, want)
		}
	}
}

// Confirmation functions are tested directly since driving isInteractive
// through a real terminal is not practical in an automated test. This is the
// part that decides whether a reversible action needs only "y" while an
// irreversible one needs the full word.
func TestStageConfirmationAcceptsShortAnswer(t *testing.T) {
	var out bytes.Buffer
	if !confirmStage(strings.NewReader("y\n"), &out, "proceed? ") {
		t.Fatal("expected 'y' to confirm a staged removal")
	}
	if !confirmStage(strings.NewReader("yes\n"), &out, "proceed? ") {
		t.Fatal("expected 'yes' to confirm a staged removal")
	}
	if confirmStage(strings.NewReader("n\n"), &out, "proceed? ") {
		t.Fatal("expected 'n' to decline")
	}
	if confirmStage(strings.NewReader("\n"), &out, "proceed? ") {
		t.Fatal("expected an empty answer to decline")
	}
}

// This is the check that gives a purge more friction than a stage. A bare "y"
// answering a purge prompt, perhaps typed reflexively after a previous stage
// prompt, must not be read as approval for something that cannot be undone.
func TestPurgeConfirmationRequiresTheFullWordNotY(t *testing.T) {
	var out bytes.Buffer
	if confirmPurge(strings.NewReader("y\n"), &out, "confirm: ") {
		t.Fatal("a bare 'y' approved an irreversible purge")
	}
	if confirmPurge(strings.NewReader("yes\n"), &out, "confirm: ") {
		t.Fatal("'yes' approved an irreversible purge, only the full confirmation word should")
	}
	if !confirmPurge(strings.NewReader("permanently\n"), &out, "confirm: ") {
		t.Fatal("the exact confirmation word did not approve the purge")
	}
	if !confirmPurge(strings.NewReader("PERMANENTLY\n"), &out, "confirm: ") {
		t.Fatal("the confirmation word should not be case sensitive")
	}
}

// filepath.Abs collapses ".." lexically before path validation ever runs,
// which would let a traversal typed at the CLI silently resolve to a
// different path instead of being rejected the way path validation rejects
// the same thing internally. Found by reviewing the CLI layer rather than by
// a failing test: nothing downstream was broken by it, since whatever path
// resulted was still fully validated, but a person typing an accidental ".."
// would have had it silently resolved rather than explained.
func TestRemoveRejectsATraversalArgumentInsteadOfSilentlyResolvingIt(t *testing.T) {
	home := newFixtureHome(t)
	deep := filepath.Join(home, "one", "two")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}

	// Built by concatenation rather than filepath.Join, which cleans its result
	// and would collapse the ".." before Run ever saw it, the same way an
	// earlier test in the path validation package caught filepath.Join
	// silently defeating a traversal test there.
	traversal := deep + "/../../escaped"

	var out, errOut bytes.Buffer
	code := Run([]string{"remove", "--dry-run", traversal}, strings.NewReader(""), &out, &errOut)
	if code == 0 {
		t.Fatal("a traversal argument should not succeed")
	}
	if !strings.Contains(errOut.String(), "parent directory reference") {
		t.Fatalf("stderr = %q, want an explanation naming the traversal", errOut.String())
	}
}

// expandHome calls filepath.Join internally, which cleans its result the same
// way filepath.Abs does. A traversal check placed after expandHome runs would
// miss a traversal hidden behind a leading tilde, since expandHome would have
// already collapsed it. This is the same defect as the test above, one call
// earlier, and it was caught by checking what expandHome actually does rather
// than assuming a fix that worked for one caller worked for both.
func TestRemoveRejectsATraversalHiddenBehindATilde(t *testing.T) {
	newFixtureHome(t)
	var out, errOut bytes.Buffer
	code := Run([]string{"remove", "--dry-run", "~/../../etc/passwd"}, strings.NewReader(""), &out, &errOut)
	if code == 0 {
		t.Fatal("a traversal hidden behind a tilde should not succeed")
	}
	if !strings.Contains(errOut.String(), "parent directory reference") {
		t.Fatalf("stderr = %q, want an explanation naming the traversal", errOut.String())
	}
}

// The rejection is component based, matching path validation's own rule, so a
// real filename that merely contains two dots is unaffected.
func TestRemoveAcceptsFilenamesContainingButNotConsistingOfDoubleDots(t *testing.T) {
	home := newFixtureHome(t)
	target := filepath.Join(home, "index..data")
	writeFixtureFile(t, target, "payload")

	var out, errOut bytes.Buffer
	code := Run([]string{"remove", "--dry-run", target}, strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "index..data") {
		t.Fatalf("output did not mention the target: %s", out.String())
	}
}

func TestExpandHomeExpandsLeadingTilde(t *testing.T) {
	home := newFixtureHome(t)
	if got := expandHome("~/Documents/file.txt"); got != filepath.Join(home, "Documents/file.txt") {
		t.Fatalf("expandHome = %q", got)
	}
	if got := expandHome("~"); got != home {
		t.Fatalf("expandHome(~) = %q, want %q", got, home)
	}
	if got := expandHome("/already/absolute"); got != "/already/absolute" {
		t.Fatalf("expandHome should leave an absolute path unchanged, got %q", got)
	}
}
