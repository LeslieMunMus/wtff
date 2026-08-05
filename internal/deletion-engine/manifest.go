package deletionengine

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	pathvalidation "github.com/lesliemusengi/wtff/internal/path-validation"
)

// manifestVersion is the schema version of a serialized manifest. A manifest
// written by a different version is refused rather than interpreted, because
// guessing at the meaning of a field in a list of things to delete is not a
// recoverable kind of wrong.
const manifestVersion = 1

// Action is what apply will do with every entry in a manifest.
//
// It is a property of the manifest rather than of each entry on purpose. A
// mixed manifest, where some entries are reversible and others are not, cannot
// be reviewed at a glance, and the whole point of showing a plan before acting
// is that a person can take it in.
type Action string

const (
	// ActionStage moves entries into the staging area, where undo can retrieve
	// them until the retention window passes. This is the default.
	ActionStage Action = "stage"

	// ActionPurge removes entries irreversibly. Reserved for cases where
	// staging is not meaningful, and never selected implicitly.
	ActionPurge Action = "purge"
)

// Entry is one thing a plan proposes to remove.
type Entry struct {
	// RequestedPath is the path a rule or discovery step named.
	RequestedPath string `json:"requested_path"`

	// ResolvedPath is where that path actually leads once links are expanded.
	// The two are both recorded because they can differ, and a person reviewing
	// a plan needs to see when they do.
	ResolvedPath string `json:"resolved_path"`

	// Device and Inode identify the object that was validated. Apply compares
	// these before acting and refuses an entry whose identity has changed.
	Device uint64 `json:"device"`
	Inode  uint64 `json:"inode"`

	// SizeBytes is the measured size. SizeKnown distinguishes a genuine zero
	// from a measurement that failed, which the log must never conflate.
	SizeBytes int64 `json:"size_bytes"`
	SizeKnown bool  `json:"size_known"`

	IsDir     bool `json:"is_dir"`
	IsSymlink bool `json:"is_symlink"`

	// RuleID and Reason record why this entry is in the plan. An entry that
	// cannot say why it is here does not belong in a plan a person is being
	// asked to approve.
	RuleID string `json:"rule_id"`
	Reason string `json:"reason"`
}

// Identity returns the entry's captured identity in the form path validation
// reports it.
func (e Entry) Identity() pathvalidation.Identity {
	return pathvalidation.Identity{Device: e.Device, Inode: e.Inode}
}

// Manifest is the reviewable, tamper evident description of a proposed change.
type Manifest struct {
	Version   int       `json:"version"`
	CreatedAt time.Time `json:"created_at"`
	Command   string    `json:"command"`
	Action    Action    `json:"action"`
	Entries   []Entry   `json:"entries"`

	// TotalBytes sums the entries whose size is known.
	TotalBytes int64 `json:"total_bytes"`

	// PartialSizing is true when at least one entry could not be measured, so
	// a reader knows TotalBytes is a floor rather than a total.
	PartialSizing bool `json:"partial_sizing"`

	// Digest covers every field above. Apply recomputes it and refuses to run
	// against a manifest that has changed since it was produced.
	Digest string `json:"digest"`
}

var (
	// ErrDigestMismatch means the manifest changed after it was generated.
	ErrDigestMismatch = errors.New("manifest digest does not match its contents")

	// ErrUnsupportedVersion means the manifest came from a different schema.
	ErrUnsupportedVersion = errors.New("unsupported manifest version")

	// ErrUnknownAction means the manifest requests something this build cannot
	// perform. Refusing is the only safe reading of an unrecognized instruction
	// in a list of things to delete.
	ErrUnknownAction = errors.New("unknown manifest action")
)

// canonicalBytes produces the exact byte sequence the digest is taken over.
//
// Every variable length field is written with an explicit length prefix. Plain
// concatenation is ambiguous: the paths "/ab" and "/c" produce the same bytes
// as "/a" and "/bc", so an attacker able to influence two entries could swap
// what gets deleted while leaving the digest unchanged. Length prefixing makes
// each field's boundary part of the hashed input.
//
// Encoding is done by hand rather than through JSON because a digest is only
// as trustworthy as the determinism of its input, and JSON leaves too much
// unpinned: field ordering on decode and re encode, numeric formatting, and
// how a zero time renders.
func (m *Manifest) canonicalBytes() []byte {
	var buf []byte

	writeUint := func(v uint64) {
		var scratch [8]byte
		binary.BigEndian.PutUint64(scratch[:], v)
		buf = append(buf, scratch[:]...)
	}
	writeString := func(s string) {
		writeUint(uint64(len(s)))
		buf = append(buf, s...)
	}
	writeBool := func(b bool) {
		if b {
			buf = append(buf, 1)
			return
		}
		buf = append(buf, 0)
	}

	writeString("wtff-manifest")
	writeUint(uint64(m.Version))
	writeUint(uint64(m.CreatedAt.UTC().UnixNano()))
	writeString(m.Command)
	writeString(string(m.Action))
	writeUint(uint64(len(m.Entries)))

	for _, entry := range m.Entries {
		writeString(entry.RequestedPath)
		writeString(entry.ResolvedPath)
		writeUint(entry.Device)
		writeUint(entry.Inode)
		writeUint(uint64(entry.SizeBytes))
		writeBool(entry.SizeKnown)
		writeBool(entry.IsDir)
		writeBool(entry.IsSymlink)
		writeString(entry.RuleID)
		writeString(entry.Reason)
	}

	writeUint(uint64(m.TotalBytes))
	writeBool(m.PartialSizing)
	return buf
}

// Seal computes and stores the manifest digest. It is called once, when a plan
// is complete.
func (m *Manifest) Seal() {
	sum := sha256.Sum256(m.canonicalBytes())
	m.Digest = hex.EncodeToString(sum[:])
}

// VerifyDigest recomputes the digest and reports whether the manifest still
// describes what it described when it was sealed.
func (m *Manifest) VerifyDigest() error {
	if m.Version != manifestVersion {
		return fmt.Errorf("%w: found %d, this build understands %d",
			ErrUnsupportedVersion, m.Version, manifestVersion)
	}
	switch m.Action {
	case ActionStage, ActionPurge:
	default:
		return fmt.Errorf("%w: %q", ErrUnknownAction, m.Action)
	}
	sum := sha256.Sum256(m.canonicalBytes())
	if hex.EncodeToString(sum[:]) != m.Digest {
		return ErrDigestMismatch
	}
	return nil
}

// Write serializes a manifest.
func (m *Manifest) Write(w io.Writer) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(m)
}

// ReadManifest deserializes a manifest and verifies it before returning it.
//
// Verification happens here rather than being left to the caller. A manifest
// that has been read but not checked is exactly the object a caller might pass
// to apply by mistake, and the type system cannot tell the two apart.
func ReadManifest(r io.Reader) (*Manifest, error) {
	var manifest Manifest
	if err := json.NewDecoder(r).Decode(&manifest); err != nil {
		return nil, fmt.Errorf("cannot decode manifest: %w", err)
	}
	if err := manifest.VerifyDigest(); err != nil {
		return nil, err
	}
	return &manifest, nil
}
