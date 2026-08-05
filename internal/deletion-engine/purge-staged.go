package deletionengine

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	operationlog "github.com/lesliemusengi/wtff/internal/operation-log"
	"golang.org/x/sys/unix"
)

// ErrBatchOutsideStagingArea is returned when a batch does not sit directly
// inside the staging area it is being purged from.
//
// Purging is the one operation in this package that destroys without a way
// back, so it refuses to act on anything it cannot first prove is wtff's own
// scratch space. A batch assembled by hand, or loaded from a directory chosen
// by a caller, is exactly the shape a traversal takes.
var ErrBatchOutsideStagingArea = errors.New("batch is not inside this staging area")

// PurgeOutcome describes what happened to one staged item during a purge.
type PurgeOutcome struct {
	Item   StagedItem
	Purged bool
	Err    error
}

// PurgeResult summarizes a purge run over a staged batch.
type PurgeResult struct {
	Outcomes    []PurgeOutcome
	PurgedCount int
	FailedCount int

	// BytesReclaimed sums only the items whose size was known when they were
	// staged. SizePartial reports whether any were not, so a caller can say
	// "at least this much" rather than presenting a short count as exact.
	BytesReclaimed int64
	SizePartial    bool
}

// PurgeBatch permanently removes a staged batch and everything in it.
//
// This is the only path in wtff that destroys data the user had already been
// promised was recoverable, so it is deliberately narrow. It acts on a batch
// that came from this staging area, it descends by descriptor from the staging
// root rather than by path, and it refuses anything whose location it cannot
// account for.
//
// Protection rules are not re-checked here, and that is correct rather than an
// omission: every item in a batch passed the rule check at plan time, staging
// is the only way an item can get into a batch, and the rules describe original
// locations that these items no longer occupy. Re-running them against a staged
// name would be checking the wrong thing and reporting it as safety.
func (s *StagingArea) PurgeBatch(batch *Batch, log *operationlog.Writer) (*PurgeResult, error) {
	if batch == nil {
		return nil, errors.New("no batch supplied")
	}

	name, err := s.containedBatchName(batch)
	if err != nil {
		return nil, err
	}

	// The removal descends from the staging root held open as a descriptor, so
	// the name resolved below is a child of the directory that was verified,
	// not of whatever that path happens to name a moment later.
	rootFD, err := unix.Open(s.root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("cannot open staging area: %w", err)
	}
	defer unix.Close(rootFD)

	itemsFD, err := unix.Openat(rootFD, filepath.Join(name, "items"),
		unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("cannot open staged items: %w", err)
	}

	result := &PurgeResult{}
	var remaining []StagedItem

	for _, item := range batch.Items {
		outcome := PurgeOutcome{Item: item}
		if removeErr := removeTreeAt(itemsFD, item.StagedName, 0); removeErr != nil {
			outcome.Err = removeErr
			result.FailedCount++
			remaining = append(remaining, item)
			log.Record(operationlog.Event{
				Command: "purge",
				Kind:    operationlog.KindFailed,
				Path:    item.OriginalPath,
				Outcome: "failed",
				Detail:  removeErr.Error(),
				BatchID: batch.BatchID,
			})
		} else {
			outcome.Purged = true
			result.PurgedCount++
			if item.SizeKnown {
				result.BytesReclaimed += item.SizeBytes
			} else {
				result.SizePartial = true
			}
			// The original path is what gets logged, not the staged name. A
			// person auditing this later needs to know what was destroyed, and
			// "0007-Cache" names nothing they would recognize.
			log.Record(operationlog.Event{
				Command:   "purge",
				Kind:      operationlog.KindPurged,
				Path:      item.OriginalPath,
				Outcome:   "purged",
				Bytes:     item.SizeBytes,
				SizeKnown: item.SizeKnown,
				BatchID:   batch.BatchID,
			})
		}
		result.Outcomes = append(result.Outcomes, outcome)
	}
	_ = unix.Close(itemsFD)

	// A batch that lost some items but not all keeps a record describing only
	// what is still there, matching what Undo does after a partial restore. A
	// record that still claimed the destroyed items were present would offer a
	// restore that cannot happen.
	batch.Items = remaining
	if len(remaining) > 0 {
		if err := batch.writeRecord(); err != nil {
			return result, fmt.Errorf("purged %d items but could not update the batch record: %w",
				result.PurgedCount, err)
		}
		return result, nil
	}

	if err := removeTreeAt(rootFD, name, 0); err != nil {
		return result, fmt.Errorf("purged everything but could not clear the batch: %w", err)
	}
	return result, nil
}

// PurgeAll permanently removes every batch in the staging area, reporting the
// first failure while continuing past it so one unreadable batch does not
// strand the rest.
func (s *StagingArea) PurgeAll(log *operationlog.Writer) (*PurgeResult, error) {
	batches, err := s.ListBatches()
	if err != nil {
		return nil, err
	}

	combined := &PurgeResult{}
	var firstFailure error
	for _, batch := range batches {
		result, purgeErr := s.PurgeBatch(batch, log)
		if result != nil {
			combined.Outcomes = append(combined.Outcomes, result.Outcomes...)
			combined.PurgedCount += result.PurgedCount
			combined.FailedCount += result.FailedCount
			combined.BytesReclaimed += result.BytesReclaimed
			combined.SizePartial = combined.SizePartial || result.SizePartial
		}
		if purgeErr != nil && firstFailure == nil {
			firstFailure = purgeErr
		}
	}
	return combined, firstFailure
}

// containedBatchName returns the batch's directory name after proving it sits
// directly inside this staging area.
//
// Both the record's own directory and its identifier are checked. The
// identifier is what a person types on the command line, and it reaching a
// path join unvalidated is how a batch id of "../../.." becomes a deletion of
// something that was never staged.
func (s *StagingArea) containedBatchName(batch *Batch) (string, error) {
	dir := filepath.Clean(batch.dir)
	root := filepath.Clean(s.root)
	if dir == root || filepath.Dir(dir) != root {
		return "", fmt.Errorf("%w: %s", ErrBatchOutsideStagingArea, batch.dir)
	}

	name := filepath.Base(dir)
	if err := validBatchName(name); err != nil {
		return "", err
	}
	// The record's identifier is not trusted to agree with where the record was
	// found. If they disagree, something rewrote one of them, and guessing
	// which is authoritative is not a decision to make while holding a delete.
	if batch.BatchID != "" && batch.BatchID != name {
		return "", fmt.Errorf("%w: record claims id %q but lives in %q",
			ErrBatchOutsideStagingArea, batch.BatchID, name)
	}
	return name, nil
}

// validBatchName rejects anything that is not a single ordinary path
// component.
func validBatchName(name string) error {
	if name == "" || name == "." || name == ".." {
		return fmt.Errorf("%w: %q is not a batch directory", ErrBatchOutsideStagingArea, name)
	}
	if strings.ContainsRune(name, os.PathSeparator) || strings.Contains(name, "/") {
		return fmt.Errorf("%w: %q is not a single path component", ErrBatchOutsideStagingArea, name)
	}
	return nil
}

// FindBatch returns the batch with the given identifier.
//
// The identifier is validated as a path component before it is joined to
// anything, so a crafted id cannot reach outside the staging area even to be
// read.
func (s *StagingArea) FindBatch(batchID string) (*Batch, error) {
	if err := validBatchName(batchID); err != nil {
		return nil, err
	}
	batch, err := LoadBatch(filepath.Join(s.root, batchID))
	if err != nil {
		return nil, fmt.Errorf("no staged batch %q: %w", batchID, err)
	}
	return batch, nil
}
