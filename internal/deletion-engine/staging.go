package deletionengine

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	pathvalidation "github.com/lesliemunmus/wtff/internal/path-validation"
	"golang.org/x/sys/unix"
)

const stagingSchemaVersion = 1

// ErrCrossVolume is returned when a target cannot be staged because it lives on
// a different volume from the staging area.
//
// This is refused rather than worked around. Moving across volumes means copy
// then delete, and a copy that silently loses extended attributes, access
// control entries, or hard link structure would make undo a lie: the caller
// would be told the change was reversible, and what came back would not be what
// left. Refusing is the honest answer until a copy that preserves those things
// is written and tested.
var ErrCrossVolume = errors.New("target is on a different volume from the staging area")

// StagedItem records one entry's journey into the staging area, with enough
// detail for undo to reverse it without consulting anything else.
type StagedItem struct {
	Index         int    `json:"index"`
	OriginalPath  string `json:"original_path"`
	RequestedPath string `json:"requested_path"`
	StagedName    string `json:"staged_name"`
	Device        uint64 `json:"device"`
	Inode         uint64 `json:"inode"`
	SizeBytes     int64  `json:"size_bytes"`
	SizeKnown     bool   `json:"size_known"`
	IsDir         bool   `json:"is_dir"`
	IsSymlink     bool   `json:"is_symlink"`
	RuleID        string `json:"rule_id"`
	Reason        string `json:"reason"`
}

// Batch is the record of one staging operation, written to disk so that undo
// works in a later process.
type Batch struct {
	Version   int          `json:"version"`
	BatchID   string       `json:"batch_id"`
	CreatedAt time.Time    `json:"created_at"`
	Command   string       `json:"command"`
	Digest    string       `json:"manifest_digest"`
	Items     []StagedItem `json:"items"`

	// dir is where this batch lives. It is not serialized: a batch record that
	// carried its own location would be trusted to say where it is, and a moved
	// or hand edited record could then point undo somewhere else.
	dir string
}

// Dir reports the directory holding this batch.
func (b *Batch) Dir() string { return b.dir }

// StagingArea manages the directory wtff moves removed items into.
type StagingArea struct {
	root string
}

// DefaultStagingRoot returns the conventional staging location.
func DefaultStagingRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return filepath.Join(home, "Library", "Application Support", "wtff", "staging"), nil
}

// NewStagingArea prepares a staging area rooted at the given path.
func NewStagingArea(root string) (*StagingArea, error) {
	// Owner only, because staged items keep whatever they contained and the
	// staging area must not widen access to anything.
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("cannot create staging area: %w", err)
	}
	return &StagingArea{root: root}, nil
}

// Root reports the staging area location.
func (s *StagingArea) Root() string { return s.root }

// beginBatch creates a directory for one staging run.
func (s *StagingArea) beginBatch(command, digest string) (*Batch, int, error) {
	var suffix [6]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return nil, -1, fmt.Errorf("cannot generate batch identifier: %w", err)
	}
	// A timestamp alone collides when two runs start inside the same second,
	// and a random value alone sorts meaninglessly in a directory listing.
	id := fmt.Sprintf("%s-%s", time.Now().UTC().Format("20060102-150405"), hex.EncodeToString(suffix[:]))
	dir := filepath.Join(s.root, id)
	itemsDir := filepath.Join(dir, "items")
	if err := os.MkdirAll(itemsDir, 0o700); err != nil {
		return nil, -1, fmt.Errorf("cannot create batch directory: %w", err)
	}

	itemsFD, err := unix.Open(itemsDir, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, -1, fmt.Errorf("cannot open batch directory: %w", err)
	}

	return &Batch{
		Version:   stagingSchemaVersion,
		BatchID:   id,
		CreatedAt: time.Now().UTC(),
		Command:   command,
		Digest:    digest,
		dir:       dir,
	}, itemsFD, nil
}

// stage moves one resolved target into the batch directory.
//
// The move is a descriptor relative rename, so it acts on the directory that
// validation pinned rather than on a path resolved a second time. A rename is
// atomic: the entry is either at its original location or in staging, never
// partially in both.
func stageEntry(resolved *pathvalidation.Resolved, itemsFD int, stagedName string) error {
	err := unix.Renameat(resolved.ParentFD(), resolved.LeafName(), itemsFD, stagedName)
	if err == nil {
		return nil
	}
	if errors.Is(err, unix.EXDEV) {
		return ErrCrossVolume
	}
	return fmt.Errorf("cannot move into staging: %w", err)
}

// stagedNameFor builds a collision free name for a staged item.
//
// The index prefix is what guarantees uniqueness. Two different directories can
// each hold a "Cache", and a name derived only from the original would have one
// overwrite the other, destroying the very item staging exists to preserve.
func stagedNameFor(index int, originalPath string) string {
	base := filepath.Base(originalPath)
	if base == "" || base == "." || base == string(os.PathSeparator) {
		base = "item"
	}
	// Trim to stay inside the per component name limit. The index carries
	// uniqueness, so truncating the readable part cannot cause a collision, and
	// a staged name that the filesystem refuses would turn a removal that
	// already succeeded into an unrecoverable one.
	const maxBaseNameBytes = 180
	if len(base) > maxBaseNameBytes {
		base = base[:maxBaseNameBytes]
	}
	return fmt.Sprintf("%04d-%s", index, base)
}

// writeRecord persists the batch record.
//
// The record is written after the moves, then fsynced. If wtff is interrupted
// mid batch the items are already in staging with no record, which is
// recoverable by hand and visible on inspection. The reverse order would give a
// record naming items that were never moved, which reads as authoritative and
// is not.
func (b *Batch) writeRecord() error {
	path := filepath.Join(b.dir, "batch.json")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("cannot write batch record: %w", err)
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(b); err != nil {
		return fmt.Errorf("cannot encode batch record: %w", err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("cannot flush batch record: %w", err)
	}
	return nil
}

// LoadBatch reads a batch record from a batch directory.
func LoadBatch(dir string) (*Batch, error) {
	file, err := os.Open(filepath.Join(dir, "batch.json"))
	if err != nil {
		return nil, fmt.Errorf("cannot open batch record: %w", err)
	}
	defer file.Close()

	var batch Batch
	if err := json.NewDecoder(file).Decode(&batch); err != nil {
		return nil, fmt.Errorf("cannot decode batch record: %w", err)
	}
	if batch.Version != stagingSchemaVersion {
		return nil, fmt.Errorf("%w: batch record version %d", ErrUnsupportedVersion, batch.Version)
	}
	// The location comes from where the record was found, never from the record
	// itself, so a record cannot redirect undo somewhere else.
	batch.dir = dir
	return &batch, nil
}

// ListBatches returns the batches currently held in the staging area, oldest
// first.
func (s *StagingArea) ListBatches() ([]*Batch, error) {
	entries, err := os.ReadDir(s.root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("cannot read staging area: %w", err)
	}

	var batches []*Batch
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		batch, loadErr := LoadBatch(filepath.Join(s.root, entry.Name()))
		if loadErr != nil {
			// A directory without a readable record is left alone rather than
			// skipped silently at a higher level: it may be a batch that was
			// interrupted before its record was written, and deleting it would
			// discard recoverable data.
			continue
		}
		batches = append(batches, batch)
	}
	return batches, nil
}
