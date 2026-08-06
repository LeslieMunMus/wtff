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

	// The system integration check runs here, inside the single function both
	// front ends already call, rather than beside it. A second check that a
	// caller has to remember is a check a caller will eventually forget, and
	// the cost of forgetting this one is a driver or a security agent removed
	// from under a running system.
	if integration := InspectSystemIntegration(app, nil); integration.Disqualifying() {
		return disqualifyingReason(app, integration), true
	}
	return "", false
}

// disqualifyingReason explains a refusal in terms of what was found, naming
// the paths, so a person can check the claim rather than take wtff's word for
// it and can go remove the thing deliberately if that is really the intent.
func disqualifyingReason(app InstalledApp, integration SystemIntegration) string {
	var what string
	switch {
	case len(integration.SystemExtensions) > 0 && len(integration.KernelExtensions) > 0:
		what = "a system extension and a kernel extension"
	case len(integration.SystemExtensions) > 0:
		what = "a system extension"
	default:
		what = "a kernel extension"
	}

	paths := append(append([]string{}, integration.SystemExtensions...),
		integration.KernelExtensions...)

	return app.DisplayName + " installs " + what + ", so wtff will not remove it: " +
		strings.Join(paths, ", ") +
		". Software that loads code into the system is where an incomplete removal " +
		"stops being an inconvenience, and this category covers drivers, network " +
		"filters, and endpoint security agents, which macOS requires to ship this " +
		"way. Use the vendor's own uninstaller, which knows how to unload the " +
		"extension before deleting what provides it."
}
