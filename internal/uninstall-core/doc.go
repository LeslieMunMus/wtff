// Package uninstallcore discovers installed applications and their leftover
// data for the uninstall command.
//
// Two responsibilities, kept deliberately separate. Discovery reads
// Info.plist inside each .app bundle to learn its bundle identifier and
// display name; nothing here removes anything. Leftover matching then
// proposes candidate paths built from that identity: this package requires
// an exact bundle identifier or an exact display name to match a location
// before proposing it, never a fuzzy, partial, or vendor-wide pattern. An app
// named "Notes" living beside a system feature also named Notes is exactly
// the kind of collision a broad match would get wrong, and getting it wrong
// here means deleting another product's data.
//
// As with internal/clean-catalog, nothing this package proposes is trusted
// on its own. Every candidate still passes through the deletion engine's
// full path validation and protection rule check before anything happens to
// it.
//
// # Why Apple's own applications are refused outright
//
// Inspecting real installed applications while this package was designed
// found three with a com.apple. bundle identifier living directly under
// /Applications rather than /System/Applications: Safari, and two
// "Creator Studio" launcher bundles for Final Cut Pro and Logic Pro. Path
// validation's structural floor denies /System entirely, which protects
// most of Apple's applications by location alone, but it does not reach
// /Applications, so it would not have caught Safari. This package checks
// bundle identity directly instead of inferring safety from where a bundle
// happens to be installed: any application reporting a com.apple. bundle
// identifier is refused as an uninstall target, regardless of its path.
package uninstallcore
