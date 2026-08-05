package deletionengine

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// maxRemoveDepth bounds recursion during a tree removal.
//
// A filesystem should not produce a cycle of directories, but a bound costs
// nothing and turns a corrupted or hostile tree into a reported failure instead
// of a stack exhaustion that takes the process down mid deletion.
const maxRemoveDepth = 128

// removeTreeAt removes an entry, and everything beneath it, relative to an open
// directory descriptor.
//
// Every step is descriptor relative and refuses to follow links. The standard
// library's recursive remove takes a path and re-resolves it, which reintroduces
// exactly the gap the rest of this package exists to close: between the moment a
// target is verified and the moment the walk begins, the name can come to mean
// something else. Holding the parent open and descending by descriptor means the
// removal stays inside the tree that was verified.
func removeTreeAt(parentFD int, name string, depth int) error {
	if depth > maxRemoveDepth {
		return fmt.Errorf("directory nesting exceeds %d levels", maxRemoveDepth)
	}

	// Try the simple case first. This succeeds for regular files, links, and
	// every other non directory, and it never follows a link.
	err := unix.Unlinkat(parentFD, name, 0)
	if err == nil {
		return nil
	}
	if errors.Is(err, unix.ENOENT) {
		// Already gone. Nothing to do and nothing to report: the desired state
		// has been reached.
		return nil
	}
	// macOS reports an attempt to unlink a directory as EPERM. Other systems
	// use EISDIR. Both mean the same thing here, so both lead to the directory
	// path rather than being treated as failures.
	if !errors.Is(err, unix.EPERM) && !errors.Is(err, unix.EISDIR) {
		return fmt.Errorf("cannot remove %s: %w", name, err)
	}

	dirFD, openErr := unix.Openat(parentFD, name,
		unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if openErr != nil {
		if errors.Is(openErr, unix.ENOENT) {
			return nil
		}
		return fmt.Errorf("cannot open %s to remove its contents: %w", name, openErr)
	}

	children, readErr := readDirNames(dirFD)
	if readErr != nil {
		_ = unix.Close(dirFD)
		return fmt.Errorf("cannot list %s: %w", name, readErr)
	}

	var firstFailure error
	for _, child := range children {
		if childErr := removeTreeAt(dirFD, child, depth+1); childErr != nil && firstFailure == nil {
			// Keep going after a failure so that as much is removed as can be,
			// but report the first cause rather than the last, which is the one
			// most likely to explain the rest.
			firstFailure = childErr
		}
	}
	_ = unix.Close(dirFD)

	if firstFailure != nil {
		return firstFailure
	}

	if err := unix.Unlinkat(parentFD, name, unix.AT_REMOVEDIR); err != nil {
		if errors.Is(err, unix.ENOENT) {
			return nil
		}
		return fmt.Errorf("cannot remove directory %s: %w", name, err)
	}
	return nil
}

// readDirNames lists the entries of an open directory without consuming the
// caller's descriptor.
//
// The descriptor is duplicated because wrapping it in an os.File transfers
// ownership, and closing that file would close the descriptor the caller is
// still using to recurse.
func readDirNames(dirFD int) ([]string, error) {
	duplicate, err := unix.Dup(dirFD)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(duplicate), "wtff-directory")
	if file == nil {
		_ = unix.Close(duplicate)
		return nil, errors.New("cannot wrap directory descriptor")
	}
	defer file.Close()

	names, err := file.Readdirnames(-1)
	if err != nil {
		return nil, err
	}
	return names, nil
}
