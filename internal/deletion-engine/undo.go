package deletionengine

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	operationlog "github.com/lesliemusengi/wtff/internal/operation-log"
	pathvalidation "github.com/lesliemusengi/wtff/internal/path-validation"
	"golang.org/x/sys/unix"
)

// RestoreOutcome describes what happened to one staged item during undo.
type RestoreOutcome struct {
	Item     StagedItem
	Restored bool
	Reason   string
	Err      error
}

// RestoreResult summarizes an undo run.
type RestoreResult struct {
	Outcomes      []RestoreOutcome
	RestoredCount int
	SkippedCount  int
	FailedCount   int
}

// ErrOriginalOccupied is returned when the place an item came from now holds
// something else.
var ErrOriginalOccupied = errors.New("original location is occupied")

// Undo returns the items in a batch to where they came from.
//
// Restoring never overwrites. If something now occupies an item's original
// location, that item is left in staging and reported. The alternative,
// replacing whatever is there, would make undo capable of destroying data that
// was never part of the original operation, which is a worse failure than an
// incomplete restore.
func Undo(batch *Batch, log *operationlog.Writer) (*RestoreResult, error) {
	if batch == nil {
		return nil, errors.New("no batch supplied")
	}

	itemsDir := filepath.Join(batch.dir, "items")
	itemsFD, err := unix.Open(itemsDir, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("cannot open staged items: %w", err)
	}
	defer unix.Close(itemsFD)

	result := &RestoreResult{}
	var remaining []StagedItem

	for _, item := range batch.Items {
		outcome := restoreItem(item, itemsFD)
		result.Outcomes = append(result.Outcomes, outcome)

		switch {
		case outcome.Err != nil:
			result.FailedCount++
			remaining = append(remaining, item)
			log.Record(operationlog.Event{
				Command: "undo",
				Kind:    operationlog.KindFailed,
				Path:    item.OriginalPath,
				Outcome: "failed",
				Detail:  outcome.Err.Error(),
				BatchID: batch.BatchID,
			})
		case !outcome.Restored:
			result.SkippedCount++
			remaining = append(remaining, item)
			log.Record(operationlog.Event{
				Command: "undo",
				Kind:    operationlog.KindSkipped,
				Path:    item.OriginalPath,
				Outcome: "skipped",
				Detail:  outcome.Reason,
				BatchID: batch.BatchID,
			})
		default:
			result.RestoredCount++
			log.Record(operationlog.Event{
				Command:   "undo",
				Kind:      operationlog.KindRestored,
				Path:      item.OriginalPath,
				Outcome:   "restored",
				Bytes:     item.SizeBytes,
				SizeKnown: item.SizeKnown,
				BatchID:   batch.BatchID,
			})
		}
	}

	// The record is rewritten to describe what is still staged. An undo that
	// restored some items but left the record claiming all of them are present
	// would make a second undo report failures for items that are already home.
	batch.Items = remaining
	if len(remaining) == 0 {
		if err := os.RemoveAll(batch.dir); err != nil {
			return result, fmt.Errorf("restored everything but could not clear the batch: %w", err)
		}
		return result, nil
	}
	if err := batch.writeRecord(); err != nil {
		return result, fmt.Errorf("restored %d items but could not update the batch record: %w",
			result.RestoredCount, err)
	}
	return result, nil
}

// restoreItem moves one staged item back.
func restoreItem(item StagedItem, itemsFD int) RestoreOutcome {
	parentDir := filepath.Dir(item.OriginalPath)
	leafName := filepath.Base(item.OriginalPath)

	// The original parent must exist, and must be reached the same link safe
	// way every other path in wtff is reached.
	//
	// Opening it directly would follow links on the final component, so a parent
	// directory replaced by a link while the item sat in staging would have undo
	// write the user's restored data into whatever that link names. Restoring is
	// a write, and a misdirected write leaks rather than destroys, which makes
	// it quieter and no less real.
	//
	// Recreating a missing parent is deliberately not attempted: wtff would have
	// to invent an owner and permissions for a directory it did not remove, and
	// guessing those is how a restore silently widens access.
	container, err := pathvalidation.ResolveDirectory(parentDir)
	if err != nil {
		return RestoreOutcome{
			Item:   item,
			Reason: "original parent directory is unavailable: " + err.Error(),
		}
	}
	defer container.Close()
	parentFD := container.FD()

	var existing unix.Stat_t
	if statErr := unix.Fstatat(parentFD, leafName, &existing, unix.AT_SYMLINK_NOFOLLOW); statErr == nil {
		return RestoreOutcome{
			Item:   item,
			Reason: "something else now occupies " + item.OriginalPath,
		}
	} else if !errors.Is(statErr, unix.ENOENT) {
		return RestoreOutcome{
			Item: item,
			Err:  fmt.Errorf("cannot inspect the original location: %w", statErr),
		}
	}

	if err := unix.Renameat(itemsFD, item.StagedName, parentFD, leafName); err != nil {
		if errors.Is(err, unix.ENOENT) {
			return RestoreOutcome{
				Item:   item,
				Reason: "staged copy is missing from the batch",
			}
		}
		if errors.Is(err, unix.EXDEV) {
			return RestoreOutcome{
				Item:   item,
				Reason: "original location is on a different volume from the staging area",
			}
		}
		return RestoreOutcome{Item: item, Err: fmt.Errorf("cannot restore: %w", err)}
	}

	return RestoreOutcome{Item: item, Restored: true}
}
