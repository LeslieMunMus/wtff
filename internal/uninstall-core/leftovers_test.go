package uninstallcore

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestDiscoverLeftoversBuildsExpectedPaths(t *testing.T) {
	home := "/Users/example"
	app := InstalledApp{
		Path: "/Applications/Example App.app", BundleID: "com.example.exampleapp",
		DisplayName: "Example App",
	}

	candidates := DiscoverLeftovers(app, home)
	paths := make(map[string]bool, len(candidates))
	for _, c := range candidates {
		paths[c.Path] = true
	}

	mustExist := []string{
		filepath.Join(home, "Library/Application Support/com.example.exampleapp"),
		filepath.Join(home, "Library/Caches/com.example.exampleapp"),
		filepath.Join(home, "Library/Preferences/com.example.exampleapp.plist"),
		filepath.Join(home, "Library/Containers/com.example.exampleapp"),
		filepath.Join(home, "Library/Application Support/Example App"),
		filepath.Join(home, "Library/Caches/Example App"),
	}
	for _, want := range mustExist {
		if !paths[want] {
			t.Errorf("missing expected candidate: %s", want)
		}
	}
}

// Every candidate must carry a rule id and reason, and the reason must name
// which evidence justified it, since that is what a person reviewing a plan
// relies on to judge whether a match makes sense.
func TestDiscoverLeftoversExplainsItsEvidence(t *testing.T) {
	app := InstalledApp{BundleID: "com.example.app", DisplayName: "Example"}
	for _, c := range DiscoverLeftovers(app, "/Users/example") {
		if c.RuleID == "" {
			t.Fatalf("candidate %s has no rule id", c.Path)
		}
		if !strings.Contains(c.Reason, "com.example.app") && !strings.Contains(c.Reason, "Example") {
			t.Fatalf("reason %q does not name the evidence that justified it", c.Reason)
		}
	}
}

// The regression this test exists for: an empty bundle identifier or display
// name must never reach a template, since the single %s in each template sits
// at the final path component and an empty value collapses the candidate
// into the shared parent directory rather than a specific app's entry within
// it.
func TestDiscoverLeftoversRefusesToWidenOnEmptyIdentity(t *testing.T) {
	app := InstalledApp{BundleID: "", DisplayName: ""}
	candidates := DiscoverLeftovers(app, "/Users/example")
	if len(candidates) != 0 {
		for _, c := range candidates {
			t.Logf("unexpected candidate: %s", c.Path)
		}
		t.Fatalf("got %d candidates from an app with no identity, want 0", len(candidates))
	}
}

func TestDiscoverLeftoversHandlesEmptyBundleIDWithDisplayNamePresent(t *testing.T) {
	app := InstalledApp{BundleID: "", DisplayName: "Example"}
	candidates := DiscoverLeftovers(app, "/Users/example")
	for _, c := range candidates {
		if strings.HasSuffix(c.Path, "Application Support") || strings.HasSuffix(c.Path, "Caches") {
			t.Fatalf("candidate %s collapsed into a shared parent directory", c.Path)
		}
	}
	if len(candidates) != len(displayNameTemplates) {
		t.Fatalf("got %d candidates, want exactly the display-name-only set (%d)",
			len(candidates), len(displayNameTemplates))
	}
}
