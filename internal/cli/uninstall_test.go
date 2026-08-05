package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFakeInstalledApp(t *testing.T, appsRoot, name, bundleID string) string {
	t.Helper()
	appPath := filepath.Join(appsRoot, name+".app")
	plist := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
<key>CFBundleIdentifier</key>
<string>` + bundleID + `</string>
<key>CFBundleDisplayName</key>
<string>` + name + `</string>
</dict>
</plist>
`
	writeFixtureFile(t, filepath.Join(appPath, "Contents", "Info.plist"), plist)
	return appPath
}

// The full lifecycle through the public entrypoint, against a real search
// root and a real Info.plist read by the real plutil binary: discover,
// stage, and undo, with restored content verified rather than only exit
// codes. This is the same rigor TestRemoveStageListAndUndoRoundTrip and
// TestCleanStagesAndUndoRestoresRealDiscoveredItems already apply.
func TestUninstallStagesAppAndLeftoversAndUndoRestoresThem(t *testing.T) {
	home := newFixtureHome(t)
	appsRoot := filepath.Join(home, "Applications")
	appPath := writeFakeInstalledApp(t, appsRoot, "SampleApp", "com.example.sampleapp")

	supportDir := filepath.Join(home, "Library", "Application Support", "com.example.sampleapp")
	writeFixtureFile(t, filepath.Join(supportDir, "config.json"), "app data")
	cacheDir := filepath.Join(home, "Library", "Caches", "com.example.sampleapp")
	writeFixtureFile(t, filepath.Join(cacheDir, "blob.bin"), "cache data")

	var removeOut, removeErr bytes.Buffer
	code := Run([]string{"uninstall", "SampleApp", "--yes"}, strings.NewReader(""), &removeOut, &removeErr)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, removeErr.String())
	}
	for _, path := range []string{appPath, supportDir, cacheDir} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("%s was not removed", path)
		}
	}

	batchID := extractBatchID(t, removeOut.String())
	var undoOut, undoErr bytes.Buffer
	if code := Run([]string{"undo", batchID}, strings.NewReader(""), &undoOut, &undoErr); code != 0 {
		t.Fatalf("undo exit code = %d, stderr = %s", code, undoErr.String())
	}

	contents, err := os.ReadFile(filepath.Join(supportDir, "config.json"))
	if err != nil || string(contents) != "app data" {
		t.Fatalf("support data not restored correctly: %q, %v", contents, err)
	}
	if _, err := os.Stat(appPath); err != nil {
		t.Fatalf("app bundle not restored: %v", err)
	}
}

func TestUninstallRefusesAppleApplications(t *testing.T) {
	home := newFixtureHome(t)
	appPath := writeFakeInstalledApp(t, filepath.Join(home, "Applications"), "FakeSafari", "com.apple.FakeSafari")

	var out, errOut bytes.Buffer
	code := Run([]string{"uninstall", "FakeSafari", "--yes"}, strings.NewReader(""), &out, &errOut)
	if code == 0 {
		t.Fatal("uninstalling an Apple-identified application should not succeed")
	}
	if !strings.Contains(errOut.String(), "Apple's own applications") {
		t.Fatalf("stderr = %q, want an explanation naming the reason", errOut.String())
	}
	if _, err := os.Stat(appPath); err != nil {
		t.Fatalf("the app was removed despite the protection: %v", err)
	}
}

func TestUninstallReportsNoMatchClearly(t *testing.T) {
	newFixtureHome(t)
	var out, errOut bytes.Buffer
	code := Run([]string{"uninstall", "Nothing Installed By This Name"}, strings.NewReader(""), &out, &errOut)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), "no installed application matches") {
		t.Fatalf("stderr = %q", errOut.String())
	}
}

// Ambiguity is reported, never silently resolved by picking one candidate.
// Uninstalling the wrong one of two same-named applications is exactly the
// kind of mistake that should never be possible to make by accident.
//
// Two directories cannot share a name in one parent, so two distinct .app
// filenames are used, with the collision constructed across match fields
// instead: the first app's display name and the second app's bundle
// identifier are both the query string. FindApp checks display name, bundle
// identifier, and filename with equal weight, so this is a realistic way two
// installed applications end up matching the same query, not a contrived
// same-name case a real filesystem could not produce.
func TestUninstallReportsAmbiguousMatchesRatherThanGuessing(t *testing.T) {
	home := newFixtureHome(t)
	appsRoot := filepath.Join(home, "Applications")

	firstApp := writeFakeInstalledApp(t, appsRoot, "Notes", "com.first.notes")
	secondApp := filepath.Join(appsRoot, "AnotherNotesApp.app")
	writeFixtureFile(t, filepath.Join(secondApp, "Contents", "Info.plist"), `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
<key>CFBundleIdentifier</key>
<string>Notes</string>
<key>CFBundleDisplayName</key>
<string>Another Notes App</string>
</dict>
</plist>
`)

	var out, errOut bytes.Buffer
	code := Run([]string{"uninstall", "Notes", "--yes"}, strings.NewReader(""), &out, &errOut)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2 for an ambiguous match", code)
	}
	if !strings.Contains(errOut.String(), "matches more than one") {
		t.Fatalf("stderr = %q", errOut.String())
	}
	if _, err := os.Stat(firstApp); err != nil {
		t.Fatalf("an ambiguous match should not remove anything: %v", err)
	}
	if _, err := os.Stat(secondApp); err != nil {
		t.Fatalf("an ambiguous match should not remove anything: %v", err)
	}
}

func TestUninstallRequiresExactlyOneArgument(t *testing.T) {
	newFixtureHome(t)
	var out, errOut bytes.Buffer
	if code := Run([]string{"uninstall"}, strings.NewReader(""), &out, &errOut); code != 2 {
		t.Fatalf("exit code with no argument = %d, want 2", code)
	}
	errOut.Reset()
	if code := Run([]string{"uninstall", "One", "Two"}, strings.NewReader(""), &out, &errOut); code != 2 {
		t.Fatalf("exit code with two arguments = %d, want 2", code)
	}
}

func TestUninstallDryRunChangesNothing(t *testing.T) {
	home := newFixtureHome(t)
	appPath := writeFakeInstalledApp(t, filepath.Join(home, "Applications"), "SampleApp", "com.example.sampleapp")

	var out, errOut bytes.Buffer
	code := Run([]string{"uninstall", "SampleApp", "--dry-run"}, strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, errOut.String())
	}
	if _, err := os.Stat(appPath); err != nil {
		t.Fatal("dry run removed the app bundle")
	}
}
