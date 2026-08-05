package uninstallcore

import "strings"

// FindApp resolves a user supplied query against discovered applications.
//
// A match requires the query to equal, case-insensitively, the display name,
// the bundle identifier, or the bundle's filename with .app removed. There is
// no partial or substring matching: a query such as "code" matching every
// installed application whose name merely contains those letters is exactly
// the kind of ambiguity that leads to uninstalling the wrong thing, and an
// exact match that returns nothing gives the person a chance to correct a
// typo rather than have it silently resolved to a guess.
//
// Zero matches and multiple matches are both reported to the caller rather
// than picked between; only a single, unambiguous match is resolved
// automatically.
func FindApp(apps []InstalledApp, query string) []InstalledApp {
	needle := strings.ToLower(strings.TrimSpace(query))
	if needle == "" {
		return nil
	}

	var matches []InstalledApp
	for _, app := range apps {
		if strings.EqualFold(app.DisplayName, query) ||
			strings.EqualFold(app.BundleID, query) ||
			strings.EqualFold(appFileName(app), query) {
			matches = append(matches, app)
		}
	}
	return matches
}

func appFileName(app InstalledApp) string {
	name := app.Path
	if idx := strings.LastIndex(name, "/"); idx >= 0 {
		name = name[idx+1:]
	}
	return strings.TrimSuffix(name, ".app")
}
