// Package duplicatescan finds files that are byte for byte identical.
//
// It only reads. Deciding what happens to a group is the caller's job, and
// anything that happens goes through the deletion engine, which applies the
// same path validation, protection rules, and staging every other command
// uses. Keeping detection and action apart means a mistake in the matching
// cannot itself remove anything.
package duplicatescan

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"
)

const (
	// maxEntries and maxDepth mirror the space scanner's bounds, so one set of
	// rules covers every walk in this project.
	maxEntries = 2_000_000
	maxDepth   = 128

	// defaultDeadline is generous because hashing is genuinely slow on large
	// media, and this is a deliberate interactive request rather than
	// something running behind a person's back.
	defaultDeadline = 5 * time.Minute

	// prefixBytes is how much is read for the cheap comparison that runs
	// before any full hash. Large enough that unrelated files of identical
	// size almost never survive it, small enough to cost nothing.
	prefixBytes = 64 * 1024

	// minSize skips files too small to be worth reclaiming. Thousands of
	// identical empty or near empty files are normal on any machine, would
	// dominate the results, and reclaim nothing.
	minSize = 4 * 1024
)

// File is one copy found on disk.
type File struct {
	Path    string
	Size    int64
	ModTime time.Time
}

// Group is a set of files with identical contents, oldest first.
//
// Ordering is by modification time because the oldest copy is the one a person
// most often put somewhere deliberately, while later copies tend to be
// accidents of downloading or duplicating. Callers that merge use the first
// entry's directory as the destination, so the ordering is load bearing rather
// than cosmetic.
type Group struct {
	Size   int64
	Digest string
	Files  []File
}

// Reclaimable is what removing every copy but the first would free.
func (g Group) Reclaimable() int64 {
	if len(g.Files) < 2 {
		return 0
	}
	return g.Size * int64(len(g.Files)-1)
}

// Oldest is the copy a merge keeps in place.
func (g Group) Oldest() File { return g.Files[0] }

// Result is one completed search.
type Result struct {
	Groups []Group

	Scanned int
	Hashed  int
	Denied  []string

	Truncated bool
	Reason    string
	Elapsed   time.Duration
}

// Reclaimable totals what could be freed across every group.
func (r Result) Reclaimable() int64 {
	var total int64
	for _, group := range r.Groups {
		total += group.Reclaimable()
	}
	return total
}

// Options configures a search.
type Options struct {
	Root     string
	Deadline time.Duration

	// MinSize overrides the default floor. Zero uses it.
	MinSize int64

	// Progress reports files examined so far, from the scanning goroutine.
	Progress func(scanned int)

	now func() time.Time
}

// ErrNoRoot is returned when a search is asked for without somewhere to start.
var ErrNoRoot = errors.New("a duplicate search needs a root directory")

// Find locates byte identical files beneath a root.
//
// Matching is deliberately conservative, in three passes: group by exact size,
// then by a hash of the first 64KB, then by a full hash of everything that
// survives. Only the last one decides. Reporting two different files as
// identical would invite someone to delete real data, and no amount of speed
// is worth that, so nothing short of a full content hash is ever treated as a
// match.
func Find(opts Options) (*Result, error) {
	if opts.Root == "" {
		return nil, ErrNoRoot
	}
	info, err := os.Lstat(opts.Root)
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", opts.Root, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", opts.Root)
	}

	now := opts.now
	if now == nil {
		now = time.Now
	}
	deadline := opts.Deadline
	if deadline <= 0 {
		deadline = defaultDeadline
	}
	floor := opts.MinSize
	if floor <= 0 {
		floor = minSize
	}

	started := now()
	f := &finder{
		deadline: started.Add(deadline),
		now:      now,
		progress: opts.Progress,
		minSize:  floor,
		bySize:   make(map[int64][]File),
	}

	f.collect(opts.Root, 0)
	groups := f.resolve()

	// Largest reclaimable first: the reason someone opened this is to find
	// what is worth acting on, not to read an inventory.
	sort.SliceStable(groups, func(i, j int) bool {
		return groups[i].Reclaimable() > groups[j].Reclaimable()
	})

	return &Result{
		Groups:    groups,
		Scanned:   f.scanned,
		Hashed:    f.hashed,
		Denied:    f.denied,
		Truncated: f.truncated,
		Reason:    f.reason,
		Elapsed:   now().Sub(started),
	}, nil
}

type finder struct {
	deadline time.Time
	now      func() time.Time
	progress func(scanned int)
	minSize  int64

	bySize map[int64][]File

	scanned   int
	hashed    int
	denied    []string
	truncated bool
	reason    string
}

// collect walks the tree and buckets candidate files by size.
func (f *finder) collect(dir string, depth int) {
	if depth > maxDepth {
		f.stop(fmt.Sprintf("directory nesting exceeded %d levels", maxDepth))
		return
	}
	if f.stopped() {
		return
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		f.denied = append(f.denied, dir)
		return
	}

	for _, entry := range entries {
		if f.stopped() {
			return
		}
		path := filepath.Join(dir, entry.Name())

		// Never followed, for the same reasons as the space scanner: a loop
		// would become an unbounded walk, and a link out of the root would
		// silently widen the search. A link is also not a duplicate of the
		// thing it points at, and reporting it as one would offer to delete a
		// file by way of its own alias.
		if entry.Type()&os.ModeSymlink != 0 {
			continue
		}

		if entry.IsDir() {
			f.collect(path, depth+1)
			continue
		}

		f.scanned++
		if f.scanned%1024 == 0 && f.progress != nil {
			f.progress(f.scanned)
		}
		if f.scanned > maxEntries {
			f.stop(fmt.Sprintf("stopped after %d entries", maxEntries))
			return
		}

		info, statErr := entry.Info()
		if statErr != nil || !info.Mode().IsRegular() || info.Size() < f.minSize {
			continue
		}
		f.bySize[info.Size()] = append(f.bySize[info.Size()], File{
			Path: path, Size: info.Size(), ModTime: info.ModTime(),
		})
	}
}

// resolve narrows same sized candidates down to byte identical groups.
func (f *finder) resolve() []Group {
	var groups []Group

	for size, candidates := range f.bySize {
		if len(candidates) < 2 || f.stopped() {
			continue
		}

		// Cheap pass first. Files sharing a size but differing in their first
		// 64KB are separated without ever reading the rest, which is what
		// keeps this usable on a directory full of large media.
		byPrefix := make(map[string][]File)
		for _, file := range candidates {
			digest, err := hashFile(file.Path, prefixBytes)
			if err != nil {
				f.denied = append(f.denied, file.Path)
				continue
			}
			byPrefix[digest] = append(byPrefix[digest], file)
		}

		for _, sharing := range byPrefix {
			if len(sharing) < 2 {
				continue
			}
			byContent := make(map[string][]File)
			for _, file := range sharing {
				digest, err := hashFile(file.Path, 0)
				if err != nil {
					f.denied = append(f.denied, file.Path)
					continue
				}
				f.hashed++
				byContent[digest] = append(byContent[digest], file)
			}

			for digest, identical := range byContent {
				if len(identical) < 2 {
					continue
				}
				// Oldest first, which is what a merge keeps in place.
				sort.SliceStable(identical, func(i, j int) bool {
					return identical[i].ModTime.Before(identical[j].ModTime)
				})
				groups = append(groups, Group{
					Size: size, Digest: digest, Files: identical,
				})
			}
		}
	}
	return groups
}

// hashFile digests a file, reading at most limit bytes when limit is positive.
func hashFile(path string, limit int64) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	digest := sha256.New()
	var reader io.Reader = file
	if limit > 0 {
		reader = io.LimitReader(file, limit)
	}
	if _, err := io.Copy(digest, reader); err != nil {
		return "", err
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func (f *finder) stopped() bool {
	if f.truncated {
		return true
	}
	if f.now().After(f.deadline) {
		f.stop("stopped at the time limit")
		return true
	}
	return false
}

func (f *finder) stop(reason string) {
	if !f.truncated {
		f.truncated = true
		f.reason = reason
	}
}
