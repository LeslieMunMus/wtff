package pathvalidation

import (
	"strings"
	"sync"

	"golang.org/x/sys/unix"
)

// The structural denylist is deliberately small.
//
// It is not the protection rule set and must not grow into one. Its job is to
// catch targets whose presence indicates a defect or an attack rather than a
// cleanup decision that went the wrong way: nobody legitimately asks wtff to
// delete /System or a user's whole home directory. Nuanced protection, such as
// a vendor license file that happens to sit in a cache directory, belongs in
// the protection-rules package, where each entry carries a reason and a source.
//
// The distinction matters because the floor is absolute. Anything listed here
// can never be overridden by a rule file, so an over-broad floor would quietly
// make whole categories of legitimate cleanup impossible to express.

// deniedExact are paths that may never be a target themselves, while entries
// beneath them can be. Deleting /Library is catastrophic; deleting one cache
// directory inside it is ordinary work.
var deniedExact = []string{
	"/",
	"/Applications",
	"/Library",
	"/Library/Application Support",
	"/Network",
	"/System",
	"/Users",
	"/Users/Shared",
	"/Volumes",
	"/cores",
	"/home",
	"/net",
	"/opt",
	"/opt/homebrew",
	"/private",
	"/private/etc",
	"/private/tmp",
	"/private/var",
	"/private/var/folders",
	"/private/var/log",
	"/private/var/tmp",
	"/usr",
	"/usr/local",
}

// deniedTrees are paths where the entry itself and everything beneath it is
// denied. These are operating system territory, or credential material, where
// no reclaimable-space argument outweighs the risk of a mistake.
var deniedTrees = []string{
	"/System",
	"/bin",
	"/dev",
	"/sbin",
	"/usr/bin",
	"/usr/include",
	"/usr/lib",
	"/usr/libexec",
	"/usr/sbin",
	"/usr/share",
	"/Library/Apple",
	"/Library/Extensions",
	"/Library/Keychains",
	"/Library/Security",
	"/private/etc",
	"/private/var/audit",
	"/private/var/db",
	"/private/var/root",
}

// identityKey is a device and inode pair, which uniquely identifies a file
// within a volume regardless of the path used to reach it.
type identityKey struct {
	device uint64
	inode  uint64
}

// Identity is the captured identity of a resolved target, used by the deletion
// engine to confirm immediately before acting that the object it is about to
// remove is still the object that was validated.
type Identity struct {
	Device uint64
	Inode  uint64
}

var (
	denyIdentityOnce sync.Once

	// deniedExactIdentities and deniedTreeIdentities hold the resolved identity
	// of every denylist entry that exists on this machine.
	//
	// String comparison alone is not sufficient. Apple filesystems are
	// typically case insensitive but case preserving, so /SYSTEM and /System
	// are the same directory while comparing unequal as strings, and a path
	// can also reach a denied directory through a link that leaves no trace in
	// the string. Comparing the identity the kernel reports closes both gaps.
	// The string lists are still checked first because they also cover entries
	// that do not currently exist.
	deniedExactIdentities map[identityKey]string
	deniedTreeIdentities  map[identityKey]string
)

func loadDenyIdentities() {
	deniedExactIdentities = make(map[identityKey]string, len(deniedExact))
	deniedTreeIdentities = make(map[identityKey]string, len(deniedTrees))

	for _, path := range deniedExact {
		if key, ok := statIdentity(path); ok {
			deniedExactIdentities[key] = path
		}
	}
	for _, path := range deniedTrees {
		if key, ok := statIdentity(path); ok {
			deniedTreeIdentities[key] = path
		}
	}
}

// statIdentity reports the identity of an existing path. A path that cannot be
// stat'ed is skipped rather than treated as an error: the denylist covers
// several paths that only exist on some macOS versions or configurations, and
// their absence is not a problem.
func statIdentity(path string) (identityKey, bool) {
	var st unix.Stat_t
	if err := unix.Stat(path, &st); err != nil {
		return identityKey{}, false
	}
	return identityKey{device: uint64(st.Dev), inode: st.Ino}, true
}

// deniedByString reports whether a normalized logical path is denied on the
// basis of its text alone.
func deniedByString(path string) (string, bool) {
	for _, entry := range deniedExact {
		if strings.EqualFold(path, entry) {
			return entry, true
		}
	}
	for _, entry := range deniedTrees {
		if strings.EqualFold(path, entry) || hasPathPrefixFold(path, entry) {
			return entry, true
		}
	}
	if isUserHomeRoot(path) {
		return path, true
	}
	return "", false
}

// isUserHomeRoot reports whether a path is exactly one component below /Users,
// meaning it is somebody's home directory root.
//
// This case cannot be written as a fixed list entry because the account name
// is not known ahead of time, and it is worth handling explicitly: a target
// built by joining a home directory with a subdirectory name collapses to the
// home root when the subdirectory variable is empty, which turns a routine
// cleanup into deleting everything the user owns.
func isUserHomeRoot(path string) bool {
	const prefix = "/Users/"
	if len(path) <= len(prefix) || !strings.EqualFold(path[:len(prefix)], prefix) {
		return false
	}
	remainder := path[len(prefix):]
	if remainder == "" || strings.Contains(remainder, "/") {
		return false
	}
	// /Users/Shared and /Users/Guest are already covered by the exact list, but
	// naming them here too costs nothing and keeps this function correct if it
	// is ever consulted on its own.
	return true
}

// deniedTreeByIdentity reports whether an identity belongs to a denied tree
// root. It is checked on every intermediate directory during the walk, so a
// link that redirects into a denied tree is caught when the walk arrives at
// that tree rather than after the fact.
func deniedTreeByIdentity(key identityKey) (string, bool) {
	denyIdentityOnce.Do(loadDenyIdentities)
	name, found := deniedTreeIdentities[key]
	return name, found
}

// deniedExactByIdentity reports whether an identity is a denylist entry that
// may not itself be a target.
func deniedExactByIdentity(key identityKey) (string, bool) {
	denyIdentityOnce.Do(loadDenyIdentities)
	if name, found := deniedExactIdentities[key]; found {
		return name, true
	}
	name, found := deniedTreeIdentities[key]
	return name, found
}
