package uninstallcore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// redirectSystemDirs points the system locations at a fixture tree for one
// test. The real ones need root to write, so without this the checks could
// only observe whatever happens to be on the machine running the test, which
// on a machine with no drivers installed means never firing at all.
func redirectSystemDirs(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	previousHelpers, previousDaemons := privilegedHelperDir, launchDaemonDir
	previousAgents, previousKexts := launchAgentDir, systemKextDir
	t.Cleanup(func() {
		privilegedHelperDir, launchDaemonDir = previousHelpers, previousDaemons
		launchAgentDir, systemKextDir = previousAgents, previousKexts
	})

	privilegedHelperDir = filepath.Join(root, "PrivilegedHelperTools")
	launchDaemonDir = filepath.Join(root, "LaunchDaemons")
	launchAgentDir = filepath.Join(root, "LaunchAgents")
	systemKextDir = filepath.Join(root, "Extensions")
	for _, dir := range []string{privilegedHelperDir, launchDaemonDir, launchAgentDir, systemKextDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("setup: %v", err)
		}
	}
	return root
}

// makeApp builds a minimal .app bundle with a readable Info.plist.
func makeApp(t *testing.T, dir, name, bundleID string) InstalledApp {
	t.Helper()
	appPath := filepath.Join(dir, name+".app")
	contents := filepath.Join(appPath, "Contents")
	if err := os.MkdirAll(contents, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	plist := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>CFBundleIdentifier</key><string>` + bundleID + `</string>
<key>CFBundleName</key><string>` + name + `</string>
</dict></plist>`
	if err := os.WriteFile(filepath.Join(contents, "Info.plist"), []byte(plist), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	return InstalledApp{Path: appPath, BundleID: bundleID, DisplayName: name}
}

func writePlist(t *testing.T, path string, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	plist := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>` + body + `</dict></plist>`
	if err := os.WriteFile(path, []byte(plist), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
}

// A network filter, a VPN client, and an endpoint security agent all ship as
// system extensions, because since macOS 10.15 that is the only supported way
// to ship one. This is the check that stands between a person and removing
// their own security software.
func TestAppShippingASystemExtensionIsRefused(t *testing.T) {
	redirectSystemDirs(t)
	app := makeApp(t, t.TempDir(), "SecurityAgent", "com.vendor.securityagent")

	ext := filepath.Join(app.Path, bundleSystemExtensionDir, "com.vendor.filter.systemextension")
	if err := os.MkdirAll(ext, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}

	integration := InspectSystemIntegration(app, nil)
	if !integration.Disqualifying() {
		t.Fatal("an app shipping a system extension must be disqualifying")
	}

	reason, protected := IsProtectedApp(app)
	if !protected {
		t.Fatal("IsProtectedApp must refuse an app shipping a system extension")
	}
	if !strings.Contains(reason, "system extension") {
		t.Fatalf("the refusal should say what was found, got: %s", reason)
	}
	if !strings.Contains(reason, ext) {
		t.Fatalf("the refusal should name the path so the claim can be checked, got: %s", reason)
	}
}

func TestAppShippingAKernelExtensionIsRefused(t *testing.T) {
	redirectSystemDirs(t)
	app := makeApp(t, t.TempDir(), "RaidDriver", "com.vendor.raiddriver")

	kext := filepath.Join(app.Path, bundleKextDir, "VendorRaid.kext")
	if err := os.MkdirAll(kext, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}

	reason, protected := IsProtectedApp(app)
	if !protected {
		t.Fatal("an app shipping a kernel extension must be refused")
	}
	if !strings.Contains(reason, "kernel extension") {
		t.Fatalf("the refusal should say what was found, got: %s", reason)
	}
}

// A driver installed system wide is still that vendor's driver even when the
// copy inside the bundle is gone, which is the normal state after the
// installer has run.
func TestSystemWideKernelExtensionIsAttributedToItsVendor(t *testing.T) {
	redirectSystemDirs(t)
	app := makeApp(t, t.TempDir(), "Controller", "com.vendor.controller")

	kext := filepath.Join(systemKextDir, "VendorDriver.kext")
	writePlist(t, filepath.Join(kext, "Contents", "Info.plist"),
		`<key>CFBundleIdentifier</key><string>com.vendor.driver</string>`)

	integration := InspectSystemIntegration(app, nil)
	if len(integration.KernelExtensions) != 1 {
		t.Fatalf("expected the vendor's system wide kext, got %v", integration.KernelExtensions)
	}
	if _, protected := IsProtectedApp(app); !protected {
		t.Fatal("an app whose vendor owns a loaded kext must be refused")
	}
}

// A different vendor's driver must not block an unrelated application, or the
// check refuses everything on any machine with any driver installed.
func TestAnotherVendorsKernelExtensionIsIgnored(t *testing.T) {
	redirectSystemDirs(t)
	app := makeApp(t, t.TempDir(), "Notes", "com.othervendor.notes")

	writePlist(t, filepath.Join(systemKextDir, "VendorDriver.kext", "Contents", "Info.plist"),
		`<key>CFBundleIdentifier</key><string>com.vendor.driver</string>`)

	if _, protected := IsProtectedApp(app); protected {
		t.Fatal("an unrelated vendor's driver must not block this app")
	}
}

// The vendor namespace is the first two components. Reducing it to one would
// match every identifier on the machine and refuse everything.
func TestVendorPrefixNeedsTwoComponents(t *testing.T) {
	if got := vendorPrefix("com.vendor.product"); got != "com.vendor" {
		t.Fatalf("vendorPrefix = %q, want com.vendor", got)
	}
	for _, tooShort := range []string{"com.vendor", "single", ""} {
		if got := vendorPrefix(tooShort); got != "" {
			t.Fatalf("vendorPrefix(%q) = %q, want empty", tooShort, got)
		}
	}
}

// A launchd job running a program inside the bundle will point at nothing once
// the bundle is deleted.
func TestLaunchJobInsideTheBundleIsReported(t *testing.T) {
	redirectSystemDirs(t)
	app := makeApp(t, t.TempDir(), "Syncer", "com.vendor.syncer")

	job := filepath.Join(launchDaemonDir, "com.vendor.syncer.helper.plist")
	writePlist(t, job, `<key>Program</key><string>`+
		filepath.Join(app.Path, "Contents", "MacOS", "helper")+`</string>`)

	integration := InspectSystemIntegration(app, nil)
	if len(integration.LaunchDaemons) != 1 || integration.LaunchDaemons[0] != job {
		t.Fatalf("expected the in-bundle job, got %v", integration.LaunchDaemons)
	}
	if !integration.Orphans() {
		t.Fatal("an in-bundle launch job should be reported as left behind")
	}
	if integration.Disqualifying() {
		t.Fatal("a launch job alone must not refuse the uninstall")
	}
}

// ProgramArguments is the more common form, and a job using it has no Program
// key at all.
func TestLaunchJobUsingProgramArgumentsIsReported(t *testing.T) {
	redirectSystemDirs(t)
	app := makeApp(t, t.TempDir(), "Agent", "com.vendor.agent")

	writePlist(t, filepath.Join(launchAgentDir, "com.vendor.agent.plist"),
		`<key>ProgramArguments</key><array><string>`+
			filepath.Join(app.Path, "Contents", "MacOS", "agent")+
			`</string><string>--daemon</string></array>`)

	if integration := InspectSystemIntegration(app, nil); len(integration.LaunchDaemons) != 1 {
		t.Fatalf("expected the job, got %v", integration.LaunchDaemons)
	}
}

// The defect this replaced: matching a job by shared vendor namespace
// attributed an Oracle database daemon and a Java updater to MySQL Workbench,
// which installed neither.
func TestLaunchJobSharingOnlyAVendorPrefixIsNotAttributed(t *testing.T) {
	redirectSystemDirs(t)
	app := makeApp(t, t.TempDir(), "Workbench", "com.vendor.workbench")

	writePlist(t, filepath.Join(launchDaemonDir, "com.vendor.database.plist"),
		`<key>Program</key><string>/usr/local/database/bin/databased</string>`)

	if integration := InspectSystemIntegration(app, nil); len(integration.LaunchDaemons) != 0 {
		t.Fatalf("a job sharing only a vendor prefix must not be attributed, got %v",
			integration.LaunchDaemons)
	}
}

// Containment is compared on path components, not on raw string prefixes.
//
// The first version of this test used FooBar.app against Foo.app, which does
// not collide under a plain string prefix at all, so it passed with the
// component check removed and proved nothing. The paths that actually collide
// are the ones extending the bundle name rather than a component of it, such
// as the backup copies an installer or a person leaves beside the original.
func TestContainmentComparesPathComponents(t *testing.T) {
	const app = "/Applications/Foo.app"

	for _, outside := range []string{
		"/Applications/Foo.app.old/Contents/MacOS/x",
		"/Applications/Foo.application/Contents/MacOS/x",
		"/Applications/Foo.appX/helper",
	} {
		if withinBundle(outside, app) {
			t.Errorf("%q is not inside %q but was treated as contained", outside, app)
		}
	}

	for _, inside := range []string{
		"/Applications/Foo.app/Contents/MacOS/x",
		"/Applications/Foo.app",
	} {
		if !withinBundle(inside, app) {
			t.Errorf("%q is inside %q but was not treated as contained", inside, app)
		}
	}
}

func TestDeclaredPrivilegedHelperIsReported(t *testing.T) {
	redirectSystemDirs(t)
	app := makeApp(t, t.TempDir(), "Updater", "com.vendor.updater")

	helperName := "com.vendor.updater.PrivilegedHelper"
	if err := os.MkdirAll(filepath.Join(app.Path, bundleHelperDir), 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(app.Path, bundleHelperDir, helperName),
		[]byte("binary"), 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	installed := filepath.Join(privilegedHelperDir, helperName)
	if err := os.WriteFile(installed, []byte("binary"), 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}

	integration := InspectSystemIntegration(app, nil)
	if len(integration.PrivilegedHelpers) != 1 || integration.PrivilegedHelpers[0] != installed {
		t.Fatalf("expected the installed helper, got %v", integration.PrivilegedHelpers)
	}
}

// A helper the bundle declares but which was never installed is not a leftover.
func TestUndeclaredOrAbsentHelperIsNotReported(t *testing.T) {
	redirectSystemDirs(t)
	app := makeApp(t, t.TempDir(), "Updater", "com.vendor.updater")

	if err := os.MkdirAll(filepath.Join(app.Path, bundleHelperDir), 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(app.Path, bundleHelperDir, "com.vendor.helper"),
		[]byte("binary"), 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}

	if integration := InspectSystemIntegration(app, nil); len(integration.PrivilegedHelpers) != 0 {
		t.Fatalf("a helper that was never installed is not a leftover, got %v",
			integration.PrivilegedHelpers)
	}
}

// Vendors ship one updater for a whole product family. On the machine this was
// designed against, a single Microsoft helper served Word, Excel, Teams, and
// OneDrive. Calling it orphaned while three of them remain is a false alarm,
// and a warning that cries wolf is one people learn to click past.
func TestSharedComponentsAreNotReportedAsLeftBehind(t *testing.T) {
	redirectSystemDirs(t)
	appDir := t.TempDir()

	word := makeApp(t, appDir, "Word", "com.vendor.word")
	excel := makeApp(t, appDir, "Excel", "com.vendor.excel")

	writePlist(t, filepath.Join(launchDaemonDir, "com.vendor.autoupdate.plist"),
		`<key>Program</key><string>`+
			filepath.Join(word.Path, "Contents", "MacOS", "update")+`</string>`)

	// Alone, the job is genuinely left behind.
	if integration := InspectSystemIntegration(word, nil); len(integration.LaunchDaemons) != 1 {
		t.Fatalf("setup: expected the job when nothing else is installed, got %v",
			integration.LaunchDaemons)
	}

	// With a sibling from the same vendor still installed, it is not.
	integration := InspectSystemIntegration(word, []InstalledApp{word, excel})
	if integration.Orphans() {
		t.Fatalf("a component shared with an installed sibling must not be reported, got %v",
			integration.LaunchDaemons)
	}
}

// Sharing suppression must never reach the disqualifying checks. A driver is a
// driver whether or not the vendor ships other applications.
func TestSharingDoesNotSuppressTheRefusal(t *testing.T) {
	redirectSystemDirs(t)
	appDir := t.TempDir()

	driver := makeApp(t, appDir, "Driver", "com.vendor.driver")
	sibling := makeApp(t, appDir, "Console", "com.vendor.console")

	if err := os.MkdirAll(filepath.Join(driver.Path, bundleKextDir, "Vendor.kext"), 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}

	integration := InspectSystemIntegration(driver, []InstalledApp{driver, sibling})
	if !integration.Disqualifying() {
		t.Fatal("a sibling application must not suppress a kernel extension refusal")
	}
}

// An ordinary application with none of this must still be uninstallable, or
// the feature has made the tool useless.
func TestOrdinaryAppIsNeitherRefusedNorFlagged(t *testing.T) {
	redirectSystemDirs(t)
	app := makeApp(t, t.TempDir(), "Notes", "com.vendor.notes")

	integration := InspectSystemIntegration(app, nil)
	if integration.Disqualifying() || integration.Orphans() {
		t.Fatalf("an ordinary app should be clean, got %+v", integration)
	}
	if _, protected := IsProtectedApp(app); protected {
		t.Fatal("an ordinary app must remain uninstallable")
	}
}

// Missing system directories are the ordinary case on many machines and must
// not be an error or a refusal.
func TestAbsentSystemDirectoriesAreHandled(t *testing.T) {
	root := redirectSystemDirs(t)
	if err := os.RemoveAll(root); err != nil {
		t.Fatalf("setup: %v", err)
	}
	app := makeApp(t, t.TempDir(), "Notes", "com.vendor.notes")

	integration := InspectSystemIntegration(app, nil)
	if integration.Disqualifying() || integration.Orphans() {
		t.Fatal("absent system directories should yield nothing, not a refusal")
	}
}
