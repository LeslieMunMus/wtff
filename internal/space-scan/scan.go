// Package spacescan measures where disk space has gone, as a tree a person
// can walk down rather than a number they have to trust.
//
// It only reads. Nothing here removes anything: a selection made against this
// tree is handed to the deletion engine, which applies the same path
// validation, protection rules, and staging every other command goes through.
// Keeping measurement and removal in separate packages means a mistake in the
// walk cannot widen what is removable.
package spacescan

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync/atomic"
	"time"
)

// Bounds on a walk. A home directory is not a bounded structure: it can hold
// millions of entries, live partly on a network mount, or contain a directory
// nested past anything reasonable.
//
// These mirror the deletion engine's own limits deliberately. A person who
// learns wtff's behaviour in one place should not have to learn a different
// set of rules here.
const (
	// maxEntries bounds how many filesystem entries one scan visits.
	maxEntries = 2_000_000

	// maxDepth bounds recursion. A filesystem should not produce a cycle of
	// directories, and refusing to find out costs nothing.
	maxDepth = 128

	// defaultDeadline bounds the whole walk. Generous, because this is a
	// deliberate interactive request rather than something running behind a
	// person's back, and a home directory on a slow disk can take a while.
	defaultDeadline = 90 * time.Second
)

// Node is one entry in the measured tree.
//
// Name is a single path component rather than a full path. A tree over a large
// home directory holds a great many of these, and storing a complete path in
// each would multiply memory by the depth of the tree for no gain: Path
// reconstructs one by walking up.
type Node struct {
	Name  string
	Size  int64
	IsDir bool

	// Children is nil for files, and for directories the walk could not read.
	Children []*Node

	// Complete is false when this directory's total is a floor rather than a
	// total, because the walk was denied, bounded, or cut short beneath it.
	// A number presented as exact when it is not is worse than an honest
	// floor, particularly when someone is deciding what to delete from it.
	Complete bool

	parent *Node
}

// Path reconstructs this node's absolute path.
func (n *Node) Path() string {
	if n == nil {
		return ""
	}
	if n.parent == nil {
		return n.Name
	}
	return filepath.Join(n.parent.Path(), n.Name)
}

// Parent reports the node this one sits inside, nil at the root.
func (n *Node) Parent() *Node { return n.parent }

// Result is one completed walk.
type Result struct {
	Root *Node

	// Scanned counts entries visited, and Denied lists directories that could
	// not be read. A scan reporting less than a person expects is almost
	// always explained by one of these two.
	Scanned int
	Denied  []string

	// Truncated is true when a bound stopped the walk early, so every total
	// above the point it stopped is a floor.
	Truncated bool
	Reason    string

	Elapsed time.Duration
}

// Options configures a scan.
type Options struct {
	Root string

	// Deadline bounds the whole walk. Zero uses the default.
	Deadline time.Duration

	// Progress, when set, is called with entries scanned so far. Called from
	// the scanning goroutine, so an interactive caller must not touch its own
	// state from inside it; the shell stores an atomic and lets its render
	// tick read it.
	Progress func(scanned int)

	// now is injected for tests, which cannot wait out a real deadline.
	now func() time.Time
}

// ErrNoRoot is returned when a scan is asked for without somewhere to start.
var ErrNoRoot = errors.New("a scan needs a root directory")

type walker struct {
	deadline time.Time
	now      func() time.Time
	progress func(scanned int)

	scanned   int
	denied    []string
	truncated bool
	reason    string

	// published lets a caller read progress without sharing the walker.
	published atomic.Int64
}

// Scan measures a directory tree.
//
// A scan that hits a bound returns what it found rather than an error. Cleanup
// work is full of directories a person cannot read, and refusing to report
// anything because one subdirectory was denied would make the feature useless
// on exactly the machines it is most needed.
func Scan(opts Options) (*Result, error) {
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

	started := now()
	w := &walker{
		deadline: started.Add(deadline),
		now:      now,
		progress: opts.Progress,
	}

	root := &Node{Name: filepath.Clean(opts.Root), IsDir: true}
	w.walk(root, 0)

	return &Result{
		Root:      root,
		Scanned:   w.scanned,
		Denied:    w.denied,
		Truncated: w.truncated,
		Reason:    w.reason,
		Elapsed:   now().Sub(started),
	}, nil
}

// walk fills one directory node and everything beneath it, returning its
// aggregate size.
func (w *walker) walk(dir *Node, depth int) int64 {
	if depth > maxDepth {
		w.stop(fmt.Sprintf("directory nesting exceeded %d levels", maxDepth))
		return 0
	}
	if w.stopped() {
		return 0
	}

	entries, err := os.ReadDir(dir.Path())
	if err != nil {
		// Denied or vanished. Recorded rather than fatal: a home directory
		// routinely holds both, and the honest report is a floor with a
		// reason, not a refusal.
		w.denied = append(w.denied, dir.Path())
		dir.Complete = false
		return 0
	}

	dir.Complete = true
	var total int64

	for _, entry := range entries {
		if w.stopped() {
			dir.Complete = false
			break
		}

		w.scanned++
		if w.scanned%2048 == 0 {
			w.publish()
		}
		if w.scanned > maxEntries {
			w.stop(fmt.Sprintf("stopped after %d entries", maxEntries))
			dir.Complete = false
			break
		}

		child := &Node{Name: entry.Name(), parent: dir}

		switch {
		case entry.Type()&os.ModeSymlink != 0:
			// A link is counted as the small entry it is, never followed. Its
			// target's bytes belong to whatever actually holds them, and
			// following would let one loop become an unbounded walk and let a
			// link out of the scan root silently widen it.
			child.Size = 0
			child.Complete = true

		case entry.IsDir():
			child.IsDir = true
			child.Size = w.walk(child, depth+1)
			if !child.Complete {
				dir.Complete = false
			}

		default:
			info, statErr := entry.Info()
			if statErr != nil {
				dir.Complete = false
				continue
			}
			child.Size = info.Size()
			child.Complete = true
		}

		total += child.Size
		dir.Children = append(dir.Children, child)
	}

	// Largest first, which is the order the whole feature exists to show.
	sort.SliceStable(dir.Children, func(i, j int) bool {
		return dir.Children[i].Size > dir.Children[j].Size
	})

	dir.Size = total
	return total
}

func (w *walker) stopped() bool {
	if w.truncated {
		return true
	}
	// Checked between entries rather than inside a syscall. A single ReadDir
	// against a mount that has stopped answering is unbounded here, the same
	// limitation the deletion engine documents; interrupting the program is
	// what covers that case.
	if w.now().After(w.deadline) {
		w.stop("stopped at the time limit")
		return true
	}
	return false
}

func (w *walker) stop(reason string) {
	if !w.truncated {
		w.truncated = true
		w.reason = reason
	}
}

func (w *walker) publish() {
	w.published.Store(int64(w.scanned))
	if w.progress != nil {
		w.progress(w.scanned)
	}
}
