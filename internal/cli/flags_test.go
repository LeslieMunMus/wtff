package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const samplePlist = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
<key>CFBundleIdentifier</key>
<string>com.example.sampleapp</string>
<key>CFBundleDisplayName</key>
<string>Sample App</string>
</dict>
</plist>
`

func TestReorderFlagsFirstMovesFlagsToFront(t *testing.T) {
	got := reorderFlagsFirst(
		[]string{"/tmp/a", "--dry-run", "/tmp/b"},
		commonBoolFlags,
	)
	want := []string{"--dry-run", "/tmp/a", "/tmp/b"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestReorderFlagsFirstPreservesOrderWithinEachGroup(t *testing.T) {
	got := reorderFlagsFirst(
		[]string{"/tmp/a", "--purge", "/tmp/b", "--yes", "/tmp/c"},
		commonBoolFlags,
	)
	want := []string{"--purge", "--yes", "/tmp/a", "/tmp/b", "/tmp/c"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestReorderFlagsFirstLeavesFlagsFirstOrderUnchanged(t *testing.T) {
	got := reorderFlagsFirst(
		[]string{"--dry-run", "--yes", "/tmp/a"},
		commonBoolFlags,
	)
	want := []string{"--dry-run", "--yes", "/tmp/a"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// An unrecognized flag-like token is not silently swallowed into the flags
// group; it stays a positional so flag.Parse still reports it as unknown,
// which is the correct behavior for a typo like --dryrun.
func TestReorderFlagsFirstLeavesUnknownFlagsAsPositional(t *testing.T) {
	got := reorderFlagsFirst(
		[]string{"/tmp/a", "--dryrun"},
		commonBoolFlags,
	)
	want := []string{"/tmp/a", "--dryrun"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// A bare "--" ends reordering, matching flag.FlagSet's own convention, so a
// path that happens to start with a dash can still be named literally rather
// than misread as a flag.
func TestReorderFlagsFirstRespectsDoubleDashTerminator(t *testing.T) {
	got := reorderFlagsFirst(
		[]string{"/tmp/a", "--", "--dry-run", "--purge"},
		commonBoolFlags,
	)
	want := []string{"/tmp/a", "--", "--dry-run", "--purge"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// This is the exact case that motivated the fix: a flag placed after the
// positional argument it modifies must still take effect. Run through the
// public Run entrypoint against a real file, not just the reordering helper
// in isolation, so the fix is proven where it matters.
func TestRemoveHonorsDryRunPlacedAfterThePath(t *testing.T) {
	home := newFixtureHome(t)
	target := filepath.Join(home, "scratch", "cache.bin")
	writeFixtureFile(t, target, "payload")

	var out, errOut bytes.Buffer
	code := Run([]string{"remove", target, "--dry-run"}, strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "dry run") {
		t.Fatalf("output did not confirm dry run: %s", out.String())
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatal("--dry-run placed after the path still removed the file")
	}
}

func TestUninstallHonorsDryRunPlacedAfterTheQuery(t *testing.T) {
	home := newFixtureHome(t)
	appPath := filepath.Join(home, "Applications", "Sample App.app")
	writeFixtureFile(t, filepath.Join(appPath, "Contents", "Info.plist"), samplePlist)

	var out, errOut bytes.Buffer
	code := Run([]string{"uninstall", "Sample App", "--dry-run"}, strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "dry run") {
		t.Fatalf("output did not confirm dry run: %s", out.String())
	}
	if _, err := os.Stat(appPath); err != nil {
		t.Fatal("--dry-run placed after the query still removed the app")
	}
}
