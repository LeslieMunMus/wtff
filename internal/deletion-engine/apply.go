package deletionengine

import (
	"errors"
	"fmt"
	"os"

	operationlog "github.com/lesliemusengi/wtff/internal/operation-log"
	pathvalidation "github.com/lesliemusengi/wtff/internal/path-validation"
	"golang.org/x/sys/unix"
)

// ApplyOptions configures an apply run.
type ApplyOptions struct {
	// Staging is where reversible removals are moved. Required when the
	// manifest action is stage.
	Staging *StagingArea

	// Log receives per entry events. A nil log discards them.
	Log *operationlog.Writer

	// Policy is re-consulted at apply time. Passing the same checker used for
	// planning is the expected case; re-running it is not redundant, because a
	// manifest can be applied long after it was planned and the rule set may
	// have changed in between.
	Policy PolicyChecker
}

// Outcome describes what happened to one entry.
type Outcome struct {
	Entry   Entry
	Applied bool
	Skipped bool
	Reason  string
	Err     error
}

// Result summarizes an apply run.
type Result struct {
	Outcomes     []Outcome
	Batch        *Batch
	AppliedCount int
	SkippedCount int
	FailedCount  int
	BytesApplied int64
}

// ErrNoStagingArea is returned when a staging manifest is applied without one.
var ErrNoStagingArea = errors.New("a staging action requires a staging area")

// Apply executes a manifest.
//
// The manifest is the only description of work accepted. There is no variant
// taking a path list, because the digest check is what makes a reviewed plan
// meaningful, and an alternative entry point would quietly bypass it.
//
// A failing entry does not abort the run. Cleanup involves many independent
// items, and stopping at the first one that changed underneath us would leave
// the operation half done with no clear report. Every entry is attempted, and
// every outcome is recorded.
func Apply(manifest *Manifest, opts ApplyOptions) (*Result, error) {
	if err := manifest.VerifyDigest(); err != nil {
		return nil, err
	}
	if opts.Policy == nil {
		return nil, ErrNoPolicy
	}
	if manifest.Action == ActionStage && opts.Staging == nil {
		return nil, ErrNoStagingArea
	}

	self := newSelfProtection()
	result := &Result{}

	var batch *Batch
	itemsFD := -1
	if manifest.Action == ActionStage {
		var err error
		batch, itemsFD, err = opts.Staging.beginBatch(manifest.Command, manifest.Digest)
		if err != nil {
			return nil, err
		}
		defer func() {
			if itemsFD >= 0 {
				_ = unix.Close(itemsFD)
			}
		}()
	}

	for index, entry := range manifest.Entries {
		outcome := applyEntry(entry, index+1, manifest, opts, self, batch, itemsFD)
		result.Outcomes = append(result.Outcomes, outcome)

		switch {
		case outcome.Err != nil:
			result.FailedCount++
			opts.Log.Record(operationlog.Event{
				Command: manifest.Command,
				Kind:    operationlog.KindFailed,
				Path:    entry.ResolvedPath,
				Outcome: "failed",
				Detail:  outcome.Err.Error(),
				BatchID: batchID(batch),
			})
		case outcome.Skipped:
			result.SkippedCount++
			opts.Log.Record(operationlog.Event{
				Command: manifest.Command,
				Kind:    operationlog.KindSkipped,
				Path:    entry.ResolvedPath,
				Outcome: "skipped",
				Detail:  outcome.Reason,
				BatchID: batchID(batch),
			})
		default:
			result.AppliedCount++
			if entry.SizeKnown {
				result.BytesApplied += entry.SizeBytes
			}
			kind := operationlog.KindStaged
			if manifest.Action == ActionPurge {
				kind = operationlog.KindPurged
			}
			opts.Log.Record(operationlog.Event{
				Command:   manifest.Command,
				Kind:      kind,
				Path:      entry.ResolvedPath,
				Outcome:   string(manifest.Action),
				Detail:    entry.Reason,
				Bytes:     entry.SizeBytes,
				SizeKnown: entry.SizeKnown,
				BatchID:   batchID(batch),
			})
		}
	}

	if batch != nil {
		if len(batch.Items) == 0 {
			// An empty batch directory is noise in the staging area and would
			// be reported by a later list as a recoverable batch holding
			// nothing. Remove the directory wtff just created itself.
			_ = os.RemoveAll(batch.dir)
			batch = nil
		} else if err := batch.writeRecord(); err != nil {
			// The items are already moved. Reporting success without a record
			// would leave them unrecoverable by undo, so this is an error for
			// the run even though the individual moves succeeded.
			return result, fmt.Errorf("staged %d items but could not write the batch record: %w",
				len(batch.Items), err)
		}
		result.Batch = batch
	}

	return result, nil
}

func batchID(batch *Batch) string {
	if batch == nil {
		return ""
	}
	return batch.BatchID
}

// applyEntry runs the gates for a single entry and performs the action.
func applyEntry(
	entry Entry,
	index int,
	manifest *Manifest,
	opts ApplyOptions,
	self selfProtection,
	batch *Batch,
	itemsFD int,
) Outcome {
	// Re-resolve from the recorded path. This is a fresh resolution, so it
	// carries every structural check again, and the identity comparison below
	// is what ties it back to what was planned.
	resolved, err := pathvalidation.Resolve(entry.ResolvedPath)
	if err != nil {
		switch {
		case errors.Is(err, pathvalidation.ErrNotFound):
			return Outcome{Entry: entry, Skipped: true, Reason: "no longer exists"}
		case errors.Is(err, pathvalidation.ErrDenied):
			return Outcome{Entry: entry, Skipped: true, Reason: "now denied by the structural floor"}
		default:
			return Outcome{Entry: entry, Skipped: true, Reason: "could not be validated: " + err.Error()}
		}
	}
	defer resolved.Close()

	// The identity recorded at plan time is the link between the reviewed plan
	// and this moment. A path that now names a different object is a different
	// deletion from the one that was approved.
	if resolved.Identity() != entry.Identity() {
		return Outcome{
			Entry:   entry,
			Skipped: true,
			Reason:  "object changed since the plan was made",
		}
	}

	if root, covered := self.covers(resolved.Path()); covered {
		return Outcome{Entry: entry, Skipped: true, Reason: "belongs to wtff's own state under " + root}
	}

	if ruleID, reason, protected := opts.Policy.Protected(resolved.Path()); protected {
		return Outcome{
			Entry:   entry,
			Skipped: true,
			Reason:  fmt.Sprintf("protected by %s: %s", ruleID, reason),
		}
	}

	// Last check before acting, against the pinned parent descriptor.
	if err := resolved.Verify(); err != nil {
		return Outcome{Entry: entry, Skipped: true, Reason: "changed immediately before the operation"}
	}

	switch manifest.Action {
	case ActionStage:
		stagedName := stagedNameFor(index, resolved.Path())
		if err := stageEntry(resolved, itemsFD, stagedName); err != nil {
			if errors.Is(err, ErrCrossVolume) {
				return Outcome{
					Entry:   entry,
					Skipped: true,
					Reason:  "on a different volume from the staging area, which is not yet supported",
				}
			}
			return Outcome{Entry: entry, Err: err}
		}
		batch.Items = append(batch.Items, StagedItem{
			Index:         index,
			OriginalPath:  resolved.Path(),
			RequestedPath: entry.RequestedPath,
			StagedName:    stagedName,
			Device:        entry.Device,
			Inode:         entry.Inode,
			SizeBytes:     entry.SizeBytes,
			SizeKnown:     entry.SizeKnown,
			IsDir:         entry.IsDir,
			IsSymlink:     entry.IsSymlink,
			RuleID:        entry.RuleID,
			Reason:        entry.Reason,
		})
		return Outcome{Entry: entry, Applied: true}

	case ActionPurge:
		if err := purgeEntry(resolved); err != nil {
			return Outcome{Entry: entry, Err: err}
		}
		return Outcome{Entry: entry, Applied: true}

	default:
		return Outcome{Entry: entry, Err: fmt.Errorf("%w: %q", ErrUnknownAction, manifest.Action)}
	}
}

// purgeEntry removes a target irreversibly, entirely through descriptors.
//
// The recursion is descriptor relative rather than path based so that the
// removal cannot be redirected after the target was verified. See removeTreeAt.
func purgeEntry(resolved *pathvalidation.Resolved) error {
	return removeTreeAt(resolved.ParentFD(), resolved.LeafName(), 0)
}
