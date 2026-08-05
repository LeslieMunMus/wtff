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
// Measuring is best effort and must never be the reason a plan hangs. A tree
// larger than this is reported with the size measured so far and SizeKnown
// false, so the manifest says the number is incomplete instead of presenting a
// partial sum as a total.
const maxSizeWalkEntries = 500_000

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

	for _, candidate := range candidates {
		entry, skipReason := evaluate(candidate, opts, self, seen)
		if skipReason != "" {
			opts.Log.Record(operationlog.Event{
				Command: opts.Command,
				Kind:    operationlog.KindSkipped,
				Path:    candidate.Path,
				Outcome: "skipped",
				Detail:  skipReason,
			})
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

	var total int64
	var visited int
	complete := true

	err := filepath.WalkDir(resolved.Path(), func(_ string, entry fs.DirEntry, err error) error {
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
