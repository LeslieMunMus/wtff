package uninstallcore

import (
	"fmt"
	"path/filepath"

	deletionengine "github.com/lesliemunmus/wtff/internal/deletion-engine"
)

// leftoverTemplate describes one location macOS conventionally uses for
// per-application data, and the evidence that justifies proposing it.
type leftoverTemplate struct {
	// relative is joined onto the home directory. %s is replaced with either
	// the bundle identifier or the display name, depending on evidenceKind.
	relative string

	// evidenceKind names which field of the app fills the template, and is
	// echoed into the generated reason so a person reading a plan can see
	// exactly what evidence justified each entry.
	evidenceKind string
}

const (
	evidenceBundleID    = "bundle identifier"
	evidenceDisplayName = "display name"
)

// bundleIDTemplates match only on the exact bundle identifier. These are
// locations Apple's own frameworks name by bundle identifier as a matter of
// platform convention, such as a sandboxed application's container, so a
// match here is strong evidence.
var bundleIDTemplates = []leftoverTemplate{
	{relative: "Library/Application Support/%s", evidenceKind: evidenceBundleID},
	{relative: "Library/Caches/%s", evidenceKind: evidenceBundleID},
	{relative: "Library/Preferences/%s.plist", evidenceKind: evidenceBundleID},
	{relative: "Library/Saved Application State/%s.savedState", evidenceKind: evidenceBundleID},
	{relative: "Library/Containers/%s", evidenceKind: evidenceBundleID},
	{relative: "Library/HTTPStorages/%s", evidenceKind: evidenceBundleID},
	{relative: "Library/WebKit/%s", evidenceKind: evidenceBundleID},
	{relative: "Library/Cookies/%s.binarycookies", evidenceKind: evidenceBundleID},
	{relative: "Library/Logs/%s", evidenceKind: evidenceBundleID},
}

// displayNameTemplates match only on the exact display name. Weaker evidence
// than a bundle identifier match, since a display name is not guaranteed
// unique the way a bundle identifier is meant to be, but still an exact
// match, never a partial or fuzzy one.
var displayNameTemplates = []leftoverTemplate{
	{relative: "Library/Application Support/%s", evidenceKind: evidenceDisplayName},
	{relative: "Library/Caches/%s", evidenceKind: evidenceDisplayName},
	{relative: "Library/Logs/%s", evidenceKind: evidenceDisplayName},
	{relative: "Library/Saved Application State/%s.savedState", evidenceKind: evidenceDisplayName},
}

// DiscoverLeftovers proposes candidate paths for an application's leftover
// data, built from the exact bundle identifier and exact display name only.
//
// No name variants are generated: no space, hyphen, or underscore
// substitutions, no case folding beyond what the filesystem itself applies,
// no stripping of version or channel suffixes. Each of those would catch more
// real leftovers and would also catch more wrong matches, and the second
// risk is the one this package is built to avoid. A leftover missed here can
// still be found and removed directly with wtff remove once a person locates
// it; a leftover wrongly matched here would have already been staged before
// anyone had the chance to notice the mismatch.
//
// Every candidate this function returns still passes through the deletion
// engine's full path validation and protection rule check before anything
// happens to it, the same as every other candidate source in this project.
func DiscoverLeftovers(app InstalledApp, home string) []deletionengine.Candidate {
	var candidates []deletionengine.Candidate

	// An empty identifier or display name must never reach a template. Every
	// template has exactly one %s, at the final path component, so an empty
	// value does not corrupt the path into something empty or malformed; it
	// silently widens it into the shared parent directory itself, such as the
	// whole of Library/Application Support. DiscoverApps guarantees both
	// fields are non-empty before an InstalledApp exists, but this function
	// is exported and callable directly, so the guarantee is re-checked here
	// rather than assumed to hold at every call site forever.
	if app.BundleID != "" {
		for _, tmpl := range bundleIDTemplates {
			candidates = append(candidates, buildCandidate(tmpl, app.BundleID, home, app))
		}
	}
	if app.DisplayName != "" {
		for _, tmpl := range displayNameTemplates {
			candidates = append(candidates, buildCandidate(tmpl, app.DisplayName, home, app))
		}
	}
	return candidates
}

func buildCandidate(tmpl leftoverTemplate, value, home string, app InstalledApp) deletionengine.Candidate {
	leaf := fmt.Sprintf(tmpl.relative, value)
	path := filepath.Join(home, leaf)
	reason := fmt.Sprintf(
		"path exactly matches %s's %s, %q",
		app.DisplayName, tmpl.evidenceKind, value,
	)
	return deletionengine.Candidate{
		Path:   path,
		RuleID: "uninstall-leftover",
		Reason: reason,
	}
}
