package deletionengine

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"time"

	operationlog "github.com/lesliemusengi/wtff/internal/operation-log"
	pathvalidation "github.com/lesliemusengi/wtff/internal/path-validation"
)

// maxSizeWalkEntries bounds how many entries a directory measurement visits.
//
// A tree larger than this is reported with the size measured so far and
// SizeKnown false, so the manifest says the number is incomplete instead of
// presenting a partial sum as a total.
const maxSizeWalkEntries = 500_000

// maxSizeWalkDuration bounds how long one directory measurement may take, and
// maxTotalMeasureDuration bounds the whole planning run's measuring.
//
// The entry cap alone does not bound time, which is the gap that produced a
// real multi hour hang: it counts entries, and a walk stalled inside a single
// filesystem call never reaches the next entry to be counted. A network mount
// that stops answering, an unresponsive user space filesystem, or a disk going
// to sleep all stall in exactly that way.
//
// A deadline checked between entries would not help either, for the same
// reason, so the walk runs on its own goroutine and is abandoned if it does not
// finish in time. The result is an honest "size unknown" rather than a plan
// that never appears.
const (
	maxSizeWalkDuration     = 3 * time.Second
	maxTotalMeasureDuration = 20 * time.Second
)

// Candidate is a proposed target, before validation.
type Candidate struct {
	// Path is what a rule or a discovery step named.
	Path string

	// RuleID and Reason record why this candidate exists. An engine that
	// accepted anonymous candidates could produce a plan no one can review.
	RuleID string
	Reason string
}

// PlanOptions configures a planning run.
type PlanOptions struct {
	// Command names the operation for the log and the manifest.
	Command string

	// Action is what apply should do. The zero value stages, which is the
	// reversible choice, so a caller that forgets to set it gets the safe
	// behavior rather than the destructive one.
	Action Action

	// Policy decides which structurally valid paths are protected anyway.
	// A nil policy is refused rather than defaulted, since silently planning
	// with no protection is exactly the mistake worth making impossible.
	Policy PolicyChecker

	// Log receives per entry events. A nil log discards them.
	Log *operationlog.Writer

	// MeasureSizes controls whether directory sizes are computed. Callers that
	// only need to know what would be touched can skip the walk.
	MeasureSizes bool

	// SkipSink, when set, receives every rejected candidate's path and reason.
	//
	// Skips are always written to Log, but a log line is not something an
	// interactive caller can read back inside the same run. A caller that wants
	// to tell a person why their selection came back short, rather than sending
	// them to a log file to find out, sets this.
	SkipSink func(path, reason string)

	// Progress, when set, is called once per candidate with how many have been
	// considered and how many there are.
	//
	// Called from whatever goroutine is running the plan, so an interactive
	// caller must not touch its own state from inside it. The shell's counter
	// stores two atomics and lets its existing render tick read them, which
	// keeps the engine free of any assumption about how the number is shown.
	Progress func(done, total int)
}

// ErrNoPolicy is returned when planning is attempted without a policy checker.
var ErrNoPolicy = errors.New("planning requires a policy checker, pass AllowAll to opt out explicitly")

// Plan validates candidates and returns a sealed manifest describing what apply
// would do.
//
// A candidate that is rejected does not fail the plan. Cleanup work is
// inherently full of paths that are protected, already gone, or no longer what
// they were, and refusing to produce any plan because one entry was skipped
// would make the tool unusable. Every rejection is recorded with its reason,
// and the returned manifest contains only entries that passed every check.
func Plan(candidates []Candidate, opts PlanOptions) (*Manifest, error) {
	if opts.Policy == nil {
		return nil, ErrNoPolicy
	}
	action := opts.Action
	if action == "" {
		action = ActionStage
	}
	switch action {
	case ActionStage, ActionPurge:
	default:
		return nil, fmt.Errorf("%w: %q", ErrUnknownAction, action)
	}

	self := newSelfProtection()

	manifest := &Manifest{
		Version:   manifestVersion,
		CreatedAt: time.Now().UTC(),
		Command:   opts.Command,
		Action:    action,
	}

	seen := make(map[pathvalidation.Identity]string, len(candidates))

	// A per-candidate deadline still allows a plan over many candidates to
	// take their sum, which on a machine with a stalled mount is minutes. Once
	// the run has spent this long measuring, the rest are planned without
	// sizes rather than making a person wait for numbers they did not ask for.
	measureDeadline := time.Now().Add(maxTotalMeasureDuration)

	for considered, candidate := range candidates {
		if opts.Progress != nil {
			opts.Progress(considered, len(candidates))
		}
		runOpts := opts
		if opts.MeasureSizes && time.Now().After(measureDeadline) {
			runOpts.MeasureSizes = false
		}
		entry, skipReason := evaluate(candidate, runOpts, self, seen)
		if skipReason != "" {
			opts.Log.Record(operationlog.Event{
				Command: opts.Command,
				Kind:    operationlog.KindSkipped,
				Path:    candidate.Path,
				Outcome: "skipped",
				Detail:  skipReason,
			})
			if opts.SkipSink != nil {
				opts.SkipSink(candidate.Path, skipReason)
			}
			continue
		}

		seen[entry.Identity()] = entry.ResolvedPath
		manifest.Entries = append(manifest.Entries, entry)
		if entry.SizeKnown {
			manifest.TotalBytes += entry.SizeBytes
		} else {
			manifest.PartialSizing = true
		}

		opts.Log.Record(operationlog.Event{
			Command:   opts.Command,
			Kind:      operationlog.KindPlanned,
			Path:      entry.ResolvedPath,
			Outcome:   string(action),
			Detail:    entry.Reason,
			Bytes:     entry.SizeBytes,
			SizeKnown: entry.SizeKnown,
		})
	}

	manifest.Seal()
	return manifest, nil
}

// evaluate runs one candidate through every gate, returning either a usable
// entry or the reason it was rejected.
func evaluate(
	candidate Candidate,
	opts PlanOptions,
	self selfProtection,
	seen map[pathvalidation.Identity]string,
) (Entry, string) {
	if candidate.RuleID == "" || candidate.Reason == "" {
		return Entry{}, "candidate has no rule identifier or reason"
	}

	resolved, err := pathvalidation.Resolve(candidate.Path)
	if err != nil {
		switch {
		case errors.Is(err, pathvalidation.ErrNotFound):
			return Entry{}, "path does not exist"
		case errors.Is(err, pathvalidation.ErrDenied):
			return Entry{}, "denied by the structural floor: " + err.Error()
		default:
			return Entry{}, "could not be validated: " + err.Error()
		}
	}
	defer resolved.Close()

	// wtff's own state is checked before policy, because policy can be
	// misconfigured and this particular mistake removes the record undo needs.
	if root, covered := self.covers(resolved.Path()); covered {
		return Entry{}, "belongs to wtff's own state under " + root
	}

	if ruleID, reason, protected := opts.Policy.Protected(resolved.Path()); protected {
		return Entry{}, fmt.Sprintf("protected by %s: %s", ruleID, reason)
	}

	// Two candidates can name the same object through different paths, which is
	// ordinary on a filesystem with links. Planning it twice would mean apply
	// finds it missing the second time and reports a skip that looks like a
	// problem but is not.
	if existing, duplicate := seen[resolved.Identity()]; duplicate {
		return Entry{}, "same object already planned as " + existing
	}

	entry := Entry{
		RequestedPath: resolved.RequestedPath(),
		ResolvedPath:  resolved.Path(),
		Device:        resolved.Identity().Device,
		Inode:         resolved.Identity().Inode,
		IsDir:         resolved.IsDir(),
		IsSymlink:     resolved.IsSymlink(),
		RuleID:        candidate.RuleID,
		Reason:        candidate.Reason,
	}

	if opts.MeasureSizes {
		size, known := measure(resolved)
		entry.SizeBytes = size
		entry.SizeKnown = known
	}

	return entry, ""
}

// measure reports the size of a resolved target and whether the number is
// complete.
//
// A failure to measure yields SizeKnown false rather than zero. The distinction
// matters downstream: a log claiming a multi gigabyte removal reclaimed nothing
// is worse than one admitting it could not tell.
func measure(resolved *pathvalidation.Resolved) (int64, bool) {
	if resolved.IsSymlink() {
		// A link occupies its own small entry. Reporting the size of what it
		// points at would attribute space to a removal that does not free it.
		return 0, true
	}
	if !resolved.IsDir() {
		info, err := statLeaf(resolved)
		if err != nil {
			return 0, false
		}
		return info, true
	}

	return measureWithin(maxSizeWalkDuration, func() (int64, bool) {
		return walkSize(resolved.Path())
	})
}

// measureWithin runs a measurement on its own goroutine and abandons it if it
// does not finish in time, reporting the size as unknown.
//
// Abandoning rather than cancelling is deliberate. A stalled walk is blocked
// inside a filesystem call that does not take a cancellation, so there is
// nothing to signal; the only thing available is to stop waiting. The
// abandoned goroutine owns its own accumulator, so nothing here reads a value
// another goroutine is still writing, and the channel is buffered so that
// goroutine can deliver and exit rather than blocking forever on a receiver
// that has moved on.
func measureWithin(limit time.Duration, walk func() (int64, bool)) (int64, bool) {
	type measurement struct {
		total    int64
		complete bool
	}

	done := make(chan measurement, 1)
	go func() {
		total, complete := walk()
		done <- measurement{total, complete}
	}()

	timer := time.NewTimer(limit)
	defer timer.Stop()

	select {
	case result := <-done:
		return result.total, result.complete
	case <-timer.C:
		return 0, false
	}
}

// walkSize sums a directory tree, reporting whether the total is complete.
func walkSize(root string) (int64, bool) {
	var total int64
	var visited int
	complete := true

	err := filepath.WalkDir(root, func(_ string, entry fs.DirEntry, err error) error {
		if err != nil {
			// An unreadable subtree makes the total a floor, not a total.
			complete = false
			return nil
		}
		visited++
		if visited > maxSizeWalkEntries {
			complete = false
			return filepath.SkipAll
		}
		if entry.IsDir() {
			return nil
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			complete = false
			return nil
		}
		// Info on a directory entry does not follow links, so a link inside the
		// tree contributes its own size rather than its destination's.
		total += info.Size()
		return nil
	})
	if err != nil {
		complete = false
	}
	return total, complete
}
