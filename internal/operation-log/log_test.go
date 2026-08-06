package operationlog

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func readLines(t *testing.T, path string) []Event {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading log: %v", err)
	}
	var events []Event
	for _, line := range strings.Split(strings.TrimSpace(string(contents)), "\n") {
		if line == "" {
			continue
		}
		var event Event
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("log line is not valid JSON: %q, %v", line, err)
		}
		events = append(events, event)
	}
	return events
}

// One JSON object per line is the whole contract: a person greps it, a future
// history command parses it, and a truncated final line stays discardable
// without invalidating what came before.
func TestRecordWritesOneJSONObjectPerLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "operations.log")
	writer, err := Open(path, "clean")
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	writer.Record(Event{Kind: KindPurged, Path: "/tmp/one", Bytes: 10, SizeKnown: true})
	writer.Record(Event{Kind: KindSkipped, Path: "/tmp/two", Detail: "protected"})
	if err := writer.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	events := readLines(t, path)
	if len(events) != 2 {
		t.Fatalf("wrote %d events, want 2", len(events))
	}
	if events[0].Kind != KindPurged || events[0].Path != "/tmp/one" {
		t.Fatalf("first event is wrong: %+v", events[0])
	}
	if events[1].Detail != "protected" {
		t.Fatalf("second event lost its detail: %+v", events[1])
	}
}

// The command and timestamp are filled in when absent, so every call site does
// not have to remember, and a record without a time is useless for auditing.
func TestRecordFillsInCommandAndTime(t *testing.T) {
	path := filepath.Join(t.TempDir(), "operations.log")
	writer, err := Open(path, "uninstall")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	writer.Record(Event{Kind: KindStaged, Path: "/tmp/x"})
	writer.Close()

	event := readLines(t, path)[0]
	if event.Command != "uninstall" {
		t.Fatalf("command = %q, want uninstall", event.Command)
	}
	if event.Time.IsZero() {
		t.Fatal("an event with no time is useless for auditing")
	}
	if time.Since(event.Time) > time.Minute {
		t.Fatalf("time looks wrong: %v", event.Time)
	}
}

// An explicit time and command are kept, since undo records events describing
// an earlier operation.
func TestRecordKeepsExplicitFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "operations.log")
	writer, _ := Open(path, "clean")
	when := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	writer.Record(Event{Kind: KindRestored, Command: "undo", Time: when})
	writer.Close()

	event := readLines(t, path)[0]
	if event.Command != "undo" {
		t.Fatalf("command = %q, want the explicit undo", event.Command)
	}
	if !event.Time.Equal(when) {
		t.Fatalf("time = %v, want the explicit %v", event.Time, when)
	}
}

// Two runs write to one log at once by design, and one run's blocks use
// several goroutines. Interleaved lines are acceptable; a corrupted line is
// not.
func TestConcurrentRecordsStayWellFormed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "operations.log")
	writer, err := Open(path, "clean")
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				writer.Record(Event{
					Kind: KindPurged,
					Path: fmt.Sprintf("/tmp/worker-%d/item-%d", n, j),
				})
			}
		}(i)
	}
	wg.Wait()
	writer.Close()

	// readLines fails the test if any line is not valid JSON.
	if events := readLines(t, path); len(events) != 16*50 {
		t.Fatalf("wrote %d events, want %d", len(events), 16*50)
	}
}

func TestDiscardAcceptsEventsAndDropsThem(t *testing.T) {
	writer := Discard()
	writer.Record(Event{Kind: KindPurged, Path: "/tmp/x"})
	if err := writer.Err(); err != nil {
		t.Fatalf("discard should not error: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("closing discard should not error: %v", err)
	}
}

// A nil writer is what a caller passing no log looks like, and every method
// has to survive it, since the alternative is a panic in the middle of a
// deletion.
func TestNilWriterIsSafe(t *testing.T) {
	var writer *Writer
	writer.Record(Event{Kind: KindPurged})
	if err := writer.Err(); err != nil {
		t.Fatalf("nil writer Err = %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("nil writer Close = %v", err)
	}
}

func TestRecordAfterCloseIsIgnored(t *testing.T) {
	path := filepath.Join(t.TempDir(), "operations.log")
	writer, _ := Open(path, "clean")
	writer.Record(Event{Kind: KindPurged, Path: "/tmp/before"})
	writer.Close()
	writer.Record(Event{Kind: KindPurged, Path: "/tmp/after"})

	if events := readLines(t, path); len(events) != 1 {
		t.Fatalf("a record after close was written: %d events", len(events))
	}
}

// growLog writes filler until the file is at least n bytes.
func growLog(t *testing.T, path string, n int64) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("setup: %v", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer file.Close()

	line := append([]byte(`{"kind":"filler","path":"`+strings.Repeat("x", 200)+`"}`), '\n')
	for written := int64(0); written < n; written += int64(len(line)) {
		if _, err := file.Write(line); err != nil {
			t.Fatalf("setup: %v", err)
		}
	}
}

// The point of the whole change: the log stops growing without limit.
func TestOversizedLogIsRotated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "operations.log")
	growLog(t, path, maxLogBytes+1024)

	writer, err := Open(path, "clean")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := writer.Err(); err != nil {
		t.Fatalf("rotation reported: %v", err)
	}
	writer.Record(Event{Kind: KindPurged, Path: "/tmp/fresh"})
	writer.Close()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("the active log should exist: %v", err)
	}
	if info.Size() >= maxLogBytes {
		t.Fatalf("the active log is still %d bytes, it was not rotated", info.Size())
	}

	// The old content moved rather than vanished. This log is the only record
	// that survives a permanent deletion, so rotation must never discard it.
	rotated, err := os.Stat(numbered(path, 1))
	if err != nil {
		t.Fatalf("the rotated log should exist: %v", err)
	}
	if rotated.Size() < maxLogBytes {
		t.Fatalf("the rotated log is only %d bytes, content was lost", rotated.Size())
	}

	events := readLines(t, path)
	if len(events) != 1 || events[0].Path != "/tmp/fresh" {
		t.Fatalf("the new log should hold only the new record, got %+v", events)
	}
}

func TestLogUnderTheLimitIsNotRotated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "operations.log")
	growLog(t, path, 1024)

	writer, _ := Open(path, "clean")
	writer.Record(Event{Kind: KindPurged, Path: "/tmp/x"})
	writer.Close()

	if _, err := os.Stat(numbered(path, 1)); !os.IsNotExist(err) {
		t.Fatal("a log under the limit should not have been rotated")
	}
}

// Older files shift along, and only the oldest is dropped, so history goes
// back several rotations rather than one.
func TestRotationShiftsOlderFilesAndDropsOnlyTheOldest(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "operations.log")

	for i := 1; i <= keptLogFiles; i++ {
		if err := os.WriteFile(numbered(path, i),
			[]byte(fmt.Sprintf("generation %d\n", i)), 0o600); err != nil {
			t.Fatalf("setup: %v", err)
		}
	}
	growLog(t, path, maxLogBytes+1024)

	writer, err := Open(path, "clean")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := writer.Err(); err != nil {
		t.Fatalf("rotation reported: %v", err)
	}
	writer.Close()

	// Each generation moved one place along.
	for i := 2; i <= keptLogFiles; i++ {
		contents, err := os.ReadFile(numbered(path, i))
		if err != nil {
			t.Fatalf("generation at .%d is missing: %v", i, err)
		}
		want := fmt.Sprintf("generation %d\n", i-1)
		if string(contents) != want {
			t.Fatalf(".%d holds %q, want %q", i, contents, want)
		}
	}

	// The oldest was dropped rather than kept forever.
	if _, err := os.Stat(numbered(path, keptLogFiles+1)); !os.IsNotExist(err) {
		t.Fatal("rotation kept more files than it promised")
	}

	// The ceiling holds: the active log plus exactly keptLogFiles others.
	matches, _ := filepath.Glob(path + "*")
	if len(matches) != keptLogFiles+1 {
		t.Fatalf("staging area holds %d log files, want %d", len(matches), keptLogFiles+1)
	}
}

// A run holding the log open when another run rotates it keeps writing into
// the renamed file. Its records move rather than being lost, which is why
// rotation renames instead of truncating.
func TestRecordsFromAnOpenWriterSurviveRotation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "operations.log")

	// The first run opens a log small enough not to rotate, so its records go
	// into the file that the second run will later move aside. Growing it
	// before this point would make the first Open do the rotating, which is a
	// different situation and the one an earlier version of this test
	// accidentally set up.
	first, err := Open(path, "clean")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	first.Record(Event{Kind: KindPurged, Path: "/tmp/written-before-rotation"})

	// Activity pushes the log past the limit while the first run still holds
	// it open.
	growLog(t, path, maxLogBytes+1024)

	// A second run opens the same path and rotates it out from under the first.
	second, err := Open(path, "purge")
	if err != nil {
		t.Fatalf("second open: %v", err)
	}
	first.Record(Event{Kind: KindPurged, Path: "/tmp/written-after-rotation"})
	first.Close()
	second.Record(Event{Kind: KindPurged, Path: "/tmp/from-the-second-run"})
	second.Close()

	rotated, err := os.ReadFile(numbered(path, 1))
	if err != nil {
		t.Fatalf("rotated log missing: %v", err)
	}
	for _, want := range []string{"written-before-rotation", "written-after-rotation"} {
		if !strings.Contains(string(rotated), want) {
			t.Errorf("%q was lost by rotation", want)
		}
	}

	active, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("active log missing: %v", err)
	}
	if !strings.Contains(string(active), "from-the-second-run") {
		t.Error("the second run's record is missing from the active log")
	}
}

// A log that cannot be rotated must still be usable, because refusing to
// delete over bookkeeping trades a real problem for a smaller one. The failure
// is remembered rather than dropped.
func TestRotationFailureIsReportedButDoesNotBlockLogging(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("running as root, which bypasses the permissions under test")
	}

	dir := filepath.Join(t.TempDir(), "logs")
	path := filepath.Join(dir, "operations.log")
	growLog(t, path, maxLogBytes+1024)

	// The directory holding the log is made unwritable, so renaming within it
	// fails. An earlier version of this test put a directory where the rotated
	// file should go, which does not fail at all: the shifting loop simply
	// renames that directory along to the next number like anything else.
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("setup: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	writer, err := Open(path, "clean")
	if err != nil {
		t.Fatalf("a failed rotation must not stop the log opening: %v", err)
	}
	writer.Record(Event{Kind: KindPurged, Path: "/tmp/still-recorded"})
	reported := writer.Err()
	writer.Close()

	if reported == nil {
		t.Fatal("a failed rotation should be reported through Err")
	}
	contents, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(contents), "still-recorded") {
		t.Fatalf("logging should have continued regardless: %v", err)
	}
}

func TestDefaultPathIsUnderTheUserLibrary(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	path, err := DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath: %v", err)
	}
	want := filepath.Join(home, "Library", "Logs", "wtff", "operations.log")
	if path != want {
		t.Fatalf("DefaultPath = %q, want %q", path, want)
	}
}

// The log records what was removed, so it must not be readable by other users
// on a shared machine.
func TestLogIsOwnerOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "operations.log")
	writer, err := Open(path, "clean")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	writer.Record(Event{Kind: KindPurged, Path: "/tmp/x"})
	writer.Close()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("log permissions are %o, want 600", perm)
	}
	dirInfo, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if perm := dirInfo.Mode().Perm(); perm != 0o700 {
		t.Fatalf("log directory permissions are %o, want 700", perm)
	}
}
