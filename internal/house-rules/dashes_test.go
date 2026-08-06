package houserules

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The project forbids these two characters everywhere: code, comments,
// documentation, rule files. They are named by code point rather than written
// literally, so this file does not itself trip the rule it enforces.
const (
	emDash = "\u2014"
	enDash = "\u2013"
)

var scannedExtensions = map[string]bool{
	".go": true, ".md": true, ".yaml": true, ".yml": true,
}

// repoRoot walks up from this package to the module root.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("cannot determine working directory: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("cannot find the module root")
		}
		dir = parent
	}
}

// findDashes returns the offending lines in one file's contents.
func findDashes(contents string) []int {
	var lines []int
	for i, line := range strings.Split(contents, "\n") {
		if strings.Contains(line, emDash) || strings.Contains(line, enDash) {
			lines = append(lines, i+1)
		}
	}
	return lines
}

// The scanner is tested before it is trusted. Without this, a detector that
// silently matched nothing would report the whole tree clean.
func TestScannerFindsWhatItIsLookingFor(t *testing.T) {
	if got := findDashes("a line with an " + emDash + " in it"); len(got) != 1 || got[0] != 1 {
		t.Fatalf("the scanner missed an em dash, got %v", got)
	}
	if got := findDashes("first\nsecond " + enDash + " here"); len(got) != 1 || got[0] != 2 {
		t.Fatalf("the scanner missed an en dash on line two, got %v", got)
	}
	if got := findDashes("hyphens - are - fine\nand so are commas, periods."); len(got) != 0 {
		t.Fatalf("the scanner flagged ordinary punctuation, got %v", got)
	}
}

// The rule itself, applied to the whole tree.
func TestNoEmOrEnDashesAnywhere(t *testing.T) {
	root := repoRoot(t)
	var offenders []string

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		if !scannedExtensions[filepath.Ext(entry.Name())] {
			return nil
		}

		contents, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		relative, _ := filepath.Rel(root, path)
		for _, line := range findDashes(string(contents)) {
			offenders = append(offenders, relative+":"+itoa(line))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scanning the tree: %v", err)
	}

	if len(offenders) > 0 {
		t.Fatalf("em or en dash found, use commas, periods, or single hyphens:\n  %s",
			strings.Join(offenders, "\n  "))
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
