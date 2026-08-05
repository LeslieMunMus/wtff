package uninstallcore

import "testing"

func sampleApps() []InstalledApp {
	return []InstalledApp{
		{Path: "/Applications/Example App.app", BundleID: "com.example.exampleapp", DisplayName: "Example App"},
		{Path: "/Applications/Other Thing.app", BundleID: "com.other.thing", DisplayName: "Other Thing"},
		{Path: "/Applications/Code.app", BundleID: "com.example.code", DisplayName: "Code"},
		{Path: "/Applications/CodeEditorPro.app", BundleID: "com.example.codeeditorpro", DisplayName: "Code Editor Pro"},
	}
}

func TestFindAppMatchesByExactDisplayName(t *testing.T) {
	matches := FindApp(sampleApps(), "Example App")
	if len(matches) != 1 || matches[0].BundleID != "com.example.exampleapp" {
		t.Fatalf("matches = %+v", matches)
	}
}

func TestFindAppMatchesByExactBundleID(t *testing.T) {
	matches := FindApp(sampleApps(), "com.other.thing")
	if len(matches) != 1 || matches[0].DisplayName != "Other Thing" {
		t.Fatalf("matches = %+v", matches)
	}
}

func TestFindAppMatchIsCaseInsensitive(t *testing.T) {
	matches := FindApp(sampleApps(), "EXAMPLE APP")
	if len(matches) != 1 {
		t.Fatalf("matches = %+v", matches)
	}
}

// This is the central discipline of matching: "Code" must never match "Code
// Editor Pro" just because one contains the other's letters. A substring
// match here is how a person uninstalling one application ends up staging
// leftovers for a different one entirely.
func TestFindAppDoesNotMatchSubstrings(t *testing.T) {
	matches := FindApp(sampleApps(), "Code")
	if len(matches) != 1 {
		t.Fatalf("matches = %+v, want exactly the app named Code and nothing that merely contains it", matches)
	}
	if matches[0].DisplayName != "Code" {
		t.Fatalf("matched %q instead of the exact match", matches[0].DisplayName)
	}
}

func TestFindAppReportsNoMatchesRatherThanGuessing(t *testing.T) {
	matches := FindApp(sampleApps(), "Nonexistent Application")
	if matches != nil {
		t.Fatalf("matches = %+v, want none", matches)
	}
}

func TestFindAppReportsAllAmbiguousMatches(t *testing.T) {
	apps := []InstalledApp{
		{Path: "/Applications/Notes.app", BundleID: "com.first.notes", DisplayName: "Notes"},
		{Path: "/Users/example/Applications/Notes.app", BundleID: "com.second.notes", DisplayName: "Notes"},
	}
	matches := FindApp(apps, "Notes")
	if len(matches) != 2 {
		t.Fatalf("matches = %+v, want both ambiguous candidates reported", matches)
	}
}

func TestFindAppEmptyQueryMatchesNothing(t *testing.T) {
	if matches := FindApp(sampleApps(), ""); matches != nil {
		t.Fatalf("matches = %+v, want none for an empty query", matches)
	}
	if matches := FindApp(sampleApps(), "   "); matches != nil {
		t.Fatalf("matches = %+v, want none for a whitespace-only query", matches)
	}
}
