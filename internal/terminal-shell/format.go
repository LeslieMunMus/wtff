package terminalshell

import "fmt"

// humanBytes formats a byte count for display, matching the decimal units
// internal/cli uses in its own output, so a size shown in the shell and the
// same size shown by wtff clean --dry-run always read the same way.
//
// Duplicated rather than imported: internal/cli's version is unexported, and
// promoting it to a shared package for one small formatting function would
// be more coupling than the four lines it saves.
func humanBytes(n int64) string {
	if n < 0 {
		n = 0
	}
	if n < 1000 {
		return fmt.Sprintf("%dB", n)
	}
	units := []string{"KB", "MB", "GB", "TB", "PB"}
	value := float64(n)
	unit := ""
	for _, u := range units {
		value /= 1000
		unit = u
		if value < 1000 {
			break
		}
	}
	return fmt.Sprintf("%.1f%s", value, unit)
}
