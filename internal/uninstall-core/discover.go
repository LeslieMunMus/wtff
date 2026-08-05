package uninstallcore

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// InstalledApp is one discovered application bundle.
type InstalledApp struct {
	// Path is the .app bundle's location.
	Path string

	// BundleID is CFBundleIdentifier. Every field derived from it elsewhere in
	// this package assumes it passed isPlausibleBundleID; a bundle without a
	// usable identifier is excluded during discovery rather than carried
	// forward with an empty or unchecked value.
	BundleID string

	// DisplayName is CFBundleDisplayName, falling back to CFBundleName, falling
	// back to the bundle's filename with the .app suffix removed. Some
	// applications only set one of the two Info.plist keys.
	DisplayName string

	// Version is CFBundleShortVersionString, for display only. Empty when
	// absent; never a reason to exclude an app from discovery.
	Version string
}

// SkippedApp records a .app bundle discovery could not use, and why.
type SkippedApp struct {
	Path   string
	Reason string
}

// ErrPlutilUnavailable means the plutil tool, part of every real macOS
// install, could not be found. Discovery refuses to proceed rather than
// silently report zero applications, since that would look identical to "you
// have nothing installed."
var ErrPlutilUnavailable = errors.New("plutil is not available on this system")

// DiscoverApps scans the given root directories, non-recursively, for .app
// bundles and reads their identity from Info.plist.
//
// A directory that does not exist is not an error; ~/Applications in
// particular is optional and most machines do not have one.
func DiscoverApps(roots []string) ([]InstalledApp, []SkippedApp, error) {
	if _, err := exec.LookPath("plutil"); err != nil {
		return nil, nil, ErrPlutilUnavailable
	}

	var apps []InstalledApp
	var skipped []SkippedApp

	for _, root := range roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !strings.HasSuffix(entry.Name(), ".app") {
				continue
			}
			appPath := filepath.Join(root, entry.Name())

			// entry.IsDir() reports the directory entry's own type, which for a
			// symlink is false regardless of what it points to. Apple ships
			// several of its own applications, Safari among them, as a symlink
			// from /Applications into a separate sealed system volume; checking
			// IsDir() here excluded Safari from discovery entirely, silently,
			// which meant the bundle identifier check built specifically to
			// refuse it was never reached. os.Stat follows the link and reports
			// what it actually resolves to, which is what determines whether
			// this is usable as an application bundle.
			info, statErr := os.Stat(appPath)
			if statErr != nil || !info.IsDir() {
				skipped = append(skipped, SkippedApp{
					Path: appPath, Reason: "does not resolve to a directory",
				})
				continue
			}

			app, err := readBundleInfo(appPath)
			if err != nil {
				skipped = append(skipped, SkippedApp{Path: appPath, Reason: err.Error()})
				continue
			}
			apps = append(apps, app)
		}
	}
	return apps, skipped, nil
}

// readBundleInfo reads one application bundle's identity from its
// Info.plist.
func readBundleInfo(appPath string) (InstalledApp, error) {
	infoPlist := filepath.Join(appPath, "Contents", "Info.plist")

	bundleID, err := plutilExtract(infoPlist, "CFBundleIdentifier")
	if err != nil || bundleID == "" {
		return InstalledApp{}, fmt.Errorf("no readable bundle identifier")
	}
	if !isPlausibleBundleID(bundleID) {
		// A malformed identifier is refused here rather than carried forward
		// hoping later validation catches it. Every leftover path this package
		// builds embeds the bundle identifier directly, and an identifier
		// containing a path separator or a parent reference has no business
		// being interpolated into a filesystem path at all, even though the
		// deletion engine's own path validation would still refuse the result.
		return InstalledApp{}, fmt.Errorf("bundle identifier is not usable in a path: %q", bundleID)
	}

	displayName, _ := plutilExtract(infoPlist, "CFBundleDisplayName")
	if displayName == "" {
		displayName, _ = plutilExtract(infoPlist, "CFBundleName")
	}
	if displayName == "" {
		displayName = strings.TrimSuffix(filepath.Base(appPath), ".app")
	}

	version, _ := plutilExtract(infoPlist, "CFBundleShortVersionString")

	return InstalledApp{
		Path:        appPath,
		BundleID:    bundleID,
		DisplayName: displayName,
		Version:     version,
	}, nil
}

// plutilExtract reads one key from a plist file as a raw string. A missing
// key or a missing file both report through the error return; callers that
// treat a field as optional check only the error, not its text.
func plutilExtract(plistPath, key string) (string, error) {
	cmd := exec.Command("plutil", "-extract", key, "raw", plistPath)
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

// isPlausibleBundleID reports whether a value is safe to embed as a path
// component. This is not a reverse-DNS identifier validator; some real
// applications ship identifiers that do not strictly follow that convention,
// and rejecting those would exclude legitimate apps from discovery. It is
// only a check against the identifier being usable to construct a path that
// escapes where it belongs.
func isPlausibleBundleID(id string) bool {
	if id == "" || len(id) > 255 {
		return false
	}
	if strings.ContainsAny(id, "/\x00") {
		return false
	}
	for _, r := range id {
		if r < 0x20 {
			return false
		}
	}
	return true
}
