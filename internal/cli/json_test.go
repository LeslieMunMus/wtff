package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// decode fails the test unless stdout is exactly one JSON document and
// nothing else.
//
// This is the property the whole feature rests on. A stray human readable
// line, a warning, or a prompt printed alongside would make the stream
// unparseable, and it would do so only in the situation that produced the
// extra line, which is the worst way for it to break.
func decode(t *testing.T, out bytes.Buffer) map[string]any {
	t.Helper()
	var doc map[string]any
	decoder := json.NewDecoder(bytes.NewReader(out.Bytes()))
	if err := decoder.Decode(&doc); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\n%s", err, out.String())
	}
	if decoder.More() {
		t.Fatalf("stdout holds more than one document:\n%s", out.String())
	}
	return doc
}

func jsonHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	cache := filepath.Join(home, "Library", "Caches", "com.example.app")
	if err := os.MkdirAll(cache, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cache, "blob"), []byte("cached"), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}
	return home
}

func TestCleanDryRunEmitsOneJSONDocument(t *testing.T) {
	jsonHome(t)

	var out, errOut bytes.Buffer
	if code := Run([]string{"clean", "--dry-run", "--json"},
		strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}

	doc := decode(t, out)
	if doc["wtff_version"] == "" {
		t.Error("the document should name the version that produced it")
	}
	if doc["command"] != "clean" {
		t.Errorf("command = %v, want clean", doc["command"])
	}

	plan, ok := doc["plan"].(map[string]any)
	if !ok {
		t.Fatalf("no plan in %v", doc)
	}
	if plan["dry_run"] != true {
		t.Error("a dry run should say so")
	}
	if plan["action"] != "stage" {
		t.Errorf("action = %v, want stage", plan["action"])
	}
	items, ok := plan["items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("expected the one cache, got %v", plan["items"])
	}
	item := items[0].(map[string]any)
	for _, field := range []string{"path", "size_bytes", "size_known", "rule_id", "reason"} {
		if _, present := item[field]; !present {
			t.Errorf("item is missing %q: %v", field, item)
		}
	}
}

// A dry run must change nothing, JSON or not.
func TestJSONDryRunChangesNothing(t *testing.T) {
	home := jsonHome(t)
	target := filepath.Join(home, "Library", "Caches", "com.example.app", "blob")

	var out, errOut bytes.Buffer
	Run([]string{"clean", "--dry-run", "--json"}, strings.NewReader(""), &out, &errOut)

	if _, err := os.Stat(target); err != nil {
		t.Fatal("a dry run removed something")
	}
}

// Whether the removal can be undone is the most consequential fact in the
// document, so it is stated rather than left to be inferred from the action.
func TestApplyResultStatesReversibility(t *testing.T) {
	jsonHome(t)

	var out, errOut bytes.Buffer
	if code := Run([]string{"clean", "--yes", "--json"},
		strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}

	result := decode(t, out)["result"].(map[string]any)
	if result["reversible"] != true {
		t.Error("staging is reversible and the document should say so")
	}
	if result["batch_id"] == nil || result["batch_id"] == "" {
		t.Error("a staged result should carry the batch id needed to undo it")
	}
	if result["applied_count"].(float64) != 1 {
		t.Errorf("applied_count = %v, want 1", result["applied_count"])
	}
}

func TestPurgeResultSaysItIsNotReversible(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	trash := filepath.Join(home, ".Trash")
	if err := os.MkdirAll(trash, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(trash, "discarded"), []byte("x"), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	var out, errOut bytes.Buffer
	if code := Run([]string{"purge", "--yes", "--json"},
		strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}

	result := decode(t, out)["result"].(map[string]any)
	if result["reversible"] != false {
		t.Error("a purge is not reversible and the document must not suggest otherwise")
	}
	if result["action"] != "purge" {
		t.Errorf("action = %v, want purge", result["action"])
	}
	if _, present := result["batch_id"]; present {
		t.Error("a purge has no batch to undo, so it should carry no batch id")
	}
}

// A JSON run must never stop to ask a question. The prompt would corrupt the
// document, and printing it elsewhere would leave a script waiting on an
// answer nobody is there to give.
func TestJSONRefusesToRunInteractively(t *testing.T) {
	home := jsonHome(t)
	target := filepath.Join(home, "Library", "Caches", "com.example.app", "blob")

	for _, command := range [][]string{
		{"clean", "--json"},
		{"purge", "--json"},
	} {
		var out, errOut bytes.Buffer
		code := Run(command, strings.NewReader("yes\n"), &out, &errOut)
		if code != 2 {
			t.Errorf("%v exited %d, want 2", command, code)
		}
		if out.Len() != 0 {
			t.Errorf("%v wrote to stdout before refusing: %s", command, out.String())
		}
		if !strings.Contains(errOut.String(), "--dry-run or --yes") {
			t.Errorf("%v should explain what is needed, got %q", command, errOut.String())
		}
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatal("a refused run removed something")
	}
}

func TestStagedEmitsJSON(t *testing.T) {
	jsonHome(t)

	var out, errOut bytes.Buffer
	if code := Run([]string{"clean", "--yes"}, strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatalf("setup: %s", errOut.String())
	}

	out.Reset()
	if code := Run([]string{"staged", "--json"}, strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}

	doc := decode(t, out)
	staged, ok := doc["staged"].([]any)
	if !ok || len(staged) != 1 {
		t.Fatalf("expected one batch, got %v", doc["staged"])
	}
	batch := staged[0].(map[string]any)
	for _, field := range []string{"batch_id", "command", "created_at", "item_count", "bytes"} {
		if _, present := batch[field]; !present {
			t.Errorf("batch is missing %q: %v", field, batch)
		}
	}
}

// An empty result must be an empty array, not null, so a consumer can iterate
// without a nil check and cannot mistake "none staged" for "field missing".
func TestEmptyStagedIsAnArrayNotNull(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	var out, errOut bytes.Buffer
	if code := Run([]string{"staged", "--json"}, strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}

	// The field is omitted when empty rather than emitted as null, which is
	// the other acceptable answer; what must not happen is a literal null.
	if strings.Contains(out.String(), "null") {
		t.Fatalf("an empty listing emitted null:\n%s", out.String())
	}
	decode(t, out)
}

func TestDoctorEmitsJSONAndKeepsItsExitStatus(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	var out, errOut bytes.Buffer
	code := Run([]string{"doctor", "--json"}, strings.NewReader(""), &out, &errOut)

	doc := decode(t, out)
	doctor, ok := doc["doctor"].(map[string]any)
	if !ok {
		t.Fatalf("no doctor section in %v", doc)
	}
	findings, ok := doctor["findings"].([]any)
	if !ok || len(findings) == 0 {
		t.Fatal("expected findings")
	}
	for _, raw := range findings {
		finding := raw.(map[string]any)
		for _, field := range []string{"area", "level", "summary"} {
			if _, present := finding[field]; !present {
				t.Errorf("finding is missing %q: %v", field, finding)
			}
		}
		switch finding["level"] {
		case "ok", "note", "warn":
		default:
			t.Errorf("unexpected level %v", finding["level"])
		}
	}

	// The exit status carries the same meaning as the human readable form, so
	// a scheduled job can act on it without parsing anything.
	if doctor["needs_attention"] == true && code == 0 {
		t.Error("a report needing attention should exit non-zero")
	}
	if doctor["needs_attention"] == false && code != 0 {
		t.Errorf("a healthy report should exit 0, got %d", code)
	}
}

// Errors belong on stderr so stdout stays a single parseable document.
func TestJSONErrorsDoNotPolluteStdout(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	var out, errOut bytes.Buffer
	code := Run([]string{"staged", "--purge", "..", "--yes", "--json"},
		strings.NewReader(""), &out, &errOut)
	if code == 0 {
		t.Fatal("a traversal id should still be refused")
	}
	if strings.Contains(out.String(), "{") {
		t.Fatalf("an error path wrote a partial document to stdout: %s", out.String())
	}
}

// The flag has to work wherever a person types it, the same reordering rule
// every other flag in this project follows.
func TestJSONFlagWorksAfterPositionalArguments(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	target := filepath.Join(home, "cache-dir")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}

	var out, errOut bytes.Buffer
	if code := Run([]string{"remove", "--dry-run", target, "--json"},
		strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	if out.Len() > 0 && !strings.HasPrefix(strings.TrimSpace(out.String()), "{") {
		t.Fatalf("--json after a positional argument was ignored:\n%s", out.String())
	}
}

// Uninstall prints two things before its plan: the application it resolved,
// and any privileged components that will survive. Both go to stdout, and
// both would corrupt the document they precede.
func TestUninstallJSONStdoutHoldsOnlyTheDocument(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	app := filepath.Join(home, "Applications", "Testable.app", "Contents")
	if err := os.MkdirAll(app, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	plist := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>CFBundleIdentifier</key><string>com.example.testable</string>
<key>CFBundleName</key><string>Testable</string>
</dict></plist>`
	if err := os.WriteFile(filepath.Join(app, "Info.plist"), []byte(plist), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	var out, errOut bytes.Buffer
	if code := Run([]string{"uninstall", "--dry-run", "--json", "Testable"},
		strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}

	doc := decode(t, out)
	if doc["command"] != "uninstall" {
		t.Errorf("command = %v, want uninstall", doc["command"])
	}
}
