package cli

import "fmt"

// humanBytes formats a byte count for display.
//
// macOS has used decimal (base 1000) units since Snow Leopard, and Finder and
// Disk Utility both report sizes that way. A tool in this space reporting
// binary units would show a number that visibly disagrees with what the rest
// of the system says about the same file.
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
