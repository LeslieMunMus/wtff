package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCleanDryRunFindsThirdPartyCacheAndProtectsKnownRisk(t *testing.T) {
	home := newFixtureHome(t)

	thirdParty := filepath.Join(home, "Library", "Caches", "SomeThirdPartyApp")
	writeFixtureFile(t, filepath.Join(thirdParty, "blob.bin"), "disposable")

	familyCircle := filepath.Join(home, "Library", "Caches", "FamilyCircle")
	writeFixtureFile(t, filepath.Join(familyCircle, "state.plist"), "account state")

	var out, errOut bytes.Buffer
	code := Run([]string{"clean", "--dry-run"}, strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "SomeThirdPartyApp") {
		t.Fatalf("output did not propose the third party cache: %s", out.String())
	}
	if !strings.Contains(out.String(), "family-sharing-cache-state") {
		t.Fatalf("output did not explain why FamilyCircle was protected: %s", out.String())
	}
	if _, err := os.Stat(thirdParty); err != nil {
		t.Fatal("dry run changed something on disk")
	}
}

func TestCleanStagesAndUndoRestoresRealDiscoveredItems(t *testing.T) {
	home := newFixtureHome(t)
	thirdParty := filepath.Join(home, "Library", "Caches", "SomeThirdPartyApp")
	writeFixtureFile(t, filepath.Join(thirdParty, "blob.bin"), "disposable content")

	var cleanOut, cleanErr bytes.Buffer
	code := Run([]string{"clean", "--yes"}, strings.NewReader(""), &cleanOut, &cleanErr)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, cleanErr.String())
	}
	if _, err := os.Stat(thirdParty); !os.IsNotExist(err) {
		t.Fatal("the third party cache was not staged away")
	}

	batchID := extractBatchID(t, cleanOut.String())
	var undoOut, undoErr bytes.Buffer
	if code := Run([]string{"undo", batchID}, strings.NewReader(""), &undoOut, &undoErr); code != 0 {
		t.Fatalf("undo exit code = %d, stderr = %s", code, undoErr.String())
	}
	contents, err := os.ReadFile(filepath.Join(thirdParty, "blob.bin"))
	if err != nil || string(contents) != "disposable content" {
		t.Fatalf("restored contents = %q, %v", contents, err)
	}
}

func TestCleanOnAnEmptyHomeReportsNothingToDo(t *testing.T) {
	newFixtureHome(t)
	var out bytes.Buffer
	code := Run([]string{"clean", "--dry-run"}, strings.NewReader(""), &out, &out)
	if code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	if !strings.Contains(out.String(), "nothing to do") {
		t.Fatalf("output = %q, want nothing to do", out.String())
	}
}

func TestCleanRejectsPositionalArguments(t *testing.T) {
	newFixtureHome(t)
	var out, errOut bytes.Buffer
	code := Run([]string{"clean", "/some/path"}, strings.NewReader(""), &out, &errOut)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2 (clean takes no path arguments)", code)
	}
}

// The same non-interactive refusal remove has must hold for clean, since it
// is the same underlying approval function. This is a deliberate, cheap check
// that the two commands did not drift apart by accident.
func TestCleanRefusesNonInteractiveWithoutYes(t *testing.T) {
	home := newFixtureHome(t)
	thirdParty := filepath.Join(home, "Library", "Caches", "SomeThirdPartyApp")
	writeFixtureFile(t, filepath.Join(thirdParty, "blob.bin"), "x")

	var out, errOut bytes.Buffer
	code := Run([]string{"clean"}, strings.NewReader("y\n"), &out, &errOut)
	if code == 0 {
		t.Fatal("a non-interactive clean without --yes should not succeed")
	}
	if _, err := os.Stat(thirdParty); err != nil {
		t.Fatal("file should still be present")
	}
}
