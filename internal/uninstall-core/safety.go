package uninstallcore

import "strings"

// appleBundlePrefix is the reserved namespace Apple uses for its own
// software. Third party software must not legitimately use it, so any
// installed application reporting an identifier in this namespace is treated
// as Apple's own, regardless of where the bundle happens to live on disk.
const appleBundlePrefix = "com.apple."

// IsProtectedApp reports whether an application must be refused as an
// uninstall target, and why.
//
// This checks identity, not location. Path validation's structural floor
// already denies /System entirely, which covers most of Apple's
// applications, but Safari and at least two "Creator Studio" launcher
// bundles were found installed directly under /Applications with a
// com.apple. identifier during this package's design, outside that floor
// entirely. Checking the bundle identifier catches all of them uniformly,
// and would continue to catch a similar case on a machine laid out
// differently than the one this was designed against.
func IsProtectedApp(app InstalledApp) (reason string, protected bool) {
	if strings.HasPrefix(strings.ToLower(app.BundleID), appleBundlePrefix) {
		return "wtff does not uninstall Apple's own applications, identified by the " +
			appleBundlePrefix + " bundle identifier prefix, regardless of where they are installed", true
	}
	return "", false
}
