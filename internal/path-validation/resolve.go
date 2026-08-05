package pathvalidation

import (
	"errors"
	"fmt"
	"strings"

	"golang.org/x/sys/unix"
)

// maxSymlinkHops bounds link resolution. The value matches the conventional
// kernel limit. Exceeding it means a loop or a deliberately pathological chain,
// either of which is a denial rather than something to keep working at.
const maxSymlinkHops = 40

// maxLinkTargetBytes bounds how large a link target this package will read.
const maxLinkTargetBytes = 64 * 1024

// Resolved is a path that passed structural validation, expressed as a handle
// rather than a string.
//
// The parent directory is held open. That is the point of this type: a caller
// holding a Resolved can act on the target with descriptor relative calls, so
// the directory the walk validated is the directory the kernel operates on. An
// attacker who replaces a directory component after validation does not change
// where an already open descriptor points.
//
// The caller owns the descriptor and must call Close when finished.
type Resolved struct {
	parentFD  int
	leafName  string
	logical   string
	requested string
	identity  Identity
	mode      uint16
	closed    bool
}

// ParentFD returns the open descriptor for the directory containing the target.
// It is intended for descriptor relative syscalls such as unlinkat and
// renameat. It is not valid after Close.
func (r *Resolved) ParentFD() int { return r.parentFD }

// LeafName returns the final path component, which is the name to pass
// alongside ParentFD in a descriptor relative call.
func (r *Resolved) LeafName() string { return r.leafName }

// Path returns the fully resolved logical path, with links expanded. This may
// differ from what the caller requested and is the value that should appear in
// logs and previews, since it is where the operation will actually land.
func (r *Resolved) Path() string { return r.logical }

// RequestedPath returns the normalized path the caller originally asked for,
// before link expansion. Keeping both matters for audit: a user needs to see
// that the path they named resolved somewhere else.
func (r *Resolved) RequestedPath() string { return r.requested }

// Identity returns the device and inode captured during validation.
func (r *Resolved) Identity() Identity { return r.identity }

// IsDir reports whether the target is a directory.
func (r *Resolved) IsDir() bool { return r.mode&unix.S_IFMT == unix.S_IFDIR }

// IsSymlink reports whether the target is itself a symbolic link. Such a target
// is the link, never what it points at.
func (r *Resolved) IsSymlink() bool { return r.mode&unix.S_IFMT == unix.S_IFLNK }

// Close releases the held directory descriptor. It is safe to call more than
// once.
func (r *Resolved) Close() error {
	if r.closed || r.parentFD < 0 {
		return nil
	}
	r.closed = true
	err := unix.Close(r.parentFD)
	r.parentFD = -1
	return err
}

// Verify re-checks that the target still has the identity captured during
// validation, using the held parent descriptor.
//
// This is the last check before a destructive act. It is not the primary
// defense, which is that the parent directory has been pinned open since
// validation. It closes the remaining narrow window in which the leaf could be
// swapped inside that pinned directory.
func (r *Resolved) Verify() error {
	if r.closed || r.parentFD < 0 {
		return fmt.Errorf("%w: handle is closed", ErrResolution)
	}
	var st unix.Stat_t
	if err := unix.Fstatat(r.parentFD, r.leafName, &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		if errors.Is(err, unix.ENOENT) {
			return fmt.Errorf("%w: %s", ErrNotFound, r.logical)
		}
		return fmt.Errorf("%w: %s: %v", ErrResolution, r.logical, err)
	}
	current := Identity{Device: uint64(st.Dev), Inode: st.Ino}
	if current != r.identity {
		return fmt.Errorf("%w: %s changed between validation and use", ErrResolution, r.logical)
	}
	return nil
}

// Resolve validates a path and returns a handle to it.
//
// On success the caller owns the returned handle and must Close it. On failure
// nothing is left open. Every failure is a denial: there is no branch on which
// an inconclusive result is reported as safe.
func Resolve(target string) (*Resolved, error) {
	logical, err := normalizeAndCheck(target)
	if err != nil {
		return nil, err
	}
	if entry, denied := deniedByString(logical); denied {
		return nil, fmt.Errorf("%w: %s is denied by %s", ErrDenied, logical, entry)
	}
	return walk(logical)
}

// walk performs the component by component resolution described in the package
// documentation.
func walk(logical string) (result *Resolved, err error) {
	dir, openErr := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if openErr != nil {
		return nil, fmt.Errorf("%w: cannot open volume root: %v", ErrResolution, openErr)
	}

	// The descriptor is handed to the caller only on the success path. Any
	// error return must not leak it.
	handedOff := false
	defer func() {
		if !handedOff && dir >= 0 {
			_ = unix.Close(dir)
		}
	}()

	dirPath := "/"
	remaining := splitComponents(logical)
	hops := 0

	for {
		if len(remaining) == 0 {
			// The path resolved to a directory root with no final component,
			// which means the target is that directory itself.
			return nil, fmt.Errorf("%w: %s is denied by the floor", ErrDenied, dirPath)
		}

		name := remaining[0]
		rest := remaining[1:]
		childPath := joinPath(dirPath, name)

		var st unix.Stat_t
		if statErr := unix.Fstatat(dir, name, &st, unix.AT_SYMLINK_NOFOLLOW); statErr != nil {
			if errors.Is(statErr, unix.ENOENT) {
				return nil, fmt.Errorf("%w: %s", ErrNotFound, childPath)
			}
			return nil, fmt.Errorf("%w: %s: %v", ErrResolution, childPath, statErr)
		}

		isLink := st.Mode&unix.S_IFMT == unix.S_IFLNK
		isLast := len(rest) == 0

		// An intermediate link is followed, with the hop counted and the
		// resulting path re-checked. A final link is never followed.
		if isLink && !isLast {
			hops++
			if hops > maxSymlinkHops {
				return nil, fmt.Errorf("%w: while resolving %s", ErrSymlinkDepth, logical)
			}
			linkTarget, linkErr := readLinkAt(dir, name)
			if linkErr != nil {
				return nil, fmt.Errorf("%w: cannot read link %s: %v", ErrResolution, childPath, linkErr)
			}
			if strings.HasPrefix(linkTarget, "/") {
				// An absolute target restarts the walk from the volume root, so
				// the accumulated logical path is discarded along with any
				// assumption it carried.
				fresh, freshErr := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
				if freshErr != nil {
					return nil, fmt.Errorf("%w: cannot reopen volume root: %v", ErrResolution, freshErr)
				}
				_ = unix.Close(dir)
				dir = fresh
				dirPath = "/"
				remaining = append(splitComponents(linkTarget), rest...)
			} else {
				remaining = append(splitComponents(linkTarget), rest...)
			}
			continue
		}

		if isLast {
			key := identityKey{device: uint64(st.Dev), inode: st.Ino}
			if entry, denied := deniedExactByIdentity(key); denied {
				return nil, fmt.Errorf("%w: %s resolves to %s", ErrDenied, logical, entry)
			}
			if entry, denied := deniedByString(childPath); denied {
				return nil, fmt.Errorf("%w: %s is denied by %s", ErrDenied, childPath, entry)
			}
			handedOff = true
			return &Resolved{
				parentFD:  dir,
				leafName:  name,
				logical:   childPath,
				requested: logical,
				identity:  Identity{Device: key.device, Inode: key.inode},
				mode:      st.Mode,
			}, nil
		}

		if st.Mode&unix.S_IFMT != unix.S_IFDIR {
			return nil, fmt.Errorf("%w: %s", ErrNotDirectory, childPath)
		}

		// Denied trees are checked on the way through, by identity as well as by
		// text, so a link that redirects into one is caught on arrival.
		key := identityKey{device: uint64(st.Dev), inode: st.Ino}
		if entry, denied := deniedTreeByIdentity(key); denied {
			return nil, fmt.Errorf("%w: %s passes through %s", ErrDenied, logical, entry)
		}
		if entry, denied := deniedTreeByString(childPath); denied {
			return nil, fmt.Errorf("%w: %s passes through %s", ErrDenied, logical, entry)
		}

		next, nextErr := unix.Openat(dir, name,
			unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if nextErr != nil {
			return nil, fmt.Errorf("%w: cannot open %s: %v", ErrResolution, childPath, nextErr)
		}
		_ = unix.Close(dir)
		dir = next
		dirPath = childPath
		remaining = rest
	}
}

// normalizeAndCheck applies the text level checks and collapses redundant
// separators and single dot components.
//
// The dot-dot check runs on raw components before anything is collapsed, so a
// traversal cannot be normalized away before it is noticed. A filename that
// merely contains two dots, such as a browser cache entry named "index..data",
// is not a traversal and is preserved.
func normalizeAndCheck(target string) (string, error) {
	if target == "" {
		return "", ErrEmptyPath
	}
	if !strings.HasPrefix(target, "/") {
		return "", fmt.Errorf("%w: %s", ErrNotAbsolute, target)
	}
	for _, r := range target {
		if r < 0x20 || r == 0x7f {
			return "", fmt.Errorf("%w: %q", ErrControlCharacter, target)
		}
	}

	parts := strings.Split(target, "/")
	kept := make([]string, 0, len(parts))
	for _, part := range parts {
		switch part {
		case "", ".":
			continue
		case "..":
			return "", fmt.Errorf("%w: %s", ErrTraversal, target)
		default:
			kept = append(kept, part)
		}
	}
	if len(kept) == 0 {
		return "/", nil
	}
	return "/" + strings.Join(kept, "/"), nil
}

// splitComponents breaks a path into its meaningful components, discarding
// empty and single dot entries. Dot-dot is not filtered here: it is rejected
// earlier for caller supplied paths, and a link target containing one is
// handled by the walk failing to find the component.
func splitComponents(path string) []string {
	parts := strings.Split(path, "/")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" || part == "." {
			continue
		}
		out = append(out, part)
	}
	return out
}

func joinPath(dir, name string) string {
	if dir == "/" {
		return "/" + name
	}
	return dir + "/" + name
}

// readLinkAt reads a link target relative to an open directory descriptor,
// growing the buffer until the whole target fits.
func readLinkAt(dir int, name string) (string, error) {
	size := 256
	for {
		buf := make([]byte, size)
		n, err := unix.Readlinkat(dir, name, buf)
		if err != nil {
			return "", err
		}
		if n < size {
			return string(buf[:n]), nil
		}
		size *= 2
		if size > maxLinkTargetBytes {
			return "", errors.New("link target exceeds maximum length")
		}
	}
}

// deniedTreeByString reports whether a path is a denied tree root or sits
// beneath one, comparing text only.
func deniedTreeByString(path string) (string, bool) {
	for _, entry := range deniedTrees {
		if strings.EqualFold(path, entry) || hasPathPrefixFold(path, entry) {
			return entry, true
		}
	}
	return "", false
}

// hasPathPrefixFold reports whether path sits beneath prefix, comparing without
// case sensitivity because Apple filesystems are usually case insensitive.
func hasPathPrefixFold(path, prefix string) bool {
	withSep := prefix + "/"
	return len(path) > len(withSep) && strings.EqualFold(path[:len(withSep)], withSep)
}
