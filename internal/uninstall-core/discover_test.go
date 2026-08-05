package uninstallcore

import (
	"os"
	"path/filepath"
	"testing"
)

// writeFakeApp creates a minimal .app bundle with a real Info.plist,
// readable by the real plutil binary. Tests in this package exercise the
// actual plutil invocation rather than a mock of it, since a wrong assumption
// about plutil's output shape is exactly the kind of thing a mock would hide.
func writeFakeApp(t *testing.T, root, name string, plistXML string) string {
	t.Helper()
	appPath := filepath.Join(root, name+".app")
	contentsDir := filepath.Join(appPath, "Contents")
	if err := os.MkdirAll(contentsDir, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(contentsDir, "Info.plist"), []byte(plistXML), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	return appPath
}

func plistEntry(key, value string) string {
	return "<key>" + key + "</key>\n<string>" + value + "</string>\n"
}

func TestDiscoverAppsReadsRealBundleIdentity(t *testing.T) {
	root := t.TempDir()
	writeFakeApp(t, root, "Example App", plistTemplateWith(
		plistEntry("CFBundleIdentifier", "com.example.exampleapp")+
			plistEntry("CFBundleDisplayName", "Example App")+
			plistEntry("CFBundleShortVersionString", "1.2.3"),
	))

	apps, skipped, err := DiscoverApps([]string{root})
	if err != nil {
		t.Fatalf("DiscoverApps: %v", err)
	}
	if len(skipped) != 0 {
		t.Fatalf("unexpected skips: %+v", skipped)
	}
	if len(apps) != 1 {
		t.Fatalf("got %d apps, want 1", len(apps))
	}
	app := apps[0]
	if app.BundleID != "com.example.exampleapp" {
		t.Errorf("BundleID = %q", app.BundleID)
	}
	if app.DisplayName != "Example App" {
		t.Errorf("DisplayName = %q", app.DisplayName)
	}
	if app.Version != "1.2.3" {
		t.Errorf("Version = %q", app.Version)
	}
}

func plistTemplateWith(body string) string {
	return "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n" +
		"<!DOCTYPE plist PUBLIC \"-//Apple//DTD PLIST 1.0//EN\" \"http://www.apple.com/DTDs/PropertyList-1.0.dtd\">\n" +
		"<plist version=\"1.0\">\n<dict>\n" + body + "</dict>\n</plist>\n"
}

// CFBundleDisplayName is optional; several real applications only set
// CFBundleName. The fallback chain must actually work, not just compile.
func TestDiscoverAppsFallsBackToCFBundleName(t *testing.T) {
	root := t.TempDir()
	writeFakeApp(t, root, "Fallback App", plistTemplateWith(
		plistEntry("CFBundleIdentifier", "com.example.fallback")+
			plistEntry("CFBundleName", "Fallback From Name"),
	))

	apps, _, err := DiscoverApps([]string{root})
	if err != nil {
		t.Fatalf("DiscoverApps: %v", err)
	}
	if len(apps) != 1 || apps[0].DisplayName != "Fallback From Name" {
		t.Fatalf("apps = %+v", apps)
	}
}

// Neither display name key is required. The final fallback is the bundle's
// own filename, which always exists.
func TestDiscoverAppsFallsBackToFilename(t *testing.T) {
	root := t.TempDir()
	writeFakeApp(t, root, "Nameless Wonder", plistTemplateWith(
		plistEntry("CFBundleIdentifier", "com.example.nameless"),
	))

	apps, _, err := DiscoverApps([]string{root})
	if err != nil {
		t.Fatalf("DiscoverApps: %v", err)
	}
	if len(apps) != 1 || apps[0].DisplayName != "Nameless Wonder" {
		t.Fatalf("apps = %+v", apps)
	}
}

// A bundle with no identifier at all is unusual and is skipped rather than
// carried forward with an empty identifier, since every leftover path this
// package builds depends on having a real one.
func TestDiscoverAppsSkipsBundlesWithNoIdentifier(t *testing.T) {
	root := t.TempDir()
	writeFakeApp(t, root, "No Identifier", plistTemplateWith(
		plistEntry("CFBundleName", "No Identifier"),
	))

	apps, skipped, err := DiscoverApps([]string{root})
	if err != nil {
		t.Fatalf("DiscoverApps: %v", err)
	}
	if len(apps) != 0 {
		t.Fatalf("got %d apps, want 0", len(apps))
	}
	if len(skipped) != 1 {
		t.Fatalf("got %d skips, want 1", len(skipped))
	}
}

func TestDiscoverAppsSkipsNonAppEntriesAndMissingRoots(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "not-an-app.txt"), []byte("x"), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "PlainFolder"), 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}

	apps, skipped, err := DiscoverApps([]string{root, filepath.Join(root, "does-not-exist")})
	if err != nil {
		t.Fatalf("DiscoverApps: %v", err)
	}
	if len(apps) != 0 || len(skipped) != 0 {
		t.Fatalf("apps=%+v skipped=%+v, want both empty", apps, skipped)
	}
}

func TestDiscoverAppsScansMultipleRoots(t *testing.T) {
	rootA, rootB := t.TempDir(), t.TempDir()
	writeFakeApp(t, rootA, "App One", plistTemplateWith(
		plistEntry("CFBundleIdentifier", "com.example.one")))
	writeFakeApp(t, rootB, "App Two", plistTemplateWith(
		plistEntry("CFBundleIdentifier", "com.example.two")))

	apps, _, err := DiscoverApps([]string{rootA, rootB})
	if err != nil {
		t.Fatalf("DiscoverApps: %v", err)
	}
	if len(apps) != 2 {
		t.Fatalf("got %d apps, want 2", len(apps))
	}
}

func TestIsPlausibleBundleIDRejectsPathLikeValues(t *testing.T) {
	cases := map[string]bool{
		"com.example.app":     true,
		"com.example.sub.app": true,
		"":                    false,
		"has/a/slash":         false,
		"has\x00null":         false,
		"has\x01control":      false,
	}
	for id, want := range cases {
		if got := isPlausibleBundleID(id); got != want {
			t.Errorf("isPlausibleBundleID(%q) = %v, want %v", id, got, want)
		}
	}
}
