package uninstallcore

import "testing"

// The three concrete cases found while designing this package: Safari and
// two Creator Studio launcher bundles, all installed directly under
// /Applications with a com.apple. identifier, none of them caught by path
// validation's structural floor since that floor only denies /System.
func TestProtectsAppleApplicationsRegardlessOfInstallLocation(t *testing.T) {
	cases := []InstalledApp{
		{Path: "/Applications/Safari.app", BundleID: "com.apple.Safari", DisplayName: "Safari"},
		{Path: "/Applications/Final Cut Pro Creator Studio.app", BundleID: "com.apple.FinalCutApp", DisplayName: "Final Cut Pro"},
		{Path: "/Applications/Logic Pro Creator Studio.app", BundleID: "com.apple.mobilelogic", DisplayName: "Logic Pro"},
		{Path: "/System/Applications/Mail.app", BundleID: "com.apple.mail", DisplayName: "Mail"},
	}
	for _, app := range cases {
		t.Run(app.DisplayName, func(t *testing.T) {
			reason, protected := IsProtectedApp(app)
			if !protected {
				t.Fatalf("%s (%s) was not protected", app.DisplayName, app.BundleID)
			}
			if reason == "" {
				t.Fatal("a protection must explain itself")
			}
		})
	}
}

func TestDoesNotProtectThirdPartyApplications(t *testing.T) {
	app := InstalledApp{Path: "/Applications/Example.app", BundleID: "com.example.app", DisplayName: "Example"}
	if _, protected := IsProtectedApp(app); protected {
		t.Fatal("a third party application was protected")
	}
}

// The check is case-insensitive and matches on prefix, not exact equality,
// since Apple's own bundle identifiers vary by component beyond the shared
// namespace prefix.
func TestAppleProtectionIsCaseInsensitiveOnThePrefix(t *testing.T) {
	app := InstalledApp{Path: "/Applications/Odd.app", BundleID: "COM.APPLE.something", DisplayName: "Odd"}
	if _, protected := IsProtectedApp(app); !protected {
		t.Fatal("a differently cased com.apple. identifier was not protected")
	}
}

// A bundle identifier that merely contains the string "apple" elsewhere, not
// as the reserved namespace prefix, must not be caught. This would otherwise
// let the protection reach far more broadly than its own stated reason
// justifies, refusing to uninstall applications that have nothing to do with
// Apple's own software.
func TestDoesNotProtectByCoincidentalSubstring(t *testing.T) {
	cases := []string{
		"com.pineapple.app",
		"org.appleseed.notes",
		"net.example.com.apple.fake",
	}
	for _, id := range cases {
		t.Run(id, func(t *testing.T) {
			app := InstalledApp{Path: "/Applications/X.app", BundleID: id, DisplayName: "X"}
			if _, protected := IsProtectedApp(app); protected {
				t.Fatalf("%q was protected by coincidental substring match", id)
			}
		})
	}
}
