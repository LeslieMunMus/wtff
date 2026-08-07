// Package duplicatemerge gathers identical copies of a file into one
// directory, keeping every one of them.
//
// This is deliberately not a deletion. A person who has two copies of
// something may want both, under names that say which is which, and the
// difference between "I have this twice by accident" and "I have two versions
// of this" is one only they can draw. Merging takes no position on it: every
// copy survives, nothing is removed, and the report says exactly where each
// one went.
//
// Detection lives in internal/duplicate-scan, which only reads. Keeping the
// action here means a mistake in the matching cannot itself move anything.
package duplicatemerge

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	duplicatescan "github.com/lesliemunmus/wtff/internal/duplicate-scan"
	operationlog "github.com/lesliemunmus/wtff/internal/operation-log"
)

// maxNameAttempts bounds the search for a free filename. A directory holding
// this many copies of one thing is a situation to report rather than to keep
// counting through.
const maxNameAttempts = 1000

var (
	// ErrNothingToMerge is returned for a group with fewer than two copies.
	ErrNothingToMerge = errors.New("a merge needs at least two copies")

	// ErrNoFreeName is returned when no unused filename could be found.
	ErrNoFreeName = errors.New("cannot find an unused name in the destination")
)

// Move is one copy's journey into the destination directory.
type Move struct {
	From string
	To   string
}

// Plan is what a merge would do, computed before anything is touched.
//
// Planning separately from applying is the same shape the deletion engine
// uses, and for the same reason: a person can be shown exactly what will
// happen, in the names it will happen under, while it is still costless to
// change their mind.
type Plan struct {
	// Destination is the oldest copy's directory, which nothing moves out of.
	Destination string

	// Keeper is the copy already in place. It is named so a report can say
	// what everything else is being gathered around.
	Keeper string

	Moves []Move
}

// PlanMerge works out where each copy would go.
//
// The oldest copy stays exactly where it is. Every other copy moves beside it,
// renamed only as far as it must be to avoid colliding with something already
// there. A copy that is already in the destination directory is left alone
// rather than renamed for the sake of consistency, because moving a file to
// its own directory under a new name achieves nothing and loses the name the
// person chose.
func PlanMerge(group duplicatescan.Group) (Plan, error) {
	if len(group.Files) < 2 {
		return Plan{}, ErrNothingToMerge
	}

	keeper := group.Oldest()
	destination := filepath.Dir(keeper.Path)

	plan := Plan{Destination: destination, Keeper: keeper.Path}

	// Names already spoken for, seeded with what is on disk and extended as
	// the plan allocates more, so two moves in one merge cannot be assigned
	// the same name before either has happened.
	taken := make(map[string]bool)

	for _, file := range group.Files[1:] {
		if filepath.Dir(file.Path) == destination {
			// Already where the merge would put it.
			continue
		}

		name, err := freeName(destination, filepath.Base(file.Path), taken)
		if err != nil {
			return Plan{}, err
		}
		taken[name] = true
		plan.Moves = append(plan.Moves, Move{
			From: file.Path,
			To:   filepath.Join(destination, name),
		})
	}
	return plan, nil
}

// freeName finds a filename in dir that nothing already occupies.
//
// The suffix goes before the extension, so "report.pdf" becomes "report
// copy.pdf" rather than "report.pdf copy", which keeps the file openable by
// whatever opened it before.
func freeName(dir, original string, taken map[string]bool) (string, error) {
	extension := filepath.Ext(original)
	stem := strings.TrimSuffix(original, extension)

	for attempt := 0; attempt < maxNameAttempts; attempt++ {
		var candidate string
		switch attempt {
		case 0:
			candidate = original
		case 1:
			candidate = stem + " copy" + extension
		default:
			candidate = fmt.Sprintf("%s copy %d%s", stem, attempt, extension)
		}

		if taken[candidate] {
			continue
		}
		// Lstat rather than Stat: a broken symlink occupies the name just as
		// firmly as a real file, and renaming onto it would destroy it.
		if _, err := os.Lstat(filepath.Join(dir, candidate)); os.IsNotExist(err) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("%w: %s", ErrNoFreeName, filepath.Join(dir, original))
}

// Outcome is what happened to one planned move.
type Outcome struct {
	Move  Move
	Moved bool
	Err   error
}

// Result summarises an applied merge.
type Result struct {
	Outcomes    []Outcome
	MovedCount  int
	FailedCount int
}

// Apply performs a planned merge.
//
// Each move is a rename, which is atomic: the file is either at its old path
// or its new one, never partly both. A move that fails leaves that copy
// exactly where it was, and the remaining moves are still attempted, because
// stopping halfway would leave a person with a merge they have to finish by
// hand without knowing how far it got.
func Apply(plan Plan, log *operationlog.Writer) (*Result, error) {
	result := &Result{}

	for _, move := range plan.Moves {
		outcome := Outcome{Move: move}

		// Re-checked immediately before the rename rather than trusted from
		// planning time. Something may have taken the name in between, and
		// os.Rename would overwrite it without complaint.
		if _, err := os.Lstat(move.To); err == nil {
			outcome.Err = fmt.Errorf("something already occupies %s", move.To)
			result.FailedCount++
			result.Outcomes = append(result.Outcomes, outcome)
			log.Record(operationlog.Event{
				Command: "duplicates", Kind: operationlog.KindFailed,
				Path: move.From, Outcome: "failed", Detail: outcome.Err.Error(),
			})
			continue
		}

		if err := os.Rename(move.From, move.To); err != nil {
			outcome.Err = err
			result.FailedCount++
			log.Record(operationlog.Event{
				Command: "duplicates", Kind: operationlog.KindFailed,
				Path: move.From, Outcome: "failed", Detail: err.Error(),
			})
		} else {
			outcome.Moved = true
			result.MovedCount++
			// Both paths are recorded. A person reading this later needs to
			// know where something went, not merely that it moved.
			log.Record(operationlog.Event{
				Command: "duplicates", Kind: operationlog.KindStaged,
				Path: move.From, Outcome: "merged", Detail: "moved to " + move.To,
			})
		}
		result.Outcomes = append(result.Outcomes, outcome)
	}
	return result, nil
}
