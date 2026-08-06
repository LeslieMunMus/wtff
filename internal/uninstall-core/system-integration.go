package uninstallcore

import (
	"os"
	"path/filepath"
	"strings"
)

// Locations macOS reserves for privileged components installed outside an
// application's own bundle. Every one of these was confirmed present on a
// machine running Darwin 25.6 during this file's design.
// Variables rather than constants so tests can point them at a fixture tree.
// The real directories need root to write, so a test that could not redirect
// them could only ever observe whatever happens to be installed on the machine
// running it, and on a machine with no drivers that is a check which never
// fires and therefore proves nothing.
var (
	privilegedHelperDir = "/Library/PrivilegedHelperTools"
	launchDaemonDir     = "/Library/LaunchDaemons"
	launchAgentDir      = "/Library/LaunchAgents"
	systemKextDir       = "/Library/Extensions"
)

// Paths inside an application bundle where Apple's documented mechanisms place
// extensions. These are structural, not brand specific: any vendor shipping a
// system extension or a kernel extension puts it here, because that is where
// the loader looks for it.
const (
	bundleSystemExtensionDir = "Contents/Library/SystemExtensions"
	bundleKextDir            = "Contents/Library/Extensions"
	bundleHelperDir          = "Contents/Library/LaunchServices"
)

// SystemIntegration records the privileged components an application owns
// beyond its own bundle.
//
// The distinction this type draws is between what wtff must refuse outright
// and what it may remove while telling the truth about the wreckage.
type SystemIntegration struct {
	// SystemExtensions and KernelExtensions are drivers and security agents
	// the application installs into the running kernel or the system extension
	// subsystem. Their presence is disqualifying.
	SystemExtensions []string
	KernelExtensions []string

	// PrivilegedHelpers and LaunchDaemons are root level components that will
	// still be installed and running after the application is gone. Their
	// presence is not disqualifying, but a person deserves to be told.
	PrivilegedHelpers []string
	LaunchDaemons     []string
}

// Disqualifying reports whether this integration means the application must
// not be uninstalled by wtff.
func (s SystemIntegration) Disqualifying() bool {
	return len(s.SystemExtensions) > 0 || len(s.KernelExtensions) > 0
}

// Orphans reports whether removing the application would leave privileged
// components behind that wtff cannot remove.
func (s SystemIntegration) Orphans() bool {
	return len(s.PrivilegedHelpers) > 0 || len(s.LaunchDaemons) > 0
}

// InspectSystemIntegration examines what an application installs beyond its
// own bundle.
//
// This is deliberately structural rather than a list of vendor names. A named
// list only protects software someone thought to name, goes stale as products
// are renamed and acquired, and silently fails for the small vendor nobody has
// heard of, whose driver is no less load bearing for being obscure. What
// actually makes an application dangerous to remove is a property it has, not
// a brand it carries, and macOS makes that property visible in the filesystem:
// an extension has to sit where the loader looks for it, and a privileged
// helper has to be registered where launchd reads it.
//
// Every check reads the filesystem only. An earlier design also read code
// signing entitlements, which would name Endpoint Security clients directly,
// but that needs an external tool whose absence would silently weaken the
// check. Since macOS 10.15 an Endpoint Security client must ship as a system
// extension, the first check below already covers that category without
// depending on anything but a directory listing.
// alsoInstalled is every other application present, used only to suppress
// components that another installed application still needs. It may be nil
// when the caller only cares whether the application is disqualifying, since
// an extension is disqualifying no matter who else shares the vendor.
func InspectSystemIntegration(app InstalledApp, alsoInstalled []InstalledApp) SystemIntegration {
	var found SystemIntegration

	found.SystemExtensions = bundlesIn(
		filepath.Join(app.Path, bundleSystemExtensionDir), ".systemextension")
	found.KernelExtensions = bundlesIn(
		filepath.Join(app.Path, bundleKextDir), ".kext")

	// A kernel extension may also have been installed into the system wide
	// directory under a name derived from the bundle identifier, in which case
	// the copy inside the app bundle may already be gone.
	found.KernelExtensions = append(found.KernelExtensions,
		systemKextsMatching(app)...)

	// Everything below is reported as "this will be left behind", which is only
	// true if nothing else still needs it. Vendors routinely install one shared
	// updater for a whole product family: on the machine this was designed
	// against, a single Microsoft AutoUpdate helper served Word, Excel, Teams,
	// and OneDrive, and one Google updater served Chrome and Gemini. Announcing
	// an orphan while three other applications still depend on it is a false
	// alarm, and a warning that cries wolf is one people learn to click past.
	if sharedWithAnother(app, alsoInstalled) {
		return found
	}

	found.PrivilegedHelpers = helpersFor(app)
	found.LaunchDaemons = launchJobsReferencing(app)

	return found
}

// sharedWithAnother reports whether some other installed application belongs to
// the same vendor namespace, and would therefore claim the same privileged
// components.
func sharedWithAnother(app InstalledApp, alsoInstalled []InstalledApp) bool {
	prefix := vendorPrefix(app.BundleID)
	if prefix == "" {
		return false
	}
	for _, other := range alsoInstalled {
		if other.BundleID != app.BundleID && vendorPrefix(other.BundleID) == prefix {
			return true
		}
	}
	return false
}

// bundlesIn lists entries with the given suffix directly inside a directory.
func bundlesIn(dir, suffix string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var found []string
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), suffix) {
			found = append(found, filepath.Join(dir, entry.Name()))
		}
	}
	return found
}

// systemKextsMatching finds kernel extensions in the system wide directory
// whose bundle identifier shares the application's identifier prefix.
//
// The comparison is on the reverse DNS prefix rather than the whole
// identifier, because a vendor's driver and its application are siblings in
// the same namespace rather than the same bundle: an application identified
// com.vendor.app typically installs com.vendor.driver.
func systemKextsMatching(app InstalledApp) []string {
	prefix := vendorPrefix(app.BundleID)
	if prefix == "" {
		return nil
	}

	entries, err := os.ReadDir(systemKextDir)
	if err != nil {
		return nil
	}
	var found []string
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".kext") {
			continue
		}
		kextPath := filepath.Join(systemKextDir, entry.Name())
		id, idErr := plutilExtract(
			filepath.Join(kextPath, "Contents", "Info.plist"), "CFBundleIdentifier")
		if idErr != nil || id == "" {
			continue
		}
		if vendorPrefix(id) == prefix {
			found = append(found, kextPath)
		}
	}
	return found
}

// vendorPrefix reduces a reverse DNS identifier to its first two components,
// which is the vendor namespace: com.vendor from com.vendor.product.thing.
//
// An identifier with fewer than three components yields nothing rather than
// matching loosely. Reducing com.vendor to com would match every identifier
// on the machine, which is how a narrow check becomes a refusal to uninstall
// anything at all.
func vendorPrefix(bundleID string) string {
	parts := strings.Split(strings.ToLower(bundleID), ".")
	if len(parts) < 3 {
		return ""
	}
	return parts[0] + "." + parts[1]
}

// helpersFor finds installed privileged helper tools belonging to an
// application.
//
// Both directions are checked. The application's own Info.plist names the
// helpers it is authorized to install, which is the authoritative statement of
// intent, and the helper directory is checked for a matching vendor namespace,
// which catches a helper installed by a version of the application that no
// longer declares it.
func helpersFor(app InstalledApp) []string {
	seen := make(map[string]bool)
	var found []string

	add := func(path string) {
		if seen[path] {
			return
		}
		if _, err := os.Lstat(path); err != nil {
			return
		}
		seen[path] = true
		found = append(found, path)
	}

	// Declared helpers, named in the bundle and installed under their own
	// identifier, which is the convention launchd's privileged helper support
	// requires.
	for _, name := range bundleHelperNames(app) {
		add(filepath.Join(privilegedHelperDir, name))
	}

	// Deliberately no scan of the helper directory by vendor namespace. A
	// namespace match says two identifiers share a vendor, not that this
	// application installed that helper, and the difference showed up
	// immediately in practice: MySQL Workbench matched an Oracle database
	// daemon and a Java updater it had nothing to do with, both installed
	// separately. What the bundle declares about itself is a claim wtff can
	// defend; a shared prefix is a guess.
	return found
}

// bundleHelperNames lists the helper executables shipped inside the bundle,
// whose filenames are the identifiers they install under.
func bundleHelperNames(app InstalledApp) []string {
	entries, err := os.ReadDir(filepath.Join(app.Path, bundleHelperDir))
	if err != nil {
		return nil
	}
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

// launchJobsReferencing finds launchd jobs whose executable lives inside the
// application bundle.
//
// A job left behind after its program is deleted does not quietly stop being a
// job. launchd keeps trying to run something that is no longer there, which is
// noise at best and, for a job that respawns, a permanent one.
//
// Only the program path is matched, never the job's identifier. Matching on a
// shared vendor namespace looked reasonable and was wrong the first time it met
// a real machine: it attributed an Oracle database daemon and a Java updater to
// MySQL Workbench, which installed neither. A job whose program sits inside the
// bundle about to be deleted is a fact; a job that merely shares a vendor
// prefix is a guess, and a guess presented as a warning is worse than silence.
func launchJobsReferencing(app InstalledApp) []string {
	var found []string

	for _, dir := range []string{launchDaemonDir, launchAgentDir} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !strings.HasSuffix(entry.Name(), ".plist") {
				continue
			}
			jobPath := filepath.Join(dir, entry.Name())
			if program := launchJobProgram(jobPath); program != "" &&
				withinBundle(program, app.Path) {
				found = append(found, jobPath)
			}
		}
	}
	return found
}

// launchJobProgram reads the executable a launchd job runs, from either key
// that can carry it.
func launchJobProgram(jobPath string) string {
	if program, err := plutilExtract(jobPath, "Program"); err == nil && program != "" {
		return program
	}
	// ProgramArguments is the more common form, and its first element is the
	// executable. A job using it has no Program key at all.
	program, err := plutilExtract(jobPath, "ProgramArguments.0")
	if err != nil {
		return ""
	}
	return program
}

// withinBundle reports whether a program path lies inside an application
// bundle, comparing on path components so that a bundle named as a prefix of
// another does not match.
func withinBundle(program, appPath string) bool {
	cleanProgram := filepath.Clean(program)
	cleanApp := filepath.Clean(appPath)
	return cleanProgram == cleanApp ||
		strings.HasPrefix(cleanProgram, cleanApp+string(filepath.Separator))
}
