package deletionengine

import (
	"bytes"
	"errors"
	"testing"
	"time"
)

func sampleManifest() *Manifest {
	m := &Manifest{
		Version:   manifestVersion,
		CreatedAt: time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC),
		Command:   "clean",
		Action:    ActionStage,
		Entries: []Entry{
			{
				RequestedPath: "/Users/example/Library/Caches/one",
				ResolvedPath:  "/Users/example/Library/Caches/one",
				Device:        1, Inode: 100,
				SizeBytes: 2048, SizeKnown: true,
				IsDir: true, RuleID: "cache-one", Reason: "rebuildable cache",
			},
			{
				RequestedPath: "/Users/example/Library/Caches/two",
				ResolvedPath:  "/Users/example/Library/Caches/two",
				Device:        1, Inode: 200,
				SizeBytes: 1024, SizeKnown: true,
				RuleID: "cache-two", Reason: "rebuildable cache",
			},
		},
		TotalBytes: 3072,
	}
	m.Seal()
	return m
}

func TestSealedManifestVerifies(t *testing.T) {
	if err := sampleManifest().VerifyDigest(); err != nil {
		t.Fatalf("VerifyDigest on a freshly sealed manifest = %v, want nil", err)
	}
}

// Every field the digest covers is a field an attacker or a bug could change to
// alter what gets deleted. Each is mutated in turn and must be caught.
func TestDigestCoversEveryFieldThatChangesBehavior(t *testing.T) {
	mutations := map[string]func(*Manifest){
		"resolved path": func(m *Manifest) {
			m.Entries[0].ResolvedPath = "/Users/example/Documents"
		},
		"requested path": func(m *Manifest) {
			m.Entries[0].RequestedPath = "/Users/example/Documents"
		},
		"device": func(m *Manifest) { m.Entries[0].Device = 9 },
		"inode":  func(m *Manifest) { m.Entries[0].Inode = 999 },
		"action to purge": func(m *Manifest) {
			m.Action = ActionPurge
		},
		"command":     func(m *Manifest) { m.Command = "uninstall" },
		"size":        func(m *Manifest) { m.Entries[0].SizeBytes = 1 },
		"size known":  func(m *Manifest) { m.Entries[0].SizeKnown = false },
		"is dir":      func(m *Manifest) { m.Entries[0].IsDir = false },
		"is symlink":  func(m *Manifest) { m.Entries[0].IsSymlink = true },
		"rule id":     func(m *Manifest) { m.Entries[0].RuleID = "something-else" },
		"reason":      func(m *Manifest) { m.Entries[0].Reason = "different justification" },
		"total bytes": func(m *Manifest) { m.TotalBytes = 0 },
		"created at":  func(m *Manifest) { m.CreatedAt = m.CreatedAt.Add(time.Second) },
		"appended entry": func(m *Manifest) {
			m.Entries = append(m.Entries, Entry{
				ResolvedPath: "/Users/example/Documents",
				RuleID:       "injected", Reason: "injected",
			})
		},
		"removed entry":  func(m *Manifest) { m.Entries = m.Entries[:1] },
		"reordered":      func(m *Manifest) { m.Entries[0], m.Entries[1] = m.Entries[1], m.Entries[0] },
		"partial sizing": func(m *Manifest) { m.PartialSizing = true },
	}

	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			manifest := sampleManifest()
			mutate(manifest)
			if err := manifest.VerifyDigest(); !errors.Is(err, ErrDigestMismatch) {
				t.Fatalf("mutating %s went undetected: VerifyDigest = %v", name, err)
			}
		})
	}
}

// A digest built by concatenating variable length fields without recording
// where each ends can be preserved while the contents shift between fields.
// Splitting a path across an adjacent field is the cheapest form of that
// attack, so it is worth an explicit case rather than trusting the encoder.
func TestDigestResistsFieldBoundaryShifting(t *testing.T) {
	first := &Manifest{
		Version: manifestVersion, Command: "clean", Action: ActionStage,
		Entries: []Entry{{
			RequestedPath: "/tmp/ab", ResolvedPath: "/tmp/c",
			RuleID: "r", Reason: "x",
		}},
	}
	second := &Manifest{
		Version: manifestVersion, Command: "clean", Action: ActionStage,
		Entries: []Entry{{
			RequestedPath: "/tmp/a", ResolvedPath: "b/tmp/c",
			RuleID: "r", Reason: "x",
		}},
	}
	first.Seal()
	second.Seal()

	if first.Digest == second.Digest {
		t.Fatal("two manifests differing only in where a field boundary falls share a digest")
	}
}

func TestUnsupportedVersionIsRefused(t *testing.T) {
	manifest := sampleManifest()
	manifest.Version = 99
	if err := manifest.VerifyDigest(); !errors.Is(err, ErrUnsupportedVersion) {
		t.Fatalf("VerifyDigest on a future version = %v, want ErrUnsupportedVersion", err)
	}
}

// An unrecognized action must be refused rather than interpreted. Falling back
// to a default would mean guessing at an instruction in a list of things to
// delete.
func TestUnknownActionIsRefused(t *testing.T) {
	manifest := sampleManifest()
	manifest.Action = "obliterate"
	manifest.Seal()
	if err := manifest.VerifyDigest(); !errors.Is(err, ErrUnknownAction) {
		t.Fatalf("VerifyDigest with an unknown action = %v, want ErrUnknownAction", err)
	}
}

func TestManifestSurvivesRoundTrip(t *testing.T) {
	original := sampleManifest()

	var buf bytes.Buffer
	if err := original.Write(&buf); err != nil {
		t.Fatalf("Write: %v", err)
	}
	restored, err := ReadManifest(&buf)
	if err != nil {
		t.Fatalf("ReadManifest: %v", err)
	}
	if restored.Digest != original.Digest {
		t.Fatalf("digest changed across serialization: %s then %s", original.Digest, restored.Digest)
	}
	if len(restored.Entries) != len(original.Entries) {
		t.Fatalf("entry count changed across serialization")
	}
}

// Reading verifies. A manifest that has been decoded but not checked is exactly
// the object a caller might hand to apply by mistake, and nothing in the type
// distinguishes it from a verified one.
func TestReadingATamperedManifestFails(t *testing.T) {
	manifest := sampleManifest()
	var buf bytes.Buffer
	if err := manifest.Write(&buf); err != nil {
		t.Fatalf("Write: %v", err)
	}

	tampered := bytes.Replace(buf.Bytes(),
		[]byte(`"action": "stage"`), []byte(`"action": "purge"`), 1)
	if bytes.Equal(tampered, buf.Bytes()) {
		t.Fatal("test setup did not actually modify the serialized manifest")
	}

	if _, err := ReadManifest(bytes.NewReader(tampered)); err == nil {
		t.Fatal("ReadManifest accepted a manifest whose action was switched to purge")
	}
}
