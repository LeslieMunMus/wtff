package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func completionScript(t *testing.T, shell string) string {
	t.Helper()
	var out, errOut bytes.Buffer
	if code := Run([]string{"completion", shell}, strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatalf("completion %s exited %d: %s", shell, code, errOut.String())
	}
	if out.Len() == 0 {
		t.Fatalf("completion %s produced nothing", shell)
	}
	return out.String()
}

// The failure mode for completions is silence: a command is added, the script
// is never updated, and the only symptom is a person pressing tab and being
// told the thing they just read about does not exist. This reads the
// dispatcher itself so the two cannot drift apart unnoticed.
func TestCompletionCoversEveryDispatchedCommand(t *testing.T) {
	source, err := os.ReadFile("app.go")
	if err != nil {
		t.Fatalf("reading the dispatcher: %v", err)
	}

	// Every case label in Run's switch, which is the actual list of things
	// wtff answers to.
	pattern := regexp.MustCompile(`case "([a-z-]+)"`)
	dispatched := map[string]bool{}
	for _, match := range pattern.FindAllStringSubmatch(string(source), -1) {
		dispatched[match[1]] = true
	}
	if len(dispatched) < 8 {
		t.Fatalf("only found %d commands in the dispatcher, the pattern is probably wrong",
			len(dispatched))
	}

	zsh := completionScript(t, "zsh")
	bash := completionScript(t, "bash")

	for command := range dispatched {
		// The hidden helpers exist for a shell to call and are deliberately
		// not offered as completions.
		if strings.HasPrefix(command, "__") {
			continue
		}
		if !strings.Contains(zsh, "'"+command+":") {
			t.Errorf("zsh completion is missing %q", command)
		}
		if !regexp.MustCompile(`\b` + regexp.QuoteMeta(command) + `\b`).MatchString(bash) {
			t.Errorf("bash completion is missing %q", command)
		}
	}
}

// A helper offered as a completion would put an internal command in front of
// a person as though it were part of the interface.
func TestHiddenHelpersAreNotOffered(t *testing.T) {
	for _, shell := range []string{"zsh", "bash"} {
		script := completionScript(t, shell)
		for _, hidden := range []string{"'__complete-batches:", "'__complete-apps:"} {
			if strings.Contains(script, hidden) {
				t.Errorf("%s completion offers the hidden helper %q", shell, hidden)
			}
		}
	}

	var out, errOut bytes.Buffer
	Run([]string{"help"}, strings.NewReader(""), &out, &errOut)
	if strings.Contains(out.String(), "__complete") {
		t.Error("usage text mentions a hidden helper")
	}
}

// The flags a command actually defines and the flags its completion offers
// have to agree, or tab suggests something the command will reject.
func TestCompletionFlagsMatchWhatCommandsAccept(t *testing.T) {
	zsh := completionScript(t, "zsh")

	for _, command := range completableCommands {
		for _, flag := range command.flags {
			if !strings.Contains(zsh, flag) {
				t.Errorf("%s completion does not offer %s", command.name, flag)
			}

			// The command must actually accept it. A flag it does not define
			// produces "flag provided but not defined" and exit 2.
			var out, errOut bytes.Buffer
			args := []string{command.name, flag, "--dry-run", "--json"}
			if command.name == "staged" || command.name == "doctor" {
				args = []string{command.name, flag}
			}
			code := Run(args, strings.NewReader(""), &out, &errOut)
			if strings.Contains(errOut.String(), "not defined") {
				t.Errorf("%s completion offers %s but the command rejects it: %s",
					command.name, flag, strings.TrimSpace(errOut.String()))
			}
			_ = code
		}
	}
}

func TestUnsupportedShellIsRefused(t *testing.T) {
	for _, args := range [][]string{
		{"completion"},
		{"completion", "fish"},
		{"completion", "zsh", "extra"},
	} {
		var out, errOut bytes.Buffer
		if code := Run(args, strings.NewReader(""), &out, &errOut); code != 2 {
			t.Errorf("%v exited %d, want 2", args, code)
		}
		if out.Len() != 0 {
			t.Errorf("%v wrote a script anyway: %s", args, out.String())
		}
	}
}

// The helpers are called from inside a prompt, so a failure has to be silent.
// An error message printed into a completion is worse than no suggestions.
func TestHelpersStaySilentAndSucceedWithNothingToOffer(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	for _, helper := range []string{"__complete-batches", "__complete-apps"} {
		var out, errOut bytes.Buffer
		if code := Run([]string{helper}, strings.NewReader(""), &out, &errOut); code != 0 {
			t.Errorf("%s exited %d, it must not fail inside a prompt", helper, code)
		}
		if errOut.Len() != 0 {
			t.Errorf("%s wrote to stderr: %s", helper, errOut.String())
		}
	}
}

// Application names routinely contain spaces, so they are emitted one per line
// rather than whitespace separated. Splitting on spaces turns "Google Chrome"
// into two useless suggestions.
func TestAppNamesAreOnePerLine(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	app := filepath.Join(home, "Applications", "Two Words.app", "Contents")
	if err := os.MkdirAll(app, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	plist := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>CFBundleIdentifier</key><string>com.example.twowords</string>
<key>CFBundleName</key><string>Two Words</string>
</dict></plist>`
	if err := os.WriteFile(filepath.Join(app, "Info.plist"), []byte(plist), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	var out, errOut bytes.Buffer
	Run([]string{"__complete-apps"}, strings.NewReader(""), &out, &errOut)

	var found bool
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if line == "Two Words" {
			found = true
		}
	}
	if !found {
		t.Fatalf("a name with a space was not emitted whole:\n%s", out.String())
	}
}

// The scripts are generated by string building, which is exactly where an
// unbalanced quote hides until someone sources the file. These are weak
// structural checks; the real syntax check is zsh -n and bash -n, run against
// the emitted scripts outside the test suite.
func TestGeneratedScriptsAreStructurallySound(t *testing.T) {
	zsh := completionScript(t, "zsh")
	if !strings.HasPrefix(zsh, "#compdef wtff") {
		t.Error("a zsh completion has to start with its #compdef line to be autoloaded")
	}
	if strings.Count(zsh, "case ") != strings.Count(zsh, "esac") {
		t.Errorf("unbalanced case and esac: %d against %d",
			strings.Count(zsh, "case "), strings.Count(zsh, "esac"))
	}

	bash := completionScript(t, "bash")
	if !strings.Contains(bash, "complete -F _wtff wtff") {
		t.Error("the bash script never registers the completion function")
	}
	if strings.Count(bash, "case ") != strings.Count(bash, "esac") {
		t.Error("unbalanced case and esac in the bash script")
	}
	// macOS ships bash 3.2, so anything from bash 4 fails on the system this
	// is written for.
	for _, modern := range []string{"mapfile", "readarray", "declare -A"} {
		if strings.Contains(bash, modern) {
			t.Errorf("the bash script uses %q, which bash 3.2 on macOS does not have", modern)
		}
	}
}
