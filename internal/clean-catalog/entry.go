package cleancatalog

// Kind is how an entry's path is turned into candidates.
type Kind string

const (
	// KindContainer enumerates the immediate children of a directory, each
	// becoming its own candidate. Used where a directory holds many
	// independent, individually meaningful items, such as one subdirectory per
	// application under ~/Library/Caches.
	KindContainer Kind = "container"

	// KindVolumeTrash enumerates the current user's trash on every mounted
	// volume other than the boot volume. Its path names the directory mount
	// points appear under rather than a directory to remove, because what it
	// expands to depends on what is plugged in at the time.
	KindVolumeTrash Kind = "volume-trash"

	// KindOpaque treats the entire path as one candidate and does not look
	// inside it. Used where a directory's internal layout is not meaningful to
	// a person reviewing what would be removed, such as a content addressed
	// store, so that reviewing a hundred hash named entries individually would
	// not communicate anything a single line does not already say.
	KindOpaque Kind = "opaque"
)

// Provenance records where an entry's justification came from, so a later
// reader can check it rather than trust it.
//
// This duplicates the shape of protectionrules.Provenance rather than
// importing it. The two packages describe opposite things for different
// reasons, and keeping them independently defined means a change to one
// schema cannot silently reshape the other; the cost is a few duplicated
// field names, which is cheaper than the coupling.
type Provenance struct {
	Source   string `yaml:"source"`
	Method   string `yaml:"method"`
	Verified string `yaml:"verified"`
}

// Entry is one proposed cache category.
type Entry struct {
	ID       string `yaml:"id"`
	Category string `yaml:"category"`
	Kind     Kind   `yaml:"kind"`
	Path     string `yaml:"path"`
	Reason   string `yaml:"reason"`

	// ExcludePrefixes skips a container's immediate children whose name starts
	// with one of these values, compared case-insensitively. Meaningless for
	// KindOpaque, since that kind never enumerates children.
	ExcludePrefixes []string `yaml:"exclude_prefixes,omitempty"`

	// Purgeable marks an entry that "wtff purge" may remove permanently,
	// without staging it for undo first.
	//
	// The bar is deliberately higher than "regenerable." Nearly every entry in
	// this catalog is regenerable, and staging exists because regenerable is a
	// claim rather than a proof: the cost of being wrong about a cache is
	// unrecoverable, and cheap to avoid. What qualifies here is narrower, an
	// entry whose contents the person has already chosen to discard, where
	// wtff is completing an intent rather than forming one.
	Purgeable bool `yaml:"purgeable,omitempty"`

	// PurgeReason explains why permanent removal is defensible for this entry
	// specifically. Required whenever Purgeable is set, so the list cannot
	// grow by someone flipping a boolean.
	PurgeReason string `yaml:"purge_reason,omitempty"`

	// PurgeOnly excludes an entry from "wtff clean", which stages.
	//
	// This exists for entries that live on another volume. Staging is a rename
	// into wtff's staging area, and a rename cannot cross a filesystem
	// boundary, so the deletion engine refuses those outright rather than
	// falling back to a copy that would quietly lose extended attributes and
	// make undo a lie. Offering such an entry under clean would mean every run
	// on a machine with an external drive reports failures it was always going
	// to report.
	PurgeOnly bool `yaml:"purge_only,omitempty"`

	Provenance Provenance `yaml:"provenance"`

	// origin names the file this entry came from, for diagnostics.
	origin string
}

// Origin reports which catalog file defined this entry.
func (e Entry) Origin() string { return e.origin }

// PurgeableEntries returns the subset that "wtff purge" may remove
// permanently. Everything else belongs to "wtff clean", which stages first.
func PurgeableEntries(entries []Entry) []Entry {
	var purgeable []Entry
	for _, entry := range entries {
		if entry.Purgeable {
			purgeable = append(purgeable, entry)
		}
	}
	return purgeable
}

// StageableEntries returns the subset "wtff clean" may stage, which is
// everything not sitting somewhere staging cannot reach.
func StageableEntries(entries []Entry) []Entry {
	var stageable []Entry
	for _, entry := range entries {
		if !entry.PurgeOnly {
			stageable = append(stageable, entry)
		}
	}
	return stageable
}

// entryDocument is the on disk shape of a catalog file.
type entryDocument struct {
	Version int     `yaml:"version"`
	Entries []Entry `yaml:"entries"`
}
